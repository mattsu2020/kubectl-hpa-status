package bundle

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/blocker"
)

// richAnalysisFixture populates every Analysis branch the bundle renderers
// consume, so a single RenderMarkdown call drives the table/list bodies that
// the all-sections smoke test (which uses an empty Analysis) leaves untouched.
func richAnalysisFixture() hpaanalysis.Analysis {
	pollingInterval := int32(30)
	var a hpaanalysis.Analysis

	a.ResourceCheck = &hpaanalysis.ResourceCheckResult{
		Warnings: []hpaanalysis.ResourceWarning{
			{Container: "app", Resource: "cpu", Category: "missing-requests", Details: "no cpu request | set one", Severity: "warning"},
		},
	}
	a.CapacityContext = &hpaanalysis.CapacityContext{
		PendingPods: []hpaanalysis.PendingPodInfo{
			{Name: "web-2", Phase: "Pending", Unschedulable: true, Reasons: []string{"Insufficient cpu"}},
		},
		QuotaConstraints: []hpaanalysis.QuotaConstraint{
			{Name: "compute", Resource: "cpu", Used: "9", Hard: "10", Message: "near limit"},
		},
		PDBInterference: []hpaanalysis.PDBInterference{
			{Name: "web-pdb", MinAvailable: "2", MaxUnavailable: "", Disruption: "blocked"},
		},
		NodeHints: []string{"2 nodes tainted NoSchedule"},
	}
	a.ScalePath = &hpaanalysis.ScalePath{
		Steps: []hpaanalysis.ScalePathStep{
			{Name: "HPA", Summary: "wants 5 replicas"},
			{Name: "Deployment", Summary: "updated"},
		},
		BlockingPoint: "scheduler",
		Evidence:      []string{"pod web-2 unschedulable"},
		NextActions:   []string{"add nodes"},
	}
	a.BlockerReport = &blocker.Report{
		Namespace:       "production",
		Name:            "web",
		Target:          "Deployment/web",
		HPAWantsScale:   true,
		DesiredReplicas: 5,
		ReadyReplicas:   3,
		Summary:         "scale-up blocked by scheduling",
		Blockers: []blocker.Finding{
			{ID: "sched-1", Severity: "HIGH", Category: "scheduling", Message: "pods unschedulable", Detail: "Insufficient cpu"},
		},
		Interpretation: "cluster is out of cpu",
		NextCommands:   []string{"kubectl describe nodes"},
	}
	a.KEDAInfo = &hpaanalysis.KEDAAnalysis{
		ScaledObjectName: "web-so",
		PollingInterval:  &pollingInterval,
		Triggers: []hpaanalysis.KEDATriggerSummary{
			{Type: "kafka", Name: "lag", Status: "Active", Threshold: "100", CurrentValue: "250", AuthRef: "kafka-auth"},
		},
		Fallback: &hpaanalysis.KEDAFallbackInfo{FailureThreshold: 3, Replicas: 2},
		Lines:    []string{"[observed] HPA is owned by KEDA"},
	}
	a.MetricsDiagnostics = &hpaanalysis.MetricsPipelineDiagnostics{
		OverallStatus: "DEGRADED",
		PerMetricChecks: []hpaanalysis.PerMetricHealthCheck{
			{MetricType: "Resource", MetricName: "cpu", Status: "OK", Details: "fresh"},
		},
		RemediationSteps: []string{"restart metrics-server"},
	}
	a.MetricFreshnessEntries = []hpaanalysis.MetricFreshness{
		{Name: "cpu", Type: "Resource", Status: "OK", Age: 30 * time.Second, Source: "metrics-server"},
		{Name: "queue_depth", Type: "External", Status: "Missing"},
	}
	a.MetricContract = &hpaanalysis.MetricContractReport{
		OverallStatus: "BROKEN",
		Checks: []hpaanalysis.MetricContractCheck{
			{MetricType: "External", MetricName: "queue_depth", APIService: "v1beta1.external.metrics.k8s.io", APIServiceAvailable: true, DataAvailable: false, Status: "NoData"},
		},
	}
	a.Actions = []string{"raise maxReplicas"}
	a.Suggestions = []hpaanalysis.Suggestion{{Title: "tune", Description: "add stabilization window"}}
	a.Interpretation = []string{"HPA is throttled"}
	return a
}

