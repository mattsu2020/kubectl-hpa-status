package cmd

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// These tests cover the GitOps detection helpers in list.go that are NOT
// exercised by root_test.go / root_extra_test.go. The filter/sort helpers
// (matchesListFilter, matchesHealthScoreRange/Threshold, listItemLess,
// problemLess, sortListItems, absReplicaDiff) already have coverage there.

func TestGitOpsToolEvidence(t *testing.T) {
	tests := []struct {
		name              string
		annotations       map[string]string
		labels            map[string]string
		wantTool          string
		wantEvidenceCount int
	}{
		{
			name:              "argocd tracking id annotation",
			annotations:       map[string]string{"argocd.argoproj.io/tracking-id": "ns:Deployment/app"},
			wantTool:          "argocd",
			wantEvidenceCount: 1,
		},
		{
			name:              "argocd managed-by label",
			labels:            map[string]string{"app.kubernetes.io/managed-by": "argocd"},
			wantTool:          "argocd",
			wantEvidenceCount: 1,
		},
		{
			name:              "flux kustomize annotation",
			annotations:       map[string]string{"kustomize.toolkit.fluxcd.io/name": "app-stack"},
			wantTool:          "flux",
			wantEvidenceCount: 1,
		},
		{
			name: "flux both kustomize and helm",
			annotations: map[string]string{
				"kustomize.toolkit.fluxcd.io/name": "ks",
				"helm.toolkit.fluxcd.io/name":      "hr",
			},
			wantTool:          "flux",
			wantEvidenceCount: 2,
		},
		{
			name:              "no gitops signals",
			annotations:       map[string]string{"foo": "bar"},
			labels:            map[string]string{"baz": "qux"},
			wantTool:          "",
			wantEvidenceCount: 0,
		},
		{
			name:              "empty maps",
			wantTool:          "",
			wantEvidenceCount: 0,
		},
		{
			name:              "argocd takes precedence over flux",
			annotations:       map[string]string{"kustomize.toolkit.fluxcd.io/name": "ks", "argocd.argoproj.io/tracking-id": "id"},
			wantTool:          "argocd",
			wantEvidenceCount: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tool, evidence := gitOpsToolEvidence(tc.annotations, tc.labels)
			if tool != tc.wantTool {
				t.Fatalf("gitOpsToolEvidence tool = %q, want %q", tool, tc.wantTool)
			}
			if len(evidence) != tc.wantEvidenceCount {
				t.Fatalf("gitOpsToolEvidence evidence count = %d, want %d (evidence=%v)", len(evidence), tc.wantEvidenceCount, evidence)
			}
		})
	}
}

func TestBuildGitOpsDriftSignals(t *testing.T) {
	hpas := []autoscalingv2.HorizontalPodAutoscaler{
		{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   "default",
				Name:        "argocd-hpa",
				Annotations: map[string]string{"argocd.argoproj.io/tracking-id": "id"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   "prod",
				Name:        "flux-hpa",
				Annotations: map[string]string{"kustomize.toolkit.fluxcd.io/name": "ks"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "dev",
				Name:      "plain-hpa",
			},
		},
	}
	signals := buildGitOpsDriftSignals(hpas)
	if len(signals) != 2 {
		t.Fatalf("expected 2 drift signals, got %d", len(signals))
	}
	if signals[0].Tool != "argocd" || signals[0].Namespace != "default" || signals[0].Name != "argocd-hpa" {
		t.Fatalf("signal[0] = %+v, want argocd/default/argocd-hpa", signals[0])
	}
	if signals[1].Tool != "flux" || signals[1].Namespace != "prod" || signals[1].Name != "flux-hpa" {
		t.Fatalf("signal[1] = %+v, want flux/prod/flux-hpa", signals[1])
	}
	if signals[0].Advice == "" {
		t.Fatalf("expected non-empty advice on signal[0]")
	}

	// Empty input is safe.
	if got := buildGitOpsDriftSignals(nil); len(got) != 0 {
		t.Fatalf("expected no signals for nil input, got %d", len(got))
	}
}
