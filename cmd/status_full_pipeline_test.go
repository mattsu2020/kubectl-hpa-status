package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// fullPipelineCluster builds a fake cluster with an HPA, its Deployment scale
// target, and pods so that every enricher has real objects to inspect.
func fullPipelineCluster() []runtime.Object {
	labels := map[string]string{"app": "web"}
	// The memory metric has no matching memory request on the container, so
	// the resource-consistency check produces a warning (a fully consistent
	// fixture would make CheckResourceConsistency return nil by design).
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithMinMax(2, 10),
		testutil.WithResourceMetric("cpu", 80, 95),
		testutil.WithResourceMetric("memory", 80, 90),
		testutil.WithExternalMetricWithStatus("queue_depth", "100", "250"),
		testutil.WithScaleTargetRef("Deployment", "web"),
	)
	deploy := testutil.BuildDeployment("default", "web",
		testutil.WithSelector(labels),
		testutil.WithReplicaStatus(3, 3),
		testutil.WithContainer(testutil.ContainerSpec{
			Name:     "app",
			Requests: map[string]string{"cpu": "100m"},
			Limits:   map[string]string{"cpu": "500m"},
		}),
	)
	pod := testutil.BuildPod("default", "web-1",
		testutil.WithPodLabels(labels),
		testutil.WithPodPhase(corev1.PodRunning),
	)
	return []runtime.Object{hpa, deploy, pod}
}

func TestRunStatusMany_AllEnrichersEnabled(t *testing.T) {
	fakeClient := testutil.NewFakeClientWithObjects(fullPipelineCluster()...)

	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: fakeClient,
			},
			OutputOptions: OutputOptions{
				Output: "json",
			},
		},
	}
	f := &opts.Features
	f.Interpret = true
	f.Explain = true
	f.Suggest = true
	f.Recommend = true
	f.HiddenFactors = true
	f.Deep = true
	f.DiagnoseMetrics = true
	f.MetricsFreshness = true
	f.MetricContract = true
	f.AdapterDiagnostics = true
	f.MetricHints = true
	f.CheckResources = true
	f.ExplainPods = true
	f.CapacityContext = true
	f.CapacityHeadroom = true
	f.CapacityDeep = true
	f.CapacityPlan = true
	f.ScalePath = true
	f.Rollout = true
	f.RolloutImpact = true
	f.ReadinessImpact = true
	f.ScaleoutBlockers = true
	f.ControllerProfile = true
	f.DecisionTrace = true
	f.GitOpsCheck = true
	f.ChurnDetect = true
	f.FlappingAdvisor = true
	f.TrendAnomaly = true
	f.ContainerAdvisor = true
	f.BehaviorAdvisor = true
	opts.Events = EventOption{Enabled: true, Limit: 5}
	opts.Normalize()

	var buf bytes.Buffer
	err := runStatusMany(context.Background(), &buf, opts, []string{"web"}, true)
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runStatusMany with all enrichers: %v", err)
	}

	var report hpaanalysis.StatusReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput:\n%s", err, buf.String())
	}
	if report.Analysis.Name != "web" {
		t.Errorf("expected analysis for web, got %q", report.Analysis.Name)
	}
	// Spot-check that deep enrichment actually populated its sections rather
	// than silently skipping. The fixture's memory metric has no memory
	// request, so the resource check must report the inconsistency.
	if report.Analysis.ResourceCheck == nil || len(report.Analysis.ResourceCheck.Warnings) == 0 {
		t.Error("expected ResourceCheck warnings for memory metric without memory request")
	}
	if report.Analysis.BlockerReport == nil {
		t.Error("expected BlockerReport to be populated with --scaleout-blockers")
	}
	if report.Analysis.CapacityContext == nil {
		t.Error("expected CapacityContext to be populated with --capacity-context")
	}
}

// TestRunStatusMany_NoEnrich verifies the --no-enrich tier still renders a
// bare report without touching workload APIs.
func TestRunStatusMany_NoEnrich(t *testing.T) {
	fakeClient := testutil.NewFakeClientWithObjects(fullPipelineCluster()...)

	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: fakeClient,
			},
			OutputOptions: OutputOptions{
				Output: "json",
			},
		},
	}
	opts.NoEnrich = true
	opts.Normalize()

	var buf bytes.Buffer
	err := runStatusMany(context.Background(), &buf, opts, []string{"web"}, false)
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runStatusMany --no-enrich: %v", err)
	}
	var report hpaanalysis.StatusReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("parse: %v\n%s", err, buf.String())
	}
	if report.Analysis.Name != "web" {
		t.Errorf("expected bare analysis, got %q", report.Analysis.Name)
	}
}

// TestBuildStatusEnrichers_EnabledGating asserts each adapter's Enabled()
// reflects its option toggle: with zero options only the always-on report
// enricher runs; with everything on, all adapters are enabled.
func TestBuildStatusEnrichers_EnabledGating(t *testing.T) {
	// report is unconditionally on; vpa-advisory gates on report content at
	// Run time rather than on an option, so its Enabled is also always true.
	alwaysOn := map[string]bool{"report": true, "vpa-advisory": true}
	off := &options{}
	for _, e := range buildStatusEnrichers(off) {
		if e.Enabled() && !alwaysOn[e.Name()] {
			t.Errorf("unexpected enricher enabled by default: %s", e.Name())
		}
	}

	on := &options{}
	f := &on.Features
	f.DecisionTrace = true
	f.DiagnoseMetrics = true
	f.MetricsFreshness = true
	f.MetricContract = true
	f.AdapterDiagnostics = true
	f.MetricHints = true
	f.CheckResources = true
	f.ExplainPods = true
	f.CapacityContext = true
	f.CapacityPlan = true
	f.Rollout = true
	f.ScaleoutBlockers = true
	f.ControllerProfile = true
	f.GitOpsCheck = true
	f.ChurnDetect = true
	f.FlappingAdvisor = true
	f.ContainerAdvisor = true
	f.BehaviorAdvisor = true
	on.Events = EventOption{Enabled: true, Limit: 5}
	on.Simulate = []string{"maxReplicas=20"}
	on.Normalize()

	for _, e := range buildStatusEnrichers(on) {
		if !e.Enabled() {
			t.Errorf("enricher %q should be enabled with all features on", e.Name())
		}
	}
}
