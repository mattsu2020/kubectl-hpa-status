package metricidentity

import (
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var (
	// unsortedSelector is declared out of key order; canonical form sorts it.
	unsortedSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"tier": "web", "app": "api"}}
	invalidSelector  = &metav1.LabelSelector{MatchLabels: map[string]string{"not a key": "x"}}
)

func utilizationTarget(value int32) autoscalingv2.MetricTarget {
	return autoscalingv2.MetricTarget{
		Type:               autoscalingv2.UtilizationMetricType,
		AverageUtilization: ptr.To(value),
	}
}

func valueTarget(n int64) autoscalingv2.MetricTarget {
	return autoscalingv2.MetricTarget{
		Type:  autoscalingv2.ValueMetricType,
		Value: resource.NewQuantity(n, resource.DecimalSI),
	}
}

func utilizationValue(value int32) autoscalingv2.MetricValueStatus {
	return autoscalingv2.MetricValueStatus{AverageUtilization: ptr.To(value)}
}

func resourceSpec(name corev1.ResourceName, target int32) autoscalingv2.MetricSpec {
	return autoscalingv2.MetricSpec{
		Type:     autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{Name: name, Target: utilizationTarget(target)},
	}
}

func resourceStatus(name corev1.ResourceName) autoscalingv2.MetricStatus {
	return autoscalingv2.MetricStatus{
		Type:     autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricStatus{Name: name, Current: utilizationValue(50)},
	}
}

func containerResourceSpec(name corev1.ResourceName, container string) autoscalingv2.MetricSpec {
	return autoscalingv2.MetricSpec{
		Type: autoscalingv2.ContainerResourceMetricSourceType,
		ContainerResource: &autoscalingv2.ContainerResourceMetricSource{
			Name:      name,
			Container: container,
			Target:    utilizationTarget(70),
		},
	}
}

func containerResourceStatus(name corev1.ResourceName, container string) autoscalingv2.MetricStatus {
	return autoscalingv2.MetricStatus{
		Type: autoscalingv2.ContainerResourceMetricSourceType,
		ContainerResource: &autoscalingv2.ContainerResourceMetricStatus{
			Name:      name,
			Container: container,
			Current:   utilizationValue(40),
		},
	}
}

func podsSpec(name string, selector *metav1.LabelSelector) autoscalingv2.MetricSpec {
	return autoscalingv2.MetricSpec{
		Type: autoscalingv2.PodsMetricSourceType,
		Pods: &autoscalingv2.PodsMetricSource{
			Metric: autoscalingv2.MetricIdentifier{Name: name, Selector: selector},
			Target: autoscalingv2.MetricTarget{
				Type:         autoscalingv2.AverageValueMetricType,
				AverageValue: resource.NewMilliQuantity(100, resource.DecimalSI),
			},
		},
	}
}

func podsStatus(name string, selector *metav1.LabelSelector) autoscalingv2.MetricStatus {
	return autoscalingv2.MetricStatus{
		Type: autoscalingv2.PodsMetricSourceType,
		Pods: &autoscalingv2.PodsMetricStatus{
			Metric:  autoscalingv2.MetricIdentifier{Name: name, Selector: selector},
			Current: autoscalingv2.MetricValueStatus{AverageValue: resource.NewMilliQuantity(80, resource.DecimalSI)},
		},
	}
}

func externalSpec(selector *metav1.LabelSelector) autoscalingv2.MetricSpec {
	return autoscalingv2.MetricSpec{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricSource{
			Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth", Selector: selector},
			Target: valueTarget(10),
		},
	}
}

func externalStatus(selector *metav1.LabelSelector) autoscalingv2.MetricStatus {
	return autoscalingv2.MetricStatus{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricStatus{
			Metric:  autoscalingv2.MetricIdentifier{Name: "queue_depth", Selector: selector},
			Current: autoscalingv2.MetricValueStatus{Value: resource.NewQuantity(4, resource.DecimalSI)},
		},
	}
}

func objectSpec(kind, name string) autoscalingv2.MetricSpec {
	return objectSpecWithSelector(kind, name, nil)
}

