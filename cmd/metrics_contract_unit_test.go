package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakediscovery "k8s.io/client-go/discovery/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func TestBuildMetricContractResourceCurrentDataIsReadOnlyLookup(t *testing.T) {
	spec := autoscalingv2.MetricSpec{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{
			Name:   corev1.ResourceCPU,
			Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType},
		},
	}

	current := map[string]bool{}
	metric := buildMetricContractMetric(spec, current, nil)
	if metric.HasCurrentData {
		t.Fatal("Resource metric without status.currentMetrics must report hasCurrentData=false")
	}
	if len(current) != 0 {
		t.Fatalf("building a spec metric must not mutate the current-data map: %#v", current)
	}

	current["Resource/cpu"] = true
	metric = buildMetricContractMetric(spec, current, nil)
	if !metric.HasCurrentData {
		t.Fatal("matching Resource status metric must report hasCurrentData=true")
	}
}

func TestBuildMetricContractMetric_PodsUsesCanonicalSelectorAndFullIdentity(t *testing.T) {
	one := resource.MustParse("1")
	statusSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"app": "web"},
		MatchExpressions: []metav1.LabelSelectorRequirement{{
			Key:      "tier",
			Operator: metav1.LabelSelectorOpIn,
			Values:   []string{"api", "worker"},
		}},
	}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentMetrics: []autoscalingv2.MetricStatus{{
				Type: autoscalingv2.PodsMetricSourceType,
				Pods: &autoscalingv2.PodsMetricStatus{
					Metric:  autoscalingv2.MetricIdentifier{Name: "requests", Selector: statusSelector},
					Current: autoscalingv2.MetricValueStatus{AverageValue: &one},
				},
			}},
		},
	}
	current := buildCurrentMetricDataMap(hpa)
	spec := autoscalingv2.MetricSpec{
		Type: autoscalingv2.PodsMetricSourceType,
		Pods: &autoscalingv2.PodsMetricSource{
			Metric: autoscalingv2.MetricIdentifier{Name: "requests", Selector: statusSelector.DeepCopy()},
		},
	}

	metric := buildMetricContractMetric(spec, current, nil)
	if metric.Resource != "pods" || metric.ResourceName != "*" {
		t.Fatalf("custom metric target = %q/%q, want pods/*", metric.Resource, metric.ResourceName)
	}
	if !metric.HasCurrentData {
		t.Fatal("matching Pods metric selector must report current data")
	}
	if !strings.Contains(metric.Selector, "app=web") || !strings.Contains(metric.Selector, "tier in (api,worker)") {
		t.Fatalf("selector = %q, want canonical Kubernetes selector", metric.Selector)
	}
	if strings.Contains(metric.Selector, "LabelSelector") {
		t.Fatalf("selector leaked struct formatting: %q", metric.Selector)
	}

	spec.Pods.Metric.Selector.MatchLabels["app"] = "other"
	if got := buildMetricContractMetric(spec, current, nil); got.HasCurrentData {
		t.Fatal("same-name Pods metric with a different selector must not match")
	}
}

func TestBuildMetricContractMetric_ObjectIncludesDescribedResourceIdentity(t *testing.T) {
	one := resource.MustParse("1")
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "critical"}}
	ref := autoscalingv2.CrossVersionObjectReference{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Name:       "web",
	}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentMetrics: []autoscalingv2.MetricStatus{{
				Type: autoscalingv2.ObjectMetricSourceType,
				Object: &autoscalingv2.ObjectMetricStatus{
					Metric:          autoscalingv2.MetricIdentifier{Name: "queue_depth", Selector: selector},
					DescribedObject: ref,
					Current:         autoscalingv2.MetricValueStatus{Value: &one},
				},
			}},
		},
	}
	current := buildCurrentMetricDataMap(hpa)
	spec := autoscalingv2.MetricSpec{
		Type: autoscalingv2.ObjectMetricSourceType,
		Object: &autoscalingv2.ObjectMetricSource{
			Metric:          autoscalingv2.MetricIdentifier{Name: "queue_depth", Selector: selector.DeepCopy()},
			DescribedObject: ref,
		},
	}

	resolution := customMetricResourceResolution{Resource: "deployments.apps", Namespaced: true}
	metric := buildMetricContractMetric(spec, current, &resolution)
	if metric.Resource != "deployments.apps" || metric.ResourceName != "web" {
		t.Fatalf("custom metric target = %q/%q, want deployments.apps/web",
			metric.Resource, metric.ResourceName)
	}
	if metric.ResourceNamespaced == nil || !*metric.ResourceNamespaced {
		t.Fatalf("Object metric scope = %v, want namespaced", metric.ResourceNamespaced)
	}
	if !metric.HasCurrentData {
		t.Fatal("matching Object metric identity must report current data")
	}

	spec.Object.Metric.Selector.MatchLabels["queue"] = "bulk"
	if got := buildMetricContractMetric(spec, current, &resolution); got.HasCurrentData {
		t.Fatal("same-name Object metric with a different selector must not match")
	}
	spec.Object.Metric.Selector = selector.DeepCopy()
	spec.Object.DescribedObject.Name = "worker"
	if got := buildMetricContractMetric(spec, current, &resolution); got.HasCurrentData {
		t.Fatal("same-name Object metric for a different object must not match")
	}
}

