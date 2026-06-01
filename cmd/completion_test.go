package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd/api"
)

func TestOutputCompletions(t *testing.T) {
	completions, directive := outputCompletions(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected NoFileComp, got %v", directive)
	}
	expected := []string{"table", "wide", "json", "yaml", "jsonpath=", "template=", "prometheus"}
	for i, exp := range expected {
		if i >= len(completions) {
			t.Errorf("missing completion for %q", exp)
			continue
		}
		if completions[i] == "" {
			t.Errorf("empty completion at index %d", i)
		}
	}
}

func TestFilterCompletions(t *testing.T) {
	completions, directive := filterCompletions(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected NoFileComp, got %v", directive)
	}
	expected := []string{"all", "ok", "error", "limited", "issue"}
	if len(completions) != len(expected) {
		t.Errorf("expected %d completions, got %d", len(expected), len(completions))
	}
	for i, exp := range expected {
		if i < len(completions) && completions[i] == "" {
			t.Errorf("empty completion at index %d", i)
		}
		if i < len(completions) {
			found := false
			for _, c := range completions {
				if len(c) >= len(exp) && c[:len(exp)] == exp {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("missing completion for %q", exp)
			}
		}
	}
}

func TestSortByCompletions(t *testing.T) {
	completions, directive := sortByCompletions(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected NoFileComp, got %v", directive)
	}
	expected := []string{"name", "namespace", "health", "healthscore", "current", "desired", "diff", "age", "issue", "min", "max", "target"}
	if len(completions) != len(expected) {
		t.Errorf("expected %d completions, got %d", len(expected), len(completions))
	}
}

func TestColorCompletions(t *testing.T) {
	completions, directive := colorCompletions(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected NoFileComp, got %v", directive)
	}
	if len(completions) != 3 {
		t.Errorf("expected 3 completions, got %d", len(completions))
	}
}

func TestLangCompletions(t *testing.T) {
	completions, directive := langCompletions(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected NoFileComp, got %v", directive)
	}
	if len(completions) != 2 {
		t.Errorf("expected 2 completions, got %d", len(completions))
	}
}

func TestEventsCompletions(t *testing.T) {
	completions, directive := eventsCompletions(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected NoFileComp, got %v", directive)
	}
	if len(completions) != 2 {
		t.Errorf("expected 2 completions, got %d", len(completions))
	}
}

func TestUntilConditionCompletions(t *testing.T) {
	completions, directive := untilConditionCompletions(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected NoFileComp, got %v", directive)
	}
	if len(completions) != 5 {
		t.Errorf("expected 5 completions, got %d", len(completions))
	}
}

func TestContextNames(t *testing.T) {
	config := &api.Config{
		Contexts: map[string]*api.Context{
			"dev":    {},
			"staging": {},
			"prod":   {},
		},
	}
	names := contextNames(config)
	if len(names) != 3 {
		t.Errorf("expected 3 context names, got %d", len(names))
	}
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	for _, exp := range []string{"dev", "staging", "prod"} {
		if !found[exp] {
			t.Errorf("missing context name %q", exp)
		}
	}
}

func TestContextNamesEmpty(t *testing.T) {
	config := &api.Config{
		Contexts: map[string]*api.Context{},
	}
	names := contextNames(config)
	if len(names) != 0 {
		t.Errorf("expected 0 context names, got %d", len(names))
	}
}