func objectSpecWithSelector(kind, name string, selector *metav1.LabelSelector) autoscalingv2.MetricSpec {
	return autoscalingv2.MetricSpec{
		Type: autoscalingv2.ObjectMetricSourceType,
		Object: &autoscalingv2.ObjectMetricSource{
			DescribedObject: autoscalingv2.CrossVersionObjectReference{Kind: kind, Name: name, APIVersion: "v1"},
			Metric:          autoscalingv2.MetricIdentifier{Name: "requests_per_second", Selector: selector},
			Target:          valueTarget(100),
		},
	}
}

func objectStatus(kind, name string) autoscalingv2.MetricStatus {
	return objectStatusWithSelector(kind, name, nil)
}

func objectStatusWithSelector(kind, name string, selector *metav1.LabelSelector) autoscalingv2.MetricStatus {
	return autoscalingv2.MetricStatus{
		Type: autoscalingv2.ObjectMetricSourceType,
		Object: &autoscalingv2.ObjectMetricStatus{
			DescribedObject: autoscalingv2.CrossVersionObjectReference{Kind: kind, Name: name, APIVersion: "v1"},
			Metric:          autoscalingv2.MetricIdentifier{Name: "requests_per_second", Selector: selector},
			Current:         autoscalingv2.MetricValueStatus{Value: resource.NewQuantity(60, resource.DecimalSI)},
		},
	}
}

func TestMetricDescriptorFromSpec(t *testing.T) {
	tests := []struct {
		name         string
		spec         autoscalingv2.MetricSpec
		wantID       MetricID
		wantSelector bool
	}{
		{
			name:   "resource",
			spec:   resourceSpec(corev1.ResourceCPU, 80),
			wantID: MetricID{Type: autoscalingv2.ResourceMetricSourceType, Name: "cpu"},
		},
		{
			name:   "container resource keeps the container name",
			spec:   containerResourceSpec(corev1.ResourceMemory, "app"),
			wantID: MetricID{Type: autoscalingv2.ContainerResourceMetricSourceType, Name: "memory", Container: "app"},
		},
		{
			name:         "pods canonicalizes the selector",
			spec:         podsSpec("http_requests", unsortedSelector),
			wantID:       MetricID{Type: autoscalingv2.PodsMetricSourceType, Name: "http_requests", Selector: "app=api,tier=web"},
			wantSelector: true,
		},
		{
			name:   "external without selector",
			spec:   externalSpec(nil),
			wantID: MetricID{Type: autoscalingv2.ExternalMetricSourceType, Name: "queue_depth"},
		},
		{
			name: "object keeps the described object",
			spec: objectSpec("Service", "web"),
			wantID: MetricID{
				Type:                autoscalingv2.ObjectMetricSourceType,
				Name:                "requests_per_second",
				DescribedObject:     "Service/web",
				DescribedAPIVersion: "v1",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MetricDescriptorFromSpec(tc.spec)
			if err != nil {
				t.Fatalf("MetricDescriptorFromSpec() error = %v", err)
			}
			if got.ID != tc.wantID {
				t.Errorf("ID = %+v, want %+v", got.ID, tc.wantID)
			}
			if got.Target == nil {
				t.Error("Target = nil, want the spec target")
			}
			if (got.Selector != nil) != tc.wantSelector {
				t.Errorf("Selector = %v, want present = %v", got.Selector, tc.wantSelector)
			}
		})
	}
}

func TestMetricDescriptorFromSpecErrors(t *testing.T) {
	tests := []struct {
		name    string
		spec    autoscalingv2.MetricSpec
		wantMsg string
	}{
		{
			name:    "nil resource source",
			spec:    autoscalingv2.MetricSpec{Type: autoscalingv2.ResourceMetricSourceType},
			wantMsg: "resource metric source is nil",
		},
		{
			name:    "nil container resource source",
			spec:    autoscalingv2.MetricSpec{Type: autoscalingv2.ContainerResourceMetricSourceType},
			wantMsg: "container resource metric source is nil",
		},
		{
			name:    "nil pods source",
			spec:    autoscalingv2.MetricSpec{Type: autoscalingv2.PodsMetricSourceType},
			wantMsg: "pods metric source is nil",
		},
		{
			name:    "nil external source",
			spec:    autoscalingv2.MetricSpec{Type: autoscalingv2.ExternalMetricSourceType},
			wantMsg: "external metric source is nil",
		},
		{
			name:    "nil object source",
			spec:    autoscalingv2.MetricSpec{Type: autoscalingv2.ObjectMetricSourceType},
			wantMsg: "object metric source is nil",
		},
		{
			name:    "unsupported type",
			spec:    autoscalingv2.MetricSpec{Type: autoscalingv2.MetricSourceType("Bogus")},
			wantMsg: `unsupported metric source type "Bogus"`,
		},
		{
			name:    "invalid pods selector",
			spec:    podsSpec("http_requests", invalidSelector),
			wantMsg: "invalid metric selector",
		},
		{
			name:    "invalid external selector",
			spec:    externalSpec(invalidSelector),
			wantMsg: "invalid metric selector",
		},
		{
			name:    "invalid object selector",
			spec:    objectSpecWithSelector("Service", "web", invalidSelector),
			wantMsg: "invalid metric selector",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MetricDescriptorFromSpec(tc.spec)
			if err == nil {
				t.Fatalf("MetricDescriptorFromSpec() error = nil, want %q", tc.wantMsg)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantMsg)
			}
			if got.ID.Type != tc.spec.Type {
				t.Errorf("ID.Type = %q on error, want the source type %q", got.ID.Type, tc.spec.Type)
			}
			if id, idErr := MetricIDFromSpec(tc.spec); idErr == nil || id != got.ID {
				t.Errorf("MetricIDFromSpec() = (%+v, %v), want the same error and ID as the descriptor", id, idErr)
			}
		})
	}
}

