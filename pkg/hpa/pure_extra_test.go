package hpa

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

func TestSuggestionDiff(t *testing.T) {
	desired := int32(4)
	tests := []struct {
		name           string
		currentMin     *int32
		currentDesired int32
		currentMax     int32
		patch          string
		want           string
	}{
		{
			name:  "invalid patch printed verbatim",
			patch: "not-json",
			want:  "  patch: not-json\n",
		},
		{
			name:  "empty spec patch printed verbatim",
			patch: `{"spec":{}}`,
			want:  "  patch: {\"spec\":{}}\n",
		},
		{
			name:           "min max and desired rendered",
			currentMin:     &desired,
			currentDesired: 4,
			currentMax:     10,
			patch:          `{"spec":{"minReplicas":2,"maxReplicas":20}}`,
			want:           "  status.desiredReplicas: 4 (current status, unchanged by patch)\n  spec.minReplicas: 4 -> 2\n  spec.maxReplicas: 10 -> 20\n",
		},
		{
			name:           "minReplicas uses default when currentMin unset",
			currentDesired: 3,
			currentMax:     12,
			patch:          `{"spec":{"minReplicas":5}}`,
			want:           "  status.desiredReplicas: 3 (current status, unchanged by patch)\n  spec.minReplicas: 1 -> 5\n",
		},
		{
			name:  "behavior change reported without value",
			patch: `{"spec":{"behavior":{}}}`,
			want:  "  status.desiredReplicas: 0 (current status, unchanged by patch)\n  spec.behavior: updated\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SuggestionDiff(tt.currentMin, tt.currentDesired, tt.currentMax, tt.patch)
			if got != tt.want {
				t.Fatalf("SuggestionDiff() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestApplyContainerResourceMetricOverride(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentMetrics: make([]autoscalingv2.MetricStatus, 1),
		},
	}
	spec := autoscalingv2.MetricSpec{
		Type: autoscalingv2.ContainerResourceMetricSourceType,
		ContainerResource: &autoscalingv2.ContainerResourceMetricSource{
			Name:      "cpu",
			Container: "app",
			Target:    autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType},
		},
	}

	if err := applyContainerResourceMetricOverride(hpa, spec, 0, "80%"); err != nil {
		t.Fatalf("util override: %v", err)
	}
	got := hpa.Status.CurrentMetrics[0].ContainerResource
	if got == nil || got.Current.AverageUtilization == nil || *got.Current.AverageUtilization != 80 {
		t.Fatalf("utilization not applied: %+v", got)
	}
	if got.Container != "app" || got.Name != "cpu" {
		t.Fatalf("container resource identity wrong: %+v", got)
	}

	// Invalid utilization propagates error.
	if err := applyContainerResourceMetricOverride(hpa, spec, 0, "not-a-number"); err == nil {
		t.Fatal("expected error for invalid utilization")
	}

	// AverageValue branch.
	avSpec := autoscalingv2.MetricSpec{
		Type: autoscalingv2.ContainerResourceMetricSourceType,
		ContainerResource: &autoscalingv2.ContainerResourceMetricSource{
			Name:      "memory",
			Container: "app",
			Target:    autoscalingv2.MetricTarget{Type: autoscalingv2.AverageValueMetricType},
		},
	}
	if err := applyContainerResourceMetricOverride(hpa, avSpec, 0, "500Mi"); err != nil {
		t.Fatalf("quantity override: %v", err)
	}
	if got := hpa.Status.CurrentMetrics[0].ContainerResource.Current.AverageValue; got == nil || got.String() != "500Mi" {
		t.Fatalf("average value not applied: %v", got)
	}
}

func TestApplyObjectMetricOverride(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentMetrics: make([]autoscalingv2.MetricStatus, 1),
		},
	}

	avgSpec := autoscalingv2.MetricSpec{
		Type: autoscalingv2.ObjectMetricSourceType,
		Object: &autoscalingv2.ObjectMetricSource{
			Metric:          autoscalingv2.MetricIdentifier{Name: "queued"},
			DescribedObject: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "api"},
			Target:          autoscalingv2.MetricTarget{Type: autoscalingv2.AverageValueMetricType},
		},
	}
	if err := applyObjectMetricOverride(hpa, avgSpec, 0, "12"); err != nil {
		t.Fatalf("average override: %v", err)
	}
	got := hpa.Status.CurrentMetrics[0].Object
	if got == nil || got.Current.AverageValue == nil || got.Current.AverageValue.String() != "12" {
		t.Fatalf("averageValue not applied: %+v", got)
	}

	valSpec := autoscalingv2.MetricSpec{
		Type: autoscalingv2.ObjectMetricSourceType,
		Object: &autoscalingv2.ObjectMetricSource{
			Metric:          autoscalingv2.MetricIdentifier{Name: "uptime"},
			DescribedObject: autoscalingv2.CrossVersionObjectReference{Kind: "Service", Name: "api"},
			Target:          autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType},
		},
	}
	if err := applyObjectMetricOverride(hpa, valSpec, 0, "9"); err != nil {
		t.Fatalf("value override: %v", err)
	}
	if got := hpa.Status.CurrentMetrics[0].Object.Current.Value; got == nil || got.String() != "9" {
		t.Fatalf("value not applied: %+v", hpa.Status.CurrentMetrics[0].Object)
	}

	// Invalid quantity propagates error.
	if err := applyObjectMetricOverride(hpa, valSpec, 0, "oops"); err == nil {
		t.Fatal("expected error for invalid quantity")
	}
}

