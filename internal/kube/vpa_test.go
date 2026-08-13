package kube

import (
	"testing"

	hpavpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestExtractVPAInfo(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name":      "web-vpa",
				"namespace": "default",
			},
			"spec": map[string]any{
				"targetRef": map[string]any{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"name":       "web",
				},
				"updatePolicy": map[string]any{
					"updateMode": "Auto",
				},
			},
		},
	}

	info := ExtractVPAInfo(u)

	if info.Name != "web-vpa" {
		t.Fatalf("expected name 'web-vpa', got %q", info.Name)
	}
	if info.TargetRef != "Deployment/web" {
		t.Fatalf("expected targetRef 'Deployment/web', got %q", info.TargetRef)
	}
	if info.TargetAPIVersion != "apps/v1" {
		t.Fatalf("expected target apiVersion apps/v1, got %q", info.TargetAPIVersion)
	}
	if info.TargetKind != "Deployment" {
		t.Fatalf("expected targetKind 'Deployment', got %q", info.TargetKind)
	}
	if info.TargetName != "web" {
		t.Fatalf("expected targetName 'web', got %q", info.TargetName)
	}
	if info.UpdateMode != "Auto" {
		t.Fatalf("expected updateMode 'Auto', got %q", info.UpdateMode)
	}
}

func TestExtractVPAInfo_RecommendationsAndControlledResources(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{"name": "web-vpa"},
			"spec": map[string]any{
				"targetRef": map[string]any{"kind": "Deployment", "name": "web"},
				"resourcePolicy": map[string]any{
					"containerPolicies": []any{
						map[string]any{"controlledResources": []any{"cpu", "memory"}},
					},
				},
			},
			"status": map[string]any{
				"recommendation": map[string]any{
					"containerRecommendations": []any{
						map[string]any{
							"containerName": "app",
							"target":        map[string]any{"cpu": "250m", "memory": "256Mi"},
							"lowerBound":    map[string]any{"cpu": "100m"},
							"upperBound":    map[string]any{"memory": "512Mi"},
						},
					},
				},
			},
		},
	}

	info := ExtractVPAInfo(u)
	if len(info.ControlledResources) != 2 || info.ControlledResources[0] != "cpu" || info.ControlledResources[1] != "memory" {
		t.Fatalf("unexpected controlled resources: %#v", info.ControlledResources)
	}
	if len(info.ContainerPolicies) != 1 ||
		info.ContainerPolicies[0].ControlledResourcesSpecified != true {
		t.Fatalf("unexpected container policies: %#v", info.ContainerPolicies)
	}
	if len(info.Recommendations) != 2 {
		t.Fatalf("expected cpu and memory recommendations, got %#v", info.Recommendations)
	}
	if info.Recommendations[0].Container != "app" || info.Recommendations[0].Resource != "cpu" || info.Recommendations[0].Target != "250m" {
		t.Fatalf("unexpected cpu recommendation: %#v", info.Recommendations[0])
	}
}

func TestExtractVPAInfoPreservesPerContainerDefaultsAndModes(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "web-vpa"},
		"spec": map[string]any{
			"resourcePolicy": map[string]any{"containerPolicies": []any{
				map[string]any{
					"containerName":       "app",
					"mode":                "Off",
					"controlledResources": []any{"memory"},
				},
				map[string]any{"containerName": "sidecar"},
			}},
		},
	}}
	info := ExtractVPAInfo(u)
	if len(info.ContainerPolicies) != 2 {
		t.Fatalf("container policies = %#v", info.ContainerPolicies)
	}
	if info.ContainerPolicies[0].Mode != "Off" {
		t.Fatalf("app mode = %q", info.ContainerPolicies[0].Mode)
	}
	if info.ContainerPolicies[1].ControlledResourcesSpecified {
		t.Fatal("omitted sidecar controlledResources was not preserved")
	}
	if len(info.ControlledResources) != 2 ||
		info.ControlledResources[0] != "cpu" ||
		info.ControlledResources[1] != "memory" {
		t.Fatalf("effective aggregate resources = %v, want cpu+memory", info.ControlledResources)
	}
}

func TestExtractVPAInfo_PreservesUnknownUpdateMode(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name": "web-vpa",
			},
			"spec": map[string]any{
				"targetRef": map[string]any{
					"kind": "Deployment",
					"name": "web",
				},
				"updatePolicy": map[string]any{
					"updateMode": "Recommender",
				},
			},
		},
	}

	info := ExtractVPAInfo(u)
	if info.UpdateMode != "Recommender" {
		t.Fatalf("expected unknown updateMode to be preserved, got %q", info.UpdateMode)
	}
}

func TestExtractVPAInfo_OffMode(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name": "web-vpa",
			},
			"spec": map[string]any{
				"targetRef": map[string]any{
					"kind": "Deployment",
					"name": "web",
				},
				"updatePolicy": map[string]any{
					"updateMode": "Off",
				},
			},
		},
	}

	info := ExtractVPAInfo(u)
	if info.UpdateMode != "Off" {
		t.Fatalf("expected updateMode 'Off', got %q", info.UpdateMode)
	}
}

