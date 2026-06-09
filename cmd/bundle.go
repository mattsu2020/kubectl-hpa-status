package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func newBundleCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "bundle NAME",
		Aliases:           []string{"collect"},
		Short:             "Bundle all HPA investigation data into a Markdown file or zip archive",
		Long:              "Collects HPA configuration, status analysis, workload details, events, metrics diagnostics, capacity context, KEDA/VPA state, quotas, PDBs, and node capacity into a single evidence pack for incident handoff, post-mortems, or GitHub issues.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, _ := cmd.Flags().GetString("format")
			output, _ := cmd.Flags().GetString("output")
			redact, _ := cmd.Flags().GetBool("redact")
			return runBundle(cmd.Context(), cmd.OutOrStdout(), opts, args[0], format, output, redact)
		},
	}
	cmd.Flags().String("format", "markdown", "output format: markdown or zip")
	cmd.Flags().StringP("output", "o", "", "output file path (default: hpa-bundle-<name>-<timestamp>.{md|zip})")
	cmd.Flags().Bool("redact", false, "redact sensitive information (IPs, node names, pod UIDs)")
	return cmd
}

// bundleData holds all collected diagnostic data for a single HPA.
type bundleData struct {
	HPA         []byte
	ScaleTarget []byte
	ReplicaSets []byte
	Pods        []byte
	Events      []byte
	MetricsAPI  []byte

	// Container statuses (restart/waiting).
	ContainerStatuses []kube.ContainerStatusDetail

	// Infrastructure context.
	ResourceQuotas []kube.QuotaInfo
	LimitRanges    []kube.LimitRangeInfo
	PDBs           []kube.PDBInfo
	NodeCapacity   *kube.NodeCapacityInfo

	// Full doctor-level analysis.
	StatusReport hpaanalysis.StatusReport

	// Raw pod info for table rendering.
	PodInfos []kube.PodInfo

	Namespace string
	HPAName   string
	Timestamp time.Time
}

// runBundle orchestrates data collection and output for the bundle command.
func runBundle(ctx context.Context, out io.Writer, opts *options, name, format, outputPath string, redact bool) error {
	client, err := opts.newClient()
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}

	data, err := collectBundleData(ctx, client, opts, name)
	if err != nil {
		return fmt.Errorf("collecting bundle data: %w", err)
	}

	if redact {
		redactBundleData(data)
	}

	if outputPath == "" {
		ts := time.Now().Format("20060102-150405")
		ext := ".md"
		if format == "zip" {
			ext = ".zip"
		}
		outputPath = fmt.Sprintf("hpa-bundle-%s-%s%s", name, ts, ext)
	}

	switch format {
	case "markdown", "md":
		if err := writeBundleMarkdownFile(data, outputPath, redact); err != nil {
			return fmt.Errorf("writing markdown bundle: %w", err)
		}
	case "zip":
		if err := writeBundleZip(data, outputPath); err != nil {
			return fmt.Errorf("writing zip bundle: %w", err)
		}
	default:
		return fmt.Errorf("unsupported format %q (use markdown or zip)", format)
	}

	if _, err := fmt.Fprintf(out, "Bundle saved to %s\n", outputPath); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	return nil
}

