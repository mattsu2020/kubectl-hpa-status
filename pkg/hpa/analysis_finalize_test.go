package hpa

import (
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/churn"
)

func TestFinalizeAnalysisIsIdempotent(t *testing.T) {
	remaining := int64(30)
	a := *NewAnalysis(FlatAnalysis{
		StabilizationRemaining: &remaining,
		ChurnAnalysis:          &churn.ChurnAnalysis{Level: churn.ChurnHigh},
		Assumptions: []Assumption{{
			Name:       "custom",
			Value:      "kept",
			Source:     "test",
			Confidence: "high",
		}},
	})

	once := FinalizeAnalysis(a)
	twice := FinalizeAnalysis(once)

	if len(twice.Assumptions()) != len(once.Assumptions()) {
		t.Fatalf("assumptions duplicated: once=%d twice=%d", len(once.Assumptions()), len(twice.Assumptions()))
	}
	if len(twice.Interpretation()) != len(once.Interpretation()) {
		t.Fatalf("interpretation duplicated: once=%d twice=%d", len(once.Interpretation()), len(twice.Interpretation()))
	}
	if twice.Assumptions()[0].Name != "custom" {
		t.Fatalf("custom assumption was not preserved: %#v", twice.Assumptions())
	}
}