func TestCapacityObservationDomain(t *testing.T) {
	tests := []struct {
		source string
		want   CapacityObservationDomain
	}{
		{"scale target", CapacityObservationScaleTarget},
		{"scale target Pod selector", CapacityObservationPendingPods},
		{"scale target Pods", CapacityObservationPendingPods},
		{"Pod resource requests", CapacityObservationPodResources},
		{"ResourceQuotas", CapacityObservationResourceQuotas},
		{"LimitRanges", CapacityObservationLimitRanges},
		{"cluster request headroom", CapacityObservationNodeCapacity},
		{"PodDisruptionBudgets", CapacityObservationPDBs},
		{"Cluster Autoscaler detection", CapacityObservationClusterAutoscaler},
		{"unknown source", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := capacityObservationDomain(CapacityObservationError{Source: tt.source})
		if got != tt.want {
			t.Fatalf("capacityObservationDomain(source=%q) = %q, want %q", tt.source, got, tt.want)
		}
	}

	// Explicit Domain wins over Source mapping.
	got := capacityObservationDomain(CapacityObservationError{
		Domain: CapacityObservationPDBs,
		Source: "PodDisruptionBudgets",
	})
	if got != CapacityObservationPDBs {
		t.Fatalf("explicit domain = %q, want %q", got, CapacityObservationPDBs)
	}
}

func TestHealthStateFromSignals(t *testing.T) {
	sig := func(sev HealthState) HealthSignal { return HealthSignal{Severity: sev, Reason: "x"} }
	tests := []struct {
		name  string
		input []HealthSignal
		want  HealthState
	}{
		{name: "empty", input: nil, want: HealthOK},
		{name: "stabilized only", input: []HealthSignal{sig(HealthStabilized)}, want: HealthStabilized},
		{name: "limited only", input: []HealthSignal{sig(HealthLimited)}, want: HealthLimited},
		{name: "limited does not downgrade from stabilized", input: []HealthSignal{sig(HealthStabilized), sig(HealthLimited)}, want: HealthLimited},
		{name: "error wins over limited", input: []HealthSignal{sig(HealthLimited), sig(HealthError)}, want: HealthError},
		{name: "stabilized after limited stays limited", input: []HealthSignal{sig(HealthLimited), sig(HealthStabilized)}, want: HealthLimited},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := healthStateFromSignals(tt.input); got != tt.want {
				t.Fatalf("healthStateFromSignals() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsDynamicHealthSignal(t *testing.T) {
	for _, dynamic := range []string{
		enrichmentPenaltyKEDAInactive,
		enrichmentPenaltyVPAConflict,
		enrichmentPenaltyChurn,
	} {
		if !isDynamicHealthSignal(dynamic) {
			t.Fatalf("expected %q to be a dynamic health signal", dynamic)
		}
	}
	for _, static := range []string{"", "ScalingActive is not True", "ScalingLimited is True"} {
		if isDynamicHealthSignal(static) {
			t.Fatalf("did not expect %q to be a dynamic health signal", static)
		}
	}
}
