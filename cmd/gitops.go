package cmd

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/gitops"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// buildGitOpsConflict gathers manifest files and live cluster state to detect
// conflicts between GitOps-managed replicas and HPA scaling decisions. It never
// returns an error: manifest parse failures are logged as warnings and live
// cluster fetch failures simply leave the corresponding fields empty, so the
// caller always gets a (possibly empty) GitOpsConflict to render.
func buildGitOpsConflict(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, manifestPath string) (*gitops.Conflict, []string) {
	var warnings []string
	// Parse manifest path to extract spec.replicas
	var manifestReplicas *int32
	targetKind := hpa.Spec.ScaleTargetRef.Kind
	targetName := hpa.Spec.ScaleTargetRef.Name

	if manifestPath != "" {
		var err error
		manifestReplicas, err = parseManifestReplicas(manifestPath, targetKind, targetName)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to parse manifests: %v", err))
		}
	}

	// Fetch live scale target for annotations and current replicas
	argoCDAnnotations := make(map[string]string)
	fluxAnnotations := make(map[string]string)
	kedaManaged := false
	var liveReplicas int32
	var liveFetchWarnings []string

	switch targetKind {
	case "Deployment":
		deploy, err := client.Interface.AppsV1().Deployments(hpa.Namespace).Get(ctx, targetName, metav1.GetOptions{})
		if err == nil {
			// Spec.Replicas is a *int32 and may be nil when the workload omits
			// an explicit replica count; fall back to the Kubernetes default (1)
			// rather than dereferencing a nil pointer.
			liveReplicas = replicasOrDefault(deploy.Spec.Replicas)
			extractGitOpsAnnotations(deploy.Annotations, argoCDAnnotations, fluxAnnotations)
			if deploy.Labels != nil {
				if deploy.Labels["app.kubernetes.io/managed-by"] == "keda-operator" ||
					deploy.Labels["keda.sh/scaledObjectName"] != "" {
					kedaManaged = true
				}
			}
		} else {
			// Surface the fetch failure instead of silently leaving liveReplicas=0,
			// which would otherwise produce a misleading drift analysis.
			liveFetchWarnings = append(liveFetchWarnings, fmt.Sprintf("could not read live Deployment %s/%s replicas: %v", hpa.Namespace, targetName, err))
		}
	case "StatefulSet":
		sts, err := client.Interface.AppsV1().StatefulSets(hpa.Namespace).Get(ctx, targetName, metav1.GetOptions{})
		if err == nil {
			// Spec.Replicas is a *int32 and may be nil (see Deployment branch).
			liveReplicas = replicasOrDefault(sts.Spec.Replicas)
			extractGitOpsAnnotations(sts.Annotations, argoCDAnnotations, fluxAnnotations)
			if sts.Labels != nil {
				if sts.Labels["app.kubernetes.io/managed-by"] == "keda-operator" ||
					sts.Labels["keda.sh/scaledObjectName"] != "" {
					kedaManaged = true
				}
			}
		} else {
			liveFetchWarnings = append(liveFetchWarnings, fmt.Sprintf("could not read live StatefulSet %s/%s replicas: %v", hpa.Namespace, targetName, err))
		}
	}

	// Assemble input for pkg/hpa analysis
	input := gitops.Input{
		Namespace:         hpa.Namespace,
		HPAName:           hpa.Name,
		TargetKind:        targetKind,
		TargetName:        targetName,
		DesiredReplicas:   hpa.Status.DesiredReplicas,
		ManifestReplicas:  manifestReplicas,
		LiveReplicas:      liveReplicas,
		ArgoCDAnnotations: argoCDAnnotations,
		FluxAnnotations:   fluxAnnotations,
		KEDAManaged:       kedaManaged,
	}

	conflict := gitops.AnalyzeConflict(input)
	conflict.Warnings = append(conflict.Warnings, liveFetchWarnings...)
	return conflict, warnings
}

