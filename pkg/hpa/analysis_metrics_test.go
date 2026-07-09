package hpa

import (
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFormatMetricStatusIncludesExternalSelector(t *testing.T) {
	target := resource.MustParse("10")
	current := resource.MustParse("20")
	hpa := baseHPA()
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name: "queue_depth",
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"queue": "payments"},
					},
				},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
	}

	got := FormatMetricStatus(hpa, autoscalingv2.MetricStatus{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricStatus{
			Metric: autoscalingv2.MetricIdentifier{
				Name: "queue_depth",
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"queue": "payments"},
				},
			},
			Current: autoscalingv2.MetricValueStatus{Value: &current},
		},
	})

	if got.Selector != "queue=payments" {
		t.Fatalf("expected selector in metric, got %#v", got)
	}
	if !strings.Contains(got.Text, `selector="queue=payments"`) {
		t.Fatalf("expected selector in text, got %q", got.Text)
	}
}

func TestDiagnoseMetricsPipeline_NilHPA(t *testing.T) {
	got := DiagnoseMetricsPipeline(nil)
	if got != nil {
		t.Fatalf("expected nil for nil HPA, got %#v", got)
	}
}

func TestDiagnoseMetricsPipeline(t *testing.T) {
	// externalValueSpec/Status build a value-type External metric pair. Reused
	// across the partial-match and external-healthy cases.
	externalValueSpec := func(name string) autoscalingv2.MetricSpec {
		target := resource.MustParse("10")
		return autoscalingv2.MetricSpec{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{Name: name},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		}
	}
	externalValueStatus := func(name string) autoscalingv2.MetricStatus {
		current := resource.MustParse("12")
		return autoscalingv2.MetricStatus{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricStatus{
				Metric:  autoscalingv2.MetricIdentifier{Name: name},
				Current: autoscalingv2.MetricValueStatus{Value: &current},
			},
		}
	}

	tests := []struct {
		name             string
		specMetrics      []autoscalingv2.MetricSpec
		currentMetrics   []autoscalingv2.MetricStatus
		wantOverall      string
		wantNumChecks    int
		wantStatusByName map[string]string // metric name -> expected status (empty means don't check)
		wantRemediation  *bool             // nil = don't check; otherwise expect non-empty matching the dereferenced value
		wantMetricType   string            // optional: assert MetricType of the first check
	}{
		{
			name:          "NoSpecMetrics",
			wantOverall:   "healthy",
			wantNumChecks: 0,
		},
		{
			name: "AllMetricsMissing",
			specMetrics: []autoscalingv2.MetricSpec{
				resourceMetricSpec(corev1.ResourceCPU, 80),
				resourceMetricSpec(corev1.ResourceMemory, 70),
			},
			// No current metrics set — simulates metrics server being down.
			wantOverall:     "error",
			wantNumChecks:   2,
			wantRemediation: boolPtr(true),
		},
		{
			name: "AllMetricsHealthy",
			specMetrics: []autoscalingv2.MetricSpec{
				resourceMetricSpec(corev1.ResourceCPU, 80),
				resourceMetricSpec(corev1.ResourceMemory, 70),
			},
			currentMetrics: []autoscalingv2.MetricStatus{
				resourceMetricStatus(corev1.ResourceCPU, 75),
				resourceMetricStatus(corev1.ResourceMemory, 65),
			},
			wantOverall:   "healthy",
			wantNumChecks: 2,
		},
		{
			name: "PartialMatches",
			specMetrics: []autoscalingv2.MetricSpec{
				resourceMetricSpec(corev1.ResourceCPU, 80),
				externalValueSpec("queue_depth"),
			},
			currentMetrics: []autoscalingv2.MetricStatus{
				resourceMetricStatus(corev1.ResourceCPU, 75),
				// External metric intentionally omitted — simulates partial missing.
			},
			wantOverall:      "degraded",
			wantNumChecks:    2,
			wantStatusByName: map[string]string{"cpu": "healthy", "queue_depth": "missing"},
			wantRemediation:  boolPtr(true),
		},
		{
			name:           "ExternalMetricHealthy",
			specMetrics:    []autoscalingv2.MetricSpec{externalValueSpec("queue_depth")},
			currentMetrics: []autoscalingv2.MetricStatus{externalValueStatus("queue_depth")},
			wantOverall:    "healthy",
			wantNumChecks:  1,
			wantMetricType: "External",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hpa := baseHPA()
			hpa.Spec.Metrics = tc.specMetrics
			hpa.Status.CurrentMetrics = tc.currentMetrics

			got := DiagnoseMetricsPipeline(hpa)
			if got == nil {
				t.Fatal("expected non-nil result")
			}
			if got.OverallStatus != tc.wantOverall {
				t.Errorf("OverallStatus = %q, want %q", got.OverallStatus, tc.wantOverall)
			}
			if len(got.PerMetricChecks) != tc.wantNumChecks {
				t.Fatalf("got %d PerMetricChecks, want %d", len(got.PerMetricChecks), tc.wantNumChecks)
			}
			if tc.wantStatusByName != nil {
				for name, wantStatus := range tc.wantStatusByName {
					found := false
					for _, check := range got.PerMetricChecks {
						if check.MetricName == name {
							found = true
							if check.Status != wantStatus {
								t.Errorf("metric %s status = %q, want %q", name, check.Status, wantStatus)
							}
						}
					}
					if !found {
						t.Errorf("metric %s not found in checks", name)
					}
				}
			}
			if tc.wantMetricType != "" && got.PerMetricChecks[0].MetricType != tc.wantMetricType {
				t.Errorf("first check MetricType = %q, want %q", got.PerMetricChecks[0].MetricType, tc.wantMetricType)
			}
			if tc.wantRemediation != nil {
				gotRemediation := len(got.RemediationSteps) > 0
				if gotRemediation != *tc.wantRemediation {
					t.Errorf("RemediationSteps present = %v, want %v", gotRemediation, *tc.wantRemediation)
				}
			}
		})
	}
}