func TestMetricDescriptorFromStatus(t *testing.T) {
	tests := []struct {
		name         string
		status       autoscalingv2.MetricStatus
		wantID       MetricID
		wantSelector bool
	}{
		{
			name:   "resource",
			status: resourceStatus(corev1.ResourceCPU),
			wantID: MetricID{Type: autoscalingv2.ResourceMetricSourceType, Name: "cpu"},
		},
		{
			name:   "container resource keeps the container name",
			status: containerResourceStatus(corev1.ResourceMemory, "app"),
			wantID: MetricID{Type: autoscalingv2.ContainerResourceMetricSourceType, Name: "memory", Container: "app"},
		},
		{
			name:         "pods canonicalizes the selector",
			status:       podsStatus("http_requests", unsortedSelector),
			wantID:       MetricID{Type: autoscalingv2.PodsMetricSourceType, Name: "http_requests", Selector: "app=api,tier=web"},
			wantSelector: true,
		},
		{
			name:   "external without selector",
			status: externalStatus(nil),
			wantID: MetricID{Type: autoscalingv2.ExternalMetricSourceType, Name: "queue_depth"},
		},
		{
			name:   "object keeps the described object",
			status: objectStatus("Service", "web"),
			wantID: MetricID{
				Type:                autoscalingv2.ObjectMetricSourceType,
				Name:                "requests_per_second",
				DescribedObject:     "Service/web",
				DescribedAPIVersion: "v1",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MetricDescriptorFromStatus(tc.status)
			if err != nil {
				t.Fatalf("MetricDescriptorFromStatus() error = %v", err)
			}
			if got.ID != tc.wantID {
				t.Errorf("ID = %+v, want %+v", got.ID, tc.wantID)
			}
			if got.Current == nil {
				t.Error("Current = nil, want the status value")
			}
			if (got.Selector != nil) != tc.wantSelector {
				t.Errorf("Selector = %v, want present = %v", got.Selector, tc.wantSelector)
			}
		})
	}
}

func TestMetricDescriptorFromStatusErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  autoscalingv2.MetricStatus
		wantMsg string
	}{
		{
			name:    "nil resource status",
			status:  autoscalingv2.MetricStatus{Type: autoscalingv2.ResourceMetricSourceType},
			wantMsg: "resource metric status is nil",
		},
		{
			name:    "nil container resource status",
			status:  autoscalingv2.MetricStatus{Type: autoscalingv2.ContainerResourceMetricSourceType},
			wantMsg: "container resource metric status is nil",
		},
		{
			name:    "nil pods status",
			status:  autoscalingv2.MetricStatus{Type: autoscalingv2.PodsMetricSourceType},
			wantMsg: "pods metric status is nil",
		},
		{
			name:    "nil external status",
			status:  autoscalingv2.MetricStatus{Type: autoscalingv2.ExternalMetricSourceType},
			wantMsg: "external metric status is nil",
		},
		{
			name:    "nil object status",
			status:  autoscalingv2.MetricStatus{Type: autoscalingv2.ObjectMetricSourceType},
			wantMsg: "object metric status is nil",
		},
		{
			name:    "unsupported type",
			status:  autoscalingv2.MetricStatus{Type: autoscalingv2.MetricSourceType("Bogus")},
			wantMsg: `unsupported metric status type "Bogus"`,
		},
		{
			name:    "invalid pods selector",
			status:  podsStatus("http_requests", invalidSelector),
			wantMsg: "invalid metric selector",
		},
		{
			name:    "invalid external selector",
			status:  externalStatus(invalidSelector),
			wantMsg: "invalid metric selector",
		},
		{
			name:    "invalid object selector",
			status:  objectStatusWithSelector("Service", "web", invalidSelector),
			wantMsg: "invalid metric selector",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MetricDescriptorFromStatus(tc.status)
			if err == nil {
				t.Fatalf("MetricDescriptorFromStatus() error = nil, want %q", tc.wantMsg)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantMsg)
			}
			if got.ID.Type != tc.status.Type {
				t.Errorf("ID.Type = %q on error, want the status type %q", got.ID.Type, tc.status.Type)
			}
			if id, idErr := MetricIDFromStatus(tc.status); idErr == nil || id != got.ID {
				t.Errorf("MetricIDFromStatus() = (%+v, %v), want the same error and ID as the descriptor", id, idErr)
			}
		})
	}
}

