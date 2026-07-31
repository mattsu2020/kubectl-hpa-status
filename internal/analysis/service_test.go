package analysis

import (
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	hpakeda "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"
	hpavpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAnalyzeBatchAppliesEnrichmentWarningsAndPreservesOrder(t *testing.T) {
	hpas := []autoscalingv2.HorizontalPodAutoscaler{
		testHPA("team-a", "first"),
		testHPA("team-b", "second"),
	}
	results := AnalyzeBatch(hpas, Options{}, BatchEnrichment{
		KEDA: map[string]*hpakeda.Analysis{
			"team-a/first": {Lines: []string{"observed"}},
		},
		VPA: map[string]*hpavpa.ConflictInfo{
			"team-b/second": hpavpa.NewConflictInfo(&hpavpa.Info{Name: "second-vpa"}),
		},
		Warnings: map[string][]string{
			"team-a": {"KEDA list unavailable"},
		},
	})

	if len(results) != 2 {
		t.Fatalf("AnalyzeBatch() returned %d results, want 2", len(results))
	}
	if results[0].Key != "team-a/first" || results[1].Key != "team-b/second" {
		t.Fatalf("AnalyzeBatch() order = %q, %q", results[0].Key, results[1].Key)
	}
	if results[0].Analysis.KEDAInfo == nil {
		t.Fatal("first result is missing KEDA enrichment")
	}
	if len(results[0].Analysis.Warnings) != 1 || results[0].Analysis.Warnings[0] != "KEDA list unavailable" {
		t.Fatalf("first warnings = %#v", results[0].Analysis.Warnings)
	}
	if results[1].Analysis.VPAConflict == nil {
		t.Fatal("second result is missing VPA enrichment")
	}
	if results[0].Report.APIVersion != hpaanalysis.SchemaVersion {
		t.Fatalf("report apiVersion = %q", results[0].Report.APIVersion)
	}
	if results[0].ListItem.Name != "first" {
		t.Fatalf("list item name = %q", results[0].ListItem.Name)
	}
}

func TestAnalyzeBatchDoesNotAliasNamespaceWarnings(t *testing.T) {
	warnings := []string{"unavailable"}
	results := AnalyzeBatch(
		[]autoscalingv2.HorizontalPodAutoscaler{testHPA("team-a", "first")},
		Options{},
		BatchEnrichment{Warnings: map[string][]string{"team-a": warnings}},
	)
	warnings[0] = "mutated"
	if got := results[0].Analysis.Warnings[0]; got != "unavailable" {
		t.Fatalf("warning aliased caller storage: %q", got)
	}
}

func TestMergeNamespaceWarningsPreservesAllSources(t *testing.T) {
	first := map[string][]string{"team-a": {"keda"}}
	second := map[string][]string{"team-a": {"vpa"}, "team-b": {"vpa"}}
	merged := MergeNamespaceWarnings(first, second)
	if len(merged["team-a"]) != 2 || merged["team-a"][0] != "keda" || merged["team-a"][1] != "vpa" {
		t.Fatalf("team-a warnings = %#v", merged["team-a"])
	}
	merged["team-a"][0] = "changed"
	if first["team-a"][0] != "keda" {
		t.Fatal("merged warnings alias an input slice")
	}
}

func testHPA(namespace, name string) autoscalingv2.HorizontalPodAutoscaler {
	minReplicas := int32(1)
	return autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: name,
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 2,
			DesiredReplicas: 2,
		},
	}
}
