package tui

import (
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// TestUpdate_ToggleSelectDoesNotMutatePreviousModel pins the value-receiver
// immutability contract: toggling a selection through Update changes the
// returned model only, never the selection map of the model it was called on.
func TestUpdate_ToggleSelectDoesNotMutatePreviousModel(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web", Health: "OK"},
	}
	m.selected = map[string]bool{}

	m2, _ := pressTUIKey(t, m, " ")

	if !m2.selected["default/web"] {
		t.Fatalf("expected updated model to carry the toggled selection, got %v", m2.selected)
	}
	if m.selected["default/web"] {
		t.Fatal("Update mutated the previous model's selected map")
	}
}

// TestClone_ReplicaHistoryIsIndependent verifies that appending to a cloned
// model's replica history cannot rewrite the older model's slices.
func TestClone_ReplicaHistoryIsIndependent(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "web", Health: "OK", Desired: 3},
	}
	m.replicaHistory = map[string][]float64{"default/web": {2}}

	m2 := m.clone()
	m2.updateReplicaHistory()

	if got := len(m2.replicaHistory["default/web"]); got != 2 {
		t.Fatalf("cloned history length = %d, want 2", got)
	}
	if got := len(m.replicaHistory["default/web"]); got != 1 {
		t.Fatalf("original history length changed to %d, want 1", got)
	}
	if m.replicaHistory["default/web"][0] != 2 {
		t.Fatalf("original history value changed: %v", m.replicaHistory["default/web"])
	}
}

// TestClone_ItemsAreIndependent verifies that sorting a cloned model's item
// slice does not reorder the original.
func TestClone_ItemsAreIndependent(t *testing.T) {
	m := NewModel(nil, "default", Options{})
	m.items = []hpaanalysis.ListItem{
		{Namespace: "default", Name: "bbb", Health: "OK"},
		{Namespace: "default", Name: "aaa", Health: "OK"},
	}

	m2 := m.clone()
	m2.sortField = "name"
	m2.sortItems()

	if m2.items[0].Name != "aaa" {
		t.Fatalf("cloned items not sorted: first = %q", m2.items[0].Name)
	}
	if m.items[0].Name != "bbb" {
		t.Fatalf("original item order changed: first = %q", m.items[0].Name)
	}
}
