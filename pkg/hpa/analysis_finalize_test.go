package hpa

import (
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/churn"
)

func TestFinalizeAnalysisIsIdempotent(t *testing.T) {
	remaining := int64(30)
	a := Analysis{
		Conditions: ConditionsView{StabilizationRemaining: &remaining},
		Stability:  StabilityView{ChurnAnalysis: &churn.ChurnAnalysis{Level: churn.ChurnHigh}},
		Actions: ActionsView{Assumptions: []Assumption{{
			Name:       "custom",
			Value:      "kept",
			Source:     "test",
			Confidence: "high",
		}}},
	}

	once := FinalizeAnalysis(a)
	twice := FinalizeAnalysis(once)

	if len(twice.Actions.Assumptions) != len(once.Actions.Assumptions) {
		t.Fatalf("assumptions duplicated: once=%d twice=%d", len(once.Actions.Assumptions), len(twice.Actions.Assumptions))
	}
	if len(twice.Actions.Interpretation) != len(once.Actions.Interpretation) {
		t.Fatalf("interpretation duplicated: once=%d twice=%d", len(once.Actions.Interpretation), len(twice.Actions.Interpretation))
	}
	if twice.Actions.Assumptions[0].Name != "custom" {
		t.Fatalf("custom assumption was not preserved: %#v", twice.Actions.Assumptions)
	}
}
