package simulate

import (
	"strings"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/core"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/rendutil"
)

// This file holds formatting helpers the simulation owns outright instead of
// injecting them from the root pkg/hpa package. They must stay byte-for-byte
// compatible with the root implementations (which themselves delegate to
// pkg/hpa/core and pkg/hpa/rendutil) so simulation output does not drift.

// repeatChar repeats a string n times, matching pkg/hpa's stabilization
// progress bars.
func repeatChar(count int, char string) string {
	if count <= 0 {
		return ""
	}
	return strings.Repeat(char, count)
}

// formatDuration formats a whole-second offset the same way the root
// analysis formats durations ("4m 12s" prose shape).
func formatDuration(seconds int32) string {
	return rendutil.DurationSpaced(time.Duration(seconds) * time.Second)
}

// formatMetricTarget renders a metric target through the shared core
// formatter so simulate and root analysis produce identical text.
func formatMetricTarget(target autoscalingv2.MetricTarget) string {
	return core.FormatMetricTarget(target)
}

// formatMetricValueStatus renders a current metric value through the shared
// core formatter.
func formatMetricValueStatus(v autoscalingv2.MetricValueStatus) string {
	return core.FormatMetricValueStatus(v)
}
