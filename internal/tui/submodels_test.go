package tui

import (
	"errors"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestInteractiveSubmodelsOwnTransitions(t *testing.T) {
	fix := &fixState{suggestions: []hpaanalysis.Suggestion{{Title: "one"}, {Title: "two"}}}
	fix.move(1)
	if fix.selected != 1 {
		t.Fatalf("fix selected = %d", fix.selected)
	}
	wantErr := errors.New("failed")
	fix.updateDryRun(dryRunResultMsg{err: wantErr})
	if !errors.Is(fix.applyErr, wantErr) || fix.dryRunResult == "" {
		t.Fatalf("fix did not own dry-run result: %+v", fix)
	}

	hints := &hintsState{flows: make([]hpaanalysis.MetricHintTroubleshooting, 2)}
	hints.move(1)
	if hints.selected != 1 {
		t.Fatalf("hints selected = %d", hints.selected)
	}
}

func TestInteractiveStateCloneDelegatesToSubmodels(t *testing.T) {
	original := interactiveStates{fixState: &fixState{suggestions: []hpaanalysis.Suggestion{{Title: "before"}}}}
	cloned := original.clone()
	cloned.fixState.suggestions[0].Title = "after"
	if original.fixState.suggestions[0].Title != "before" {
		t.Fatal("submodel clone shared its suggestions slice")
	}
}