func TestExtractVPAInfo_Nil(t *testing.T) {
	info := ExtractVPAInfo(nil)
	if info.Name != "" {
		t.Fatalf("expected empty name for nil input, got %q", info.Name)
	}
}

func TestExtractVPAInfo_NoSpec(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name": "web-vpa",
			},
		},
	}

	info := ExtractVPAInfo(u)
	if info.Name != "web-vpa" {
		t.Fatalf("expected name 'web-vpa', got %q", info.Name)
	}
	if info.TargetRef != "" {
		t.Fatalf("expected empty targetRef when spec is missing, got %q", info.TargetRef)
	}
}

func TestExtractVPAInfo_NoUpdatePolicy(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name": "web-vpa",
			},
			"spec": map[string]any{
				"targetRef": map[string]any{
					"kind": "Deployment",
					"name": "web",
				},
			},
		},
	}

	info := ExtractVPAInfo(u)
	if info.UpdateMode != "" {
		t.Fatalf("expected empty updateMode when updatePolicy is missing, got %q", info.UpdateMode)
	}
	if info.TargetRef != "Deployment/web" {
		t.Fatalf("expected targetRef 'Deployment/web', got %q", info.TargetRef)
	}
}

func TestHasResourceMetrics_CPUMetric(t *testing.T) {
	targetUtil := int32(80)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetUtil,
						},
					},
				},
			},
		},
	}

	if !hasResourceMetrics(hpa) {
		t.Fatal("expected hasResourceMetrics=true for CPU metric")
	}
}

func TestHasResourceMetrics_MemoryMetric(t *testing.T) {
	targetUtil := int32(70)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceMemory,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetUtil,
						},
					},
				},
			},
		},
	}

	if !hasResourceMetrics(hpa) {
		t.Fatal("expected hasResourceMetrics=true for memory metric")
	}
}

func TestHasResourceMetrics_ContainerResourceMetric(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ContainerResourceMetricSourceType,
				ContainerResource: &autoscalingv2.ContainerResourceMetricSource{
					Name:      corev1.ResourceMemory,
					Container: "app",
				},
			}},
		},
	}

	if !hasResourceMetrics(hpa) {
		t.Fatal("expected hasResourceMetrics=true for a container memory metric")
	}
}

func TestHasResourceMetrics_ExternalOnly(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
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

	if hasResourceMetrics(hpa) {
		t.Fatal("expected hasResourceMetrics=false for external-only metrics")
	}
}

func TestHasResourceMetrics_NoMetrics(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	if hasResourceMetrics(hpa) {
		t.Fatal("expected hasResourceMetrics=false for no metrics")
	}
}

func TestVPAControlsHPAResourceRequiresIntersection(t *testing.T) {
	target := int32(80)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{Spec: autoscalingv2.HorizontalPodAutoscalerSpec{Metrics: []autoscalingv2.MetricSpec{{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{Name: corev1.ResourceCPU,
			Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: &target}},
	}}}}
	if vpaControlsHPAResource(hpa, []string{"memory"}) {
		t.Fatal("memory-only VPA must not conflict with a CPU-only HPA")
	}
	if !vpaControlsHPAResource(hpa, []string{"cpu"}) {
		t.Fatal("CPU-controlled VPA should overlap a CPU HPA")
	}
	if !vpaControlsHPAResource(hpa, nil) {
		t.Fatal("omitted controlledResources defaults to cpu and memory")
	}
}

