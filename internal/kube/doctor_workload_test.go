package kube

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	restclient "k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
)

func TestCheckAPIServices(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.Resources = []*metav1.APIResourceList{
		{GroupVersion: "metrics.k8s.io/v1beta1"},
	}

	results := CheckAPIServices(context.Background(), client)
	if len(results) != 3 {
		t.Fatalf("expected 3 API service checks, got %d", len(results))
	}
	byName := map[string]APIServiceStatus{}
	for _, r := range results {
		byName[r.Name] = r
	}
	if got := byName["metrics.k8s.io/v1beta1"].Status; got != "available" {
		t.Errorf("metrics.k8s.io should be available, got %q", got)
	}
	if got := byName["custom.metrics.k8s.io/v1beta1"].Status; got != "unavailable" {
		t.Errorf("custom.metrics.k8s.io should be unavailable, got %q", got)
	}
	if byName["external.metrics.k8s.io/v1beta1"].Message == "" {
		t.Error("unavailable service should carry an actionable message")
	}
}

func TestCheckMetricsServer(t *testing.T) {
	t.Run("ready deployment found", func(t *testing.T) {
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "metrics-server", Namespace: "kube-system"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "metrics-server", Image: "registry.k8s.io/metrics-server:v0.7.0"}},
					},
				},
			},
			Status: appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1},
		}
		status := CheckMetricsServer(context.Background(), fake.NewSimpleClientset(deploy))
		if !status.Available || !status.Ready {
			t.Fatalf("expected available+ready, got %+v", status)
		}
		if status.Version == "" {
			t.Error("expected version extracted from container image")
		}
	})

	t.Run("deployment present but not ready", func(t *testing.T) {
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "metrics-server", Namespace: "kube-system"},
			Status:     appsv1.DeploymentStatus{Replicas: 2, ReadyReplicas: 0},
		}
		status := CheckMetricsServer(context.Background(), fake.NewSimpleClientset(deploy))
		if !status.Available || status.Ready {
			t.Fatalf("expected available but not ready, got %+v", status)
		}
		if status.Message == "" {
			t.Error("not-ready metrics-server should explain the replica state")
		}
	})

	t.Run("absent", func(t *testing.T) {
		status := CheckMetricsServer(context.Background(), fake.NewSimpleClientset())
		if status.Available {
			t.Fatalf("expected unavailable, got %+v", status)
		}
		if status.Message == "" {
			t.Error("absent metrics-server should explain the impact on HPA")
		}
	})
}

func TestCheckRBAC(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "selfsubjectaccessreviews",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			sar := action.(k8stesting.CreateAction).GetObject().(*authorizationv1.SelfSubjectAccessReview)
			// Allow HPA reads only, so both branches are exercised.
			allowed := sar.Spec.ResourceAttributes.Resource == "horizontalpodautoscalers"
			sar.Status.Allowed = allowed
			return true, sar, nil
		})

	status := CheckRBAC(context.Background(), client, "")
	if !status.CanGetHPA || !status.CanListHPA {
		t.Errorf("expected HPA access allowed, got %+v", status)
	}
	if status.CanGetPods || status.CanGetEvents {
		t.Errorf("expected pods/events access denied, got %+v", status)
	}
}

func TestGetHPAFromClient(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "staging"},
	}
	client := &Client{Interface: fake.NewSimpleClientset(hpa), Namespace: "staging"}

	got, err := GetHPAFromClient(context.Background(), client, "web")
	if err != nil {
		t.Fatalf("GetHPAFromClient: %v", err)
	}
	if got.Name != "web" || got.Namespace != "staging" {
		t.Fatalf("unexpected HPA returned: %s/%s", got.Namespace, got.Name)
	}

	if _, err := GetHPAFromClient(context.Background(), client, "missing"); err == nil {
		t.Fatal("expected NotFound error for missing HPA")
	}
}