// parseManifestReplicas reads YAML/JSON manifest files and extracts spec.replicas
// for the target resource. Supports both single files and directories. Returns
// (nil, nil) when no matching resource is found; callers treat a nil *int32 as
// "manifest did not pin replicas" rather than an error.
//
//nolint:nilnil // nil replica pointer with no error means "not pinned in manifest"
func parseManifestReplicas(manifestPath string, targetKind, targetName string) (*int32, error) {
	info, err := os.Stat(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("stat manifest path: %w", err)
	}

	var files []string
	if info.IsDir() {
		entries, err := os.ReadDir(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("read manifest directory: %w", err)
		}
		for _, e := range entries {
			if !e.IsDir() && (strings.HasSuffix(e.Name(), ".yaml") ||
				strings.HasSuffix(e.Name(), ".yml") ||
				strings.HasSuffix(e.Name(), ".json")) {
				files = append(files, filepath.Join(manifestPath, e.Name()))
			}
		}
	} else {
		files = []string{manifestPath}
	}

	var parseErrors []error
	for _, file := range files {
		replicas, found, err := parseFileForReplicas(file, targetKind, targetName)
		if err != nil {
			parseErrors = append(parseErrors, err)
		}
		if found {
			return replicas, errors.Join(parseErrors...)
		}
	}

	return nil, errors.Join(parseErrors...)
}

// parseFileForReplicas parses a single manifest file and extracts spec.replicas
// if the file contains the target resource.
func parseFileForReplicas(filePath, targetKind, targetName string) (*int32, bool, error) {
	data, err := readFileBounded(filePath)
	if err != nil {
		return nil, false, fmt.Errorf("read manifest %s: %w", filePath, err)
	}

	docs, err := readYAMLDocuments(data)
	if err != nil {
		return nil, false, fmt.Errorf("parse manifest %s: %w", filePath, err)
	}
	for index, doc := range docs {
		var object unstructured.Unstructured
		if err := yaml.Unmarshal(doc, &object); err != nil {
			return nil, false, fmt.Errorf("parse manifest %s document %d: %w", filePath, index+1, err)
		}
		if replicas, found, err := extractReplicasFromUnstructured(&object, targetKind, targetName); err != nil {
			return nil, false, fmt.Errorf("parse manifest %s document %d: %w", filePath, index+1, err)
		} else if found {
			return replicas, true, nil
		}
		if object.IsList() {
			items, err := object.ToList()
			if err != nil {
				return nil, false, fmt.Errorf("parse manifest %s document %d list: %w", filePath, index+1, err)
			}
			for itemIndex := range items.Items {
				replicas, found, err := extractReplicasFromUnstructured(&items.Items[itemIndex], targetKind, targetName)
				if err != nil {
					return nil, false, fmt.Errorf("parse manifest %s document %d item %d: %w", filePath, index+1, itemIndex+1, err)
				}
				if found {
					return replicas, true, nil
				}
			}
		}
	}

	return nil, false, nil
}

// extractReplicasFromUnstructured extracts spec.replicas from an unstructured object
// if it matches the target kind and name.
func extractReplicasFromUnstructured(u *unstructured.Unstructured, targetKind, targetName string) (*int32, bool, error) {
	kind := u.GetKind()
	name := u.GetName()

	// Normalize kind (handle both short and full forms)
	switch kind {
	case "Deployment", "deployment", "Deployment.apps":
		kind = "Deployment"
	case "StatefulSet", "statefulset", "StatefulSet.apps":
		kind = "StatefulSet"
	}

	if kind != targetKind || name != targetName {
		return nil, false, nil
	}

	// Only process Deployment and StatefulSet
	if kind != "Deployment" && kind != "StatefulSet" {
		return nil, false, nil
	}

	replicas, found, err := unstructured.NestedInt64(u.Object, "spec", "replicas")
	if err != nil {
		return nil, false, fmt.Errorf("read spec.replicas for %s/%s: %w", kind, name, err)
	}
	if !found {
		return nil, false, nil
	}
	if replicas < 0 || replicas > math.MaxInt32 {
		return nil, false, fmt.Errorf("spec.replicas for %s/%s must be between 0 and %d, got %d", kind, name, math.MaxInt32, replicas)
	}

	result := int32(replicas)
	return &result, true, nil
}

// extractGitOpsAnnotations extracts Argo CD and Flux annotations from the resource.
func extractGitOpsAnnotations(annotations map[string]string, argoCD, flux map[string]string) {
	if annotations == nil {
		return
	}

	for k, v := range annotations {
		switch {
		case strings.HasPrefix(k, "argocd.argoproj.io/"):
			argoCD[k] = v
		case strings.HasPrefix(k, "kustomize.toolkit.fluxcd.io/"),
			strings.HasPrefix(k, "helm.toolkit.fluxcd.io/"):
			flux[k] = v
		}
	}
}
