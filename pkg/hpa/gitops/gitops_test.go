package gitops

import (
	"strings"
	"testing"
)

func TestAnalyzeConflict_NoConflicts(t *testing.T) {
	result := AnalyzeConflict(Input{
		Namespace:       "default",
		HPAName:         "web",
		TargetKind:      "Deployment",
		TargetName:      "web",
		DesiredReplicas: 5,
		LiveReplicas:    5,
	})

	if len(result.Conflicts) != 0 {
		t.Errorf("expected no conflicts, got %+v", result.Conflicts)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings, got %+v", result.Warnings)
	}
	if result.Summary != "No GitOps conflicts detected" {
		t.Errorf("unexpected summary: %q", result.Summary)
	}
	if result.Target != "Deployment/web" {
		t.Errorf("unexpected target: %q", result.Target)
	}
}

func TestAnalyzeConflict_ManifestReplicasMismatch(t *testing.T) {
	result := AnalyzeConflict(Input{
		Namespace:        "default",
		HPAName:          "web",
		TargetKind:       "Deployment",
		TargetName:       "web",
		DesiredReplicas:  8,
		LiveReplicas:     8,
		ManifestReplicas: int32Ptr(3),
	})

	if len(result.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d: %+v", len(result.Conflicts), result.Conflicts)
	}
	c := result.Conflicts[0]
	if c.Field != "spec.replicas" {
		t.Errorf("expected field spec.replicas, got %q", c.Field)
	}
	if c.Severity != "conflict" {
		t.Errorf("expected severity conflict, got %q", c.Severity)
	}
	if c.ManifestValue != "3" || c.HPADesired != "8" {
		t.Errorf("expected manifest=3 hpaDesired=8, got manifest=%q hpaDesired=%q", c.ManifestValue, c.HPADesired)
	}
	if len(result.Patches) != 1 {
		t.Errorf("expected 1 suggested patch, got %d", len(result.Patches))
	}
	if !strings.Contains(result.Summary, "1 conflict") {
		t.Errorf("expected summary to mention 1 conflict, got %q", result.Summary)
	}
}

func TestAnalyzeConflict_ManifestReplicasMatch(t *testing.T) {
	result := AnalyzeConflict(Input{
		Namespace:        "default",
		HPAName:          "web",
		TargetKind:       "Deployment",
		TargetName:       "web",
		DesiredReplicas:  5,
		ManifestReplicas: int32Ptr(5),
	})

	if len(result.Conflicts) != 0 {
		t.Errorf("expected no conflict when manifest matches desired, got %+v", result.Conflicts)
	}
}

func TestAnalyzeConflict_ArgoCDManaged(t *testing.T) {
	result := AnalyzeConflict(Input{
		TargetKind:        "Deployment",
		TargetName:        "web",
		ArgoCDAnnotations: map[string]string{"argocd.argoproj.io/instance": "web-app"},
	})

	if len(result.Conflicts) != 1 || result.Conflicts[0].Severity != "info" {
		t.Fatalf("expected 1 info-severity conflict, got %+v", result.Conflicts)
	}
	if !strings.Contains(result.Conflicts[0].Detail, "Argo CD managed") {
		t.Errorf("expected Argo CD detail, got %q", result.Conflicts[0].Detail)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "Argo CD") {
		t.Errorf("expected Argo CD warning, got %+v", result.Warnings)
	}
}

func TestAnalyzeConflict_FluxManaged(t *testing.T) {
	result := AnalyzeConflict(Input{
		TargetKind:      "Deployment",
		TargetName:      "web",
		FluxAnnotations: map[string]string{"kustomize.toolkit.fluxcd.io/name": "web-app"},
	})

	if len(result.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %+v", result.Conflicts)
	}
	if !strings.Contains(result.Conflicts[0].Detail, "Flux managed") {
		t.Errorf("expected Flux detail, got %q", result.Conflicts[0].Detail)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "Flux") {
		t.Errorf("expected Flux warning, got %+v", result.Warnings)
	}
}

func TestAnalyzeConflict_KEDAManaged(t *testing.T) {
	result := AnalyzeConflict(Input{
		TargetKind:  "Deployment",
		TargetName:  "web",
		KEDAManaged: true,
	})

	if len(result.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %+v", result.Conflicts)
	}
	if !strings.Contains(result.Conflicts[0].Detail, "KEDA managed") {
		t.Errorf("expected KEDA detail, got %q", result.Conflicts[0].Detail)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "KEDA") {
		t.Errorf("expected KEDA warning, got %+v", result.Warnings)
	}
}

func TestAnalyzeConflict_MultipleFindingsCombineIntoSummary(t *testing.T) {
	result := AnalyzeConflict(Input{
		TargetKind:        "Deployment",
		TargetName:        "web",
		DesiredReplicas:   8,
		ManifestReplicas:  int32Ptr(3),
		ArgoCDAnnotations: map[string]string{"argocd.argoproj.io/instance": "web-app"},
	})

	if len(result.Conflicts) != 2 {
		t.Fatalf("expected 2 conflicts (manifest + argocd), got %d: %+v", len(result.Conflicts), result.Conflicts)
	}
	if !strings.Contains(result.Summary, "2 conflict") || !strings.Contains(result.Summary, "1 warning") {
		t.Errorf("expected summary to report 2 conflicts and 1 warning, got %q", result.Summary)
	}
}

func TestAnnotationKeys(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]string
		want func(string) bool
	}{
		{
			name: "single key",
			in:   map[string]string{"a": "1"},
			want: func(s string) bool { return s == "a" },
		},
		{
			name: "two keys",
			in:   map[string]string{"a": "1", "b": "2"},
			want: func(s string) bool { return strings.Contains(s, "a") && strings.Contains(s, "b") },
		},
		{
			name: "many keys truncates with ellipsis",
			in:   map[string]string{"a": "1", "b": "2", "c": "3", "d": "4"},
			want: func(s string) bool { return strings.HasSuffix(s, ", ...") },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := annotationKeys(tt.in)
			if !tt.want(got) {
				t.Errorf("annotationKeys(%v) = %q, did not match expectation", tt.in, got)
			}
		})
	}
}
