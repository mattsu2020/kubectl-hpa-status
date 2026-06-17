package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ownershipReport struct {
	Namespace          string             `json:"namespace" yaml:"namespace"`
	Name               string             `json:"name" yaml:"name"`
	Target             string             `json:"target" yaml:"target"`
	TargetSpecReplicas *int32             `json:"targetSpecReplicas,omitempty" yaml:"targetSpecReplicas,omitempty"`
	HPADesiredReplicas int32              `json:"hpaDesiredReplicas" yaml:"hpaDesiredReplicas"`
	Managers           []ownershipManager `json:"managers,omitempty" yaml:"managers,omitempty"`
	Risks              []string           `json:"risks,omitempty" yaml:"risks,omitempty"`
	Recommendations    []string           `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
}

type ownershipManager struct {
	Manager   string `json:"manager" yaml:"manager"`
	Operation string `json:"operation,omitempty" yaml:"operation,omitempty"`
	Field     string `json:"field" yaml:"field"`
}

type ownershipListReport struct {
	Items []ownershipReport `json:"items" yaml:"items"`
}

func newOwnershipCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "ownership NAME [NAME...]",
		Short:             "Check HPA and GitOps ownership of scale target replicas",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOwnership(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runOwnership(ctx context.Context, out io.Writer, opts *options, names []string) error {
	client, err := opts.newClient()
	if err != nil {
		return err
	}
	reports := make([]ownershipReport, 0, len(names))
	for _, name := range names {
		hpa, err := client.Interface.AutoscalingV2().HorizontalPodAutoscalers(client.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		report, err := buildOwnershipReport(ctx, client, hpa)
		if err != nil {
			return err
		}
		reports = append(reports, report)
	}

	var value any
	if len(reports) == 1 {
		value = reports[0]
	} else {
		value = ownershipListReport{Items: reports}
	}
	return writeOutput(out, opts.Output, opts.Template, value, func() error {
		writeOwnershipText(out, reports)
		return nil
	})
}

func buildOwnershipReport(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) (ownershipReport, error) {
	ref := hpa.Spec.ScaleTargetRef
	report := ownershipReport{
		Namespace:          hpa.Namespace,
		Name:               hpa.Name,
		Target:             fmt.Sprintf("%s/%s", ref.Kind, ref.Name),
		HPADesiredReplicas: hpa.Status.DesiredReplicas,
	}
	replicas, fields, err := fetchOwnershipTarget(ctx, client, hpa.Namespace, ref)
	if err != nil {
		return report, err
	}
	report.TargetSpecReplicas = replicas
	report.Managers = replicaOwnershipManagers(fields)

	if replicas != nil {
		report.Risks = append(report.Risks, "scale target spec.replicas is present; GitOps or kubectl apply may reset HPA-managed replicas")
		if hpa.Status.DesiredReplicas > 0 && *replicas != hpa.Status.DesiredReplicas {
			report.Risks = append(report.Risks, fmt.Sprintf("spec.replicas=%d differs from HPA desiredReplicas=%d", *replicas, hpa.Status.DesiredReplicas))
		}
	}
	for _, manager := range report.Managers {
		if !looksLikeHPAOwner(manager.Manager) {
			report.Risks = append(report.Risks, fmt.Sprintf("manager %q appears to own spec.replicas", manager.Manager))
		}
	}
	if len(report.Risks) > 0 {
		report.Recommendations = append(report.Recommendations,
			"remove spec.replicas from GitOps manifests after HPA owns scaling",
			"verify server-side apply field ownership before applying workload manifests",
			"keep minReplicas/maxReplicas in the HPA as the scaling contract",
		)
	}
	return report, nil
}

func fetchOwnershipTarget(ctx context.Context, client *kube.Client, namespace string, ref autoscalingv2.CrossVersionObjectReference) (*int32, []metav1.ManagedFieldsEntry, error) {
	switch strings.ToLower(ref.Kind) {
	case "deployment":
		obj, err := client.Interface.AppsV1().Deployments(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, err
		}
		return obj.Spec.Replicas, obj.ManagedFields, nil
	case "statefulset":
		obj, err := client.Interface.AppsV1().StatefulSets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, err
		}
		return obj.Spec.Replicas, obj.ManagedFields, nil
	case "replicaset":
		obj, err := client.Interface.AppsV1().ReplicaSets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, err
		}
		return obj.Spec.Replicas, obj.ManagedFields, nil
	default:
		return nil, nil, fmt.Errorf("unsupported scale target kind %q for ownership analysis", ref.Kind)
	}
}

func replicaOwnershipManagers(entries []metav1.ManagedFieldsEntry) []ownershipManager {
	var managers []ownershipManager
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.FieldsV1 == nil || !strings.Contains(string(entry.FieldsV1.GetRawBytes()), "f:replicas") {
			continue
		}
		key := entry.Manager + "\x00" + string(entry.Operation)
		if seen[key] {
			continue
		}
		seen[key] = true
		managers = append(managers, ownershipManager{
			Manager:   entry.Manager,
			Operation: string(entry.Operation),
			Field:     "spec.replicas",
		})
	}
	return managers
}

func looksLikeHPAOwner(manager string) bool {
	manager = strings.ToLower(manager)
	return strings.Contains(manager, "horizontal-pod-autoscaler") || strings.Contains(manager, "hpa")
}

func writeOwnershipText(out io.Writer, reports []ownershipReport) {
	for i, report := range reports {
		if i > 0 {
			_, _ = fmt.Fprintln(out)
		}
		_, _ = fmt.Fprintf(out, "Scale ownership: %s/%s\n", report.Namespace, report.Name)
		_, _ = fmt.Fprintf(out, "  target: %s\n", report.Target)
		if report.TargetSpecReplicas != nil {
			_, _ = fmt.Fprintf(out, "  target spec.replicas: %d\n", *report.TargetSpecReplicas)
		} else {
			_, _ = fmt.Fprintln(out, "  target spec.replicas: unset")
		}
		_, _ = fmt.Fprintf(out, "  HPA desiredReplicas: %d\n", report.HPADesiredReplicas)
		if len(report.Managers) > 0 {
			_, _ = fmt.Fprintln(out, "  managers owning spec.replicas:")
			for _, manager := range report.Managers {
				_, _ = fmt.Fprintf(out, "    - %s (%s)\n", manager.Manager, manager.Operation)
			}
		}
		if len(report.Risks) > 0 {
			_, _ = fmt.Fprintln(out, "  Risks:")
			for _, risk := range report.Risks {
				_, _ = fmt.Fprintf(out, "    - %s\n", risk)
			}
		} else {
			_, _ = fmt.Fprintln(out, "  Risks: none detected")
		}
		if len(report.Recommendations) > 0 {
			_, _ = fmt.Fprintln(out, "  Recommendations:")
			for _, recommendation := range report.Recommendations {
				_, _ = fmt.Fprintf(out, "    - %s\n", recommendation)
			}
		}
	}
}
