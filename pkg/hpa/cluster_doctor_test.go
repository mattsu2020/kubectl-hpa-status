package hpa

import (
	"strings"
	"testing"
)

func TestBuildClusterDiagnosticsSummary_Healthy(t *testing.T) {
	d := &ClusterDiagnostics{
		APIServices:   []APIServiceCheck{{Name: "metrics.k8s.io/v1beta1", Status: "available"}},
		MetricsServer: &MetricsServerCheck{Available: true, Ready: true},
		RBAC:          &RBACCheckResult{CanGetHPA: true, CanListHPA: true, CanGetPods: true},
	}
	BuildClusterDiagnosticsSummary(d)
	if d.OverallStatus != "healthy" {
		t.Fatalf("OverallStatus = %q, want healthy", d.OverallStatus)
	}
	if !strings.Contains(d.Summary, "prerequisites are met") {
		t.Fatalf("unexpected summary: %q", d.Summary)
	}
}

func TestBuildClusterDiagnosticsSummary_UnhealthyCombination(t *testing.T) {
	d := &ClusterDiagnostics{
		APIServices: []APIServiceCheck{
			{Name: "metrics.k8s.io/v1beta1", Status: "available"},
			{Name: "custom.metrics.k8s.io/v1beta1", Status: "unavailable"},
		},
		MetricsServer: &MetricsServerCheck{Available: false},
		RBAC:          &RBACCheckResult{CanGetHPA: true, CanListHPA: false, CanGetPods: true},
	}
	BuildClusterDiagnosticsSummary(d)
	if d.OverallStatus != "unhealthy" {
		t.Fatalf("OverallStatus = %q, want unhealthy", d.OverallStatus)
	}
	for _, want := range []string{"1 API service(s) unavailable", "metrics-server not available", "insufficient RBAC permissions"} {
		if !strings.Contains(d.Summary, want) {
			t.Errorf("summary missing %q: %q", want, d.Summary)
		}
	}
}

func TestBuildClusterDiagnosticsSummary_NilSubchecks(t *testing.T) {
	d := &ClusterDiagnostics{}
	BuildClusterDiagnosticsSummary(d)
	if d.OverallStatus != "healthy" {
		t.Fatalf("OverallStatus = %q, want healthy when no checks ran", d.OverallStatus)
	}
}

func TestJoinWithComma(t *testing.T) {
	tests := []struct {
		parts []string
		want  string
	}{
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a, b"},
		{[]string{"a", "b", "c"}, "a, b, c"},
	}
	for _, tc := range tests {
		if got := joinWithComma(tc.parts); got != tc.want {
			t.Errorf("joinWithComma(%v) = %q, want %q", tc.parts, got, tc.want)
		}
	}
}
