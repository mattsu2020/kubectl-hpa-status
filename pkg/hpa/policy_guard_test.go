package hpa

import (
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

func TestGuardFixBlocksCriticalViolation(t *testing.T) {
	hpa := kube.BuildHPA("default", "web", kube.WithMinMax(2, 10))
	policy := PolicyFile{Rules: []PolicyRule{{
		ID:       "replica-range",
		Name:     "Replica range",
		Severity: "critical",
		Parameters: PolicyParams{
			"maxRatio": 10,
		},
	}}}
	suggestions := []Suggestion{{
		Title: "raise max too far",
		Patch: `{"spec":{"maxReplicas":50}}`,
		Apply: true,
	}}

	got := GuardFix(suggestions, policy, hpa)
	if len(got.Blocked) != 1 {
		t.Fatalf("expected one blocked suggestion, got %+v", got)
	}
	if got.Blocked[0].PolicyRule != "replica-range" {
		t.Fatalf("unexpected policy rule: %s", got.Blocked[0].PolicyRule)
	}
	if len(got.Allowed) != 0 {
		t.Fatalf("expected no allowed suggestions, got %+v", got.Allowed)
	}
}

func TestGuardFixAllowsWarningViolation(t *testing.T) {
	hpa := kube.BuildHPA("default", "web", kube.WithMinMax(2, 10))
	policy := PolicyFile{Rules: []PolicyRule{{
		ID:       "replica-range",
		Name:     "Replica range",
		Severity: "warning",
		Parameters: PolicyParams{
			"maxRatio": 10,
		},
	}}}
	suggestions := []Suggestion{{
		Title: "raise max with warning",
		Patch: `{"spec":{"maxReplicas":30}}`,
		Apply: true,
	}}

	got := GuardFix(suggestions, policy, hpa)
	if len(got.Blocked) != 0 {
		t.Fatalf("expected no blocked suggestions, got %+v", got.Blocked)
	}
	if len(got.Warnings) != 1 {
		t.Fatalf("expected one warning, got %+v", got)
	}
	if len(got.Allowed) != 1 {
		t.Fatalf("expected one allowed suggestion, got %+v", got.Allowed)
	}
}

func TestGuardFixAllowsCompliantPatch(t *testing.T) {
	hpa := kube.BuildHPA("default", "web", kube.WithMinMax(2, 10))
	policy := PolicyFile{Rules: []PolicyRule{{
		ID:       "replica-range",
		Name:     "Replica range",
		Severity: "critical",
		Parameters: PolicyParams{
			"maxRatio": 10,
		},
	}}}
	suggestions := []Suggestion{{
		Title: "raise max safely",
		Patch: `{"spec":{"maxReplicas":15}}`,
		Apply: true,
	}}

	got := GuardFix(suggestions, policy, hpa)
	if len(got.Blocked) != 0 || len(got.Warnings) != 0 {
		t.Fatalf("expected clean guard result, got %+v", got)
	}
	if len(got.Allowed) != 1 {
		t.Fatalf("expected one allowed suggestion, got %+v", got.Allowed)
	}
}
