package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/cmdoptions"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestWriteOutputJSONPath(t *testing.T) {
	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{
			Namespace: "default",
			Name:      "web",
			Summary:   "HPA currently keeps the replica count unchanged.",
		},
	}

	var out bytes.Buffer
	if err := writeOutput(&out, "jsonpath={.analysis.summary}", "", report, nil); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "HPA currently keeps the replica count unchanged." {
		t.Fatalf("unexpected jsonpath output: %q", out.String())
	}

	// Test separate jsonpath format and template argument
	out.Reset()
	if err := writeOutput(&out, "jsonpath", "{.analysis.summary}", report, nil); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "HPA currently keeps the replica count unchanged." {
		t.Fatalf("unexpected jsonpath output: %q", out.String())
	}
}

func TestWriteOutputTemplate(t *testing.T) {
	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{
			Namespace: "default",
			Name:      "web",
		},
	}

	var out bytes.Buffer
	if err := writeOutput(&out, "template={{ .Analysis.Namespace }}/{{ .Analysis.Name }}", "", report, nil); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "default/web" {
		t.Fatalf("unexpected template output: %q", out.String())
	}

	// Test separate template format and template argument
	out.Reset()
	if err := writeOutput(&out, "go-template", "{{ .Analysis.Namespace }}/{{ .Analysis.Name }}", report, nil); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "default/web" {
		t.Fatalf("unexpected template output: %q", out.String())
	}
}

func TestOutputSelectionUsesNamedConfigTemplate(t *testing.T) {
	opts := &options{
		Common: cmdoptions.Common{
			Output:          "names",
			OutputTemplates: map[string]outputTemplateConfig{"names": {Type: "go-template", Template: "{{ .Analysis.Namespace }}/{{ .Analysis.Name }}"}},
		},
	}

	format, templateStr := outputSelection(outputConfig{
		output: opts.Output, outputTemplates: opts.OutputTemplates,
	})
	if format != "go-template" {
		t.Fatalf("expected go-template format, got %q", format)
	}
	if templateStr != "{{ .Analysis.Namespace }}/{{ .Analysis.Name }}" {
		t.Fatalf("unexpected template %q", templateStr)
	}
}

func TestOutputSelectionUsesNamedJSONPathTemplate(t *testing.T) {
	opts := &options{
		Common: cmdoptions.Common{
			Output:          "jsonpath:summary",
			OutputTemplates: map[string]outputTemplateConfig{"summary": {Template: "{.analysis.summary}"}},
		},
	}

	format, templateStr := outputSelection(outputConfig{
		output: opts.Output, outputTemplates: opts.OutputTemplates,
	})
	if format != "jsonpath" {
		t.Fatalf("expected jsonpath format, got %q", format)
	}
	if templateStr != "{.analysis.summary}" {
		t.Fatalf("unexpected template %q", templateStr)
	}
}

func TestApplyHealthWeightOverrides(t *testing.T) {
	opts := &options{
		Common: cmdoptions.Common{
			HealthWeightOverrides: []string{"scalingInactive=50", "atMinimumReplicas=0"},
		},
	}
	if err := applyHealthWeightOverrides(opts); err != nil {
		t.Fatal(err)
	}
	if opts.HealthWeights.ScalingInactive == nil || *opts.HealthWeights.ScalingInactive != 50 {
		t.Fatalf("expected scalingInactive=50, got %v", opts.HealthWeights.ScalingInactive)
	}
	if opts.HealthWeights.AtMinimumReplicas == nil || *opts.HealthWeights.AtMinimumReplicas != 0 {
		t.Fatalf("expected atMinimumReplicas=0, got %v", opts.HealthWeights.AtMinimumReplicas)
	}
}

func TestPodUnschedulable(t *testing.T) {
	pod := corev1.Pod{
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable},
			},
		},
	}
	if !podUnschedulable(pod) {
		t.Fatal("expected pod to be unschedulable")
	}
}

func TestMatchesListFilter(t *testing.T) {
	item := hpaanalysis.ListItem{
		Health: "LIMITED",
		Issue:  "LIMITED: TooManyReplicas",
	}

	for _, filter := range []string{"limited", "scaling-limited", "TooManyReplicas"} {
		if !matchesListFilter(item, filter) {
			t.Fatalf("expected filter %q to match %#v", filter, item)
		}
	}
	if matchesListFilter(item, "error") {
		t.Fatalf("did not expect error filter to match %#v", item)
	}
}

func TestMatchesHealthScoreRange(t *testing.T) {
	item := hpaanalysis.ListItem{HealthScore: 75}

	if !matchesHealthScoreRange(item, 60, 90) {
		t.Fatal("expected score 75 to match 60..90")
	}
	if matchesHealthScoreRange(item, 80, -1) {
		t.Fatal("did not expect score 75 to match min score 80")
	}
	if matchesHealthScoreRange(item, -1, 60) {
		t.Fatal("did not expect score 75 to match max score 60")
	}
}

