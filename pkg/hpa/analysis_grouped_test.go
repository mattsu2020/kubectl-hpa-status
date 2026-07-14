package hpa

import "testing"

func TestGroupedAnalysisMapsFlatFields(t *testing.T) {
	a := Analysis{Namespace: "ns", Name: "hpa", Current: 2, Desired: 3, Summary: "scale up", Warnings: []string{"warning"}}
	grouped := a.Grouped()
	if grouped.Meta.Namespace != "ns" || grouped.Meta.Name != "hpa" {
		t.Fatalf("meta mapping failed: %#v", grouped.Meta)
	}
	if grouped.Replicas.Current != 2 || grouped.Replicas.Desired != 3 {
		t.Fatalf("replica mapping failed: %#v", grouped.Replicas)
	}
	if grouped.Decision.Summary != "scale up" || len(grouped.Actions.Warnings) != 1 {
		t.Fatalf("decision/actions mapping failed: %#v", grouped)
	}
}

func TestNilAnalysisGrouped(t *testing.T) {
	var analysis *Analysis
	if got := analysis.Grouped(); got.Meta != (MetaView{}) {
		t.Fatalf("nil Grouped() = %#v", got)
	}
}
