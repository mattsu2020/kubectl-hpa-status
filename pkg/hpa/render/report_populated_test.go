package render

import (
	"bytes"
	"strings"
	"testing"

	hpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// populatedAnalysis exercises every optional Analysis section renderers.go
// touches: pod analysis, simulation, metric freshness, capacity context, and
// the structured decision trace. It intentionally sets at least one field in
// each nested slice so both the Markdown and HTML section writers walk their
// row/table loops instead of only their "no data" early returns.
func populatedAnalysis() hpa.Analysis {
	return hpa.Analysis{
		Namespace: "default",
		Name:      "web",
		Health:    "OK",
		PodAnalysis: &hpa.PodAnalysis{
			Total: 5, Ready: 4, Unready: 1, Pending: 1, Terminating: 0,
			ResourceIssues:  []hpa.PodResourceIssue{{Pod: "web-1", Container: "app", Resource: "cpu", Category: "missing-request"}},
			ContainerChecks: []hpa.ContainerCheck{{Container: "app", Found: true}, {Container: "sidecar", Found: false, Message: "not found in pod spec"}},
		},
		FlappingSimulation: &hpa.SimulationResult{
			Parameter:      "maxReplicas",
			OriginalValue:  "10",
			SimulatedValue: "20",
			Before:         hpa.SimulationState{DesiredReplicas: 8, Health: "OK", HealthScore: 90},
			After:          hpa.SimulationState{DesiredReplicas: 15, Health: "OK", HealthScore: 85},
			RiskAssessment: "Raising maxReplicas increases capacity; verify node headroom.",
			Interpretation: []string{"desiredReplicas would increase from 8 to 15"},
		},
		MetricFreshnessEntries: []hpa.MetricFreshness{{
			Name: "cpu", Type: "Resource", Status: "OK", Source: "metrics.k8s.io", Window: "30s",
			Risk: "none", Evidence: []string{"last seen 5s ago"}, NextSteps: []string{"kubectl top pods"},
		}},
		CapacityContext: &hpa.CapacityContext{
			PendingPods: []hpa.PendingPodInfo{{Name: "web-1", Unschedulable: true, Reasons: []string{"Insufficient cpu"}}},
			NodeHints:   []string{"consider adding a node pool"},
		},
		StructuredDecisionTrace: &hpa.StructuredDecisionTrace{
			SchemaVersion:          "v1",
			Namespace:              "default",
			Name:                   "web",
			CurrentReplicas:        8,
			VisibleDesiredReplicas: 8,
		},
	}
}

func TestWriteMarkdownReport_PopulatedSections(t *testing.T) {
	var buf bytes.Buffer
	report := hpa.StatusReport{Analysis: populatedAnalysis()}
	if err := WriteMarkdownReport(&buf, report); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"## Pod Analysis", "Missing Resources", "Container Checks",
		"## Simulation", "**Parameter:** maxReplicas",
		"## Metrics Freshness", "cpu (Resource)",
		"## Capacity Context", "Pending Pods", "consider adding a node pool",
		"## Structured Decision Trace",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected markdown output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestWriteHTMLReport_PopulatedSections(t *testing.T) {
	var buf bytes.Buffer
	report := hpa.StatusReport{Analysis: populatedAnalysis()}
	if err := WriteHTMLReport(&buf, report); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"Pod Analysis", "Simulation", "Metrics Freshness", "Capacity Context", "Structured Decision Trace",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected HTML output to contain %q, got:\n%s", want, out)
		}
	}
}
