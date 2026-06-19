package hpa

import (
	"errors"
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSimulateMetricChange_NilHPA(t *testing.T) {
	_, err := SimulateMetricChange(nil, map[string]string{"cpu": "80%"}, HealthWeights{})
	if err == nil {
		t.Error("expected error for nil HPA")
	}
}

func TestSimulateMetricChange_CPU80Percent(t *testing.T) {
	hpa := buildMetricSimHPA(4, 4, 10, 50) // current=4, desired=4, max=10, cpu target=50%

	result, err := SimulateMetricChange(hpa, map[string]string{"cpu": "80%"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.MetricSimulations) != 1 {
		t.Fatalf("expected 1 metric simulation, got %d", len(result.MetricSimulations))
	}

	ms := result.MetricSimulations[0]
	if ms.MetricName != "cpu" {
		t.Errorf("expected metricName=cpu, got %q", ms.MetricName)
	}
	if ms.OriginalValue != "50%" {
		t.Errorf("expected originalValue=50%%, got %q", ms.OriginalValue)
	}
	if ms.SimulatedValue != "80%" {
		t.Errorf("expected simulatedValue=80%%, got %q", ms.SimulatedValue)
	}
	if ms.ProjectedRatio == nil {
		t.Fatal("expected projectedRatio to be set")
	}
	ratio := *ms.ProjectedRatio
	if ratio != 1.6 {
		t.Errorf("expected ratio=1.6, got %.2f", ratio)
	}
	// projected replicas = ceil(4 * 80/50) = ceil(6.4) = 7
	if ms.ProjectedReplicas != 7 {
		t.Errorf("expected projectedReplicas=7, got %d", ms.ProjectedReplicas)
	}
}

func TestSimulateMetricChange_RelativeIncrease(t *testing.T) {
	hpa := buildMetricSimHPA(4, 4, 10, 50)

	result, err := SimulateMetricChange(hpa, map[string]string{"cpu": "+20%"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ms := result.MetricSimulations[0]
	// current 50% * 1.2 = 60%
	if ms.SimulatedValue != "60%" {
		t.Errorf("expected simulatedValue=60%%, got %q", ms.SimulatedValue)
	}
	if ms.ProjectedRatio == nil {
		t.Fatal("expected projectedRatio to be set")
	}
	ratio := *ms.ProjectedRatio
	if ratio != 1.2 {
		t.Errorf("expected ratio=1.2, got %.2f", ratio)
	}
	// projected replicas = ceil(4 * 1.2) = ceil(4.8) = 5
	if ms.ProjectedReplicas != 5 {
		t.Errorf("expected projectedReplicas=5, got %d", ms.ProjectedReplicas)
	}
}

func TestSimulateMetricChange_RelativeDecrease(t *testing.T) {
	hpa := buildMetricSimHPA(8, 8, 20, 50)

	result, err := SimulateMetricChange(hpa, map[string]string{"cpu": "-10%"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ms := result.MetricSimulations[0]
	// current 50% * 0.9 = 45%
	if ms.SimulatedValue != "45%" {
		t.Errorf("expected simulatedValue=45%%, got %q", ms.SimulatedValue)
	}
	if ms.ProjectedRatio == nil {
		t.Fatal("expected projectedRatio to be set")
	}
	ratio := *ms.ProjectedRatio
	if ratio != 0.9 {
		t.Errorf("expected ratio=0.9, got %.2f", ratio)
	}
	// projected replicas = ceil(8 * 0.9) = ceil(7.2) = 8
	if ms.ProjectedReplicas != 8 {
		t.Errorf("expected projectedReplicas=8, got %d", ms.ProjectedReplicas)
	}
}

func TestSimulateMetricChange_InvalidMetricName(t *testing.T) {
	hpa := buildMetricSimHPA(4, 4, 10, 50)

	_, err := SimulateMetricChange(hpa, map[string]string{"nonexistent": "80%"}, HealthWeights{})
	if err == nil {
		t.Fatal("expected error for unknown metric name")
	}
	if !errors.Is(err, ErrMetricNotFound) {
		t.Errorf("expected ErrMetricNotFound, got: %v", err)
	}
}

func TestSimulateMetricChange_InvalidValueFormat(t *testing.T) {
	hpa := buildMetricSimHPA(4, 4, 10, 50)

	_, err := SimulateMetricChange(hpa, map[string]string{"cpu": "abc%"}, HealthWeights{})
	if err == nil {
		t.Error("expected error for invalid value format")
	}
}

func TestSimulateMetricChange_MetricNotInSpec(t *testing.T) {
	hpa := buildSimHPA(4, 4, 10) // no metrics in spec

	_, err := SimulateMetricChange(hpa, map[string]string{"cpu": "80%"}, HealthWeights{})
	if err == nil {
		t.Error("expected error when metric not found in spec")
	}
}

func TestSimulateMetricChange_MultipleOverrides(t *testing.T) {
	hpa := buildMultiMetricSimHPA(4, 4, 10)

	result, err := SimulateMetricChange(hpa, map[string]string{"cpu": "80%", "memory": "70%"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.MetricSimulations) != 2 {
		t.Fatalf("expected 2 metric simulations, got %d", len(result.MetricSimulations))
	}

	names := map[string]bool{}
	for _, ms := range result.MetricSimulations {
		names[ms.MetricName] = true
	}
	if !names["cpu"] || !names["memory"] {
		t.Errorf("expected cpu and memory simulations, got names: %v", names)
	}
}

func TestSimulateMetricChange_DeepCopyIsolation(t *testing.T) {
	hpa := buildMetricSimHPA(4, 4, 10, 50)

	_, err := SimulateMetricChange(hpa, map[string]string{"cpu": "80%"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Original HPA should not be mutated
	origUtil := hpa.Status.CurrentMetrics[0].Resource.Current.AverageUtilization
	if origUtil == nil {
		t.Fatal("original HPA current metric was mutated: AverageUtilization is nil")
	}
	if *origUtil != 50 {
		t.Errorf("original HPA was mutated: cpu utilization=%d, want 50", *origUtil)
	}
}

func TestSimulateMetricChange_MemoryQuantity(t *testing.T) {
	hpa := buildResourceQuantitySimHPA(4, 4, 10)

	result, err := SimulateMetricChange(hpa, map[string]string{"memory": "8Gi"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.MetricSimulations) != 1 {
		t.Fatalf("expected 1 metric simulation, got %d", len(result.MetricSimulations))
	}

	ms := result.MetricSimulations[0]
	if ms.MetricName != "memory" {
		t.Errorf("expected metricName=memory, got %q", ms.MetricName)
	}
}

func TestSimulateMetricChange_InterpretationGenerated(t *testing.T) {
	hpa := buildMetricSimHPA(4, 4, 10, 50)

	result, err := SimulateMetricChange(hpa, map[string]string{"cpu": "80%"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Interpretation) == 0 {
		t.Error("expected interpretation lines to be generated")
	}
}

func TestSimulateMetricChange_RiskAssessmentAtMax(t *testing.T) {
	hpa := buildMetricSimHPA(4, 4, 10, 50)

	result, err := SimulateMetricChange(hpa, map[string]string{"cpu": "200%"}, HealthWeights{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// At 200% utilization, projected replicas should hit max
	ms := result.MetricSimulations[0]
	if ms.ProjectedReplicas > 10 {
		t.Errorf("projectedReplicas should be bounded by maxReplicas=10, got %d", ms.ProjectedReplicas)
	}

	if result.RiskAssessment == "" {
		t.Error("expected risk assessment for high utilization simulation")
	}
}

func TestParseRelativeValue(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		current int32
		want    int32
		wantErr bool
	}{
		{"increase 20%", "+20%", 50, 60, false},
		{"decrease 10%", "-10%", 50, 45, false},
		{"increase 100%", "+100%", 50, 100, false},
		{"decrease 50%", "-50%", 100, 50, false},
		{"invalid no percent", "+20", 50, 0, true},
		{"invalid format", "abc", 50, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRelativeValue(tt.value, tt.current)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("parseRelativeValue(%q, %d) = %d, want %d", tt.value, tt.current, got, tt.want)
			}
		})
	}
}

func TestComputeProjectedReplicas(t *testing.T) {
	tests := []struct {
		name            string
		currentReplicas int32
		ratio           float64
		minReplicas     int32
		maxReplicas     int32
		want            int32
	}{
		{"exact", 4, 1.0, 1, 10, 4},
		{"round up", 4, 1.2, 1, 10, 5},
		{"ceil 6.4", 4, 1.6, 1, 10, 7},
		{"bounded by max", 4, 5.0, 1, 10, 10},
		{"bounded by min", 4, 0.1, 3, 10, 3},
		{"large ratio", 10, 3.0, 1, 100, 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeProjectedReplicas(tt.currentReplicas, tt.ratio, tt.minReplicas, tt.maxReplicas)
			if got != tt.want {
				t.Errorf("computeProjectedReplicas(%d, %.1f, %d, %d) = %d, want %d",
					tt.currentReplicas, tt.ratio, tt.minReplicas, tt.maxReplicas, got, tt.want)
			}
		})
	}
}

func TestApplyMetricOverride_ExternalMetric(t *testing.T) {
	tests := []struct {
		name           string
		overrideValue  string
		hasLabels      bool
		hasAvgValue    bool
		wantValueField bool // true = Value set, false = AverageValue set
	}{
		{
			name:           "external metric value override without labels",
			overrideValue:  "500",
			hasLabels:      false,
			hasAvgValue:    true,
			wantValueField: false, // AverageValue because hasAvgValue && !hasLabels
		},
		{
			name:           "external metric value override without labels no avg",
			overrideValue:  "500",
			hasLabels:      false,
			hasAvgValue:    false,
			wantValueField: true, // Value because !hasLabels && !hasAvgValue
		},
		{
			name:           "external metric value override with labels",
			overrideValue:  "1000",
			hasLabels:      true,
			hasAvgValue:    true,
			wantValueField: true, // Value because hasLabels
		},
		{
			name:           "external metric quantity override",
			overrideValue:  "2k",
			hasLabels:      false,
			hasAvgValue:    false,
			wantValueField: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hpa := buildExternalMetricSimHPA(4, 4, 10, tt.hasLabels, tt.hasAvgValue)

			err := applyMetricOverride(hpa, "http_requests", tt.overrideValue)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(hpa.Status.CurrentMetrics) == 0 {
				t.Fatal("expected current metrics to be present")
			}

			ext := hpa.Status.CurrentMetrics[0].External
			if ext == nil {
				t.Fatal("expected external metric status to be set")
			}

			if tt.wantValueField {
				if ext.Current.Value == nil {
					t.Error("expected Value to be set")
				}
			} else {
				if ext.Current.AverageValue == nil {
					t.Error("expected AverageValue to be set")
				}
			}
		})
	}
}

func TestApplyMetricOverride_PodsMetric(t *testing.T) {
	hpa := buildPodsMetricSimHPA(4, 4, 10)

	err := applyMetricOverride(hpa, "http_requests_per_pod", "200m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(hpa.Status.CurrentMetrics) == 0 {
		t.Fatal("expected current metrics to be present")
	}

	pods := hpa.Status.CurrentMetrics[0].Pods
	if pods == nil {
		t.Fatal("expected pods metric status to be set")
	}
	if pods.Current.AverageValue == nil {
		t.Error("expected AverageValue to be set for pods metric")
	}
	if pods.Metric.Name != "http_requests_per_pod" {
		t.Errorf("expected metric name http_requests_per_pod, got %q", pods.Metric.Name)
	}
}

func TestApplyRelativeOverride_External(t *testing.T) {
	tests := []struct {
		name          string
		value         string
		currentQty    string
		wantErr       bool
		errContains   string
		useValueField bool // true = use Value, false = use AverageValue
	}{
		{
			name:          "relative increase on external metric with Value",
			value:         "+20%",
			currentQty:    "500",
			useValueField: true,
		},
		{
			name:          "relative decrease on external metric with Value",
			value:         "-10%",
			currentQty:    "1000",
			useValueField: true,
		},
		{
			name:          "relative increase on external metric with AverageValue",
			value:         "+50%",
			currentQty:    "200",
			useValueField: false,
		},
		{
			name:        "relative change with no current value",
			value:       "+20%",
			currentQty:  "",
			wantErr:     true,
			errContains: "no current value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hpa := buildExternalMetricSimHPA(4, 4, 10, false, false)

			// Clear external status if testing no-current-value case
			if tt.currentQty == "" {
				hpa.Status.CurrentMetrics[0].External = nil
			} else {
				q := resource.MustParse(tt.currentQty)
				if tt.useValueField {
					hpa.Status.CurrentMetrics[0].External = &autoscalingv2.ExternalMetricStatus{
						Metric: autoscalingv2.MetricIdentifier{Name: "http_requests"},
						Current: autoscalingv2.MetricValueStatus{
							Value: &q,
						},
					}
				} else {
					hpa.Status.CurrentMetrics[0].External = &autoscalingv2.ExternalMetricStatus{
						Metric: autoscalingv2.MetricIdentifier{Name: "http_requests"},
						Current: autoscalingv2.MetricValueStatus{
							AverageValue: &q,
						},
					}
				}
			}

			spec, found := resolveMetricSpec(hpa, "http_requests")
			if !found {
				t.Fatal("expected to find http_requests metric spec")
			}

			err := applyRelativeOverride(hpa, spec, 0, tt.value)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			ext := hpa.Status.CurrentMetrics[0].External
			if ext == nil {
				t.Fatal("expected external metric status to be set")
			}
			if ext.Current.Value == nil {
				t.Error("expected Value to be set after relative override")
			}
		})
	}
}

func TestParseRelativeQuantity(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		current int64
		want    int64
		wantErr bool
	}{
		{"increase 20%", "+20%", 1000, 1200, false},
		{"decrease 10%", "-10%", 1000, 900, false},
		{"increase 100%", "+100%", 500, 1000, false},
		{"decrease 50%", "-50%", 1000, 500, false},
		{"increase 33%", "+33%", 1000, 1330, false},
		{"zero result from -100%", "-100%", 500, 0, false},
		{"invalid no percent", "+20", 1000, 0, true},
		{"too short", "+", 1000, 0, true},
		{"invalid format", "abc%", 1000, 0, true},
		{"empty string", "", 1000, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := resource.NewQuantity(tt.current, resource.DecimalSI)
			got, err := parseRelativeQuantity(tt.value, current)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Value() != tt.want {
				t.Errorf("parseRelativeQuantity(%q, %d) = %d, want %d", tt.value, tt.current, got.Value(), tt.want)
			}
		})
	}
}

func TestFormatMetricValue(t *testing.T) {
	tests := []struct {
		name       string
		metric     autoscalingv2.MetricStatus
		metricType autoscalingv2.MetricSourceType
		want       string
	}{
		{
			name: "resource metric with utilization",
			metric: autoscalingv2.MetricStatus{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricStatus{
					Current: autoscalingv2.MetricValueStatus{
						AverageUtilization: ptrInt32(75),
					},
				},
			},
			metricType: autoscalingv2.ResourceMetricSourceType,
			want:       "75%",
		},
		{
			name: "resource metric with average value",
			metric: autoscalingv2.MetricStatus{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricStatus{
					Current: autoscalingv2.MetricValueStatus{
						AverageValue: ptrQuantity("4Gi"),
					},
				},
			},
			metricType: autoscalingv2.ResourceMetricSourceType,
			want:       "4Gi",
		},
		{
			name: "external metric with value",
			metric: autoscalingv2.MetricStatus{
				Type: autoscalingv2.ExternalMetricSourceType,
				External: &autoscalingv2.ExternalMetricStatus{
					Current: autoscalingv2.MetricValueStatus{
						Value: ptrQuantity("500"),
					},
				},
			},
			metricType: autoscalingv2.ExternalMetricSourceType,
			want:       "500",
		},
		{
			name: "external metric with average value",
			metric: autoscalingv2.MetricStatus{
				Type: autoscalingv2.ExternalMetricSourceType,
				External: &autoscalingv2.ExternalMetricStatus{
					Current: autoscalingv2.MetricValueStatus{
						AverageValue: ptrQuantity("200"),
					},
				},
			},
			metricType: autoscalingv2.ExternalMetricSourceType,
			want:       "200",
		},
		{
			name: "pods metric with average value",
			metric: autoscalingv2.MetricStatus{
				Type: autoscalingv2.PodsMetricSourceType,
				Pods: &autoscalingv2.PodsMetricStatus{
					Current: autoscalingv2.MetricValueStatus{
						AverageValue: ptrQuantity("100m"),
					},
				},
			},
			metricType: autoscalingv2.PodsMetricSourceType,
			want:       "100m",
		},
		{
			name: "resource metric with nil resource",
			metric: autoscalingv2.MetricStatus{
				Type:     autoscalingv2.ResourceMetricSourceType,
				Resource: nil,
			},
			metricType: autoscalingv2.ResourceMetricSourceType,
			want:       "<unknown>",
		},
		{
			name: "external metric with nil external",
			metric: autoscalingv2.MetricStatus{
				Type:     autoscalingv2.ExternalMetricSourceType,
				External: nil,
			},
			metricType: autoscalingv2.ExternalMetricSourceType,
			want:       "<unknown>",
		},
		{
			name: "pods metric with nil pods",
			metric: autoscalingv2.MetricStatus{
				Type: autoscalingv2.PodsMetricSourceType,
				Pods: nil,
			},
			metricType: autoscalingv2.PodsMetricSourceType,
			want:       "<unknown>",
		},
		{
			name: "object metric type returns unknown",
			metric: autoscalingv2.MetricStatus{
				Type: autoscalingv2.ObjectMetricSourceType,
			},
			metricType: autoscalingv2.ObjectMetricSourceType,
			want:       "<unknown>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatMetricValue(tt.metric, tt.metricType)
			if got != tt.want {
				t.Errorf("formatMetricValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindCurrentMetric(t *testing.T) {
	tests := []struct {
		name       string
		buildHPA   func() *autoscalingv2.HorizontalPodAutoscaler
		metricName string
		wantFound  bool
	}{
		{
			name:       "resource metric found",
			buildHPA:   func() *autoscalingv2.HorizontalPodAutoscaler { return buildMetricSimHPA(4, 4, 10, 50) },
			metricName: "cpu",
			wantFound:  true,
		},
		{
			name:       "resource metric case insensitive",
			buildHPA:   func() *autoscalingv2.HorizontalPodAutoscaler { return buildMetricSimHPA(4, 4, 10, 50) },
			metricName: "CPU",
			wantFound:  true,
		},
		{
			name: "external metric found",
			buildHPA: func() *autoscalingv2.HorizontalPodAutoscaler {
				return buildExternalMetricSimHPA(4, 4, 10, false, false)
			},
			metricName: "http_requests",
			wantFound:  true,
		},
		{
			name: "external metric case insensitive",
			buildHPA: func() *autoscalingv2.HorizontalPodAutoscaler {
				return buildExternalMetricSimHPA(4, 4, 10, false, false)
			},
			metricName: "HTTP_Requests",
			wantFound:  true,
		},
		{
			name:       "pods metric found",
			buildHPA:   func() *autoscalingv2.HorizontalPodAutoscaler { return buildPodsMetricSimHPA(4, 4, 10) },
			metricName: "http_requests_per_pod",
			wantFound:  true,
		},
		{
			name:       "pods metric case insensitive",
			buildHPA:   func() *autoscalingv2.HorizontalPodAutoscaler { return buildPodsMetricSimHPA(4, 4, 10) },
			metricName: "HTTP_Requests_Per_Pod",
			wantFound:  true,
		},
		{
			name: "object metric found",
			buildHPA: func() *autoscalingv2.HorizontalPodAutoscaler {
				return buildObjectMetricSimHPA(4, 4, 10)
			},
			metricName: "hits",
			wantFound:  true,
		},
		{
			name:       "metric not found",
			buildHPA:   func() *autoscalingv2.HorizontalPodAutoscaler { return buildMetricSimHPA(4, 4, 10, 50) },
			metricName: "nonexistent",
			wantFound:  false,
		},
		{
			name:       "empty current metrics",
			buildHPA:   func() *autoscalingv2.HorizontalPodAutoscaler { return buildSimHPA(4, 4, 10) },
			metricName: "cpu",
			wantFound:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hpa := tt.buildHPA()
			idx, found := findCurrentMetric(hpa, tt.metricName)
			if found != tt.wantFound {
				t.Errorf("findCurrentMetric(%q) found=%v, want %v", tt.metricName, found, tt.wantFound)
			}
			if tt.wantFound && idx < 0 {
				t.Errorf("findCurrentMetric(%q) returned negative index %d for found metric", tt.metricName, idx)
			}
		})
	}
}

func TestResolveMetricSpec(t *testing.T) {
	tests := []struct {
		name       string
		buildHPA   func() *autoscalingv2.HorizontalPodAutoscaler
		metricName string
		wantFound  bool
		wantType   autoscalingv2.MetricSourceType
	}{
		{
			name:       "resource metric found",
			buildHPA:   func() *autoscalingv2.HorizontalPodAutoscaler { return buildMetricSimHPA(4, 4, 10, 50) },
			metricName: "cpu",
			wantFound:  true,
			wantType:   autoscalingv2.ResourceMetricSourceType,
		},
		{
			name:       "resource metric case insensitive",
			buildHPA:   func() *autoscalingv2.HorizontalPodAutoscaler { return buildMetricSimHPA(4, 4, 10, 50) },
			metricName: "CPU",
			wantFound:  true,
			wantType:   autoscalingv2.ResourceMetricSourceType,
		},
		{
			name: "external metric found",
			buildHPA: func() *autoscalingv2.HorizontalPodAutoscaler {
				return buildExternalMetricSimHPA(4, 4, 10, false, false)
			},
			metricName: "http_requests",
			wantFound:  true,
			wantType:   autoscalingv2.ExternalMetricSourceType,
		},
		{
			name:       "pods metric found",
			buildHPA:   func() *autoscalingv2.HorizontalPodAutoscaler { return buildPodsMetricSimHPA(4, 4, 10) },
			metricName: "http_requests_per_pod",
			wantFound:  true,
			wantType:   autoscalingv2.PodsMetricSourceType,
		},
		{
			name: "object metric found",
			buildHPA: func() *autoscalingv2.HorizontalPodAutoscaler {
				return buildObjectMetricSimHPA(4, 4, 10)
			},
			metricName: "hits",
			wantFound:  true,
			wantType:   autoscalingv2.ObjectMetricSourceType,
		},
		{
			name:       "metric not found in spec",
			buildHPA:   func() *autoscalingv2.HorizontalPodAutoscaler { return buildMetricSimHPA(4, 4, 10, 50) },
			metricName: "nonexistent",
			wantFound:  false,
		},
		{
			name:       "empty spec metrics",
			buildHPA:   func() *autoscalingv2.HorizontalPodAutoscaler { return buildSimHPA(4, 4, 10) },
			metricName: "cpu",
			wantFound:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hpa := tt.buildHPA()
			spec, found := resolveMetricSpec(hpa, tt.metricName)
			if found != tt.wantFound {
				t.Errorf("resolveMetricSpec(%q) found=%v, want %v", tt.metricName, found, tt.wantFound)
			}
			if tt.wantFound && spec.Type != tt.wantType {
				t.Errorf("resolveMetricSpec(%q) type=%v, want %v", tt.metricName, spec.Type, tt.wantType)
			}
		})
	}
}

// ptrQuantity parses a quantity string and returns a pointer to it.
func ptrQuantity(s string) *resource.Quantity {
	q := resource.MustParse(s)
	return &q
}

// buildExternalMetricSimHPA creates an HPA with an external metric for simulation tests.
func buildExternalMetricSimHPA(current, desired, maxReplicas int32, hasLabels, hasAvgValue bool) *autoscalingv2.HorizontalPodAutoscaler {
	hpa := buildSimHPA(current, desired, maxReplicas)
	currentVal := resource.MustParse("500")
	spec := autoscalingv2.MetricSpec{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricSource{
			Metric: autoscalingv2.MetricIdentifier{
				Name:     "http_requests",
				Selector: &metav1.LabelSelector{},
			},
			Target: autoscalingv2.MetricTarget{
				Type:  autoscalingv2.ValueMetricType,
				Value: &currentVal,
			},
		},
	}
	if hasLabels {
		spec.External.Metric.Selector.MatchLabels = map[string]string{"app": "web"}
	}
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{spec}

	status := autoscalingv2.MetricStatus{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricStatus{
			Metric: autoscalingv2.MetricIdentifier{
				Name:     "http_requests",
				Selector: &metav1.LabelSelector{},
			},
			Current: autoscalingv2.MetricValueStatus{
				Value: &currentVal,
			},
		},
	}
	if hasLabels {
		status.External.Metric.Selector.MatchLabels = map[string]string{"app": "web"}
	}
	if hasAvgValue {
		avgVal := resource.MustParse("250")
		status.External.Current.AverageValue = &avgVal
	}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{status}
	return hpa
}

// buildPodsMetricSimHPA creates an HPA with a pods metric for simulation tests.
func buildPodsMetricSimHPA(current, desired, maxReplicas int32) *autoscalingv2.HorizontalPodAutoscaler {
	hpa := buildSimHPA(current, desired, maxReplicas)
	avgVal := resource.MustParse("100m")
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.PodsMetricSourceType,
			Pods: &autoscalingv2.PodsMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name: "http_requests_per_pod",
				},
				Target: autoscalingv2.MetricTarget{
					Type:         autoscalingv2.AverageValueMetricType,
					AverageValue: &avgVal,
				},
			},
		},
	}
	currentVal := resource.MustParse("100m")
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		{
			Type: autoscalingv2.PodsMetricSourceType,
			Pods: &autoscalingv2.PodsMetricStatus{
				Metric: autoscalingv2.MetricIdentifier{
					Name: "http_requests_per_pod",
				},
				Current: autoscalingv2.MetricValueStatus{
					AverageValue: &currentVal,
				},
			},
		},
	}
	return hpa
}

// buildObjectMetricSimHPA creates an HPA with an object metric for simulation tests.
func buildObjectMetricSimHPA(current, desired, maxReplicas int32) *autoscalingv2.HorizontalPodAutoscaler {
	hpa := buildSimHPA(current, desired, maxReplicas)
	targetVal := resource.MustParse("100")
	currentVal := resource.MustParse("80")
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ObjectMetricSourceType,
			Object: &autoscalingv2.ObjectMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name: "hits",
				},
				Target: autoscalingv2.MetricTarget{
					Type:  autoscalingv2.ValueMetricType,
					Value: &targetVal,
				},
				DescribedObject: autoscalingv2.CrossVersionObjectReference{
					Kind: "Ingress",
					Name: "main-ingress",
				},
			},
		},
	}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		{
			Type: autoscalingv2.ObjectMetricSourceType,
			Object: &autoscalingv2.ObjectMetricStatus{
				Metric: autoscalingv2.MetricIdentifier{
					Name: "hits",
				},
				Current: autoscalingv2.MetricValueStatus{
					Value: &currentVal,
				},
			},
		},
	}
	return hpa
}

