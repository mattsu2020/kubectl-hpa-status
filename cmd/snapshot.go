package cmd

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/cmd/bundle"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/audit"
	hparender "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/render"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func newSnapshotCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "snapshot NAME",
		Short:             "Bundle HPA diagnostic data into a zip file for incident sharing",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			output, _ := cmd.Flags().GetString("output")
			redact, _ := cmd.Flags().GetBool("redact")
			return runSnapshot(cmd.Context(), cmd.OutOrStdout(), opts, args[0], output, redact)
		},
	}
	cmd.Flags().StringP("output", "o", "", "output zip file path (default: hpa-snapshot-<name>-<timestamp>.zip)")
	cmd.Flags().Bool("redact", false, "redact sensitive information (IPs, node names, pod UIDs) from the bundle")
	return cmd
}

// snapshotData holds all collected diagnostic data for a single HPA.
type snapshotData struct {
	HPA         []byte
	Deployment  []byte
	ReplicaSets []byte
	Pods        []byte
	Events      []byte
	MetricsAPI  []byte
	Analysis    []byte
	Report      []byte
	Namespace   string
	HPAName     string
	Timestamp   time.Time
}

func runSnapshot(ctx context.Context, out io.Writer, opts *options, name, outputPath string, redact bool) error {
	client, err := newClientOrDefault(opts)
	if err != nil {
		return err
	}

	data, err := collectSnapshotData(ctx, client, opts, name)
	if err != nil {
		return fmt.Errorf("collecting snapshot data: %w", err)
	}

	if redact {
		redactSnapshotData(data)
	}

	if outputPath == "" {
		ts := time.Now().Format("20060102-150405")
		outputPath = fmt.Sprintf("hpa-snapshot-%s-%s.zip", name, ts)
	}

	if err := writeSnapshotZip(data, outputPath); err != nil {
		return fmt.Errorf("writing snapshot zip: %w", err)
	}

	if _, err := fmt.Fprintf(out, "Snapshot saved to %s\n", outputPath); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	return nil
}

func collectSnapshotData(ctx context.Context, client *kube.Client, opts *options, name string) (*snapshotData, error) {
	data := &snapshotData{
		Namespace: client.Namespace,
		HPAName:   name,
		Timestamp: time.Now(),
	}

	// 1. Fetch HPA
	hpa, err := kube.GetHPAFromClient(ctx, client, name)
	if err != nil {
		return nil, wrapHPALookupError(client.Namespace, name, err)
	}
	data.HPA, _ = yaml.Marshal(hpa)

	// 2. Fetch scale target (Deployment/StatefulSet)
	data.Deployment = fetchSnapshotTarget(ctx, client, hpa)

	// 3. Fetch ReplicaSets
	data.ReplicaSets = fetchSnapshotReplicaSets(ctx, client, hpa)

	// 4. Fetch Pods
	data.Pods = fetchSnapshotPods(ctx, client, hpa)

	// 5. Fetch Events
	data.Events = fetchSnapshotEvents(ctx, client, hpa)

	// 6. Fetch metrics API status
	data.MetricsAPI = fetchSnapshotMetricsAPI(ctx, client)

	// 7. Build full analysis
	data.Analysis = buildSnapshotAnalysis(hpa, opts)

	// 8. Generate markdown report
	data.Report = buildSnapshotReport(ctx, client, opts, hpa)

	return data, nil
}

func writeSnapshotZip(data *snapshotData, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	zw := zip.NewWriter(file)
	defer func() { _ = zw.Close() }()

	entries := []struct {
		Name    string
		Content []byte
	}{
		{"hpa.yaml", data.HPA},
		{"deployment.yaml", data.Deployment},
		{"replicasets.yaml", data.ReplicaSets},
		{"pods.yaml", data.Pods},
		{"events.txt", data.Events},
		{"metrics-api.txt", data.MetricsAPI},
		{"analysis.json", data.Analysis},
		{"report.md", data.Report},
		{"metadata.txt", []byte(fmt.Sprintf(
			"HPA: %s/%s\nNamespace: %s\nTimestamp: %s\n",
			data.Namespace, data.HPAName, data.Namespace, data.Timestamp.Format(time.RFC3339),
		))},
	}

	for _, entry := range entries {
		if len(entry.Content) == 0 {
			continue
		}
		w, err := zw.Create(entry.Name)
		if err != nil {
			return fmt.Errorf("creating zip entry %s: %w", entry.Name, err)
		}
		if _, err := w.Write(entry.Content); err != nil {
			return fmt.Errorf("writing zip entry %s: %w", entry.Name, err)
		}
	}

	return nil
}

