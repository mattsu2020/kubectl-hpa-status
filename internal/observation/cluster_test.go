package observation

import (
	"errors"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestClusterSnapshotSharesReadsAndKeepsTargetPlacement(t *testing.T) {
	node := func(name, zone, cpu string) *corev1.Node {
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"zone": zone}}, Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse("8Gi"), corev1.ResourcePods: resource.MustParse("100")}, Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}
	}
	client := fake.NewClientset(node("node-a", "a", "4"), node("node-b", "b", "8"), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "busy", Namespace: "other"}, Spec: corev1.PodSpec{NodeName: "node-a", Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}}}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}})
	cluster := &ClusterSnapshot{}
	hpa := testSnapshotHPA()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			snapshot := New(client, &hpa, cluster)
			headroom, err := snapshot.ClusterHeadroom(t.Context(), &corev1.PodSpec{NodeSelector: map[string]string{"zone": "a"}})
			if err != nil {
				t.Error(err)
				return
			}
			if headroom.AvailableCPU.Cmp(resource.MustParse("3")) != 0 || headroom.NodeCapacity.SchedulableNodes != 1 {
				t.Errorf("incorrect placement result: %+v", headroom)
			}
			// Each projection is owned by the caller and cannot alter shared quantities.
			headroom.AvailableCPU.Add(resource.MustParse("99"))
		})
	}
	wg.Wait()
	nodes, pods := 0, 0
	for _, action := range client.Actions() {
		if action.Matches("list", "nodes") {
			nodes++
		}
		if action.Matches("list", "pods") {
			pods++
		}
	}
	if nodes != 1 || pods != 1 {
		t.Fatalf("cluster list calls: nodes=%d pods=%d", nodes, pods)
	}
	other, err := New(client, &hpa, cluster).ClusterHeadroom(t.Context(), &corev1.PodSpec{NodeSelector: map[string]string{"zone": "b"}})
	if err != nil || other.AvailableCPU.Cmp(resource.MustParse("8")) != 0 {
		t.Fatalf("target B reused target A's placement: %+v %v", other, err)
	}
	changed, err := client.CoreV1().Nodes().Get(t.Context(), "node-a", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	changed.Status.Allocatable[corev1.ResourceCPU] = resource.MustParse("6")
	if _, err = client.CoreV1().Nodes().Update(t.Context(), changed, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	fresh, err := New(client, &hpa).ClusterHeadroom(t.Context(), &corev1.PodSpec{NodeSelector: map[string]string{"zone": "a"}})
	if err != nil || fresh.AvailableCPU.Cmp(resource.MustParse("5")) != 0 {
		t.Fatalf("new observation remained stale: %+v %v", fresh, err)
	}
}

func TestClusterSnapshotMemoizesFailuresForOneRun(t *testing.T) {
	client := fake.NewClientset()
	failure := errors.New("forbidden")
	client.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, failure })
	cluster := &ClusterSnapshot{}
	hpa := testSnapshotHPA()
	for i := 0; i < 2; i++ {
		if _, err := New(client, &hpa, cluster).ClusterHeadroom(t.Context(), nil); !errors.Is(err, failure) {
			t.Fatalf("lost observation error: %v", err)
		}
	}
	if len(client.Actions()) != 2 {
		t.Fatalf("repeated failed cluster reads: %v", client.Actions())
	}
	client.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) { return true, &corev1.PodList{}, nil })
	if _, err := New(client, &hpa).ClusterHeadroom(t.Context(), nil); err != nil {
		t.Fatalf("fresh request did not recover: %v", err)
	}
}
