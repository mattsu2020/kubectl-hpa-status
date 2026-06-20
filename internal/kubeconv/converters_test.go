package kubeconv

import (
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

func TestPendingPodInfos(t *testing.T) {
	t.Run("empty input returns nil", func(t *testing.T) {
		if got := PendingPodInfos(nil); got != nil {
			t.Fatalf("PendingPodInfos(nil) = %v, want nil", got)
		}
	})
	t.Run("maps all fields with phase Pending", func(t *testing.T) {
		in := []kube.PendingPodDetail{
			{Name: "p1", Unschedulable: true, Reasons: []string{"Insufficient cpu"}},
			{Name: "p2"},
		}
		got := PendingPodInfos(in)
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		if got[0].Name != "p1" || got[0].Phase != "Pending" || !got[0].Unschedulable {
			t.Fatalf("got[0] = %+v", got[0])
		}
		if len(got[0].Reasons) != 1 || got[0].Reasons[0] != "Insufficient cpu" {
			t.Fatalf("got[0].Reasons = %v", got[0].Reasons)
		}
	})
}

func TestToBlockerPodInfos(t *testing.T) {
	in := []kube.PendingPodDetail{{Name: "p1", Unschedulable: true}}
	got := ToBlockerPodInfos(in)
	if len(got) != 1 || got[0].Name != "p1" || got[0].Phase != "Pending" {
		t.Fatalf("ToBlockerPodInfos = %+v", got)
	}
	if got := ToBlockerPodInfos(nil); got != nil {
		t.Fatalf("nil input should produce nil, got %v", got)
	}
}

func TestPendingPodDetail_GenericBuilder(t *testing.T) {
	in := []kube.PendingPodDetail{{Name: "p1"}}
	got := PendingPodDetail(in, func(d kube.PendingPodDetail) string { return d.Name + "-x" })
	if len(got) != 1 || got[0] != "p1-x" {
		t.Fatalf("PendingPodDetail generic = %v", got)
	}
}

func TestResourceRequests(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		if got := ResourceRequests(nil); got != nil {
			t.Fatalf("ResourceRequests(nil) = %v, want nil", got)
		}
	})
	t.Run("maps containers and clones maps", func(t *testing.T) {
		in := &kube.ResourceRequests{
			Containers: []kube.ContainerResources{
				{Name: "c1", Requests: map[string]string{"cpu": "100m"}, Limits: map[string]string{"cpu": "500m"}},
				{Name: "c2"}, // empty maps -> nil maps on output to preserve omitempty
			},
		}
		got := ResourceRequests(in)
		if got == nil || len(got.Containers) != 2 {
			t.Fatalf("got = %+v", got)
		}
		if got.Containers[0].Name != "c1" || got.Containers[0].Requests["cpu"] != "100m" {
			t.Fatalf("c1 = %+v", got.Containers[0])
		}
		// Mutating the output must not affect the input.
		got.Containers[0].Requests["cpu"] = "999"
		if in.Containers[0].Requests["cpu"] != "100m" {
			t.Fatalf("input map was mutated through shared backing array")
		}
		if got.Containers[1].Requests != nil || got.Containers[1].Limits != nil {
			t.Fatalf("empty input maps should stay nil, got %+v", got.Containers[1])
		}
	})
}

func TestQuotas(t *testing.T) {
	in := []kube.QuotaInfo{
		{Name: "q1", Resource: "cpu", Used: "10", Hard: "20"},
	}
	got := Quotas(in)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name != "q1" || got[0].Resource != "cpu" || got[0].Used != "10" || got[0].Hard != "20" {
		t.Fatalf("quota fields: %+v", got[0])
	}
	if !strings.Contains(got[0].Message, "ResourceQuota \"q1\"") {
		t.Fatalf("Message not formatted: %q", got[0].Message)
	}
	if got := Quotas(nil); got != nil {
		t.Fatalf("Quotas(nil) = %v, want nil", got)
	}
}

func TestQuotaDetail_GenericBuilder(t *testing.T) {
	in := []kube.QuotaInfo{{Name: "q1"}}
	got := QuotaDetail(in, func(q kube.QuotaInfo) string { return q.Name + "!" })
	if len(got) != 1 || got[0] != "q1!" {
		t.Fatalf("QuotaDetail generic = %v", got)
	}
}

