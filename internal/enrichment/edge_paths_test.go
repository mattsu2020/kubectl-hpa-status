package enrichment

import (
	"context"
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// The tests in this file cover the error and edge paths of the enrichment
// engine: pipeline policies, context cancellation, and API list failures that
// must surface as warnings or error states instead of silent "not found".

func TestRunPipelineDisabledAndNilRunnerPolicies(t *testing.T) {
	t.Run("disabled task with nil runner is skipped", func(t *testing.T) {
		var seen []string
		err := RunPipeline(context.Background(), []PipelineTask{
			{Name: "disabled", Enabled: false, Run: nil},
			{Name: "ok", Enabled: true, Run: func(context.Context) error { seen = append(seen, "ok"); return nil }},
		}, func(name string, err error) {
			t.Fatalf("unexpected error callback for %q: %v", name, err)
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(seen) != 1 {
			t.Fatalf("expected only the enabled task to run, saw %v", seen)
		}
	})

	t.Run("nil runner without abort reports and continues", func(t *testing.T) {
		var failures []string
		ran := false
		err := RunPipeline(context.Background(), []PipelineTask{
			{Name: "broken", Enabled: true, AbortOnError: false, Run: nil},
			{Name: "after", Enabled: true, Run: func(context.Context) error { ran = true; return nil }},
		}, func(name string, err error) { failures = append(failures, name) })
		if err != nil {
			t.Fatalf("best-effort nil runner must not fail the pipeline: %v", err)
		}
		if !ran || len(failures) != 1 || failures[0] != "broken" {
			t.Fatalf("expected the pipeline to continue after reporting, ran=%v failures=%v", ran, failures)
		}
	})

	t.Run("nil runner with abort fails fast", func(t *testing.T) {
		err := RunPipeline(context.Background(), []PipelineTask{
			{Name: "broken", Enabled: true, AbortOnError: true, Run: nil},
			{Name: "after", Enabled: true, Run: func(context.Context) error { t.Error("must not run after abort"); return nil }},
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "broken") {
			t.Fatalf("expected fail-fast error naming the task, got %v", err)
		}
	})

	t.Run("nil onError with failing task returns the original error", func(t *testing.T) {
		want := errors.New("boom")
		err := RunPipeline(context.Background(), []PipelineTask{
			{Name: "failing", Enabled: true, AbortOnError: true, Run: func(context.Context) error { return want }},
		}, nil)
		if !errors.Is(err, want) {
			t.Fatalf("expected the original error back, got %v", err)
		}
	})
}

func TestNewContextCanceledContextRecordsReason(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ec := NewContext(ctx, Config{KEDA: "on", VPA: "on"})
	if ec == nil {
		t.Fatal("NewContext must always return a non-nil context")
	}
	status := ec.Status()
	if status.KEDA == nil || status.VPA == nil {
		t.Fatalf("expected both entries present, got %+v", status)
	}
	if status.KEDA.Reason == "" || status.VPA.Reason == "" {
		t.Fatalf("expected the cancellation reason on both entries, got %+v", status)
	}
	if ec.KEDAEnabled() || ec.VPAEnabled() {
		t.Fatal("enrichment must not be enabled after cancellation")
	}
}

// TestEnrichReportRecordsEnrichmentFailures checks the EnrichReport error
// branches: when the dynamic client is unavailable, both sources must record
// an error entry and append a warning to the analysis.
func TestEnrichReportRecordsEnrichmentFailures(t *testing.T) {
	ec := &Context{kedaEnabled: true, vpaEnabled: true}
	hpa := kedaManagedHPA("default", "web", "web")
	report := &hpaanalysis.StatusReport{Analysis: hpaanalysis.Analysis{
		Meta: hpaanalysis.MetaView{Namespace: "default", Name: "web"},
	}}

	EnrichReport(context.Background(), ec, hpa, report, hpaanalysis.HealthWeights{})

	status := report.Analysis.Lifecycle.EnrichmentStatus
	if status == nil || status.KEDA == nil || status.VPA == nil {
		t.Fatalf("expected enrichment status for both sources, got %+v", status)
	}
	if status.KEDA.State != StateError || status.VPA.State != StateError {
		t.Fatalf("expected error states, got KEDA=%v VPA=%v", status.KEDA.State, status.VPA.State)
	}
	var kedaWarning, vpaWarning bool
	for _, w := range report.Analysis.Actions.Warnings {
		if strings.HasPrefix(w, "KEDA enrichment failed") {
			kedaWarning = true
		}
		if strings.HasPrefix(w, "VPA enrichment failed") {
			vpaWarning = true
		}
	}
	if !kedaWarning || !vpaWarning {
		t.Fatalf("expected both enrichment failure warnings, got %v", report.Analysis.Actions.Warnings)
	}
}

// failingDynClient returns a dynamic fake whose list calls for the given
// resource always fail, exercising the API-failure paths.
func failingDynClient(t *testing.T, gvr schema.GroupVersionResource, listKind string, verb string) *dynamicfake.FakeDynamicClient {
	t.Helper()
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{gvr: listKind},
	)
	dyn.PrependReactor(verb, gvr.Resource, func(_ ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden: cluster-scoped list denied")
	})
	return dyn
}

func TestEnrichVPAListFailureIsErrorState(t *testing.T) {
	dyn := failingDynClient(t, vpaGVRForTest, "VerticalPodAutoscalerList", "list")
	ec := &Context{vpaEnabled: true, dynClient: dyn}
	hpa := hpaWithResourceMetric("default", "web", "web")
	report := &hpaanalysis.StatusReport{Analysis: hpaanalysis.Analysis{
		Meta: hpaanalysis.MetaView{Namespace: "default", Name: "web"},
	}}

	entry := EnrichVPA(context.Background(), ec, hpa, report)
	if entry.State != StateError || !strings.Contains(entry.Reason, "forbidden") {
		t.Fatalf("expected an error state carrying the API failure, got %+v", entry)
	}
	if report.Analysis.Advisory.VPAConflict != nil {
		t.Fatal("no conflict info may be recorded when the VPA list fails")
	}
}

func TestFindConflictingVPAListFailure(t *testing.T) {
	dyn := failingDynClient(t, vpaGVRForTest, "VerticalPodAutoscalerList", "list")
	hpa := hpaWithResourceMetric("default", "web", "web")

	info, err := FindConflictingVPA(context.Background(), dyn, "default", hpa)
	if err == nil {
		t.Fatal("expected the list failure to propagate")
	}
	if info != nil {
		t.Fatalf("expected no VPA info on failure, got %+v", info)
	}
}

func TestBatchVPAListFailureRecordsWarning(t *testing.T) {
	dyn := failingDynClient(t, vpaGVRForTest, "VerticalPodAutoscalerList", "list")
	ec := &Context{vpaEnabled: true, dynClient: dyn}
	hpas := []autoscalingv2.HorizontalPodAutoscaler{*hpaWithResourceMetric("default", "web", "web")}

	results, warnings := BatchVPA(context.Background(), ec, hpas)
	if len(results) != 0 {
		t.Fatalf("expected no results on list failure, got %v", results)
	}
	if msgs := warnings["default"]; len(msgs) != 1 || !strings.Contains(msgs[0], "VPA list failed") {
		t.Fatalf("expected a per-namespace warning for the failed list, got %v", warnings)
	}
}

func TestBatchKEDAListFailureRecordsWarning(t *testing.T) {
	dyn := failingDynClient(t, schema.GroupVersionResource{Group: "keda.sh", Version: "v1alpha1", Resource: "scaledobjects"}, "ScaledObjectList", "list")
	ec := &Context{kedaEnabled: true, dynClient: dyn}
	hpas := []autoscalingv2.HorizontalPodAutoscaler{*kedaManagedHPA("default", "web", "web")}

	results, warnings := BatchKEDA(context.Background(), ec, hpas)
	// The list failure must surface as a per-namespace warning instead of
	// being indistinguishable from "no ScaledObjects exist".
	if msgs := warnings["default"]; len(msgs) != 1 || !strings.Contains(msgs[0], "KEDA ScaledObject list failed") {
		t.Fatalf("expected a per-namespace warning for the failed list, got %v", warnings)
	}
	// With the index empty the HPA degrades to the no-match analysis; it must
	// not claim a resolved ScaledObject.
	if analysis := results["default/web"]; analysis == nil || analysis.ScaledObjectName != "" {
		t.Fatalf("expected the no-match analysis, got %+v", analysis)
	}
}

// TestAnalyzeKEDAHPAAmbiguousTargets covers the ownership-ambiguity line: two
// ScaledObjects target the same workload and the HPA carries no preferred
// name, so no single owner can be resolved.
func TestAnalyzeKEDAHPAAmbiguousTargets(t *testing.T) {
	hpa := kedaManagedHPA("default", "web", "web")
	hpa.Name = "keda-hpa-web" // name-prefix detection carries no preferred ScaledObject name
	hpa.Labels = nil
	// The lookup key is the HPA's own name (not the workload's).

	so1 := scaledObjectUnstructured("default", "web-so-a", "Deployment", "web")
	so2 := scaledObjectUnstructured("default", "web-so-b", "Deployment", "web")
	indexes := kedaIndexes{
		byName: map[string]*unstructured.Unstructured{},
		byTarget: map[string][]*unstructured.Unstructured{
			"default/Deployment/web": {&so1, &so2},
		},
	}

	key, analysis := analyzeKEDAHPA(hpa, indexes)
	if key != "default/keda-hpa-web" || analysis == nil {
		t.Fatalf("expected an analysis for default/keda-hpa-web, got key=%q analysis=%+v", key, analysis)
	}
	if len(analysis.Lines) != 1 || !strings.Contains(analysis.Lines[0], "ownership is ambiguous") {
		t.Fatalf("expected the ambiguity line, got %+v", analysis.Lines)
	}
}

// TestAnalyzeKEDAHPANamedObjectTakesPrecedence covers the byName hit and the
// candidate de-duplication: the named ScaledObject wins and must not be
// duplicated by the targetRef candidates.
func TestAnalyzeKEDAHPANamedObjectTakesPrecedence(t *testing.T) {
	hpa := kedaManagedHPA("default", "web", "web")
	// The preferred-name lookup reads scaledobject.keda.sh/name.
	hpa.Labels["scaledobject.keda.sh/name"] = "web-so"

	named := scaledObjectUnstructured("default", "web-so", "Deployment", "web")
	indexes := kedaIndexes{
		byName: map[string]*unstructured.Unstructured{
			"default/web-so": &named,
		},
		byTarget: map[string][]*unstructured.Unstructured{},
	}

	key, analysis := analyzeKEDAHPA(hpa, indexes)
	if key != "default/web" || analysis == nil {
		t.Fatalf("expected an analysis for default/web, got key=%q analysis=%+v", key, analysis)
	}
	if analysis.ScaledObjectName != "web-so" {
		t.Fatalf("expected the named ScaledObject to win, got %+v", analysis.ScaledObjectName)
	}
}
