package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
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