// buildMetricSimHPA creates an HPA with a CPU resource metric for simulation tests.
func buildMetricSimHPA(current, desired, maxReplicas int32, cpuTargetUtil int32) *autoscalingv2.HorizontalPodAutoscaler {
	hpa := buildSimHPA(current, desired, maxReplicas)
	cpuUtil := cpuTargetUtil
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: "cpu",
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: &cpuUtil,
				},
			},
		},
	}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricStatus{
				Name: "cpu",
				Current: autoscalingv2.MetricValueStatus{
					AverageUtilization: &cpuUtil,
				},
			},
		},
	}
	return hpa
}

// buildMultiMetricSimHPA creates an HPA with CPU and memory resource metrics.
func buildMultiMetricSimHPA(current, desired, maxReplicas int32) *autoscalingv2.HorizontalPodAutoscaler {
	hpa := buildSimHPA(current, desired, maxReplicas)
	cpuUtil := int32(50)
	memUtil := int32(60)
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: "cpu",
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: &cpuUtil,
				},
			},
		},
		{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: "memory",
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: &memUtil,
				},
			},
		},
	}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricStatus{
				Name: "cpu",
				Current: autoscalingv2.MetricValueStatus{
					AverageUtilization: &cpuUtil,
				},
			},
		},
		{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricStatus{
				Name: "memory",
				Current: autoscalingv2.MetricValueStatus{
					AverageUtilization: &memUtil,
				},
			},
		},
	}
	return hpa
}

// buildResourceQuantitySimHPA creates an HPA with a memory resource metric using quantity target.
func buildResourceQuantitySimHPA(current, desired, maxReplicas int32) *autoscalingv2.HorizontalPodAutoscaler {
	hpa := buildSimHPA(current, desired, maxReplicas)
	memTarget := resource.MustParse("4Gi")
	memCurrent := resource.MustParse("3Gi")
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: "memory",
				Target: autoscalingv2.MetricTarget{
					Type:         autoscalingv2.AverageValueMetricType,
					AverageValue: &memTarget,
				},
			},
		},
	}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricStatus{
				Name: "memory",
				Current: autoscalingv2.MetricValueStatus{
					AverageValue: &memCurrent,
				},
			},
		},
	}
	return hpa
}