// collectBundleData gathers all diagnostic data for the bundle.
// It creates a shallow copy of opts with all doctor flags enabled so the
// original opts is not mutated. Pointer fields (clientOverride, outputTemplates)
// are shared but bundle code never writes through them.
func collectBundleData(ctx context.Context, client *kube.Client, opts *options, name string) (*bundleData, error) {
	// Enable all doctor-level flags on a shallow copy of opts.
	bundleOpts := *opts
	bundleOpts.explain = true
	bundleOpts.diagnoseMetrics = true
	bundleOpts.metricsFreshness = true
	bundleOpts.checkResources = true
	bundleOpts.explainPods = true
	bundleOpts.capacityContext = true
	bundleOpts.gitopsCheck = true
	bundleOpts.metricContract = true
	bundleOpts.churnDetect = true
	bundleOpts.metricHints = true
	bundleOpts.containerAdvisor = true
	bundleOpts.behaviorAdvisor = true
	bundleOpts.capacityDeep = true
	bundleOpts.readinessImpact = true
	bundleOpts.rolloutImpact = true
	bundleOpts.scaleoutBlockers = true
	bundleOpts.controllerProfile = true
	bundleOpts.keda = true
	bundleOpts.vpa = true
	bundleOpts.scalePath = true
	bundleOpts.events = eventOption{enabled: true, limit: 20}

	data := &bundleData{
		Namespace: client.Namespace,
		HPAName:   name,
		Timestamp: time.Now(),
	}

	// 1. Fetch HPA YAML.
	hpa, err := client.Interface.AutoscalingV2().
		HorizontalPodAutoscalers(client.Namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting HPA %s: %w", name, err)
	}
	data.HPA, _ = yaml.Marshal(hpa)

	// 2. Fetch scale target YAML (reuse snapshot helper).
	data.ScaleTarget = fetchSnapshotTarget(ctx, client, hpa)

	// 3. Build full doctor-level analysis with KEDA+VPA enrichment.
	ec := newEnrichmentContext(ctx, &bundleOpts)
	includeInterpretation := !bundleOpts.noInterpret
	statusReport, err := buildStatusReport(ctx, &bundleOpts, client, name, includeInterpretation, ec)
	if err != nil {
		return nil, fmt.Errorf("building status report: %w", err)
	}
	data.StatusReport = statusReport

	// 4. Resolve selector for additional data collection.
	selector := capacitySelector(ctx, client, hpa)

	// 5. ReplicaSets (reuse snapshot helper).
	data.ReplicaSets = fetchSnapshotReplicaSets(ctx, client, hpa)

	// 6. Pods (reuse snapshot helper) + raw PodInfos for table rendering.
	data.Pods = fetchSnapshotPods(ctx, client, hpa)
	if selector != "" {
		podInfos, _ := kube.FetchPodInfosForSelector(ctx, client.Interface, client.Namespace, selector)
		data.PodInfos = podInfos
	}

	// 7. Events with wider scope (HPA + scale target + pods).
	objectNames := bundleEventObjectNames(ctx, client, hpa)
	events := kube.FetchRecentEventsForObjects(ctx, client.Interface, hpa.Namespace, objectNames, 30)
	data.Events = formatBundleEvents(events)

	// 8. Metrics API status (reuse snapshot helper).
	data.MetricsAPI = fetchSnapshotMetricsAPI(ctx, client)

	// 9. Container statuses.
	if selector != "" {
		containerStatuses, _ := kube.FetchContainerStatuses(ctx, client.Interface, hpa.Namespace, selector)
		data.ContainerStatuses = containerStatuses
	}

	// 10. All ResourceQuotas (not just >= 80%).
	data.ResourceQuotas = kube.FetchAllResourceQuotas(ctx, client.Interface, hpa.Namespace)

	// 11. LimitRanges.
	data.LimitRanges = kube.FetchLimitRanges(ctx, client.Interface, hpa.Namespace)

	// 12. PDBs.
	data.PDBs = kube.FetchPodDisruptionBudgets(ctx, client.Interface, hpa.Namespace, hpa.UID)

	// 13. Node capacity.
	nodeCap, _ := kube.FetchNodeCapacity(ctx, client.Interface)
	data.NodeCapacity = nodeCap

	return data, nil
}

// bundleEventObjectNames collects object names for event fetching:
// HPA itself, the scale target, and all pods of the scale target.
func bundleEventObjectNames(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) []string {
	names := []string{hpa.Name, hpa.Spec.ScaleTargetRef.Name}

	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	if err != nil || info == nil || info.SelectorStr == "" {
		return names
	}

	pods, err := kube.FetchPodInfosForSelector(ctx, client.Interface, hpa.Namespace, info.SelectorStr)
	if err != nil {
		return names
	}
	for _, pod := range pods {
		names = append(names, pod.Name)
	}
	return names
}

// formatBundleEvents formats events as a markdown-compatible text block.
func formatBundleEvents(events []kube.EventInfo) []byte {
	if len(events) == 0 {
		return []byte("No recent events found.\n")
	}

	var sb strings.Builder
	for _, event := range events {
		sb.WriteString(fmt.Sprintf("[%s] %s: %s\n",
			event.Timestamp.Format(time.RFC3339),
			event.Reason,
			event.Message,
		))
	}
	return []byte(sb.String())
}

// redactBundleData applies redaction to all byte-slice fields and redacts
// sensitive fields in PodInfos. The StatusReport is redacted at render time
// when the full markdown is assembled and passed through redactBytes.
func redactBundleData(data *bundleData) {
	data.HPA = redactBytes(data.HPA)
	data.ScaleTarget = redactBytes(data.ScaleTarget)
	data.ReplicaSets = redactBytes(data.ReplicaSets)
	data.Pods = redactBytes(data.Pods)
	data.Events = redactBytes(data.Events)
	data.MetricsAPI = redactBytes(data.MetricsAPI)

	// Redact node names from PodInfos so the markdown table is safe.
	for i := range data.PodInfos {
		if data.PodInfos[i].NodeName != "" {
			data.PodInfos[i].NodeName = "[REDACTED-NODE]"
		}
	}
	// Redact node names from ContainerStatuses.
	for i := range data.ContainerStatuses {
		data.ContainerStatuses[i].Pod = redactString(data.ContainerStatuses[i].Pod)
	}
}

// writeBundleMarkdownFile renders the bundle as a single Markdown file.
// When redact is true, the final assembled markdown bytes are passed through
// redactBytes to catch any remaining sensitive data from the StatusReport.
func writeBundleMarkdownFile(data *bundleData, outputPath string, redact bool) error {
	var buf bytes.Buffer
	writeBundleMarkdown(&buf, data)

	content := buf.Bytes()
	if redact {
		content = redactBytes(content)
	}

	return os.WriteFile(outputPath, content, 0o644)
}

// mdEscape escapes the pipe character for safe use inside markdown tables.
func mdEscape(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// redactString applies common redaction patterns to a single string.
func redactString(s string) string {
	return string(redactBytes([]byte(s)))
}
