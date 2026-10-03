package enrichment

import (
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestKEDARealHealthFlowsIntoDiagnosis(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web", testutil.WithExternalMetricWithStatus("s1-prometheus", "10", "20"))
	object := &unstructured.Unstructured{Object: map[string]any{
		"spec":   map[string]any{"triggers": []any{map[string]any{"type": "redis"}, map[string]any{"type": "prometheus", "name": "http", "metricType": "AverageValue"}}},
		"status": map[string]any{"health": map[string]any{"s1-prometheus": map[string]any{"status": "Failing", "numberOfFailures": int64(3)}}, "triggersActivity": map[string]any{"http": map[string]any{"isActive": false}}},
	}}
	info := kube.ExtractKEDAInfo(object)
	k := buildKEDAAnalysis(info, hpa)
	if k.Triggers[1].MetricName != "s1-prometheus" || k.Triggers[1].MetricType != "AverageValue" || k.Triggers[1].NumberOfFailures == nil || *k.Triggers[1].NumberOfFailures != 3 {
		t.Fatalf("lost status: %+v", k)
	}
	lines := strings.Join(k.Lines, "\n")
	if !strings.Contains(lines, "Failing") || !strings.Contains(lines, `trigger "http" (type prometheus) produces`) || strings.Contains(lines, `type redis) produces`) {
		t.Fatalf("wrong diagnosis: %s", lines)
	}
	a := hpaanalysis.Analysis{Decision: hpaanalysis.DecisionView{Health: "OK", HealthScore: 100}, Controllers: hpaanalysis.ControllersView{KEDAInfo: k}}
	hpaanalysis.ApplyEnrichmentPenalties(&a, hpaanalysis.HealthWeights{})
	if a.Decision.Health != "LIMITED" {
		t.Fatalf("failing health missed: %+v", a.Decision)
	}
	health := k.Health["s1-prometheus"]
	health.Status = "Happy"
	k.Health["s1-prometheus"] = health
	k.Triggers[1].HealthStatus = "Happy"
	hpaanalysis.ApplyEnrichmentPenalties(&a, hpaanalysis.HealthWeights{})
	if a.Decision.Health != "OK" || a.Decision.HealthScore != 100 {
		t.Fatalf("idle trigger penalized: %+v", a.Decision)
	}
}