func TestCanonicalMetricSelector(t *testing.T) {
	tests := []struct {
		name     string
		selector *metav1.LabelSelector
		want     string
	}{
		{name: "nil selector", selector: nil, want: ""},
		{name: "empty selector", selector: &metav1.LabelSelector{}, want: ""},
		{
			name:     "labels are sorted",
			selector: unsortedSelector,
			want:     "app=api,tier=web",
		},
		{
			name: "expression values are sorted",
			selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: "tier", Operator: metav1.LabelSelectorOpIn, Values: []string{"web", "api"}},
			}},
			want: "tier in (api,web)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanonicalMetricSelector(tc.selector)
			if err != nil {
				t.Fatalf("CanonicalMetricSelector() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("CanonicalMetricSelector() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCanonicalMetricSelectorRejectsInvalidKeys(t *testing.T) {
	got, err := CanonicalMetricSelector(invalidSelector)
	if err == nil {
		t.Fatal("CanonicalMetricSelector() error = nil, want an error for an invalid label key")
	}
	if !strings.Contains(err.Error(), "invalid metric selector") {
		t.Errorf("error = %q, want it to wrap the cause with %q", err, "invalid metric selector")
	}
	if got != "" {
		t.Errorf("CanonicalMetricSelector() = %q on error, want empty", got)
	}
}

func TestIdentityMatches(t *testing.T) {
	tests := []struct {
		name   string
		spec   autoscalingv2.MetricSpec
		status autoscalingv2.MetricStatus
		want   bool
	}{
		{
			name:   "same resource metric",
			spec:   resourceSpec(corev1.ResourceCPU, 80),
			status: resourceStatus(corev1.ResourceCPU),
			want:   true,
		},
		{
			name:   "different resource",
			spec:   resourceSpec(corev1.ResourceCPU, 80),
			status: resourceStatus(corev1.ResourceMemory),
		},
		{
			name:   "same container resource",
			spec:   containerResourceSpec(corev1.ResourceMemory, "app"),
			status: containerResourceStatus(corev1.ResourceMemory, "app"),
			want:   true,
		},
		{
			name:   "different container",
			spec:   containerResourceSpec(corev1.ResourceMemory, "app"),
			status: containerResourceStatus(corev1.ResourceMemory, "sidecar"),
		},
		{
			name:   "equivalent selectors written in a different order",
			spec:   podsSpec("http_requests", unsortedSelector),
			status: podsStatus("http_requests", &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api", "tier": "web"}}),
			want:   true,
		},
		{
			name:   "different selector",
			spec:   podsSpec("http_requests", unsortedSelector),
			status: podsStatus("http_requests", nil),
		},
		{
			name:   "different metric name",
			spec:   podsSpec("http_requests", nil),
			status: podsStatus("http_errors", nil),
		},
		{
			name:   "same external metric",
			spec:   externalSpec(nil),
			status: externalStatus(nil),
			want:   true,
		},
		{
			name:   "same object metric",
			spec:   objectSpec("Service", "web"),
			status: objectStatus("Service", "web"),
			want:   true,
		},
		{
			name:   "different described object",
			spec:   objectSpec("Service", "web"),
			status: objectStatus("Deployment", "web"),
		},
		{
			name:   "different source types never match",
			spec:   resourceSpec(corev1.ResourceCPU, 80),
			status: podsStatus("cpu", nil),
		},
		{
			name:   "malformed spec",
			spec:   autoscalingv2.MetricSpec{Type: autoscalingv2.ResourceMetricSourceType},
			status: resourceStatus(corev1.ResourceCPU),
		},
		{
			name:   "malformed status",
			spec:   resourceSpec(corev1.ResourceCPU, 80),
			status: autoscalingv2.MetricStatus{Type: autoscalingv2.ResourceMetricSourceType},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IdentityMatches(tc.spec, tc.status); got != tc.want {
				t.Errorf("IdentityMatches() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCurrentMetricValueStatus(t *testing.T) {
	status := autoscalingv2.MetricStatus{
		Type:     autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricStatus{Name: corev1.ResourceCPU, Current: utilizationValue(65)},
	}
	got, ok := CurrentMetricValueStatus(status)
	if !ok {
		t.Fatal("CurrentMetricValueStatus() ok = false, want true")
	}
	if got.AverageUtilization == nil || *got.AverageUtilization != 65 {
		t.Errorf("AverageUtilization = %v, want 65", got.AverageUtilization)
	}

	for name, malformed := range map[string]autoscalingv2.MetricStatus{
		"missing source":   {Type: autoscalingv2.ResourceMetricSourceType},
		"unsupported type": {Type: autoscalingv2.MetricSourceType("Bogus")},
		"invalid selector": podsStatus("http_requests", invalidSelector),
	} {
		if value, ok := CurrentMetricValueStatus(malformed); ok {
			t.Errorf("CurrentMetricValueStatus(%s) = (%+v, true), want ok = false", name, value)
		}
	}
}

func TestMatchingTarget(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{
				resourceSpec(corev1.ResourceMemory, 60),
				resourceSpec(corev1.ResourceCPU, 80),
			},
		},
	}

	target, ok := MatchingTarget(hpa, resourceStatus(corev1.ResourceCPU))
	if !ok {
		t.Fatal("MatchingTarget() ok = false, want the CPU target")
	}
	if target.AverageUtilization == nil || *target.AverageUtilization != 80 {
		t.Errorf("AverageUtilization = %v, want 80 (the target of the matching metric, not the first one)", target.AverageUtilization)
	}

	if target, ok := MatchingTarget(hpa, resourceStatus(corev1.ResourceStorage)); ok {
		t.Errorf("MatchingTarget() = (%+v, true) for an unknown metric, want ok = false", target)
	}
}

func TestHasValueForTarget(t *testing.T) {
	quantity := resource.NewQuantity(5, resource.DecimalSI)
	tests := []struct {
		name   string
		value  autoscalingv2.MetricValueStatus
		target autoscalingv2.MetricTargetType
		want   bool
	}{
		{
			name:   "utilization present",
			value:  autoscalingv2.MetricValueStatus{AverageUtilization: ptr.To(int32(50))},
			target: autoscalingv2.UtilizationMetricType,
			want:   true,
		},
		{
			name:   "utilization absent",
			value:  autoscalingv2.MetricValueStatus{Value: quantity},
			target: autoscalingv2.UtilizationMetricType,
		},
		{
			name:   "average value present",
			value:  autoscalingv2.MetricValueStatus{AverageValue: quantity},
			target: autoscalingv2.AverageValueMetricType,
			want:   true,
		},
		{
			name:   "average value absent",
			value:  autoscalingv2.MetricValueStatus{Value: quantity},
			target: autoscalingv2.AverageValueMetricType,
		},
		{
			name:   "value present",
			value:  autoscalingv2.MetricValueStatus{Value: quantity},
			target: autoscalingv2.ValueMetricType,
			want:   true,
		},
		{
			name:   "value absent",
			value:  autoscalingv2.MetricValueStatus{AverageValue: quantity},
			target: autoscalingv2.ValueMetricType,
		},
		{
			name:   "unknown target type",
			value:  autoscalingv2.MetricValueStatus{Value: quantity},
			target: autoscalingv2.MetricTargetType("Bogus"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasValueForTarget(tc.value, tc.target); got != tc.want {
				t.Errorf("HasValueForTarget() = %v, want %v", got, tc.want)
			}
		})
	}
}
