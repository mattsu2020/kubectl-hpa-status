package cmd

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
)

func TestPrintBatchSummary(t *testing.T) {
	t.Run("counts distinct HPAs across multiple patches", func(t *testing.T) {
		entries := []batchEntry{
			{Namespace: "default", Name: "web", Suggestions: []hpaanalysis.Suggestion{
				{Title: "raise-max", Risk: "low"},
				{Title: "add-behavior", Risk: "low"},
			}},
			{Namespace: "prod", Name: "api", Suggestions: []hpaanalysis.Suggestion{
				{Title: "raise-max", Risk: "medium"},
			}},
		}
		var buf bytes.Buffer
		if err := printBatchSummary(&buf, entries); err != nil {
			t.Fatalf("printBatchSummary: %v", err)
		}
		out := buf.String()
		// 3 patches across 2 HPAs (web appears twice).
		if !strings.Contains(out, "3 patches across 2 HPA(s)") {
			t.Fatalf("expected aggregate counts, got:\n%s", out)
		}
		// Header row present.
		if !strings.Contains(out, "NAMESPACE/NAME") {
			t.Fatalf("expected header row, got:\n%s", out)
		}
		// Each entry's risk line present.
		if !strings.Contains(out, "raise-max") || !strings.Contains(out, "add-behavior") {
			t.Fatalf("expected entry titles in output, got:\n%s", out)
		}
	})

	t.Run("empty entries still prints zero summary", func(t *testing.T) {
		var buf bytes.Buffer
		if err := printBatchSummary(&buf, nil); err != nil {
			t.Fatalf("printBatchSummary: %v", err)
		}
		if !strings.Contains(buf.String(), "0 patches across 0 HPA(s)") {
			t.Fatalf("expected zero counts, got:\n%s", buf.String())
		}
	})
}

func TestCollectBatchEntries_FiltersBySelectedAndApplyPatch(t *testing.T) {
	// Build an HPA that yields an applyable suggestion. testutil.BuildHPA with
	// ScalingLimited + a low max produces a "raise maxReplicas" suggestion.
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithResourceMetric("cpu", 70, 65),
		testutil.WithMinMax(2, 4),
	)
	*hpa.Spec.MinReplicas = 2
	// Force the capped-at-max condition so an applyable suggestion surfaces.
	setStatusCurrentReplicas(hpa, 4)
	setStatusDesiredReplicas(hpa, 4)
	setScalingLimited(hpa)

	opts := &options{}
	hpas := []autoscalingv2.HorizontalPodAutoscaler{*hpa}

	t.Run("selected HPA with applyable suggestion produces entries", func(t *testing.T) {
		selected := map[string]bool{"default/web": true}
		entries := collectBatchEntries(opts, hpas, selected)
		if len(entries) == 0 {
			t.Fatal("fixture contract broken: expected an applyable suggestion")
		}
		// The selected HPA must produce one grouped entry.
		if len(entries) != 1 {
			t.Fatalf("expected one HPA-level entry, got %d", len(entries))
		}
		for _, e := range entries {
			if e.Namespace != "default" || e.Name != "web" {
				t.Fatalf("entry referenced unexpected HPA: %s/%s", e.Namespace, e.Name)
			}
			for _, suggestion := range e.Suggestions {
				if !suggestion.Apply || suggestion.Patch == "" {
					t.Fatalf("entry suggestion must be applyable with a patch: %+v", suggestion)
				}
			}
		}
	})

	t.Run("unselected HPA yields no entries", func(t *testing.T) {
		selected := map[string]bool{"default/other": true}
		entries := collectBatchEntries(opts, hpas, selected)
		if len(entries) != 0 {
			t.Fatalf("expected no entries for unselected HPA, got %d", len(entries))
		}
	})

	t.Run("empty selection yields no entries", func(t *testing.T) {
		entries := collectBatchEntries(opts, hpas, map[string]bool{})
		if len(entries) != 0 {
			t.Fatalf("expected no entries for empty selection, got %d", len(entries))
		}
	})
}

