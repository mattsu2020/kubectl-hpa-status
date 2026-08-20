package core

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
)

// Metric is the stable formatted representation of one HPA metric status.
type Metric struct {
	Type     string   `json:"type" yaml:"type"`
	Name     string   `json:"name,omitempty" yaml:"name,omitempty"`
	Selector string   `json:"selector,omitempty" yaml:"selector,omitempty"`
	Object   string   `json:"object,omitempty" yaml:"object,omitempty"`
	Current  string   `json:"current,omitempty" yaml:"current,omitempty"`
	Target   string   `json:"target,omitempty" yaml:"target,omitempty"`
	Ratio    *float64 `json:"ratio,omitempty" yaml:"ratio,omitempty"`
	Note     string   `json:"note,omitempty" yaml:"note,omitempty"`
	Text     string   `json:"text" yaml:"text"`
	// NumericValue is the typed reading behind Current: the utilization
	// percent, or the canonical decimal value of the quantity. nil when the
	// status carried no numeric value.
	NumericValue *float64 `json:"-" yaml:"-"`
	// NumericTarget is the typed target behind Target, when the spec carries
	// a matching numeric target.
	NumericTarget *float64 `json:"-" yaml:"-"`
	// Unit qualifies the numeric pair: "%" for utilization targets, "" for
	// canonical decimal quantities.
	Unit string `json:"-" yaml:"-"`
}

// HasReading reports whether the metric carries a typed numeric value.
func (m Metric) HasReading() bool {
	return m.NumericValue != nil
}

// MetricStatusFormatter formats one supported metric source type.
type MetricStatusFormatter interface {
	FormatStatus(*autoscalingv2.HorizontalPodAutoscaler, autoscalingv2.MetricStatus) Metric
}

// MetricStatusResolver returns the formatter registered for a source type.
type MetricStatusResolver func(autoscalingv2.MetricSourceType) MetricStatusFormatter

// FormatMetricStatus is the canonical status-formatting dispatch path. The
// resolver keeps the core package independent of optional remediation and
// matching behavior implemented by the root handler registry.
func FormatMetricStatus(hpa *autoscalingv2.HorizontalPodAutoscaler, status autoscalingv2.MetricStatus, resolve MetricStatusResolver) Metric {
	if status.Type == "" {
		return Metric{Text: "Metric status is present, but details are unavailable"}
	}
	if resolve == nil {
		return Metric{Type: string(status.Type), Text: string(status.Type) + " metric is present, but no formatter is registered"}
	}
	formatter := resolve(status.Type)
	if formatter == nil {
		return Metric{Type: string(status.Type), Text: string(status.Type) + " metric is present, but no formatter is registered"}
	}
	return formatter.FormatStatus(hpa, status)
}

// FormatMetricTarget returns a human-readable metric target.
func FormatMetricTarget(target autoscalingv2.MetricTarget) string {
	switch target.Type {
	case autoscalingv2.UtilizationMetricType:
		if target.AverageUtilization != nil {
			return fmt.Sprintf("%d%%", *target.AverageUtilization)
		}
	case autoscalingv2.AverageValueMetricType:
		if target.AverageValue != nil {
			return target.AverageValue.String()
		}
	case autoscalingv2.ValueMetricType:
		if target.Value != nil {
			return target.Value.String()
		}
	}
	return "<unknown>"
}

// FormatMetricSelector returns a stable selector string.
func FormatMetricSelector(selector *metav1.LabelSelector) string {
	if selector == nil {
		return ""
	}
	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return klabels.FormatLabels(selector.MatchLabels)
	}
	if parsed.Empty() {
		return ""
	}
	return parsed.String()
}

// FormatMetricValue formats utilization or average value fields.
func FormatMetricValue(utilization *int32, averageValue *resource.Quantity) string {
	if utilization != nil {
		return fmt.Sprintf("%d%%", *utilization)
	}
	if averageValue != nil && !averageValue.IsZero() {
		return averageValue.String()
	}
	return "<unknown>"
}

// FormatMetricValueStatus formats a Kubernetes metric value status.
func FormatMetricValueStatus(value autoscalingv2.MetricValueStatus) string {
	if value.AverageUtilization != nil {
		return fmt.Sprintf("%d%%", *value.AverageUtilization)
	}
	if value.AverageValue != nil && !value.AverageValue.IsZero() {
		return value.AverageValue.String()
	}
	if value.Value != nil && !value.Value.IsZero() {
		return value.Value.String()
	}
	return "<unknown>"
}
