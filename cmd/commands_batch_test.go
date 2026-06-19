package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// This file holds cross-command smoke tests that exercise one happy-path
// invocation of each run* command. They were split out of the former
// feature_batch_test.go grab-bag so each command's deeper tests live next to
// its source while these broad smoke checks stay grouped.

func TestRunBehavior_TextOutput(t *testing.T) {
	podsPolicy := autoscalingv2.PodsScalingPolicy
	maxPolicy := autoscalingv2.MaxChangePolicySelect
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(10, 40),
	)
	hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleUp: &autoscalingv2.HPAScalingRules{
			SelectPolicy: &maxPolicy,
			Policies: []autoscalingv2.HPAScalingPolicy{{
				Type:          podsPolicy,
				Value:         10,
				PeriodSeconds: 15,
			}},
		},
	}
	fakeClient := testutil.NewFakeClient(hpa)
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
	}

	var buf bytes.Buffer
	if err := runBehavior(context.Background(), &buf, opts, "web"); err != nil {
		t.Fatalf("runBehavior returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "ScaleUp behavior") || !strings.Contains(output, "t+15s") {
		t.Fatalf("expected behavior policy and estimated path, got:\n%s", output)
	}
}

func TestRunEstimate_TextOutput(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web", testutil.WithMinMax(2, 10))
	fakeClient := testutil.NewFakeClient(hpa)
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
	}

	var buf bytes.Buffer
	if err := runEstimate(context.Background(), &buf, opts, "web", 30, 0.12, 0.01); err != nil {
		t.Fatalf("runEstimate returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Additional worst-case pods: 20") || !strings.Contains(output, "$2.40/hour") || !strings.Contains(output, "0.2000 kgCO2e/hour") {
		t.Fatalf("expected cost estimate, got:\n%s", output)
	}
}

func TestRunConflictScanDetectsMultipleHPAsAndKEDA(t *testing.T) {
	first := testutil.BuildHPA("prod", "web-cpu")
	first.Spec.ScaleTargetRef.Name = "web"
	second := testutil.BuildHPA("prod", "web-keda")
	second.Spec.ScaleTargetRef.Name = "web"
	second.Labels = map[string]string{"scaledobject.keda.sh/name": "web"}

	fakeClient := testutil.NewFakeClient(first, second)
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			Namespace:      "prod",
		},
		List: listOptions{
			Conflicts: true,
		},
	}

	var buf bytes.Buffer
	if err := runConflictScan(context.Background(), &buf, opts); err != nil {
		t.Fatalf("runConflictScan returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Conflicts:") ||
		!strings.Contains(output, "multiple HPAs target the same scale subresource") ||
		!strings.Contains(output, "KEDA-managed HPA") {
		t.Fatalf("expected conflict scan output, got:\n%s", output)
	}
}

func TestRunReadinessEnablesImpactSections(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web")
	fakeClient := testutil.NewFakeClient(hpa)
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}

	var buf bytes.Buffer
	if err := runReadiness(context.Background(), &buf, opts, []string{"web"}); err != nil {
		t.Fatalf("runReadiness returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Readiness Impact") ||
		!strings.Contains(output, "scale target selector could not be resolved") {
		t.Fatalf("expected readiness impact output, got:\n%s", output)
	}
}

func TestRunFleetSummarizesMaxSurgeRisk(t *testing.T) {
	web := testutil.BuildHPA("prod", "web", testutil.WithReplicas(3, 5), testutil.WithMinMax(2, 10))
	api := testutil.BuildHPA("prod", "api", testutil.WithReplicas(6, 6), testutil.WithMinMax(2, 8))
	fakeClient := testutil.NewFakeClient(web, api)
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			Namespace:      "prod",
		},
	}

	var buf bytes.Buffer
	if err := runFleet(context.Background(), &buf, opts, "max-surge"); err != nil {
		t.Fatalf("runFleet returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Fleet HPA Risk Summary") ||
		!strings.Contains(output, "worst-case pods at maxReplicas: 18") ||
		!strings.Contains(output, "additional pods: +9") {
		t.Fatalf("expected fleet risk summary, got:\n%s", output)
	}
}

func TestStatusHiddenFactorsText(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 3),
		testutil.WithResourceMetric("cpu", 80, 84),
		testutil.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := testutil.NewFakeClient(hpa)
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
			Features: featuresOptions{
				HiddenFactors: true,
			},
		},
	}

	var buf bytes.Buffer
	if err := runStatus(context.Background(), &buf, opts, "web", true); err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runStatus returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Score Breakdown") || !strings.Contains(output, "Hidden decision factors") {
		t.Fatalf("expected score breakdown and hidden factors, got:\n%s", output)
	}
}

func TestStatusStructuredFormat(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 120),
	)
	fakeClient := testutil.NewFakeClient(hpa)
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
			Format: "structured",
		},
	}

	var buf bytes.Buffer
	if err := runStatus(context.Background(), &buf, opts, "web", true); err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, `"schemaVersion": "v1"`) || !strings.Contains(output, `"decisionPath"`) {
		t.Fatalf("expected structured decision trace, got:\n%s", output)
	}
}

func TestRunTuneSuggest(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web", testutil.WithMinMax(2, 10))
	fakeClient := testutil.NewFakeClient(hpa)
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
	}

	var buf bytes.Buffer
	if err := runTune(context.Background(), &buf, opts, "web", "stable", true); err != nil {
		t.Fatalf("runTune returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "HPA Tuning Advisor") || !strings.Contains(output, "stabilizationWindowSeconds: 300") {
		t.Fatalf("expected tune advisor output, got:\n%s", output)
	}
}