func TestKubernetesVersions(t *testing.T) {
	v := KubernetesVersions()
	if v.MinAPIVersion == "" || v.StableSinceVersion == "" {
		t.Fatalf("version thresholds must be populated: %+v", v)
	}
	if v.StableSinceMinor <= 0 || v.ContainerResourceMinor <= v.StableSinceMinor {
		t.Errorf("minor version ordering is inconsistent: %+v", v)
	}
}

func TestHomeDir(t *testing.T) {
	want, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir in this environment: %v", err)
	}
	if got := homeDir(); got != want {
		t.Fatalf("homeDir() = %q, want %q", got, want)
	}
}

func TestFetchPodInfosForSelector(t *testing.T) {
	readyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "default", Labels: map[string]string{"app": "web"}},
		Spec:       corev1.PodSpec{NodeName: "node-1"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
	unschedulablePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-2", Namespace: "default", Labels: map[string]string{"app": "web"}},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{
				{
					Type:    corev1.PodScheduled,
					Status:  corev1.ConditionFalse,
					Reason:  corev1.PodReasonUnschedulable,
					Message: "0/3 nodes are available: insufficient cpu",
				},
			},
		},
	}
	client := fake.NewSimpleClientset(readyPod, unschedulablePod)

	infos, err := FetchPodInfosForSelector(context.Background(), client, "default", "app=web")
	if err != nil {
		t.Fatalf("FetchPodInfosForSelector: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("expected 2 pods, got %d", len(infos))
	}
	byName := map[string]PodInfo{}
	for _, info := range infos {
		byName[info.Name] = info
	}
	if !byName["web-1"].Ready || byName["web-1"].NodeName != "node-1" {
		t.Errorf("web-1 should be ready on node-1: %+v", byName["web-1"])
	}
	if !byName["web-2"].Unschedulable || len(byName["web-2"].Reasons) == 0 {
		t.Errorf("web-2 should be unschedulable with reasons: %+v", byName["web-2"])
	}

	empty, err := FetchPodInfosForSelector(context.Background(), client, "default", "")
	if err != nil || empty != nil {
		t.Fatalf("empty selector should return (nil, nil), got (%v, %v)", empty, err)
	}
}

func TestFetchPodsForScaleTargetSelectors(t *testing.T) {
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Selector: selector},
	}
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "default"},
		Spec:       appsv1.StatefulSetSpec{Selector: selector},
	}
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Name: "web-rs", Namespace: "default"},
		Spec:       appsv1.ReplicaSetSpec{Selector: selector},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "default", Labels: map[string]string{"app": "web"}},
	}
	client := fake.NewSimpleClientset(deploy, sts, rs, pod)

	for _, kind := range []string{"Deployment", "StatefulSet", "ReplicaSet"} {
		name := map[string]string{"Deployment": "web", "StatefulSet": "db", "ReplicaSet": "web-rs"}[kind]
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: kind, Name: name},
			},
		}
		names, err := FetchPodsForScaleTarget(context.Background(), client, "default", hpa)
		if err != nil {
			t.Fatalf("%s: FetchPodsForScaleTarget: %v", kind, err)
		}
		if len(names) != 1 || names[0] != "web-1" {
			t.Errorf("%s: expected [web-1], got %v", kind, names)
		}
	}

	unsupported := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "CronJob", Name: "job"},
		},
	}
	if _, err := FetchPodsForScaleTarget(context.Background(), client, "default", unsupported); err == nil {
		t.Fatal("expected unsupported-kind error for CronJob scale target")
	}
}

