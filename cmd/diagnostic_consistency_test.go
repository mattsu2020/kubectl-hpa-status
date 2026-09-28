package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/retrospective"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
	"sigs.k8s.io/yaml"
)

func TestSnapshotSharesWorkloadObservations(t *testing.T) {
	objects := fullPipelineCluster()
	clientset := testutil.NewFakeClientWithObjects(objects...)
	opts := defaultRootOptions()
	opts.ClientOverride = clientset
	opts.Namespace = "default"
	opts.ScalePath = true
	opts.Rollout = true
	opts.ExplainPods = true
	opts.ScaleoutBlockers = true
	opts.Normalize()
	client, err := opts.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	data, err := collectSnapshotData(context.Background(), client, &opts, "web")
	if err != nil {
		t.Fatal(err)
	}
	var deploy appsv1.Deployment
	if err := yaml.Unmarshal(data.Deployment, &deploy); err != nil {
		t.Fatal(err)
	}
	if deploy.Name != "web" || deploy.Status.ReadyReplicas != objects[1].(*appsv1.Deployment).Status.ReadyReplicas {
		t.Fatalf("wrong archived workload: %+v", deploy)
	}
	if !strings.Contains(string(data.Report), "web") || strings.Contains(string(data.Report), "Error building") {
		t.Fatalf("invalid report: %s", data.Report)
	}
	counts := map[string]int{}
	for _, action := range clientset.Actions() {
		key := action.GetVerb() + "/" + action.GetResource().Resource
		counts[key]++
	}
	for _, key := range []string{"get/horizontalpodautoscalers", "get/deployments", "list/pods", "list/replicasets"} {
		if counts[key] != 1 {
			t.Errorf("%s calls=%d, want 1; all=%v", key, counts[key], counts)
		}
	}
}

func TestEventFailureVisibleInDiagnosticReports(t *testing.T) {
	objects := fullPipelineCluster()
	hpa := objects[0].(*autoscalingv2.HorizontalPodAutoscaler)
	clientset := testutil.NewFakeClientWithObjects(objects...)
	clientset.PrependReactor("list", "events", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("events forbidden") })
	opts := defaultRootOptions()
	opts.ClientOverride = clientset
	opts.Namespace = "default"
	client, err := opts.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	eventText := string(fetchSnapshotEvents(context.Background(), client, hpa))
	if !strings.Contains(eventText, "events forbidden") || strings.Contains(eventText, "No recent events") {
		t.Fatalf("misleading snapshot: %s", eventText)
	}
	path := buildScalePathWithSnapshot(context.Background(), client, hpa, nil)
	if !strings.Contains(strings.Join(path.ProbeWarnings, " "), "events forbidden") {
		t.Fatalf("missing scale-path warning: %+v", path.ProbeWarnings)
	}
	blockers := buildBlockerReportForStatusWithSnapshot(context.Background(), client, hpa, "Deployment/web", nil)
	if !strings.Contains(strings.Join(blockers.Warnings, " "), "events forbidden") {
		t.Fatalf("missing blocker warning: %+v", blockers.Warnings)
	}
	data, err := collectBundleData(context.Background(), client, &opts, "web")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(data.Warnings, " "), "events forbidden") || strings.Contains(string(data.Events), "No recent events") {
		t.Fatalf("misleading bundle: %s; %v", data.Events, data.Warnings)
	}
}

func TestTimelineStructuredFormats(t *testing.T) {
	for _, replay := range []bool{false, true} {
		for _, format := range []string{"jsonl", "jsonpath", "go-template"} {
			t.Run(format+map[bool]string{false: "", true: "_replay"}[replay], func(t *testing.T) {
				opts := defaultRootOptions()
				opts.Output = format
				if format == "jsonpath" {
					opts.Template = "{.hpaName}"
				}
				if format == "go-template" {
					opts.Template = "{{.HPAName}}"
				}
				tl := retrospective.Timeline{HPAName: "web", Namespace: "default"}
				var out bytes.Buffer
				var err error
				if replay {
					report := &retrospective.ReplayAnalysis{Summary: "web"}
					if format == "jsonpath" {
						opts.Template = "{.Summary}"
					}
					if format == "go-template" {
						opts.Template = "{{.Summary}}"
					}
					err = renderRetrospectiveReplay(&out, report, tl, format, &opts)
				} else {
					err = renderRetrospective(&out, tl, format, &opts)
				}
				if err != nil {
					t.Fatal(err)
				}
				if format == "jsonl" {
					if !json.Valid(out.Bytes()) || bytes.Count(out.Bytes(), []byte("\n")) != 1 {
						t.Fatalf("invalid JSONL: %q", out.String())
					}
				} else if strings.TrimSpace(out.String()) != "web" {
					t.Fatalf("wrong projection: %q", out.String())
				}
			})
		}
	}
}

func TestTimelineRejectsUnsupportedOutputBeforeReads(t *testing.T) {
	clientset := testutil.NewFakeClient()
	opts := defaultRootOptions()
	opts.ClientOverride = clientset
	for _, format := range []string{"json", "jsonl", "yaml", "jsonpath={.hpaName}", "html"} {
		opts.Output = format
		err := runTimeline(context.Background(), &bytes.Buffer{}, &opts, "web", time.Second)
		if err == nil || !strings.Contains(err.Error(), "live timeline does not support") {
			t.Fatalf("%s: %v", format, err)
		}
	}
	opts.Output = "prometheus"
	if err := runRetrospectiveTimeline(context.Background(), &bytes.Buffer{}, &opts, "web", time.Hour, false); err == nil || !strings.Contains(err.Error(), "timeline does not support") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(clientset.Actions()) != 0 {
		t.Fatalf("unexpected API calls: %v", clientset.Actions())
	}
}

func TestTimelineFromRecordUsesTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.json")
	trace := hpaanalysis.TimelineTrace{HPAName: "web", Namespace: "default", Snapshots: []hpaanalysis.TimelineSnapshot{{Timestamp: time.Now()}}}
	data, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	opts := defaultRootOptions()
	opts.Output = "go-template"
	opts.Template = "{{.HPAName}}"
	var out bytes.Buffer
	if err := runTimelineFromRecord(&out, &opts, "web", path); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "web" {
		t.Fatalf("wrong template result: %q", out.String())
	}
}

func TestRetrospectiveCommandsExcludeReplacedHPA(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web")
	hpa.UID = "current"
	old := testutil.BuildEventWithTimestamp("default", "web", "SuccessfulRescale", "New size: 99; reason: old HPA", time.Now().Add(-time.Minute))
	old.InvolvedObject.UID = "replaced"
	opts := defaultRootOptions()
	opts.ClientOverride = testutil.NewFakeClientWithObjects(hpa, old)
	opts.Namespace = "default"
	opts.Output = "json"
	var out bytes.Buffer
	if err := runFlapLive(context.Background(), &out, &opts, "web", time.Hour); err != nil {
		t.Fatal(err)
	}
	var flap flapReport
	if err := json.Unmarshal(out.Bytes(), &flap); err != nil {
		t.Fatal(err)
	}
	if flap.ScaleEvents != 0 {
		t.Fatalf("old HPA affected flap: %+v", flap)
	}
	out.Reset()
	if err := runRetrospectiveTimeline(context.Background(), &out, &opts, "web", time.Hour, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "old HPA") || strings.Contains(out.String(), "New size: 99") {
		t.Fatalf("old event in timeline: %s", out.String())
	}
}
