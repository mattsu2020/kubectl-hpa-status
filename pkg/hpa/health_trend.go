package hpa

import (
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
)

// This file is a thin re-export facade for the health trend domain, which now
// lives in pkg/hpa/healthtrend. The functions below preserve the existing
// hpaanalysis.* API surface so cmd/ and internal/ callers keep compiling
// without changing their imports. The canonical implementations are in
// pkg/hpa/healthtrend/.

// AnalyzeHealthTrend computes trend statistics from a series of health snapshots.
// Delegates to healthtrend.AnalyzeHealthTrend.
//
// Deprecated: Use healthtrend.AnalyzeHealthTrend instead. Scheduled for removal in v3.0.0.
func AnalyzeHealthTrend(snapshots []HealthSnapshot) HealthTrendResult {
	return healthtrend.AnalyzeHealthTrend(snapshots)
}

// DetectFlapping identifies rapid oscillation in health states.
// Delegates to healthtrend.DetectFlapping.
//
// Deprecated: Use healthtrend.DetectFlapping instead. Scheduled for removal in v3.0.0.
func DetectFlapping(snapshots []HealthSnapshot) (bool, string) {
	return healthtrend.DetectFlapping(snapshots)
}

// ComputeHealthVariance returns the population variance of health scores.
// Delegates to healthtrend.ComputeHealthVariance.
//
// Deprecated: Use healthtrend.ComputeHealthVariance instead. Scheduled for removal in v3.0.0.
func ComputeHealthVariance(scores []int) float64 {
	return healthtrend.ComputeHealthVariance(scores)
}

// FormatHealthSparkline renders a compact sparkline from health scores.
// Delegates to healthtrend.FormatHealthSparkline.
//
// Deprecated: Use healthtrend.FormatHealthSparkline instead. Scheduled for removal in v3.0.0.
func FormatHealthSparkline(scores []int, width int) string {
	return healthtrend.FormatHealthSparkline(scores, width)
}

// DetectAnomalies analyzes a sorted series of health snapshots and returns
// any detected anomalies. Delegates to healthtrend.DetectAnomalies.
//
// Deprecated: Use healthtrend.DetectAnomalies instead. Scheduled for removal in v3.0.0.
func DetectAnomalies(snapshots []HealthSnapshot) []AnomalyDetection {
	return healthtrend.DetectAnomalies(snapshots)
}

// RenderHealthTrendASCII renders a horizontal ASCII time-series graph.
// Delegates to healthtrend.RenderHealthTrendASCII.
//
// Deprecated: Use healthtrend.RenderHealthTrendASCII instead. Scheduled for removal in v3.0.0.
func RenderHealthTrendASCII(snapshots []HealthSnapshot, width int) string {
	return healthtrend.RenderHealthTrendASCII(snapshots, width)
}

// FormatTrendText renders a health trend summary for text output.
// Delegates to healthtrend.FormatTrendText.
//
// Deprecated: Use healthtrend.FormatTrendText instead. Scheduled for removal in v3.0.0.
func FormatTrendText(result HealthTrendResult) string {
	return healthtrend.FormatTrendText(result)
}

// FormatTrendAnomalyText renders anomaly detection results as text.
// Delegates to healthtrend.FormatTrendAnomalyText.
//
// Deprecated: Use healthtrend.FormatTrendAnomalyText instead. Scheduled for removal in v3.0.0.
func FormatTrendAnomalyText(result HealthTrendResult) string {
	return healthtrend.FormatTrendAnomalyText(result)
}

// FormatTrendAnomalyGraph renders the full trend text plus anomaly section
// and ASCII graph. Delegates to healthtrend.FormatTrendAnomalyGraph.
//
// Deprecated: Use healthtrend.FormatTrendAnomalyGraph instead. Scheduled for removal in v3.0.0.
func FormatTrendAnomalyGraph(result HealthTrendResult, graphWidth int) string {
	return healthtrend.FormatTrendAnomalyGraph(result, graphWidth)
}

// FormatTrendListRow renders a compact trend indicator for list view.
// Delegates to healthtrend.FormatTrendListRow.
//
// Deprecated: Use healthtrend.FormatTrendListRow instead. Scheduled for removal in v3.0.0.
func FormatTrendListRow(result HealthTrendResult) string {
	return healthtrend.FormatTrendListRow(result)
}
