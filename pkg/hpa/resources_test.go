package hpa

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCheckResourceConsistency_NilHPA(t *testing.T) {
	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{Name: "app", Requests: map[string]string{"cpu": "100m"}},
		},
	}
	result := CheckResourceConsistency(nil, resources)
	if result != nil {
		t.Fatal("expected nil for nil HPA")
	}
}

func TestCheckResourceConsistency_NilResources(t *testing.T) {
	hpa := buildTestHPA("cpu", 80)
	result := CheckResourceConsistency(hpa, nil)
	if result != nil {
		t.Fatal("expected nil for nil resources")
	}
}

func TestCheckResourceConsistency_HealthyNoWarnings(t *testing.T) {
	hpa := buildTestHPA("cpu", 80)
	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"cpu": "100m"},
				Limits:   map[string]string{"cpu": "500m"},
			},
		},
	}
	result := CheckResourceConsistency(hpa, resources)
	if result != nil {
		t.Fatalf("expected nil for healthy configuration, got warnings: %+v", result.Warnings)
	}
}

func TestCheckResourceConsistency_MissingRequests(t *testing.T) {
	hpa := buildTestHPA("cpu", 80)
	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{},
			},
		},
	}
	result := CheckResourceConsistency(hpa, resources)
	if result == nil {
		t.Fatal("expected warnings for missing requests")
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(result.Warnings))
	}
	w := result.Warnings[0]
	if w.Category != "missing-requests" {
		t.Fatalf("expected category 'missing-requests', got %q", w.Category)
	}
	if w.Severity != "error" {
		t.Fatalf("expected severity 'error', got %q", w.Severity)
	}
	if w.Container != "app" {
		t.Fatalf("expected container 'app', got %q", w.Container)
	}
	if w.Resource != "cpu" {
		t.Fatalf("expected resource 'cpu', got %q", w.Resource)
	}
}

func TestCheckResourceConsistency_ZeroRequests(t *testing.T) {
	hpa := buildTestHPA("cpu", 80)
	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"cpu": "0"},
			},
		},
	}
	result := CheckResourceConsistency(hpa, resources)
	if result == nil {
		t.Fatal("expected warnings for zero requests")
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(result.Warnings))
	}
	w := result.Warnings[0]
	if w.Category != "zero-requests" {
		t.Fatalf("expected category 'zero-requests', got %q", w.Category)
	}
	if w.Severity != "error" {
		t.Fatalf("expected severity 'error', got %q", w.Severity)
	}
}

func TestCheckResourceConsistency_ZeroRequestsWithSuffix(t *testing.T) {
	hpa := buildTestHPA("cpu", 80)
	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"cpu": "0m"},
			},
		},
	}
	result := CheckResourceConsistency(hpa, resources)
	if result == nil {
		t.Fatal("expected warnings for zero cpu requests with suffix")
	}
	if result.Warnings[0].Category != "zero-requests" {
		t.Fatalf("expected category 'zero-requests', got %q", result.Warnings[0].Category)
	}
}

func TestCheckResourceConsistency_HighTargetUtilization(t *testing.T) {
	hpa := buildTestHPA("cpu", 95)
	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"cpu": "100m"},
				Limits:   map[string]string{"cpu": "500m"},
			},
		},
	}
	result := CheckResourceConsistency(hpa, resources)
	if result == nil {
		t.Fatal("expected warnings for high target utilization")
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(result.Warnings))
	}
	w := result.Warnings[0]
	if w.Category != "target-vs-request-mismatch" {
		t.Fatalf("expected category 'target-vs-request-mismatch', got %q", w.Category)
	}
	if w.Severity != "warning" {
		t.Fatalf("expected severity 'warning', got %q", w.Severity)
	}
}

func TestCheckResourceConsistency_TargetUtilizationExactly90(t *testing.T) {
	hpa := buildTestHPA("cpu", 90)
	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"cpu": "100m"},
				Limits:   map[string]string{"cpu": "500m"},
			},
		},
	}
	result := CheckResourceConsistency(hpa, resources)
	if result != nil {
		t.Fatalf("expected no warnings for 90%% target utilization, got %+v", result.Warnings)
	}
}