func TestBuildMetricContractMetric_ExternalSelectorIdentityAndZeroValue(t *testing.T) {
	t.Parallel()

	zero := resource.MustParse("0")
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"queue": "critical"}}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentMetrics: []autoscalingv2.MetricStatus{{
				Type: autoscalingv2.ExternalMetricSourceType,
				External: &autoscalingv2.ExternalMetricStatus{
					Metric:  autoscalingv2.MetricIdentifier{Name: "queue_depth", Selector: selector},
					Current: autoscalingv2.MetricValueStatus{Value: &zero},
				},
			}},
		},
	}
	current := buildCurrentMetricDataMap(hpa)
	spec := autoscalingv2.MetricSpec{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricSource{
			Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth", Selector: selector.DeepCopy()},
		},
	}

	if got := buildMetricContractMetric(spec, current, nil); !got.HasCurrentData {
		t.Fatal("matching External selector with a populated zero must report current data")
	}
	spec.External.Metric.Selector.MatchLabels["queue"] = "bulk"
	if got := buildMetricContractMetric(spec, current, nil); got.HasCurrentData {
		t.Fatal("same-name External metric with a different selector must not match")
	}
}

func TestResolveCustomMetricResourceUsesDiscoveryForIrregularResources(t *testing.T) {
	t.Parallel()

	clientset := kubernetesfake.NewSimpleClientset()
	clientset.Discovery().(*fakediscovery.FakeDiscovery).Resources = []*metav1.APIResourceList{
		{
			GroupVersion: "example.io/v1",
			APIResources: []metav1.APIResource{
				{Name: "people", Kind: "Person", Namespaced: false},
				{Name: "mice", Kind: "Mouse", Namespaced: true},
				{Name: "mice/status", Kind: "Mouse", Namespaced: true},
			},
		},
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "namespaces", Kind: "Namespace", Namespaced: false},
			},
		},
	}
	client := &kube.Client{Interface: clientset, Namespace: "default"}

	tests := []struct {
		apiVersion     string
		kind           string
		wantResource   string
		wantNamespaced bool
	}{
		{apiVersion: "example.io/v1", kind: "Person", wantResource: "people.example.io", wantNamespaced: false},
		{apiVersion: "example.io/v1", kind: "Mouse", wantResource: "mice.example.io", wantNamespaced: true},
		{apiVersion: "v1", kind: "Namespace", wantResource: "namespaces", wantNamespaced: false},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			got := resolveCustomMetricResource(client, autoscalingv2.CrossVersionObjectReference{
				APIVersion: tt.apiVersion,
				Kind:       tt.kind,
			})
			if got.Err != nil {
				t.Fatalf("resolveCustomMetricResource() error = %v", got.Err)
			}
			if got.Resource != tt.wantResource || got.Namespaced != tt.wantNamespaced {
				t.Fatalf(
					"resolveCustomMetricResource() = (%q, %v), want (%q, %v)",
					got.Resource, got.Namespaced, tt.wantResource, tt.wantNamespaced,
				)
			}
		})
	}
}

func TestResolveCustomMetricResourceReportsDiscoveryMiss(t *testing.T) {
	t.Parallel()

	clientset := kubernetesfake.NewSimpleClientset()
	clientset.Discovery().(*fakediscovery.FakeDiscovery).Resources = []*metav1.APIResourceList{{
		GroupVersion: "example.io/v1",
	}}
	got := resolveCustomMetricResource(
		&kube.Client{Interface: clientset},
		autoscalingv2.CrossVersionObjectReference{APIVersion: "example.io/v1", Kind: "Person"},
	)
	if got.Err == nil || got.Resource != "" {
		t.Fatalf("resolveCustomMetricResource() = %+v, want an unresolved-resource error", got)
	}
}

func TestResolveMetricTargetSelector(t *testing.T) {
	t.Parallel()

	clientset := kubernetesfake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "web"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      "tier",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"api", "worker"},
				}},
			},
		},
	})
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "prod"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "web",
			},
		},
	}

	got := resolveMetricTargetSelector(
		context.Background(),
		&kube.Client{Interface: clientset, Namespace: "prod"},
		hpa,
	)
	if got.Err != nil {
		t.Fatalf("resolveMetricTargetSelector() error = %v", got.Err)
	}
	if !strings.Contains(got.Selector, "app=web") || !strings.Contains(got.Selector, "tier in (api,worker)") {
		t.Fatalf("selector = %q, want canonical scale-target selector", got.Selector)
	}
}

