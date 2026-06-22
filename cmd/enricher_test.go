package cmd

import (
	"context"
	"errors"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// fakeEnricher is a test double for the Enricher interface.
type fakeEnricher struct {
	name        string
	enabled     bool
	abortOnErr  bool
	runErr      error
	ran         bool
	recordValue string // if non-empty, appended to report.Analysis.Warnings by Run
}

func (f *fakeEnricher) Name() string       { return f.name }
func (f *fakeEnricher) Enabled() bool      { return f.enabled }
func (f *fakeEnricher) AbortOnError() bool { return f.abortOnErr }
func (f *fakeEnricher) Run(_ context.Context, _ *PipelineContext, _ *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) error {
	f.ran = true
	if f.recordValue != "" {
		report.Analysis.Warnings = append(report.Analysis.Warnings, f.recordValue)
	}
	return f.runErr
}

func testHPA() *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web"},
	}
}

// runEnrichers skips disabled enrichers.
func TestRunEnrichers_SkipsDisabled(t *testing.T) {
	disabled := &fakeEnricher{name: "off", enabled: false}
	enabled := &fakeEnricher{name: "on", enabled: true, recordValue: "ran"}
	report := &hpaanalysis.StatusReport{}
	err := runEnrichers(context.Background(), []Enricher{disabled, enabled}, &PipelineContext{}, testHPA(), report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if disabled.ran {
		t.Error("disabled enricher should not run")
	}
	if !enabled.ran {
		t.Error("enabled enricher should run")
	}
	if len(report.Analysis.Warnings) != 1 || report.Analysis.Warnings[0] != "ran" {
		t.Errorf("expected 1 warning from enabled enricher, got %v", report.Analysis.Warnings)
	}
}

// runEnrichers records a warning for a non-aborting enricher that errors.
func TestRunEnrichers_ErrorRecordedAsWarningWhenNotAborting(t *testing.T) {
	failing := &fakeEnricher{
		name: "best-effort", enabled: true, abortOnErr: false,
		runErr: errors.New("boom"),
	}
	after := &fakeEnricher{name: "after", enabled: true, recordValue: "after-ran"}
	report := &hpaanalysis.StatusReport{}
	err := runEnrichers(context.Background(), []Enricher{failing, after}, &PipelineContext{}, testHPA(), report)
	if err != nil {
		t.Fatalf("non-aborting error should not propagate: %v", err)
	}
	if !after.ran {
		t.Error("pipeline should continue past a best-effort error")
	}
	// Warning from runner + record from after enricher.
	if len(report.Analysis.Warnings) != 2 {
		t.Fatalf("expected 2 warnings (error + after), got %v", report.Analysis.Warnings)
	}
	if report.Analysis.Warnings[0] != `enrichment "best-effort" failed: boom` {
		t.Errorf("unexpected warning text: %q", report.Analysis.Warnings[0])
	}
}

// runEnrichers aborts the pipeline when an AbortOnError enricher fails.
func TestRunEnrichers_AbortsOnAbortOnError(t *testing.T) {
	failing := &fakeEnricher{
		name: "critical", enabled: true, abortOnErr: true,
		runErr: errors.New("fatal"),
	}
	after := &fakeEnricher{name: "after", enabled: true, recordValue: "after-ran"}
	report := &hpaanalysis.StatusReport{}
	err := runEnrichers(context.Background(), []Enricher{failing, after}, &PipelineContext{}, testHPA(), report)
	if err == nil || err.Error() != "fatal" {
		t.Fatalf("expected fatal error to propagate, got %v", err)
	}
	if after.ran {
		t.Error("pipeline should not continue past an aborting error")
	}
	// The runner still records the warning before propagating.
	if len(report.Analysis.Warnings) != 1 {
		t.Errorf("expected 1 warning recorded before abort, got %v", report.Analysis.Warnings)
	}
}

// The simulations adapter is the one that must abort on error (regression
// guard for the historical short-circuit behavior).
func TestSimulationsEnricher_AbortsOnError(t *testing.T) {
	opts := defaultRootOptions()
	e := newSimulationsEnricher(&opts)
	if !e.AbortOnError() {
		t.Error("simulationsEnricher must return AbortOnError()=true to preserve historical behavior")
	}
}

// All non-simulations adapters default to AbortOnError()=false via defaultAbort.
func TestOtherAdapters_DoNotAbortOnError(t *testing.T) {
	opts := defaultRootOptions()
	// Enable each adapter's Enabled() path minimally so we exercise the
	// concrete type's AbortOnError (it does not depend on options).
	nonAborting := []Enricher{
		newDecisionTracesEnricher(&opts),
		newEventsEnricher(&opts),
		newReportEnricher(&opts),
		newMetricsDiagnosticsEnricher(&opts),
		newMetricFreshnessEnricher(&opts),
		newResourceCheckEnricher(&opts),
		newTargetReplicaObservationsEnricher(&opts),
		newPodAnalysisEnricher(&opts),
		newCapacityAnalysisEnricher(&opts),
		newRolloutAndBlockersEnricher(&opts),
		newControllerProfileEnricher(&opts),
		newCapacityPlanEnricher(&opts),
		newGitOpsConflictEnricher(&opts),
		newMetricContractAndAdapterEnricher(&opts),
		newChurnAndFlappingEnricher(&opts),
		newVPAAdvisoryEnricher(&opts),
		newMetricHintsEnricher(&opts),
		newAdvisorsEnricher(&opts),
	}
	for _, e := range nonAborting {
		if e.AbortOnError() {
			t.Errorf("%s: expected AbortOnError()=false", e.Name())
		}
	}
}