// TestRenderMarkdown_RichData drives the table/list bodies of every section
// renderer with populated analysis and infrastructure data, asserting a
// representative payload cell from each.
func TestRenderMarkdown_RichData(t *testing.T) {
	t.Parallel()
	data := &Data{
		Namespace:   "production",
		HPAName:     "web",
		Timestamp:   time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
		HPA:         []byte("kind: HorizontalPodAutoscaler\n"),
		ScaleTarget: []byte("kind: Deployment\n"),
		Events:      []byte("SuccessfulRescale to 5\n"),
		MetricsAPI:  []byte("metrics-server: available\n"),
		PodInfos: []kube.PodInfo{
			{Name: "web-1", Phase: "Running", Ready: true, NodeName: "node-a"},
			{Name: "web-2", Phase: "Pending", Ready: false, Unschedulable: true, Reasons: []string{"Insufficient cpu"}},
		},
		ContainerStatuses: []kube.ContainerStatusDetail{
			{Pod: "web-1", Container: "app", Waiting: true, WaitingReason: "ImagePullBackOff", RestartCount: 7},
		},
		ResourceQuotas: []kube.QuotaInfo{{Name: "compute", Resource: "cpu", Used: "9", Hard: "10", Ratio: 0.9}},
		LimitRanges:    []kube.LimitRangeInfo{{Name: "lr", Type: "Container", Resource: "memory", Min: "16Mi", Max: "1Gi"}},
		PDBs:           []kube.PDBInfo{{Name: "web-pdb", MinAvailable: "2"}},
		NodeCapacity:   &kube.NodeCapacityInfo{TotalNodes: 5, TaintedNodes: 2},
		StatusReport:   hpaanalysis.StatusReport{Analysis: richAnalysisFixture()},
	}

	var buf bytes.Buffer
	if err := RenderMarkdown(&buf, data); err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	out := buf.String()

	wantPayloads := []string{
		// Pod / container tables.
		"| web-1 | Running | ✓ | - | - | node-a |",
		"| web-2 | Pending | ✗ | Yes | Insufficient cpu | - |",
		"| web-1 | app | Yes | ImagePullBackOff | 7 |",
		// Resource requests.
		"| app | cpu | missing-requests | warning |",
		// Capacity context sub-sections.
		"### Pending Pods",
		"### Quota Constraints",
		"### PDB Interference",
		"### Node Hints",
		"- 2 nodes tainted NoSchedule",
		// Scale path.
		"| 1 | HPA | wants 5 replicas |",
		"**Blocking Point:** scheduler",
		"- add nodes",
		// Blocker report.
		"**Summary:** scale-up blocked by scheduling",
		"| HIGH | scheduling | pods unschedulable | Insufficient cpu |",
		"**Interpretation:** cluster is out of cpu",
		"kubectl describe nodes",
		// KEDA.
		"## KEDA Status",
		"**ScaledObject:** web-so",
		"**Polling Interval:** 30s",
		"| kafka | lag | Active | 100 | 250 | kafka-auth |",
		"**Fallback:** failureThreshold=3, replicas=2",
		// Metrics diagnostics family.
		"**Overall Status:** DEGRADED",
		"- restart metrics-server",
		"### Metric Freshness",
		"| cpu | Resource | OK | 30s | metrics-server |",
		"| queue_depth | External | Missing | - | - |",
		"### Metric Contract",
		"| External | queue_depth | v1beta1.external.metrics.k8s.io | Yes | No | NoData |",
		// Infra tables.
		"| compute | cpu | 9 | 10 | 90% |",
		"| lr | Container | memory | 16Mi | 1Gi |",
		"| web-pdb | 2 | - |",
		"| Total Nodes | 5 |",
		"| Tainted Nodes | 2 |",
		// Recommendations.
		"- raise maxReplicas",
		"- add stabilization window",
		"- HPA is throttled",
	}
	for _, want := range wantPayloads {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in rendered markdown", want)
		}
	}
}

// TestMdEscape verifies pipe escaping for markdown table safety.
func TestMdEscape(t *testing.T) {
	t.Parallel()
	if got := mdEscape("a|b|c"); got != `a\|b\|c` {
		t.Errorf("mdEscape = %q", got)
	}
	if got := mdEscape("plain"); got != "plain" {
		t.Errorf("mdEscape(plain) = %q", got)
	}
}

// TestWriterErrCapture verifies the Writer keeps the first error and stops
// writing afterwards.
func TestWriterErrCapture(t *testing.T) {
	t.Parallel()
	w := &Writer{w: &failingWriter{}}
	if w.Err() != nil {
		t.Fatal("fresh writer should have nil Err")
	}
	w.Printf("x%d", 1)
	first := w.Err()
	if first == nil {
		t.Fatal("expected captured error after failed write")
	}
	w.Print("more")
	w.Println("more")
	w.Write([]byte("more"))
	if w.Err() != first {
		t.Error("later writes must not overwrite the first error")
	}
}