func TestCheckResourceConsistency_MemoryMetric(t *testing.T) {
	hpa := buildTestHPA("memory", 85)
	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"memory": "128Mi"},
				Limits:   map[string]string{"memory": "256Mi"},
			},
		},
	}
	result := CheckResourceConsistency(hpa, resources)
	if result != nil {
		t.Fatalf("expected no warnings for memory with healthy config, got %+v", result.Warnings)
	}
}

func TestCheckResourceConsistency_MissingMemoryRequests(t *testing.T) {
	hpa := buildTestHPA("memory", 70)
	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"cpu": "100m"},
				Limits:   map[string]string{"cpu": "500m"},
			},
		},
	}
	result := CheckResourceConsistency(hpa, resources)
	if result == nil {
		t.Fatal("expected warnings for missing memory requests")
	}
	if result.Warnings[0].Category != "missing-requests" {
		t.Fatalf("expected category 'missing-requests', got %q", result.Warnings[0].Category)
	}
	if result.Warnings[0].Resource != "memory" {
		t.Fatalf("expected resource 'memory', got %q", result.Warnings[0].Resource)
	}
}

func TestCheckResourceConsistency_MultipleContainers(t *testing.T) {
	hpa := buildTestHPA("cpu", 80)
	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"cpu": "100m"},
				Limits:   map[string]string{"cpu": "500m"},
			},
			{
				Name:     "sidecar",
				Requests: map[string]string{},
			},
		},
	}
	result := CheckResourceConsistency(hpa, resources)
	if result == nil {
		t.Fatal("expected warnings for sidecar missing cpu request")
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(result.Warnings))
	}
	w := result.Warnings[0]
	if w.Container != "sidecar" {
		t.Fatalf("expected warning for 'sidecar' container, got %q", w.Container)
	}
}

func TestCheckResourceConsistency_ContainerResourceMetric(t *testing.T) {
	targetUtil := int32(95)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "web-hpa",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "web",
			},
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ContainerResourceMetricSourceType,
					ContainerResource: &autoscalingv2.ContainerResourceMetricSource{
						Name:      corev1.ResourceCPU,
						Container: "app",
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetUtil,
						},
					},
				},
			},
		},
	}

	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"cpu": "100m"},
				Limits:   map[string]string{"cpu": "500m"},
			},
			{
				Name:     "sidecar",
				Requests: map[string]string{"cpu": "50m"},
			},
		},
	}

	result := CheckResourceConsistency(hpa, resources)
	if result == nil {
		t.Fatal("expected warning for high target utilization on container resource metric")
	}
	// ContainerResource metric only checks the specified container, not sidecar
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(result.Warnings))
	}
	if result.Warnings[0].Container != "app" {
		t.Fatalf("expected warning for 'app' container, got %q", result.Warnings[0].Container)
	}
	if result.Warnings[0].Category != "target-vs-request-mismatch" {
		t.Fatalf("expected category 'target-vs-request-mismatch', got %q", result.Warnings[0].Category)
	}
}

func TestCheckResourceConsistency_ContainerResourceMetric_ContainerNotFound(t *testing.T) {
	targetUtil := int32(80)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "web-hpa",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "web",
			},
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ContainerResourceMetricSourceType,
					ContainerResource: &autoscalingv2.ContainerResourceMetricSource{
						Name:      corev1.ResourceCPU,
						Container: "nonexistent",
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetUtil,
						},
					},
				},
			},
		},
	}

	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"cpu": "100m"},
				Limits:   map[string]string{"cpu": "500m"},
			},
		},
	}

	result := CheckResourceConsistency(hpa, resources)
	if result == nil {
		t.Fatal("expected warning for missing container")
	}
	if result.Warnings[0].Category != "missing-requests" {
		t.Fatalf("expected category 'missing-requests', got %q", result.Warnings[0].Category)
	}
	if result.Warnings[0].Container != "nonexistent" {
		t.Fatalf("expected container 'nonexistent', got %q", result.Warnings[0].Container)
	}
}

func TestCheckResourceConsistency_NoMetrics(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "web-hpa",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "web",
			},
		},
	}

	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{Name: "app", Requests: map[string]string{"cpu": "100m"}},
		},
	}

	result := CheckResourceConsistency(hpa, resources)
	if result != nil {
		t.Fatalf("expected nil for HPA with no resource metrics, got %+v", result.Warnings)
	}
}