func fetchSnapshotTarget(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) []byte {
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	if err != nil || info == nil {
		return []byte(fmt.Sprintf("# Error fetching scale target: %v\n", err))
	}

	switch strings.ToLower(info.Kind) {
	case "deployment":
		deploy, getErr := client.Interface.AppsV1().Deployments(hpa.Namespace).Get(ctx, info.Name, metav1.GetOptions{})
		if getErr != nil {
			return []byte(fmt.Sprintf("# Error fetching Deployment %s: %v\n", info.Name, getErr))
		}
		content, _ := yaml.Marshal(deploy)
		return content
	case "statefulset":
		sts, getErr := client.Interface.AppsV1().StatefulSets(hpa.Namespace).Get(ctx, info.Name, metav1.GetOptions{})
		if getErr != nil {
			return []byte(fmt.Sprintf("# Error fetching StatefulSet %s: %v\n", info.Name, getErr))
		}
		content, _ := yaml.Marshal(sts)
		return content
	default:
		return []byte(fmt.Sprintf("# Unsupported kind: %s\n", info.Kind))
	}
}

func fetchSnapshotReplicaSets(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) []byte {
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	if err != nil || info == nil {
		return nil
	}

	replicaSets, err := kube.FetchReplicaSetsForScaleTarget(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef, info.SelectorStr)
	if err != nil {
		return []byte(fmt.Sprintf("# Error fetching ReplicaSets: %v\n", err))
	}

	content, _ := json.MarshalIndent(replicaSets, "", "  ")
	return content
}

func fetchSnapshotPods(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) []byte {
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	if err != nil || info == nil {
		return nil
	}

	pods, err := kube.FetchPodInfosForSelector(ctx, client.Interface, hpa.Namespace, info.SelectorStr)
	if err != nil {
		return []byte(fmt.Sprintf("# Error fetching pods: %v\n", err))
	}

	content, _ := json.MarshalIndent(pods, "", "  ")
	return content
}

func fetchSnapshotEvents(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) []byte {
	objectNames := []string{hpa.Name, hpa.Spec.ScaleTargetRef.Name}
	events := kube.FetchRecentEventsForObjects(ctx, client.Interface, hpa.Namespace, objectNames, 20)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Recent Events for %s/%s\n\n", hpa.Namespace, hpa.Name))
	for _, event := range events {
		sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", event.Timestamp.Format(time.RFC3339), event.Reason, event.Message))
	}
	if len(events) == 0 {
		sb.WriteString("No recent events found.\n")
	}
	return []byte(sb.String())
}

func fetchSnapshotMetricsAPI(_ context.Context, client *kube.Client) []byte {
	var sb strings.Builder
	sb.WriteString("# Metrics API Status\n\n")

	apiGroups := []struct {
		name    string
		group   string
		version string
	}{
		{"metrics.k8s.io", "metrics.k8s.io", "v1beta1"},
		{"custom.metrics.k8s.io", "custom.metrics.k8s.io", "v1beta1"},
		{"external.metrics.k8s.io", "external.metrics.k8s.io", "v1beta1"},
	}

	for _, api := range apiGroups {
		gv := fmt.Sprintf("%s/%s", api.group, api.version)
		_, err := client.Interface.Discovery().ServerResourcesForGroupVersion(gv)
		if err != nil {
			sb.WriteString(fmt.Sprintf("%s: UNAVAILABLE (%v)\n", api.name, err))
		} else {
			sb.WriteString(fmt.Sprintf("%s: AVAILABLE\n", api.name))
		}
	}

	return []byte(sb.String())
}

func buildSnapshotAnalysis(hpa *autoscalingv2.HorizontalPodAutoscaler, _ *options) []byte {
	minReplicas := hpaanalysis.DefaultMinReplicas
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}

	report := audit.Run(hpa, minReplicas)
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return []byte(fmt.Sprintf("# Error serializing analysis: %v\n", err))
	}
	return content
}

func buildSnapshotReport(ctx context.Context, _ *kube.Client, opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler) []byte {
	includeInterpretation := true
	ec := newEnrichmentContext(ctx, opts)
	statusReport, err := buildStatusReportWithClient(ctx, opts, hpa.Name, includeInterpretation, ec)
	if err != nil {
		return []byte(fmt.Sprintf("# Error building status report: %v\n", err))
	}

	var sb strings.Builder
	if err := hparender.WriteMarkdownReport(&sb, statusReport); err != nil {
		return []byte(fmt.Sprintf("# Error rendering report: %v\n", err))
	}
	return []byte(sb.String())
}

// redactSnapshotData redacts sensitive information from the snapshot bundle.
// It replaces IP addresses, node names, pod UIDs, and other identifying data
// with generic placeholders.
func redactSnapshotData(data *snapshotData) {
	data.HPA = redactBytes(data.HPA)
	data.Deployment = redactBytes(data.Deployment)
	data.ReplicaSets = redactBytes(data.ReplicaSets)
	data.Pods = redactBytes(data.Pods)
	data.Events = redactBytes(data.Events)
	data.MetricsAPI = redactBytes(data.MetricsAPI)
	data.Analysis = redactBytes(data.Analysis)
	data.Report = redactBytes(data.Report)
}

// redactBytes re-exports cmd/bundle.RedactBytes under the unexported name the
// rest of cmd/ already uses. The canonical implementation lives in
// cmd/bundle/redact.go; this thin facade preserves the historical call sites.
// When the cmd/ sub-package split completes, callers should migrate to
// bundle.RedactBytes directly and this wrapper can be deleted.
func redactBytes(data []byte) []byte {
	return bundle.RedactBytes(data)
}
