package metricsapi

import (
	"encoding/json"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGroupVersions(t *testing.T) {
	tests := []struct {
		source string
		want   []string
	}{
		{Resource, []string{"metrics.k8s.io/v1beta1"}},
		{Custom, []string{"custom.metrics.k8s.io/v1beta2", "custom.metrics.k8s.io/v1beta1"}},
		{External, []string{"external.metrics.k8s.io/v1beta1"}},
		{"unknown", nil},
		{"", nil},
	}
	for _, tt := range tests {
		got := GroupVersions(tt.source)
		if len(got) != len(tt.want) {
			t.Fatalf("GroupVersions(%q) = %v, want %v", tt.source, got, tt.want)
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Fatalf("GroupVersions(%q) = %v, want %v", tt.source, got, tt.want)
			}
		}
	}
}

func TestSources(t *testing.T) {
	want := []string{Resource, Custom, External}
	got := Sources()
	if len(got) != len(want) {
		t.Fatalf("Sources() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("Sources() = %v, want %v", got, want)
		}
	}
}

func TestPodMetricsListJSONEmptyShape(t *testing.T) {
	// Regression for the empty metrics shape fix: an empty `items` array and a
	// null `items` must both decode without error and yield no names.
	for name, raw := range map[string]string{
		"empty items": `{"items":[]}`,
		"null items":  `{"items":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			var list PodMetricsList
			if err := json.Unmarshal([]byte(raw), &list); err != nil {
				t.Fatalf("unmarshal %q: %v", raw, err)
			}
			// Both shapes must decode without error and yield no names; a null
			// items array is legitimately represented as a nil (not an empty)
			// slice, which Names() must tolerate.
			if got := list.Names(); len(got) != 0 {
				t.Fatalf("Names() = %v, want empty for %q", got, raw)
			}
		})
	}
}

func TestPodMetricsListNamesFiltersEmpty(t *testing.T) {
	list := PodMetricsList{Items: []PodMetrics{
		{Metadata: metav1.ObjectMeta{Name: "web-0"}},
		{Metadata: metav1.ObjectMeta{Name: ""}}, // pod with no name is skipped
		{Metadata: metav1.ObjectMeta{Name: "web-2"}},
	}}
	want := []string{"web-0", "web-2"}
	if got := list.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
}

func TestPodMetricsListJSONRoundTrip(t *testing.T) {
	list := PodMetricsList{Items: []PodMetrics{{
		Metadata: metav1.ObjectMeta{Name: "api-0", Namespace: "default"},
		Containers: []ContainerMetrics{{
			Name: "app",
			Usage: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("250m"),
			},
		}},
	}}}
	raw, err := json.Marshal(&list)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got PodMetricsList
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Metadata.Name != "api-0" {
		t.Fatalf("round-trip mismatch: %+v", got.Items)
	}
	if q, ok := got.Items[0].Containers[0].Usage[corev1.ResourceCPU]; !ok || q.String() != "250m" {
		t.Fatalf("cpu usage mismatch: %v %v", q, ok)
	}
	if got := list.Names(); !reflect.DeepEqual(got, []string{"api-0"}) {
		t.Fatalf("Names() = %v, want [api-0]", got)
	}
}