func TestCheckResourceConsistency_ExternalMetricOnly(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "web-hpa",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "web",
			},
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ExternalMetricSourceType,
					External: &autoscalingv2.ExternalMetricSource{
						Metric: autoscalingv2.MetricIdentifier{Name: "queue-depth"},
						Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType},
					},
				},
			},
		},
	}

	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{Name: "app", Requests: map[string]string{"cpu": "100m"}},
		},
	}

	result := CheckResourceConsistency(hpa, resources)
	if result != nil {
		t.Fatalf("expected nil for external-only metrics, got %+v", result.Warnings)
	}
}

func TestCheckResourceConsistency_CombinedWarnings(t *testing.T) {
	cpuTarget := int32(95)
	memTarget := int32(80)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "web-hpa",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "web",
			},
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &cpuTarget,
						},
					},
				},
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceMemory,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &memTarget,
						},
					},
				},
			},
		},
	}

	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"cpu": "100m"},
				Limits:   map[string]string{"cpu": "500m"},
				// memory request is missing
			},
		},
	}

	result := CheckResourceConsistency(hpa, resources)
	if result == nil {
		t.Fatal("expected warnings")
	}
	// Should have: cpu target-vs-request-mismatch (95%), memory missing-requests
	if len(result.Warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %+v", len(result.Warnings), result.Warnings)
	}

	categories := map[string]bool{}
	for _, w := range result.Warnings {
		categories[w.Category] = true
	}
	if !categories["target-vs-request-mismatch"] {
		t.Error("expected target-vs-request-mismatch warning")
	}
	if !categories["missing-requests"] {
		t.Error("expected missing-requests warning")
	}
}

func TestIsZeroQuantity(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"0", true},
		{"0m", true},
		{"0Ki", true},
		{"0Mi", true},
		{"0Gi", true},
		{"100m", false},
		{"1", false},
		{"128Mi", false},
		{"0.0", true},
		{"", true},
		{"0.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isZeroQuantity(tt.input)
			if got != tt.expected {
				t.Errorf("isZeroQuantity(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCheckResourceConsistency_MissingLimits(t *testing.T) {
	hpa := buildTestHPA("cpu", 80)
	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"cpu": "100m"},
				Limits:   map[string]string{},
			},
		},
	}
	result := CheckResourceConsistency(hpa, resources)
	if result == nil {
		t.Fatal("expected warnings")
	}
	found := false
	for _, w := range result.Warnings {
		if w.Category == "missing-limits" {
			found = true
			if w.Severity != "warning" {
				t.Errorf("expected severity warning, got %s", w.Severity)
			}
		}
	}
	if !found {
		t.Error("expected missing-limits warning")
	}
}

func TestCheckResourceConsistency_TinyCPURequest(t *testing.T) {
	hpa := buildTestHPA("cpu", 80)
	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"cpu": "5m"},
				Limits:   map[string]string{"cpu": "100m"},
			},
		},
	}
	result := CheckResourceConsistency(hpa, resources)
	if result == nil {
		t.Fatal("expected warning for tiny CPU request")
	}
	found := false
	for _, w := range result.Warnings {
		if w.Category == "tiny-request" {
			found = true
			if w.Severity != "warning" {
				t.Errorf("expected severity warning, got %s", w.Severity)
			}
			if w.Container != "app" {
				t.Errorf("expected container app, got %s", w.Container)
			}
		}
	}
	if !found {
		t.Errorf("expected tiny-request warning, got categories: %v", warningCategories(result.Warnings))
	}
}

func TestCheckResourceConsistency_TinyMemoryRequest(t *testing.T) {
	hpa := buildTestHPA("memory", 80)
	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"memory": "8Mi"},
				Limits:   map[string]string{"memory": "256Mi"},
			},
		},
	}
	result := CheckResourceConsistency(hpa, resources)
	if result == nil {
		t.Fatal("expected warning for tiny memory request")
	}
	found := false
	for _, w := range result.Warnings {
		if w.Category == "tiny-request" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected tiny-request warning, got categories: %v", warningCategories(result.Warnings))
	}
}