// setStatusCurrentReplicas mutates an HPA fixture's status.currentReplicas.
func setStatusCurrentReplicas(hpa *autoscalingv2.HorizontalPodAutoscaler, n int32) {
	hpa.Status.CurrentReplicas = n
}

func setStatusDesiredReplicas(hpa *autoscalingv2.HorizontalPodAutoscaler, n int32) {
	hpa.Status.DesiredReplicas = n
}

// setScalingLimited flips the ScalingLimited condition true so the analysis
// surfaces a "raise maxReplicas"-style applyable suggestion.
func setScalingLimited(hpa *autoscalingv2.HorizontalPodAutoscaler) {
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{
			Type:   autoscalingv2.ScalingLimited,
			Status: corev1.ConditionTrue,
		},
	}
}

func TestExecuteBatchPatchesGroupsSuggestionsPerHPA(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web")
	fakeClient := testutil.NewFakeClient(hpa)
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			DryRun:         false,
			Yes:            true,
		},
	}
	entries := []batchEntry{{
		Namespace: "default",
		Name:      "web",
		Suggestions: []hpaanalysis.Suggestion{
			{Title: "raise-min", Apply: true, Patch: `{"spec":{"minReplicas":2}}`},
			{Title: "raise-max", Apply: true, Patch: `{"spec":{"maxReplicas":20}}`},
		},
	}}

	var out bytes.Buffer
	if err := executeBatchPatches(context.Background(), &out, opts, entries); err != nil {
		t.Fatalf("executeBatchPatches: %v\n%s", err, out.String())
	}

	livePatches := 0
	for _, action := range fakeClient.Actions() {
		patchAction, ok := action.(k8stesting.PatchActionImpl)
		if !ok || patchAction.GetName() != "web" {
			continue
		}
		if len(patchAction.GetPatchOptions().DryRun) == 0 {
			livePatches++
			patch := string(patchAction.GetPatch())
			if !strings.Contains(patch, "minReplicas") || !strings.Contains(patch, "maxReplicas") {
				t.Fatalf("live patch was not merged: %s", patch)
			}
		}
	}
	if livePatches != 1 {
		t.Fatalf("live patch calls = %d, want one atomic HPA patch; actions=%+v", livePatches, fakeClient.Actions())
	}
}

func TestExecuteBatchPatchesReturnsAggregateFailure(t *testing.T) {
	good := testutil.BuildHPA("default", "good")
	bad := testutil.BuildHPA("default", "bad")
	fakeClient := testutil.NewFakeClient(good, bad)
	fakeClient.PrependReactor("patch", "horizontalpodautoscalers", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.(k8stesting.PatchAction).GetName() == "bad" {
			return true, nil, fmt.Errorf("injected patch failure")
		}
		return false, nil, nil
	})

	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			DryRun:         true,
			Yes:            true,
		},
	}
	entries := []batchEntry{
		{
			Namespace: "default",
			Name:      "good",
			Suggestions: []hpaanalysis.Suggestion{
				{Title: "raise-max", Apply: true, Patch: `{"spec":{"maxReplicas":20}}`},
			},
		},
		{
			Namespace: "default",
			Name:      "bad",
			Suggestions: []hpaanalysis.Suggestion{
				{Title: "raise-max", Apply: true, Patch: `{"spec":{"maxReplicas":20}}`},
			},
		},
	}

	var out bytes.Buffer
	err := executeBatchPatches(context.Background(), &out, opts, entries)
	if err == nil || !strings.Contains(err.Error(), "1 of 2 HPA") {
		t.Fatalf("aggregate error = %v, want one failed target", err)
	}
	if !strings.Contains(out.String(), "1 succeeded, 1 failed") {
		t.Fatalf("missing aggregate summary:\n%s", out.String())
	}
}
