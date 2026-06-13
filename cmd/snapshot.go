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

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
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
	client, err := opts.newClient()
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
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
	hpa, err := client.Interface.AutoscalingV2().
		HorizontalPodAutoscalers(client.Namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting HPA %s: %w", name, err)
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

	report := hpaanalysis.AuditHPA(hpa, minReplicas)
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
	if err := hpaanalysis.WriteMarkdownReport(&sb, statusReport); err != nil {
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

// redactBytes applies redaction patterns to a byte slice.
func redactBytes(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	s := string(data)

	// Redact IPv4 addresses.
	s = redactIPv4(s)

	// Redact IPv6 addresses.
	s = redactIPv6(s)

	// Redact node names (heuristic: alphanumeric strings after "node:" or "Node:").
	s = redactNodeNames(s)

	// Redact pod UIDs (UUIDs).
	s = redactUIDs(s)

	// Redact hostnames.
	s = redactHostnames(s)

	return []byte(s)
}

// redactIPv4 replaces IPv4 addresses with redacted placeholders.
func redactIPv4(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		// Try to match an IPv4 address pattern.
		start := i
		octets := 0
		j := i
		for octets < 4 && j < len(s) {
			num := 0
			digits := 0
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				num = num*10 + int(s[j]-'0')
				digits++
				j++
			}
			if digits == 0 || num > 255 {
				break
			}
			octets++
			if octets < 4 {
				if j >= len(s) || s[j] != '.' {
					break
				}
				j++
			}
		}
		if octets == 4 {
			result.WriteString("[REDACTED-IP]")
			i = j
		} else {
			result.WriteByte(s[start])
			i = start + 1
		}
	}
	return result.String()
}

// redactIPv6 replaces IPv6 addresses with redacted placeholders.
func redactIPv6(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == ':' && i > 0 && i < len(s)-1 && looksLikeIPv6(s, i) {
			// Find the end of the IPv6 address.
			j := i + 1
			for j < len(s) && isIPv6Char(s[j]) {
				j++
			}
			result.WriteString("[REDACTED-IP]")
			i = j
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}

// looksLikeIPv6 checks if the position looks like part of an IPv6 address.
func looksLikeIPv6(s string, pos int) bool {
	colonCount := 0
	// Check backwards.
	for j := pos - 1; j >= 0 && isIPv6Char(s[j]); j-- {
		if s[j] == ':' {
			colonCount++
		}
	}
	// Check forwards.
	for j := pos + 1; j < len(s) && isIPv6Char(s[j]); j++ {
		if s[j] == ':' {
			colonCount++
		}
	}
	return colonCount >= 2
}

// isIPv6Char checks if a character is valid in an IPv6 address.
func isIPv6Char(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') || c == ':'
}

// redactNodeNames replaces node names after "node:" or "Node:" keywords.
func redactNodeNames(s string) string {
	replacements := []string{
		"node: ", "node: [REDACTED-NODE]",
		"Node: ", "Node: [REDACTED-NODE]",
		"node=", "node=[REDACTED-NODE]",
		"NodeName: ", "NodeName: [REDACTED-NODE]",
	}
	for i := 0; i < len(replacements); i += 2 {
		s = replaceAfterKeyword(s, replacements[i], replacements[i+1])
	}
	return s
}

// replaceAfterKeyword replaces the value after a keyword.
func replaceAfterKeyword(s, keyword, _ string) string {
	idx := strings.Index(s, keyword)
	if idx < 0 {
		return s
	}
	// Find the end of the value (next space, newline, or end of string).
	start := idx + len(keyword)
	end := start
	for end < len(s) && s[end] != ' ' && s[end] != '\n' && s[end] != '\r' && s[end] != '\t' && s[end] != ',' && s[end] != '}' && s[end] != ']' {
		end++
	}
	if end == start {
		return s
	}
	return s[:start] + "[REDACTED-NODE]" + s[end:]
}

// redactUIDs replaces UUID-style UIDs.
func redactUIDs(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if i+36 <= len(s) && isUUID(s[i:i+36]) {
			result.WriteString("[REDACTED-UID]")
			i += 36
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}

// isUUID checks if a string matches the UUID format (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
				return false
			}
		}
	}
	return true
}

// redactHostnames replaces hostname-like patterns (e.g., ip-10-0-1-23.ec2.internal).
func redactHostnames(s string) string {
	// Common cloud hostname patterns.
	patterns := []string{
		"ip-",
		"ec2-",
		"gke-",
		"aks-",
		"eks-",
	}
	var result strings.Builder
	i := 0
	for i < len(s) {
		matched := false
		for _, p := range patterns {
			if i+len(p) <= len(s) && s[i:i+len(p)] == p {
				// Find the end of the hostname.
				j := i
				for j < len(s) && s[j] != ' ' && s[j] != '\n' && s[j] != '\r' && s[j] != '\t' && s[j] != ',' && s[j] != '"' && s[j] != '\'' {
					j++
				}
				result.WriteString("[REDACTED-HOSTNAME]")
				i = j
				matched = true
				break
			}
		}
		if !matched {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}