func TestCheckResourceConsistency_NormalRequestNoTinyWarning(t *testing.T) {
	hpa := buildTestHPA("cpu", 80)
	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"cpu": "100m"},
				Limits:   map[string]string{"cpu": "500m"},
			},
		},
	}
	result := CheckResourceConsistency(hpa, resources)
	if result != nil {
		for _, w := range result.Warnings {
			if w.Category == "tiny-request" {
				t.Errorf("did not expect tiny-request warning for 100m CPU: %+v", w)
			}
		}
	}
}

func TestCheckResourceConsistency_SidecarDistortion(t *testing.T) {
	hpa := buildTestHPA("cpu", 80)
	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"cpu": "100m"},
				Limits:   map[string]string{"cpu": "500m"},
			},
			{
				Name:     "sidecar",
				Requests: map[string]string{"cpu": "10m"},
				Limits:   map[string]string{"cpu": "50m"},
			},
		},
	}
	result := CheckResourceConsistency(hpa, resources)
	if result == nil {
		t.Fatal("expected sidecar-distortion warning")
	}
	found := false
	for _, w := range result.Warnings {
		if w.Category == "sidecar-distortion" {
			found = true
			if w.Severity != "warning" {
				t.Errorf("expected severity warning, got %s", w.Severity)
			}
		}
	}
	if !found {
		t.Errorf("expected sidecar-distortion warning, got categories: %v", warningCategories(result.Warnings))
	}
}

func TestCheckResourceConsistency_NoSidecarDistortionForSimilarRequests(t *testing.T) {
	hpa := buildTestHPA("cpu", 80)
	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"cpu": "100m"},
				Limits:   map[string]string{"cpu": "500m"},
			},
			{
				Name:     "sidecar",
				Requests: map[string]string{"cpu": "50m"},
				Limits:   map[string]string{"cpu": "100m"},
			},
		},
	}
	result := CheckResourceConsistency(hpa, resources)
	if result != nil {
		for _, w := range result.Warnings {
			if w.Category == "sidecar-distortion" {
				t.Errorf("did not expect sidecar-distortion warning for 2x ratio: %+v", w)
			}
		}
	}
}

func TestCheckResourceConsistency_NoSidecarDistortionForSingleContainer(t *testing.T) {
	hpa := buildTestHPA("cpu", 80)
	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"cpu": "100m"},
				Limits:   map[string]string{"cpu": "500m"},
			},
		},
	}
	result := CheckResourceConsistency(hpa, resources)
	if result != nil {
		for _, w := range result.Warnings {
			if w.Category == "sidecar-distortion" {
				t.Errorf("did not expect sidecar-distortion warning for single container: %+v", w)
			}
		}
	}
}

func TestCheckResourceConsistency_NoSidecarDistortionForContainerResourceMetric(t *testing.T) {
	targetUtil := int32(80)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "web-hpa",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "web",
			},
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ContainerResourceMetricSourceType,
					ContainerResource: &autoscalingv2.ContainerResourceMetricSource{
						Name:      corev1.ResourceCPU,
						Container: "app",
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetUtil,
						},
					},
				},
			},
		},
	}

	resources := &ResourceRequests{
		Containers: []ContainerResources{
			{
				Name:     "app",
				Requests: map[string]string{"cpu": "100m"},
				Limits:   map[string]string{"cpu": "500m"},
			},
			{
				Name:     "sidecar",
				Requests: map[string]string{"cpu": "10m"},
				Limits:   map[string]string{"cpu": "50m"},
			},
		},
	}

	result := CheckResourceConsistency(hpa, resources)
	if result != nil {
		for _, w := range result.Warnings {
			if w.Category == "sidecar-distortion" {
				t.Errorf("ContainerResource metric should not trigger sidecar-distortion: %+v", w)
			}
		}
	}
}

func warningCategories(warnings []ResourceWarning) []string {
	cats := make([]string, len(warnings))
	for i, w := range warnings {
		cats[i] = w.Category
	}
	return cats
}

func buildTestHPA(resourceName string, targetUtil int32) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "web-hpa",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "web",
			},
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceName(resourceName),
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetUtil,
						},
					},
				},
			},
		},
	}
}
