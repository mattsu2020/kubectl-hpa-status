package tui

import (
	"fmt"
	"slices"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/audit"
)

// The six interactive states below are deliberately small submodels. They own
// their mutable containers and message/cursor transitions; Model only selects
// the active controller and supplies shared application dependencies.

func (s *simState) clone() *simState {
	out := *s
	out.fields = slices.Clone(s.fields)
	if s.hpa != nil {
		out.hpa = s.hpa.DeepCopy()
	}
	return &out
}

func (s *simState) update(msg simResultMsg) { s.result, s.err = msg.result, msg.err }

func (s *fixState) clone() *fixState {
	out := *s
	out.suggestions = slices.Clone(s.suggestions)
	return &out
}

func (s *fixState) move(delta int) {
	s.selected = clampCursor(s.selected+delta, len(s.suggestions)-1)
	s.applyConfirm = false
}

func (s *fixState) updateApply(msg applyResultMsg) {
	s.applyConfirm = false
	s.applied = true
	s.applyErr = msg.err
}

func (s *fixState) updateDryRun(msg dryRunResultMsg) {
	s.applyConfirm = false
	s.applied = false
	s.applyErr = msg.err
	if msg.err != nil {
		s.dryRunResult = fmt.Sprintf("validation failed: %v", msg.err)
		return
	}
	s.dryRunResult = fmt.Sprintf("server-side validation passed: %s", msg.title)
}

func (s *replayState) clone() *replayState { out := *s; return &out }

func (s *replayState) move(delta int) {
	if s.trace != nil {
		s.scrollPos = clampCursor(s.scrollPos+delta, maxInt(0, len(s.trace.Snapshots)-1))
	}
}

func (s *replayState) update(msg replayLoadedMsg) {
	s.loading = false
	s.trace = msg.trace
	s.err = msg.err
}

func (s *batchAuditState) clone() *batchAuditState {
	out := *s
	out.results = slices.Clone(s.results)
	out.reports = make(map[string]*audit.Report, len(s.reports))
	for key, report := range s.reports {
		out.reports[key] = report
	}
	return &out
}

func (s *batchAuditState) update(msg batchAuditMsg) {
	s.loading = false
	if msg.err != nil {
		s.err = msg.err
		return
	}
	s.reports = msg.reports
	s.results = buildBatchAuditEntries(msg.reports)
}

func (s *historyState) clone() *historyState {
	out := *s
	out.snapshots = slices.Clone(s.snapshots)
	return &out
}

func (s *historyState) move(delta int) {
	s.scrollPos = clampCursor(s.scrollPos+delta, maxInt(0, len(s.snapshots)-1))
}

func (s *hintsState) clone() *hintsState {
	out := *s
	out.flows = slices.Clone(s.flows)
	return &out
}

func (s *hintsState) move(delta int) {
	s.selected = clampCursor(s.selected+delta, len(s.flows)-1)
}
