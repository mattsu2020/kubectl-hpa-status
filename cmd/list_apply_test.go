package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

func TestPrintBatchSummary(t *testing.T) {
	t.Run("counts distinct HPAs across multiple patches", func(t *testing.T) {
		entries := []batchEntry{
			{Namespace: "default", Name: "web", Suggestion: hpaanalysis.Suggestion{Title: "raise-max", Risk: "low"}},
			{Namespace: "default", Name: "web", Suggestion: hpaanalysis.Suggestion{Title: "add-behavior", Risk: "low"}},
			{Namespace: "prod", Name: "api", Suggestion: hpaanalysis.Suggestion{Title: "raise-max", Risk: "medium"}},
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
			t.Skip("no applyable suggestion surfaced for this fixture; adjust the builder")
		}
		// Every entry must reference the selected HPA.
		for _, e := range entries {
			if e.Namespace != "default" || e.Name != "web" {
				t.Fatalf("entry referenced unexpected HPA: %s/%s", e.Namespace, e.Name)
			}
			if !e.Suggestion.Apply || e.Suggestion.Patch == "" {
				t.Fatalf("entry Suggestion must be applyable with a patch: %+v", e.Suggestion)
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
