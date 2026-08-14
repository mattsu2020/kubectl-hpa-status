package core

import "testing"

func TestDefaultLabels(t *testing.T) {
	labels := DefaultLabels{}
	if got := labels.Get("label_target"); got != "Target" {
		t.Fatalf("label_target = %q", got)
	}
	if got := labels.Get("unknown"); got != "unknown" {
		t.Fatalf("unknown key = %q", got)
	}
}
