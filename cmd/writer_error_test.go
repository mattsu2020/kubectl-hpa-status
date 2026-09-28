package cmd

import (
	"errors"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	hpaflapping "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/flapping"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/fleet"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

type alwaysFailWriter struct{}

func (alwaysFailWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestTextRenderersPropagateWriterErrors(t *testing.T) {
	nonEmptyFlap := flapReport{
		Namespace:      "default",
		Name:           "web",
		Source:         "events since 1h",
		ScaleEvents:    4,
		DirectionFlips: 2,
		Level:          "MEDIUM",
		Diagnosis: &hpaflapping.Diagnosis{
			Detected:        true,
			Severity:        "HIGH",
			FlipCount:       2,
			WindowSeconds:   300,
			EstimatedCauses: []hpaflapping.Cause{{Type: "tolerance", Description: "d", Confidence: "medium"}},
			Recommendations: []hpaflapping.Fix{{Action: "a", Rationale: "r", Patch: "{}"}},
		},
		Prevention: &hpaflapping.PreventionReport{
			CurrentWindow:         300,
			CurrentDirectionFlips: 2,
			ObservationWindow:     "1h",
			Recommendations:       []hpaflapping.Simulation{{WindowSeconds: 600, Confidence: "medium"}},
			Summary:               "s",
		},
	}
	tests := map[string]func() error{
		"simulate": func() error { return writeSimulateText(alwaysFailWriter{}, simulateReport{}, style.Theme{}) },
		"behavior": func() error { return writeBehaviorText(alwaysFailWriter{}, behaviorOutput{}) },
		"summary-markdown": func() error {
			return writeClusterSummaryMarkdown(alwaysFailWriter{}, hpaanalysis.ListReport{})
		},
		"summary-html": func() error { return writeClusterSummaryHTML(alwaysFailWriter{}, hpaanalysis.ListReport{}) },
		"flap":         func() error { return writeFlapReport(alwaysFailWriter{}, &options{}, nonEmptyFlap) },
		"fleet": func() error {
			return writeFleetReport(alwaysFailWriter{}, &options{}, fleet.Report{TopRisks: []fleet.RiskItem{{Namespace: "default", Name: "web"}}})
		},
		"conflicts": func() error {
			return writeConflictScanReport(alwaysFailWriter{}, &options{}, conflictScanReport{
				Items:    []conflictItem{{Namespace: "default", Target: "Deployment/web", HPAs: []string{"a"}, Risks: []string{"r"}, Evidence: []string{"e"}}},
				Warnings: map[string][]string{"default": {"w"}},
			})
		},
		"conflicts-empty": func() error {
			return writeConflictScanReport(alwaysFailWriter{}, &options{}, conflictScanReport{})
		},
		"ownership": func() error {
			return writeOwnershipReport(alwaysFailWriter{}, &options{}, []ownershipReport{{Namespace: "default", Name: "web", Managers: []ownershipManager{{Manager: "m", Operation: "Apply", Field: "f"}}, Risks: []string{"r"}, Recommendations: []string{"c"}}})
		},
		"analyze-record": func() error {
			return writeRecordAnalysis(alwaysFailWriter{}, &options{}, recordAnalysis{
				Items: []hpaflapping.TraceReport{{Namespace: "default", Name: "web", DesiredChanges: 3, Snapshots: 10, DirectionFlips: 2, Level: "HIGH", Suggestions: []string{"s"}}},
			})
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			if err := run(); err == nil {
				t.Fatal("expected writer error")
			}
		})
	}
}
