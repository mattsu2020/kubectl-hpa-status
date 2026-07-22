package render

import (
	"strings"
	"testing"

	hpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func populatedCapacityContext() *hpa.CapacityContext {
	return &hpa.CapacityContext{
		PendingPods: []hpa.PendingPodInfo{
			{Name: "web-1", Unschedulable: true, Reasons: []string{"Insufficient cpu"}},
		},
		QuotaConstraints: []hpa.QuotaConstraint{
			{Name: "compute-quota", Resource: "cpu", Used: "8", Hard: "10", Message: "near limit"},
		},
		PDBInterference: []hpa.PDBInterference{
			{Name: "web-pdb", Disruption: "0 allowed"},
		},
	}
}

func TestCapacityContextTables(t *testing.T) {
	t.Run("empty context produces no tables", func(t *testing.T) {
		tables := capacityContextTables(&hpa.CapacityContext{})
		if len(tables) != 0 {
			t.Fatalf("expected no tables for empty context, got %d", len(tables))
		}
	})

	t.Run("populated context produces one table per section", func(t *testing.T) {
		tables := capacityContextTables(populatedCapacityContext())
		if len(tables) != 3 {
			t.Fatalf("expected 3 tables, got %d: %+v", len(tables), tables)
		}
		titles := []string{tables[0].title, tables[1].title, tables[2].title}
		want := []string{"Pending Pods", "ResourceQuotas", "PodDisruptionBudgets"}
		for i, w := range want {
			if titles[i] != w {
				t.Errorf("table[%d].title = %q, want %q", i, titles[i], w)
			}
		}
		if len(tables[0].rows) != 1 || tables[0].rows[0][0] != "web-1" {
			t.Errorf("unexpected pending pods rows: %+v", tables[0].rows)
		}
	})
}

func TestWriteMarkdownTable(t *testing.T) {
	var out strings.Builder
	writeMarkdownTable(&out, reportTable{
		title:   "Example",
		headers: []string{"A", "BB"},
		rows:    [][]string{{"1", "2|pipe"}},
	})
	got := out.String()
	for _, want := range []string{"### Example", "| A | BB |", `2\|pipe`} {
		if !strings.Contains(got, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestMarkdownSeparatorRow(t *testing.T) {
	got := markdownSeparatorRow([]string{"A", "BB"})
	want := "|---|---|\n"
	if got != want {
		t.Fatalf("markdownSeparatorRow() = %q, want %q", got, want)
	}
}

func TestWriteHTMLTable(t *testing.T) {
	var out strings.Builder
	writeHTMLTable(&out, reportTable{
		title:   "Example<script>",
		headers: []string{"A"},
		rows:    [][]string{{"<b>1</b>"}},
	})
	got := out.String()
	for _, want := range []string{"<h3>Example&lt;script&gt;</h3>", "<th>A</th>", "&lt;b&gt;1&lt;/b&gt;"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, got)
		}
	}
}
