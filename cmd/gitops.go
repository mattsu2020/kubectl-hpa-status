package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
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
func buildGitOpsConflict(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, manifestPath string) *hpaanalysis.GitOpsConflict {
	// Parse manifest path to extract spec.replicas
	var manifestReplicas *int32
	targetKind := hpa.Spec.ScaleTargetRef.Kind
	targetName := hpa.Spec.ScaleTargetRef.Name

	if manifestPath != "" {
		var err error
		manifestReplicas, err = parseManifestReplicas(manifestPath, targetKind, targetName)
		if err != nil {
			// Log warning but continue - we can still detect ArgoCD/Flux
			fmt.Fprintf(os.Stderr, "warning: failed to parse manifests: %v\n", err)
		}
	}

	// Fetch live scale target for annotations and current replicas
	argoCDAnnotations := make(map[string]string)
	fluxAnnotations := make(map[string]string)
	kedaManaged := false
	var liveReplicas int32

	switch targetKind {
	case "Deployment":
		deploy, err := client.Interface.AppsV1().Deployments(hpa.Namespace).Get(ctx, targetName, metav1.GetOptions{})
		if err == nil {
			liveReplicas = *deploy.Spec.Replicas
			extractGitOpsAnnotations(deploy.Annotations, argoCDAnnotations, fluxAnnotations)
			if deploy.Labels != nil {
				if deploy.Labels["app.kubernetes.io/managed-by"] == "keda-operator" ||
					deploy.Labels["keda.sh/scaledObjectName"] != "" {
					kedaManaged = true
				}
			}
		}
	case "StatefulSet":
		sts, err := client.Interface.AppsV1().StatefulSets(hpa.Namespace).Get(ctx, targetName, metav1.GetOptions{})
		if err == nil {
			liveReplicas = *sts.Spec.Replicas
			extractGitOpsAnnotations(sts.Annotations, argoCDAnnotations, fluxAnnotations)
			if sts.Labels != nil {
				if sts.Labels["app.kubernetes.io/managed-by"] == "keda-operator" ||
					sts.Labels["keda.sh/scaledObjectName"] != "" {
					kedaManaged = true
				}
			}
		}
	}

	// Assemble input for pkg/hpa analysis
	input := hpaanalysis.GitOpsInput{
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

	return hpaanalysis.AnalyzeGitOpsConflict(input)
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

	for _, file := range files {
		if replicas, found := parseFileForReplicas(file, targetKind, targetName); found {
			return replicas, nil
		}
	}

	return nil, nil
}

// parseFileForReplicas parses a single manifest file and extracts spec.replicas
// if the file contains the target resource.
func parseFileForReplicas(filePath, targetKind, targetName string) (*int32, bool) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, false
	}

	// Try parsing as a single document first
	var single unstructured.Unstructured
	if err := yaml.Unmarshal(data, &single); err == nil {
		if replicas, found := extractReplicasFromUnstructured(&single, targetKind, targetName); found {
			return replicas, true
		}
	}

	// Try multi-document YAML
	var multi []map[string]interface{}
	if err := yaml.Unmarshal(data, &multi); err == nil {
		for _, doc := range multi {
			u := &unstructured.Unstructured{Object: doc}
			if replicas, found := extractReplicasFromUnstructured(u, targetKind, targetName); found {
				return replicas, true
			}
		}
	}

	return nil, false
}

// extractReplicasFromUnstructured extracts spec.replicas from an unstructured object
// if it matches the target kind and name.
func extractReplicasFromUnstructured(u *unstructured.Unstructured, targetKind, targetName string) (*int32, bool) {
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
		return nil, false
	}

	// Only process Deployment and StatefulSet
	if kind != "Deployment" && kind != "StatefulSet" {
		return nil, false
	}

	replicas, found, err := unstructured.NestedInt64(u.Object, "spec", "replicas")
	if err != nil || !found {
		return nil, false
	}

	result := int32(replicas)
	return &result, true
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