func TestExternalMetricMatching_DistinguishesSelector(t *testing.T) {
	target := resource.MustParse("10")
	currentA := resource.MustParse("20")
	currentB := resource.MustParse("5")
	hpa := baseHPA()
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name:     "queue_depth",
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "payments"}},
				},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name:     "queue_depth",
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "orders"}},
				},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
	}
	// Only the "payments" selector metric is present in currentMetrics.
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricStatus{
				Metric:  autoscalingv2.MetricIdentifier{Name: "queue_depth", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "payments"}}},
				Current: autoscalingv2.MetricValueStatus{Value: &currentA},
			},
		},
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricStatus{
				Metric:  autoscalingv2.MetricIdentifier{Name: "queue_depth", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "orders"}}},
				Current: autoscalingv2.MetricValueStatus{Value: &currentB},
			},
		},
	}

	got := Analyze(hpa, true)

	// Both metrics should be found (no "missing" diagnostic for either)
	paymentsFound := false
	ordersFound := false
	for _, line := range got.Interpretation {
		if strings.Contains(line, `queue_depth`) && strings.Contains(line, "payments") && strings.Contains(line, "is configured but no matching") {
			t.Errorf("payments metric should not be reported missing: %s", line)
		}
		if strings.Contains(line, `queue_depth`) && strings.Contains(line, "2.000x") {
			paymentsFound = true
		}
		if strings.Contains(line, `queue_depth`) && strings.Contains(line, "0.500x") {
			ordersFound = true
		}
	}
	if !paymentsFound {
		t.Fatal("expected payments external metric ratio diagnostic")
	}
	if !ordersFound {
		t.Fatal("expected orders external metric ratio diagnostic")
	}

	// Diagnostics should show "payments" selector and "orders" selector separately
	pipeline := DiagnoseMetricsPipeline(hpa)
	if pipeline.OverallStatus != "healthy" {
		t.Fatalf("expected healthy pipeline, got %s", pipeline.OverallStatus)
	}
}