func TestVPAConflictsWithHPA(t *testing.T) {
	resourceMetric := func(name corev1.ResourceName) autoscalingv2.MetricSpec {
		return autoscalingv2.MetricSpec{
			Type:     autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{Name: name},
		}
	}
	containerResourceMetric := func(name corev1.ResourceName) autoscalingv2.MetricSpec {
		return autoscalingv2.MetricSpec{
			Type: autoscalingv2.ContainerResourceMetricSourceType,
			ContainerResource: &autoscalingv2.ContainerResourceMetricSource{
				Name:      name,
				Container: "app",
			},
		}
	}
	externalMetric := autoscalingv2.MetricSpec{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricSource{
			Metric: autoscalingv2.MetricIdentifier{Name: "queue-depth"},
		},
	}
	hpaFor := func(metrics ...autoscalingv2.MetricSpec) *autoscalingv2.HorizontalPodAutoscaler {
		return &autoscalingv2.HorizontalPodAutoscaler{
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
					Kind: "Deployment",
					Name: "web",
				},
				Metrics: metrics,
			},
		}
	}
	vpaFor := func(mode string, controlled ...string) *VPAInfo {
		return &VPAInfo{
			TargetKind:          "Deployment",
			TargetName:          "web",
			UpdateMode:          mode,
			ControlledResources: controlled,
		}
	}

	tests := []struct {
		name string
		hpa  *autoscalingv2.HorizontalPodAutoscaler
		vpa  *VPAInfo
		want bool
	}{
		{
			name: "resource CPU uses VPA defaults",
			hpa:  hpaFor(resourceMetric(corev1.ResourceCPU)),
			vpa:  vpaFor("Auto"),
			want: true,
		},
		{
			name: "container memory overlaps controlled memory",
			hpa:  hpaFor(containerResourceMetric(corev1.ResourceMemory)),
			vpa:  vpaFor("Auto", "memory"),
			want: true,
		},
		{
			name: "different target API version does not match",
			hpa: func() *autoscalingv2.HorizontalPodAutoscaler {
				h := hpaFor(resourceMetric(corev1.ResourceCPU))
				h.Spec.ScaleTargetRef.APIVersion = "apps/v1"
				return h
			}(),
			vpa: &VPAInfo{
				TargetAPIVersion:    "extensions/v1beta1",
				TargetKind:          "Deployment",
				TargetName:          "web",
				UpdateMode:          "Auto",
				ControlledResources: []string{"cpu"},
			},
		},
		{
			name: "container exact Off overrides active wildcard",
			hpa:  hpaFor(containerResourceMetric(corev1.ResourceCPU)),
			vpa: &VPAInfo{
				TargetKind: "Deployment", TargetName: "web", UpdateMode: "Auto",
				ContainerPolicies: []VPAContainerPolicy{
					{ContainerName: "*"},
					{ContainerName: "app", Mode: "Off"},
				},
			},
		},
		{
			name: "different container policy does not create false positive",
			hpa:  hpaFor(containerResourceMetric(corev1.ResourceCPU)),
			vpa: &VPAInfo{
				TargetKind: "Deployment", TargetName: "web", UpdateMode: "Auto",
				ContainerPolicies: []VPAContainerPolicy{
					{ContainerName: "*", Mode: "Off"},
					{ContainerName: "sidecar", ControlledResources: []string{"cpu"}, ControlledResourcesSpecified: true},
				},
			},
		},
		{
			name: "omitted resources default per container",
			hpa:  hpaFor(containerResourceMetric(corev1.ResourceCPU)),
			vpa: &VPAInfo{
				TargetKind: "Deployment", TargetName: "web", UpdateMode: "Auto",
				ContainerPolicies: []VPAContainerPolicy{
					{ContainerName: "app"},
				},
			},
			want: true,
		},
		{
			name: "controlled resources do not overlap",
			hpa:  hpaFor(resourceMetric(corev1.ResourceCPU)),
			vpa:  vpaFor("Auto", "memory"),
		},
		{
			name: "Off mode is recommendation only",
			hpa:  hpaFor(resourceMetric(corev1.ResourceCPU)),
			vpa:  vpaFor("Off", "cpu"),
		},
		{
			name: "target does not match",
			hpa:  hpaFor(resourceMetric(corev1.ResourceCPU)),
			vpa: &VPAInfo{
				TargetKind:          "StatefulSet",
				TargetName:          "web",
				UpdateMode:          "Auto",
				ControlledResources: []string{"cpu"},
			},
		},
		{
			name: "external metric is not a resource conflict",
			hpa:  hpaFor(externalMetric),
			vpa:  vpaFor("Auto", "cpu"),
		},
		{
			name: "non CPU or memory resource is ignored",
			hpa:  hpaFor(resourceMetric(corev1.ResourceEphemeralStorage)),
			vpa:  vpaFor("Auto", string(corev1.ResourceEphemeralStorage)),
		},
		{
			name: "nil HPA",
			vpa:  vpaFor("Auto", "cpu"),
		},
		{
			name: "nil VPA",
			hpa:  hpaFor(resourceMetric(corev1.ResourceCPU)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VPAConflictsWithHPA(tt.hpa, tt.vpa); got != tt.want {
				t.Fatalf("VPAConflictsWithHPA() = %v, want %v", got, tt.want)
			}
		})
	}
}

func domainVPAInfo(info *VPAInfo) *hpavpa.Info {
	if info == nil {
		return nil
	}
	out := &hpavpa.Info{
		Name: info.Name, TargetAPIVersion: info.TargetAPIVersion,
		TargetKind: info.TargetKind, TargetName: info.TargetName,
		UpdateMode:          info.UpdateMode,
		ControlledResources: append([]string(nil), info.ControlledResources...),
	}
	for _, policy := range info.ContainerPolicies {
		out.ContainerPolicies = append(out.ContainerPolicies, hpavpa.ContainerPolicy{
			ContainerName: policy.ContainerName, Mode: policy.Mode,
			ControlledResources:          append([]string(nil), policy.ControlledResources...),
			ControlledResourcesSpecified: policy.ControlledResourcesSpecified,
		})
	}
	return out
}

func hasResourceMetrics(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	return len(hpavpa.ConflictResources(hpa, &hpavpa.Info{})) > 0
}

func vpaControlsHPAResource(hpa *autoscalingv2.HorizontalPodAutoscaler, resources []string) bool {
	return len(hpavpa.ConflictResources(hpa, &hpavpa.Info{ControlledResources: resources})) > 0
}

func VPAConflictsWithHPA(hpa *autoscalingv2.HorizontalPodAutoscaler, info *VPAInfo) bool {
	return hpavpa.ConflictsWithHPA(hpa, domainVPAInfo(info))
}
