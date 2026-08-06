package completion

import (
	"testing"

	"k8s.io/client-go/tools/clientcmd/api"
)

func TestContextNames(t *testing.T) {
	config := &api.Config{
		Contexts: map[string]*api.Context{
			"dev":     {},
			"staging": {},
			"prod":    {},
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

func TestStaticCompletionsNonEmpty(t *testing.T) {
	for name, values := range map[string][]completionValue{
		"output":          OutputValues,
		"filter":          FilterValues,
		"sort-by":         SortByValues,
		"color":           ColorValues,
		"lang":            LangValues,
		"events":          EventsValues,
		"until-condition": UntilConditionValues,
	} {
		if len(values) == 0 {
			t.Errorf("%s value list is empty", name)
		}
		for _, v := range values {
			if v.value == "" || v.desc == "" {
				t.Errorf("%s entry has empty value/desc: %+v", name, v)
			}
		}
	}
}