func TestPDBs_AndPDBsPlain(t *testing.T) {
	in := []kube.PDBInfo{
		{Name: "a", MinAvailable: "3"},
		{Name: "b", MaxUnavailable: "1"},
		{Name: "c"}, // neither set
	}

	t.Run("PDBs includes disruption message", func(t *testing.T) {
		got := PDBs(in)
		if len(got) != 3 {
			t.Fatalf("len = %d, want 3", len(got))
		}
		if got[0].Disruption == "" {
			t.Fatalf("expected non-empty Disruption for %v", got[0])
		}
		if !strings.Contains(got[0].Disruption, "minAvailable=3") {
			t.Fatalf("Disruption = %q", got[0].Disruption)
		}
		if !strings.Contains(got[1].Disruption, "maxUnavailable=1") {
			t.Fatalf("Disruption = %q", got[1].Disruption)
		}
		if !strings.Contains(got[2].Disruption, "no availability constraint") {
			t.Fatalf("Disruption fallback = %q", got[2].Disruption)
		}
	})
	t.Run("PDBsPlain omits disruption message", func(t *testing.T) {
		got := PDBsPlain(in)
		if len(got) != 3 {
			t.Fatalf("len = %d, want 3", len(got))
		}
		for i := range got {
			if got[i].Disruption != "" {
				t.Fatalf("PDBsPlain[%d] has Disruption set: %q", i, got[i].Disruption)
			}
		}
	})
	if got := PDBs(nil); got != nil {
		t.Fatalf("PDBs(nil) = %v, want nil", got)
	}
	if got := PDBsPlain(nil); got != nil {
		t.Fatalf("PDBsPlain(nil) = %v, want nil", got)
	}
}

func TestPDBDisruptionMessage(t *testing.T) {
	cases := []struct {
		name string
		in   kube.PDBInfo
		want string
	}{
		{"minAvailable wins", kube.PDBInfo{MinAvailable: "2", MaxUnavailable: "1"}, "minAvailable=2"},
		{"maxUnavailable", kube.PDBInfo{MaxUnavailable: "1"}, "maxUnavailable=1"},
		{"neither", kube.PDBInfo{}, "no availability constraint"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PDBDisruptionMessage(tc.in)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("PDBDisruptionMessage(%+v) = %q, want to contain %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCloneStringMap(t *testing.T) {
	t.Run("nil for empty", func(t *testing.T) {
		if got := cloneStringMap(nil); got != nil {
			t.Fatalf("cloneStringMap(nil) = %v, want nil", got)
		}
		if got := cloneStringMap(map[string]string{}); got != nil {
			t.Fatalf("cloneStringMap(empty) = %v, want nil", got)
		}
	})
	t.Run("copies contents independently", func(t *testing.T) {
		src := map[string]string{"a": "1"}
		cp := cloneStringMap(src)
		cp["a"] = "2"
		if src["a"] != "1" {
			t.Fatalf("source mutated through clone: %v", src)
		}
	})
}

func TestMakeSlice(t *testing.T) {
	s := makeSlice[int](3)
	if cap(s) != 3 || len(s) != 0 {
		t.Fatalf("makeSlice(3) = len=%d cap=%d, want len=0 cap=3", len(s), cap(s))
	}
}

func TestVPAInfo(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		if got := VPAInfo(nil); got != nil {
			t.Fatalf("VPAInfo(nil) = %v, want nil", got)
		}
	})
	t.Run("maps all fields and copies slices", func(t *testing.T) {
		in := &kube.VPAInfo{
			Name:                "v1",
			TargetRef:           "Deployment/web",
			TargetKind:          "Deployment",
			TargetName:          "web",
			UpdateMode:          "Auto",
			ControlledResources: []string{"cpu", "memory"},
			Recommendations: []kube.VPARecommendationInfo{
				{Container: "app", Resource: "cpu", Target: "500m", Lower: "250m", Upper: "1"},
			},
		}
		got := VPAInfo(in)
		if got == nil {
			t.Fatalf("VPAInfo returned nil for non-nil input")
		}
		if got.Name != "v1" || got.TargetRef != "Deployment/web" || got.UpdateMode != "Auto" {
			t.Fatalf("scalar fields wrong: %+v", got)
		}
		if len(got.ControlledResources) != 2 || got.ControlledResources[0] != "cpu" {
			t.Fatalf("ControlledResources = %v", got.ControlledResources)
		}
		// Mutating the output must not affect the input.
		got.ControlledResources[0] = "mutated"
		if in.ControlledResources[0] != "cpu" {
			t.Fatalf("input ControlledResources mutated: %v", in.ControlledResources)
		}
		if len(got.Recommendations) != 1 {
			t.Fatalf("Recommendations = %v", got.Recommendations)
		}
		rec := got.Recommendations[0]
		if rec.Container != "app" || rec.Resource != "cpu" || rec.Target != "500m" || rec.Lower != "250m" || rec.Upper != "1" {
			t.Fatalf("recommendation fields wrong: %+v", rec)
		}
	})
	t.Run("empty recommendations yields nil slice", func(t *testing.T) {
		got := VPAInfo(&kube.VPAInfo{Name: "v1"})
		if len(got.Recommendations) != 0 {
			t.Fatalf("expected no recommendations, got %v", got.Recommendations)
		}
	})
}
