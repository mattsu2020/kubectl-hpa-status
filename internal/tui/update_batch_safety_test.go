package tui

import (
	"context"
	"reflect"
	"testing"

	hpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestBatchApplyPreservesSuggestionSource(t *testing.T) {
	source := hpa.Suggestion{Title: "raise maximum", Patch: `{"spec":{"maxReplicas":12}}`, Apply: true, SourceUID: "uid-original", SourceResourceVersion: "10"}
	reports := map[string]*hpa.StatusReport{"default/web": {Analysis: hpa.Analysis{Meta: hpa.MetaView{Namespace: "default", Name: "web"}, Actions: hpa.ActionsView{Suggestions: []hpa.Suggestion{source}}}}}
	calls := 0
	entries := collectBatchApplyPatches([]string{"default/web"}, reports)
	executeBatchApply(context.Background(), func(_ context.Context, _, _ string, s []hpa.Suggestion) error {
		calls++
		if len(s) != 1 || !reflect.DeepEqual(s[0], source) {
			t.Errorf("batch apply changed suggestion: %+v", s)
		}
		return nil
	}, entries)
	if calls != 1 {
		t.Fatalf("apply called %d times, want 1", calls)
	}
}