func TestResolveMetricTargetSelectorReportsUnsupportedTarget(t *testing.T) {
	t.Parallel()

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "prod"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Widget", Name: "web"},
		},
	}
	got := resolveMetricTargetSelector(
		context.Background(),
		&kube.Client{Interface: kubernetesfake.NewSimpleClientset()},
		hpa,
	)
	if got.Err == nil || got.Selector != "" {
		t.Fatalf("resolveMetricTargetSelector() = %+v, want an explicit unsupported-target error", got)
	}
}

func TestSelectMetricAPIServiceUsesServedCustomMetricsVersion(t *testing.T) {
	t.Parallel()

	clientset := kubernetesfake.NewSimpleClientset()
	clientset.Discovery().(*fakediscovery.FakeDiscovery).Resources = []*metav1.APIResourceList{{
		GroupVersion: "custom.metrics.k8s.io/v1beta2",
	}}
	statuses := make(map[string]hpaanalysis.APIServiceStatus)
	got := selectMetricAPIService(
		context.Background(),
		&kube.Client{Interface: clientset},
		statuses,
		"custom.metrics.k8s.io/v1beta2",
		"custom.metrics.k8s.io/v1beta1",
	)
	if got != "custom.metrics.k8s.io/v1beta2" {
		t.Fatalf("selectMetricAPIService() = %q, want served v1beta2", got)
	}
	if !statuses[got].Available {
		t.Fatalf("selected API status = %+v, want available", statuses[got])
	}
}

func TestBuildMetricContractInputUsesServedVersionAndExactPodSelectors(t *testing.T) {
	t.Parallel()

	metricSelector := &metav1.LabelSelector{MatchLabels: map[string]string{"series": "frontend"}}
	one := resource.MustParse("1")
	clientset := kubernetesfake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "web"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
		},
	})
	clientset.Discovery().(*fakediscovery.FakeDiscovery).Resources = []*metav1.APIResourceList{
		{GroupVersion: "metrics.k8s.io/v1beta1"},
		{GroupVersion: "custom.metrics.k8s.io/v1beta2"},
		{GroupVersion: "external.metrics.k8s.io/v1beta1"},
	}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "web-hpa"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "web",
			},
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.PodsMetricSourceType,
				Pods: &autoscalingv2.PodsMetricSource{
					Metric: autoscalingv2.MetricIdentifier{Name: "requests", Selector: metricSelector},
				},
			}},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentMetrics: []autoscalingv2.MetricStatus{{
				Type: autoscalingv2.PodsMetricSourceType,
				Pods: &autoscalingv2.PodsMetricStatus{
					Metric:  autoscalingv2.MetricIdentifier{Name: "requests", Selector: metricSelector.DeepCopy()},
					Current: autoscalingv2.MetricValueStatus{AverageValue: &one},
				},
			}},
		},
	}

	input := buildMetricContractInput(
		context.Background(),
		&kube.Client{Interface: clientset, Namespace: "prod"},
		hpa,
	)
	report := hpaanalysis.AnalyzeMetricContract(input)
	if report.OverallStatus != "healthy" || len(report.Checks) != 1 {
		t.Fatalf("AnalyzeMetricContract() = %+v, want one healthy check", report)
	}
	commands := hpaanalysis.GenerateContractCommands(report)
	if len(commands) != 1 {
		t.Fatalf("GenerateContractCommands() = %v, want one command", commands)
	}
	for _, want := range []string{
		"/apis/custom.metrics.k8s.io/v1beta2/",
		"labelSelector=app%3Dweb",
		"metricLabelSelector=series%3Dfrontend",
	} {
		if !strings.Contains(commands[0], want) {
			t.Fatalf("command = %q, want substring %q", commands[0], want)
		}
	}
}

func TestBuildCurrentMetricDataMap_DistinguishesZeroFromUnavailable(t *testing.T) {
	zero := int32(0)
	zeroQuantity := resource.MustParse("0")
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentMetrics: []autoscalingv2.MetricStatus{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricStatus{
						Name:    corev1.ResourceCPU,
						Current: autoscalingv2.MetricValueStatus{AverageUtilization: &zero},
					},
				},
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricStatus{
						Name:    corev1.ResourceMemory,
						Current: autoscalingv2.MetricValueStatus{},
					},
				},
				{
					Type: autoscalingv2.PodsMetricSourceType,
					Pods: &autoscalingv2.PodsMetricStatus{
						Metric:  autoscalingv2.MetricIdentifier{Name: "requests"},
						Current: autoscalingv2.MetricValueStatus{AverageValue: &zeroQuantity},
					},
				},
			},
		},
	}

	current := buildCurrentMetricDataMap(hpa)
	if !current["Resource/cpu"] {
		t.Fatal("populated zero must count as current data")
	}
	if current["Resource/memory"] {
		t.Fatal("all-nil metric value must not count as current data")
	}
	if !current[metricContractIdentity("Pods", "requests", "")] {
		t.Fatal("populated zero AverageValue must count as current data")
	}
}