func TestSortListItemsByDesired(t *testing.T) {
	items := []hpaanalysis.ListItem{
		{Name: "api", Desired: 5},
		{Name: "web", Desired: 2},
	}

	sortListItems(items, "desired")
	if items[0].Name != "web" {
		t.Fatalf("expected web first, got %#v", items)
	}
}

func TestSortListItemsByDiff(t *testing.T) {
	items := []hpaanalysis.ListItem{
		{Name: "api", Current: 2, Desired: 2}, // diff = 0
		{Name: "web", Current: 3, Desired: 8}, // diff = 5
		{Name: "db", Current: 5, Desired: 2},  // diff = 3
	}

	sortListItems(items, "diff")
	if items[0].Name != "web" || items[1].Name != "db" || items[2].Name != "api" {
		t.Fatalf("expected order [web, db, api], got order: %s, %s, %s", items[0].Name, items[1].Name, items[2].Name)
	}
}

func TestSortListItemsProblemDefaultsWorstHealthFirst(t *testing.T) {
	items := []hpaanalysis.ListItem{
		{Namespace: "default", Name: "limited", HealthScore: 75, Current: 5, Desired: 5},
		{Namespace: "default", Name: "broken", HealthScore: 50, Current: 2, Desired: 2},
		{Namespace: "default", Name: "large-diff", HealthScore: 75, Current: 1, Desired: 8},
	}

	sortListItems(items, "problem")
	if items[0].Name != "broken" || items[1].Name != "large-diff" || items[2].Name != "limited" {
		t.Fatalf("expected [broken, large-diff, limited], got [%s, %s, %s]", items[0].Name, items[1].Name, items[2].Name)
	}
}

func TestSortListItemsByAge(t *testing.T) {
	now := metav1.Now()
	past := metav1.NewTime(now.Add(-10 * time.Minute))
	future := metav1.NewTime(now.Add(10 * time.Minute))

	items := []hpaanalysis.ListItem{
		{Name: "api", CreationTimestamp: now},
		{Name: "web", CreationTimestamp: future},
		{Name: "db", CreationTimestamp: past},
	}

	sortListItems(items, "age")
	if items[0].Name != "db" || items[1].Name != "api" || items[2].Name != "web" {
		t.Fatalf("expected order [db, api, web], got order: %s, %s, %s", items[0].Name, items[1].Name, items[2].Name)
	}
}

func TestPatchDiffIncludesCurrentDesiredReplicas(t *testing.T) {
	minReplicas := int32(2)
	diff := hpaanalysis.SuggestionDiff(&minReplicas, 7, 10, `{"spec":{"maxReplicas":20}}`)
	if !strings.Contains(diff, "status.desiredReplicas: 7") {
		t.Fatalf("expected desiredReplicas context, got %q", diff)
	}
	if !strings.Contains(diff, "spec.maxReplicas: 10 -> 20") {
		t.Fatalf("expected maxReplicas diff, got %q", diff)
	}
}

func TestReportHasConditionNormalizesConditionName(t *testing.T) {
	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{
			Conditions: []hpaanalysis.Condition{{Type: "ScalingLimited"}},
		},
	}

	if !reportHasCondition(report, "scaling-limited") {
		t.Fatalf("expected scaling-limited to match ScalingLimited")
	}
}

func TestVersionCommandPrintsBuildMetadata(t *testing.T) {
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"kubectl-hpa-status version", "commit:", "built:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in version output, got %q", want, text)
		}
	}
}

func TestCompletionCommandSupportsPowerShell(t *testing.T) {
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"completion", "powershell"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Register-ArgumentCompleter") {
		t.Fatalf("expected powershell completion script, got %q", out.String())
	}
}