func TestExternalMetricMatching_SameNameDifferentSelector_MissingDetected(t *testing.T) {
	target := resource.MustParse("10")
	currentA := resource.MustParse("20")
	hpa := baseHPA()
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name:     "queue_depth",
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "payments"}},
				},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name:     "queue_depth",
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "orders"}},
				},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
	}
	// Only "payments" is present — "orders" should be detected as missing.
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricStatus{
				Metric:  autoscalingv2.MetricIdentifier{Name: "queue_depth", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "payments"}}},
				Current: autoscalingv2.MetricValueStatus{Value: &currentA},
			},
		},
	}

	pipeline := DiagnoseMetricsPipeline(hpa)
	if pipeline.OverallStatus != "degraded" {
		t.Fatalf("expected degraded pipeline, got %s", pipeline.OverallStatus)
	}
	healthyCount := 0
	missingCount := 0
	for _, check := range pipeline.PerMetricChecks {
		switch check.Status {
		case "healthy":
			healthyCount++
		case "missing":
			missingCount++
		}
	}
	if healthyCount != 1 {
		t.Fatalf("expected 1 healthy, got %d", healthyCount)
	}
	if missingCount != 1 {
		t.Fatalf("expected 1 missing, got %d", missingCount)
	}
}

func TestObjectMetricMatching_DistinguishesDescribedObject(t *testing.T) {
	target := resource.MustParse("100")
	currentA := resource.MustParse("150")
	hpa := baseHPA()
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ObjectMetricSourceType,
			Object: &autoscalingv2.ObjectMetricSource{
				DescribedObject: autoscalingv2.CrossVersionObjectReference{Kind: "Service", Name: "web"},
				Metric:          autoscalingv2.MetricIdentifier{Name: "requests"},
				Target:          autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
		{
			Type: autoscalingv2.ObjectMetricSourceType,
			Object: &autoscalingv2.ObjectMetricSource{
				DescribedObject: autoscalingv2.CrossVersionObjectReference{Kind: "Service", Name: "api"},
				Metric:          autoscalingv2.MetricIdentifier{Name: "requests"},
				Target:          autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
	}
	// Only the "web" object is present — "api" should be missing.
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		{
			Type: autoscalingv2.ObjectMetricSourceType,
			Object: &autoscalingv2.ObjectMetricStatus{
				DescribedObject: autoscalingv2.CrossVersionObjectReference{Kind: "Service", Name: "web"},
				Metric:          autoscalingv2.MetricIdentifier{Name: "requests"},
				Current:         autoscalingv2.MetricValueStatus{Value: &currentA},
			},
		},
	}

	got := Analyze(hpa, true)
	if !containsLine(got.Interpretation, "Object metric \"requests\"") {
		t.Fatal("expected object metric diagnostic")
	}

	// Diagnostics pipeline should detect "api" as missing.
	pipeline := DiagnoseMetricsPipeline(hpa)
	if pipeline.OverallStatus != "degraded" {
		t.Fatalf("expected degraded pipeline, got %s", pipeline.OverallStatus)
	}
}

func TestPodsMetricMatching_DistinguishesSelector(t *testing.T) {
	averageTarget := resource.MustParse("100m")
	averageCurrentA := resource.MustParse("120m")
	hpa := baseHPA()
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.PodsMetricSourceType,
			Pods: &autoscalingv2.PodsMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name:     "requests_per_second",
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
				},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.AverageValueMetricType, AverageValue: &averageTarget},
			},
		},
		{
			Type: autoscalingv2.PodsMetricSourceType,
			Pods: &autoscalingv2.PodsMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name:     "requests_per_second",
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
				},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.AverageValueMetricType, AverageValue: &averageTarget},
			},
		},
	}
	// Only the "web" selector metric is present.
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		{
			Type: autoscalingv2.PodsMetricSourceType,
			Pods: &autoscalingv2.PodsMetricStatus{
				Metric:  autoscalingv2.MetricIdentifier{Name: "requests_per_second", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}},
				Current: autoscalingv2.MetricValueStatus{AverageValue: &averageCurrentA},
			},
		},
	}

	pipeline := DiagnoseMetricsPipeline(hpa)
	if pipeline.OverallStatus != "degraded" {
		t.Fatalf("expected degraded pipeline, got %s", pipeline.OverallStatus)
	}
	healthyCount := 0
	missingCount := 0
	for _, check := range pipeline.PerMetricChecks {
		switch check.Status {
		case "healthy":
			healthyCount++
		case "missing":
			missingCount++
		}
	}
	if healthyCount != 1 {
		t.Fatalf("expected 1 healthy, got %d", healthyCount)
	}
	if missingCount != 1 {
		t.Fatalf("expected 1 missing, got %d", missingCount)
	}
}