func TestFetchReplicaSetsForScaleTarget(t *testing.T) {
	owned := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-abc",
			Namespace: "default",
			Labels:    map[string]string{"app": "web"},
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Deployment", Name: "web"},
			},
		},
		Spec:   appsv1.ReplicaSetSpec{Replicas: ptrInt32(3)},
		Status: appsv1.ReplicaSetStatus{Replicas: 3, ReadyReplicas: 2},
	}
	unowned := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-def",
			Namespace: "default",
			Labels:    map[string]string{"app": "web"},
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Deployment", Name: "other"},
			},
		},
	}
	client := fake.NewSimpleClientset(owned, unowned)

	t.Run("deployment filters by owner", func(t *testing.T) {
		ref := autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"}
		infos, err := FetchReplicaSetsForScaleTarget(context.Background(), client, "default", ref, "app=web")
		if err != nil {
			t.Fatalf("FetchReplicaSetsForScaleTarget: %v", err)
		}
		if len(infos) != 1 {
			t.Fatalf("expected 1 owned ReplicaSet, got %d", len(infos))
		}
		got := infos[0]
		if got.Name != "web-abc" || got.DesiredReplicas != 3 || got.ReadyReplicas != 2 {
			t.Errorf("unexpected ReplicaSetInfo: %+v", got)
		}
	})

	t.Run("replicaset direct get", func(t *testing.T) {
		ref := autoscalingv2.CrossVersionObjectReference{Kind: "ReplicaSet", Name: "web-abc"}
		infos, err := FetchReplicaSetsForScaleTarget(context.Background(), client, "default", ref, "")
		if err != nil || len(infos) != 1 {
			t.Fatalf("expected direct ReplicaSet info, got (%v, %v)", infos, err)
		}
	})

	t.Run("statefulset returns nil", func(t *testing.T) {
		ref := autoscalingv2.CrossVersionObjectReference{Kind: "StatefulSet", Name: "db"}
		infos, err := FetchReplicaSetsForScaleTarget(context.Background(), client, "default", ref, "app=db")
		if err != nil || infos != nil {
			t.Fatalf("StatefulSet should return (nil, nil), got (%v, %v)", infos, err)
		}
	})

	t.Run("deployment with empty selector returns nil", func(t *testing.T) {
		ref := autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"}
		infos, err := FetchReplicaSetsForScaleTarget(context.Background(), client, "default", ref, "")
		if err != nil || infos != nil {
			t.Fatalf("empty selector should return (nil, nil), got (%v, %v)", infos, err)
		}
	})
}

func TestFetchRecentEventsForObjects(t *testing.T) {
	now := metav1.NewTime(time.Now())
	older := metav1.NewTime(time.Now().Add(-time.Hour))
	events := []runtime.Object{
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "e1", Namespace: "default"},
			InvolvedObject: corev1.ObjectReference{Name: "web"},
			Reason:         "SuccessfulRescale",
			Message:        "New size: 4\nreason: cpu",
			LastTimestamp:  now,
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "e2", Namespace: "default"},
			InvolvedObject: corev1.ObjectReference{Name: "web"},
			Reason:         "SuccessfulRescale",
			Message:        "New size: 2",
			LastTimestamp:  older,
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "e3", Namespace: "default"},
			InvolvedObject: corev1.ObjectReference{Name: "unrelated"},
			Reason:         "Pulled",
			LastTimestamp:  now,
		},
	}
	client := fake.NewSimpleClientset(events...)

	got := FetchRecentEventsForObjects(context.Background(), client, "default", []string{"web", "", "web"}, 5)
	if len(got) != 2 {
		t.Fatalf("expected 2 events for web, got %d: %+v", len(got), got)
	}
	if got[0].Timestamp.Before(got[1].Timestamp) {
		t.Error("events should be sorted most recent first")
	}
	if got[0].Message != "New size: 4 reason: cpu" {
		t.Errorf("newlines should be flattened, got %q", got[0].Message)
	}

	if trimmed := FetchRecentEventsForObjects(context.Background(), client, "default", []string{"web"}, 1); len(trimmed) != 1 {
		t.Errorf("limit should trim results, got %d", len(trimmed))
	}
	if none := FetchRecentEventsForObjects(context.Background(), client, "default", nil, 5); none != nil {
		t.Errorf("no object names should return nil, got %v", none)
	}
	if none := FetchRecentEventsForObjects(context.Background(), client, "default", []string{"web"}, 0); none != nil {
		t.Errorf("limit 0 should return nil, got %v", none)
	}
}

const testKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    namespace: staging
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user: {}
`

func writeTestKubeconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(testKubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

func TestNewClientFromKubeconfig(t *testing.T) {
	opts := Options{
		Kubeconfig: writeTestKubeconfig(t),
		QPS:        42,
		Burst:      84,
		Timeout:    17 * time.Second,
	}

	client, err := NewClient(opts)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.Namespace != "staging" {
		t.Errorf("namespace should resolve from kubeconfig context, got %q", client.Namespace)
	}

	if _, err := NewDiscoveryClient(opts); err != nil {
		t.Errorf("NewDiscoveryClient: %v", err)
	}
	dyn, ns, err := NewDynamicClient(opts)
	if err != nil {
		t.Fatalf("NewDynamicClient: %v", err)
	}
	if dyn == nil || ns != "staging" {
		t.Errorf("dynamic client should resolve namespace staging, got %q", ns)
	}

	explicit, err := NewClient(Options{Kubeconfig: opts.Kubeconfig, Namespace: "prod"})
	if err != nil {
		t.Fatalf("NewClient with explicit namespace: %v", err)
	}
	if explicit.Namespace != "prod" {
		t.Errorf("explicit namespace should win, got %q", explicit.Namespace)
	}
}

func TestApplyRestOptions(t *testing.T) {
	cfg := &restclient.Config{}
	applyRestOptions(cfg, Options{QPS: 10, Burst: 20, Timeout: 5 * time.Second})
	if cfg.QPS != 10 || cfg.Burst != 20 || cfg.Timeout != 5*time.Second {
		t.Fatalf("options not applied: %+v", cfg)
	}

	untouched := &restclient.Config{QPS: 3, Burst: 6, Timeout: time.Minute}
	applyRestOptions(untouched, Options{})
	if untouched.QPS != 3 || untouched.Burst != 6 || untouched.Timeout != time.Minute {
		t.Fatalf("zero options must not override existing config: %+v", untouched)
	}
}

func newVPAUnstructured(name, targetKind, targetName, updateMode string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "autoscaling.k8s.io/v1",
			"kind":       "VerticalPodAutoscaler",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "default",
			},
			"spec": map[string]any{
				"targetRef": map[string]any{
					"kind": targetKind,
					"name": targetName,
				},
				"updatePolicy": map[string]any{
					"updateMode": updateMode,
				},
			},
		},
	}
}

func TestFindConflictingVPA(t *testing.T) {
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{vpaGVR: "VerticalPodAutoscalerList"},
		newVPAUnstructured("web-vpa", "Deployment", "web", "Auto"),
		newVPAUnstructured("off-vpa", "Deployment", "batch", "Off"),
	)

	cpuHPA := func(targetName string) *autoscalingv2.HorizontalPodAutoscaler {
		return &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Name: targetName, Namespace: "default"},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: targetName},
				Metrics: []autoscalingv2.MetricSpec{{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
					},
				}},
			},
		}
	}

	info, err := FindConflictingVPA(context.Background(), dyn, "default", cpuHPA("web"))
	if err != nil {
		t.Fatalf("FindConflictingVPA: %v", err)
	}
	if info == nil || info.Name != "web-vpa" {
		t.Fatalf("expected conflicting web-vpa, got %+v", info)
	}

	// Off-mode VPAs only recommend and must not be reported as conflicts.
	info, err = FindConflictingVPA(context.Background(), dyn, "default", cpuHPA("batch"))
	if err != nil || info != nil {
		t.Fatalf("Off-mode VPA should not conflict, got (%+v, %v)", info, err)
	}

	// HPAs without resource metrics skip VPA lookup entirely.
	noMetrics := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
		},
	}
	info, err = FindConflictingVPA(context.Background(), dyn, "default", noMetrics)
	if err != nil || info != nil {
		t.Fatalf("no-resource-metric HPA should return (nil, nil), got (%+v, %v)", info, err)
	}
}

func ptrInt32(v int32) *int32 { return &v }