func TestApplyConfigDefaultsDoesNotOverrideExplicitFlags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hpa-status.yaml")
	if err := os.WriteFile(path, []byte("namespace: team-a\nlang: ja\nevents: 3\nminScore: 60\ncolor: always\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	root := NewRootCommand()
	root.SetArgs([]string{"--config", path, "--lang", "en", "version"})
	if err := root.ParseFlags([]string{"--config", path, "--lang", "en"}); err != nil {
		t.Fatal(err)
	}

	opts := &options{
		Common: cmdoptions.Common{
			Config: path,
			Lang:   "en",
		},
		Status: cmdoptions.Status{
			Events: EventOption{Enabled: true, Limit: 5},
		},
		List: cmdoptions.List{
			HealthScoreMin: -1,
			HealthScoreMax: -1,
		},
	}
	if err := applyConfigDefaults(root, opts); err != nil {
		t.Fatal(err)
	}
	if opts.Namespace != "team-a" {
		t.Fatalf("expected namespace from config, got %q", opts.Namespace)
	}
	if opts.Lang != "en" {
		t.Fatalf("expected explicit lang to win, got %q", opts.Lang)
	}
	if opts.Events.Limit != 3 {
		t.Fatalf("expected events from config, got %d", opts.Events.Limit)
	}
	if opts.HealthScoreMin != 60 {
		t.Fatalf("expected min score from config, got %d", opts.HealthScoreMin)
	}
}

func TestWriteOutputPrometheus(t *testing.T) {
	report := hpaanalysis.ListReport{
		Items: []hpaanalysis.ListItem{
			{Namespace: "default", Name: "web", HealthScore: 75, Current: 3, Desired: 5, Min: 1, Max: 10},
			{Namespace: "prod", Name: "api", HealthScore: 100, Current: 2, Desired: 2, Min: 1, Max: 5},
		},
	}

	var out bytes.Buffer
	if err := writeOutput(&out, "prometheus", "", report, nil); err != nil {
		t.Fatal(err)
	}

	output := out.String()

	// Verify HELP and TYPE comments for each metric appear
	for _, metric := range []string{"hpa_health_score", "hpa_current_replicas", "hpa_desired_replicas", "hpa_min_replicas", "hpa_max_replicas"} {
		help := "# HELP " + metric
		typ := "# TYPE " + metric
		if !strings.Contains(output, help) {
			t.Fatalf("expected %q in prometheus output, got:\n%s", help, output)
		}
		if !strings.Contains(output, typ) {
			t.Fatalf("expected %q in prometheus output, got:\n%s", typ, output)
		}
	}

	// Verify metric values for first item
	if !strings.Contains(output, `hpa_health_score{namespace="default",name="web"} 75`) {
		t.Fatalf("expected health score metric for web, got:\n%s", output)
	}
	if !strings.Contains(output, `hpa_current_replicas{namespace="default",name="web"} 3`) {
		t.Fatalf("expected current replicas metric for web, got:\n%s", output)
	}
	if !strings.Contains(output, `hpa_max_replicas{namespace="default",name="web"} 10`) {
		t.Fatalf("expected max replicas metric for web, got:\n%s", output)
	}

	// Verify metric values for second item
	if !strings.Contains(output, `hpa_health_score{namespace="prod",name="api"} 100`) {
		t.Fatalf("expected health score metric for api, got:\n%s", output)
	}
	if !strings.Contains(output, `hpa_desired_replicas{namespace="prod",name="api"} 2`) {
		t.Fatalf("expected desired replicas metric for api, got:\n%s", output)
	}
}

func TestWriteOutputPrometheusStatusReport(t *testing.T) {
	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{
			Namespace:   "staging",
			Name:        "worker",
			HealthScore: 50,
			Current:     4,
			Desired:     8,
			Min:         2,
			Max:         20,
		},
	}

	var out bytes.Buffer
	if err := writeOutput(&out, "prometheus", "", report, nil); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	if !strings.Contains(output, `hpa_health_score{namespace="staging",name="worker"} 50`) {
		t.Fatalf("expected health score metric for worker, got:\n%s", output)
	}
	if !strings.Contains(output, `hpa_desired_replicas{namespace="staging",name="worker"} 8`) {
		t.Fatalf("expected desired replicas metric for worker, got:\n%s", output)
	}
}

func TestWriteOutputPrometheusLabelEscaping(t *testing.T) {
	report := hpaanalysis.ListReport{
		Items: []hpaanalysis.ListItem{
			{Namespace: `team\"a`, Name: `my"hpa`, HealthScore: 90, Current: 1, Desired: 1, Min: 1, Max: 3},
		},
	}

	var out bytes.Buffer
	if err := writeOutput(&out, "prometheus", "", report, nil); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	if !strings.Contains(output, `namespace="team\\\"a"`) {
		t.Fatalf("expected escaped namespace label, got:\n%s", output)
	}
	if !strings.Contains(output, `name="my\"hpa"`) {
		t.Fatalf("expected escaped name label, got:\n%s", output)
	}
}

func TestWriteOutputPrometheusUnknownType(t *testing.T) {
	var out bytes.Buffer
	err := writeOutput(&out, "prometheus", "", "not a report", nil)
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
	if !strings.Contains(err.Error(), "prometheus output requires") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteErrorJSON(t *testing.T) {
	var out bytes.Buffer
	writeError(&out, "json", fmt.Errorf("HPA not found"))
	output := strings.TrimSpace(out.String())
	if !strings.Contains(output, `"error"`) {
		t.Fatalf("expected JSON error key, got %q", output)
	}
	if !strings.Contains(output, "HPA not found") {
		t.Fatalf("expected error message in JSON, got %q", output)
	}
}

func TestWriteErrorYAML(t *testing.T) {
	var out bytes.Buffer
	writeError(&out, "yaml", fmt.Errorf("HPA not found"))
	output := strings.TrimSpace(out.String())
	if !strings.Contains(output, "error:") {
		t.Fatalf("expected YAML error key, got %q", output)
	}
	if !strings.Contains(output, "HPA not found") {
		t.Fatalf("expected error message in YAML, got %q", output)
	}
}

func TestWriteErrorText(t *testing.T) {
	var out bytes.Buffer
	writeError(&out, "", fmt.Errorf("HPA not found"))
	output := strings.TrimSpace(out.String())
	if !strings.Contains(output, "Error: HPA not found") {
		t.Fatalf("expected plain text error, got %q", output)
	}
}
