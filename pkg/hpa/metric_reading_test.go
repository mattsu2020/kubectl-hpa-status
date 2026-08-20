package hpa

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

func TestNumericReading(t *testing.T) {
	tests := []struct {
		name       string
		current    autoscalingv2.MetricValueStatus
		target     autoscalingv2.MetricTarget
		wantValue  *float64
		wantTarget *float64
		wantUnit   string
	}{
		{
			name:       "utilization",
			current:    autoscalingv2.MetricValueStatus{AverageUtilization: ptr.To(int32(45))},
			target:     autoscalingv2.MetricTarget{AverageUtilization: ptr.To(int32(60))},
			wantValue:  ptr.To(45.0),
			wantTarget: ptr.To(60.0),
			wantUnit:   "%",
		},
		{
			name:      "utilization without target",
			current:   autoscalingv2.MetricValueStatus{AverageUtilization: ptr.To(int32(45))},
			wantValue: ptr.To(45.0),
			wantUnit:  "%",
		},
		{
			name:       "average value quantity canonicalizes suffixes",
			current:    autoscalingv2.MetricValueStatus{AverageValue: resourceQty("812m")},
			target:     autoscalingv2.MetricTarget{AverageValue: resourceQty("1")},
			wantValue:  ptr.To(0.812),
			wantTarget: ptr.To(1.0),
		},
		{
			name:       "point value quantity",
			current:    autoscalingv2.MetricValueStatus{Value: resourceQty("1500")},
			target:     autoscalingv2.MetricTarget{Value: resourceQty("1k")},
			wantValue:  ptr.To(1500.0),
			wantTarget: ptr.To(1000.0),
		},
		{
			name: "no numeric field",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, target, unit := numericReading(tt.current, tt.target)
			if !floatPtrEqual(value, tt.wantValue) {
				t.Errorf("value = %v, want %v", derefFloat(value), derefFloat(tt.wantValue))
			}
			if !floatPtrEqual(target, tt.wantTarget) {
				t.Errorf("target = %v, want %v", derefFloat(target), derefFloat(tt.wantTarget))
			}
			if unit != tt.wantUnit {
				t.Errorf("unit = %q, want %q", unit, tt.wantUnit)
			}
		})
	}
}

// TestFormatMetricStatusCapturesNumericReading verifies the handler dispatch
// path keeps the typed numbers that the formatted strings render, so record
// snapshots can store them without reparsing display text.
func TestFormatMetricStatusCapturesNumericReading(t *testing.T) {
	hpa := baseHPA()
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: "cpu",
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: ptr.To(int32(60)),
				},
			},
		},
	}

	got := FormatMetricStatus(hpa, autoscalingv2.MetricStatus{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricStatus{
			Name: "cpu",
			Current: autoscalingv2.MetricValueStatus{
				AverageUtilization: ptr.To(int32(83)),
			},
		},
	})

	if !got.HasReading() {
		t.Fatalf("expected a typed numeric reading, got %#v", got)
	}
	if *got.NumericValue != 83 {
		t.Errorf("NumericValue = %v, want 83", derefFloat(got.NumericValue))
	}
	if *got.NumericTarget != 60 {
		t.Errorf("NumericTarget = %v, want 60", derefFloat(got.NumericTarget))
	}
	if got.Unit != "%" {
		t.Errorf("Unit = %q, want %%", got.Unit)
	}
}

func TestSnapshotFromReportRecordsMetricValues(t *testing.T) {
	value := 83.0
	target := 60.0
	report := StatusReport{
		Analysis: Analysis{
			Namespace: "default",
			Name:      "web",
			Metrics: []Metric{
				{
					Type:          MetricTypeResource,
					Name:          "cpu",
					Text:          "Resource cpu current=83% target=60%",
					NumericValue:  &value,
					NumericTarget: &target,
					Unit:          "%",
				},
				{Type: MetricTypeExternal, Name: "queue_depth", Text: "External queue_depth: details unavailable"},
			},
		},
	}

	snap := SnapshotFromReport(report)

	if len(snap.MetricValues) != 1 {
		t.Fatalf("expected 1 typed reading (metrics without numbers are skipped), got %d", len(snap.MetricValues))
	}
	reading := snap.MetricValues[0]
	if reading.Type != MetricTypeResource || reading.Name != "cpu" {
		t.Errorf("reading identity = %s/%s, want Resource/cpu", reading.Type, reading.Name)
	}
	if reading.Value != 83 {
		t.Errorf("Value = %v, want 83", reading.Value)
	}
	if reading.Target != 60 {
		t.Errorf("Target = %v, want 60", reading.Target)
	}
	if reading.Unit != "%" {
		t.Errorf("Unit = %q, want %%", reading.Unit)
	}
}

func resourceQty(s string) *resource.Quantity {
	q := resource.MustParse(s)
	return &q
}

func derefFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func floatPtrEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
