package suggestion

import (
	"encoding/json"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestSuggestionJSONRoundTrip(t *testing.T) {
	s := Suggestion{
		Title:         "Increase maxReplicas",
		Description:   "Current maxReplicas is too low for the observed load.",
		Command:       "kubectl patch hpa web -n default --type=merge -p '{\"spec\":{\"maxReplicas\":10}}'",
		Patch:         `{"spec":{"maxReplicas":10}}`,
		Risk:          "low",
		Preconditions: []string{"CPU utilization > 80%"},
		Warnings:      []string{"May increase cloud costs"},
		Apply:         true,
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got Suggestion
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.Title != s.Title {
		t.Errorf("Title = %q, want %q", got.Title, s.Title)
	}
	if got.Description != s.Description {
		t.Errorf("Description = %q, want %q", got.Description, s.Description)
	}
	if got.Command != s.Command {
		t.Errorf("Command = %q, want %q", got.Command, s.Command)
	}
	if got.Patch != s.Patch {
		t.Errorf("Patch = %q, want %q", got.Patch, s.Patch)
	}
	if got.Risk != s.Risk {
		t.Errorf("Risk = %q, want %q", got.Risk, s.Risk)
	}
	if len(got.Preconditions) != 1 || got.Preconditions[0] != s.Preconditions[0] {
		t.Errorf("Preconditions = %v, want %v", got.Preconditions, s.Preconditions)
	}
	if len(got.Warnings) != 1 || got.Warnings[0] != s.Warnings[0] {
		t.Errorf("Warnings = %v, want %v", got.Warnings, s.Warnings)
	}
	if got.Apply != s.Apply {
		t.Errorf("Apply = %v, want %v", got.Apply, s.Apply)
	}
}

func TestSuggestionJSONOmitEmpty(t *testing.T) {
	s := Suggestion{Title: "minimal"}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	for _, key := range []string{"command", "patch", "risk", "preconditions", "warnings", "apply"} {
		if _, ok := raw[key]; ok {
			t.Errorf("expected %q to be omitted from JSON, but it was present", key)
		}
	}
	if raw["title"] != "minimal" {
		t.Errorf("title = %v, want %q", raw["title"], "minimal")
	}
}

func TestSuggestionYAMLRoundTrip(t *testing.T) {
	s := Suggestion{
		Title:       "Set behavior",
		Description: "Add scale-down stabilization.",
		Patch:       `{"spec":{"behavior":{"scaleDown":{"stabilizationWindowSeconds":300}}}}`,
		Risk:        "medium",
	}

	data, err := yaml.Marshal(s)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}

	var got Suggestion
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	if got.Title != s.Title {
		t.Errorf("Title = %q, want %q", got.Title, s.Title)
	}
	if got.Patch != s.Patch {
		t.Errorf("Patch = %q, want %q", got.Patch, s.Patch)
	}
}

func TestGuardResultJSONRoundTrip(t *testing.T) {
	gr := GuardResult{
		Allowed: []Suggestion{{Title: "ok", Risk: "low"}},
		Blocked: []GuardBlocked{{
			Suggestion: Suggestion{Title: "blocked", Patch: "{}"},
			Reason:     "exceeds max replica limit",
			PolicyRule: "max-replicas",
		}},
		Warnings: []GuardWarning{{
			Suggestion: Suggestion{Title: "warned"},
			Reason:     "cost impact",
			PolicyRule: "budget",
		}},
	}

	data, err := json.Marshal(gr)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got GuardResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if len(got.Allowed) != 1 || got.Allowed[0].Title != "ok" {
		t.Errorf("Allowed = %v, want 1 entry with title %q", got.Allowed, "ok")
	}
	if len(got.Blocked) != 1 || got.Blocked[0].Reason != "exceeds max replica limit" {
		t.Errorf("Blocked = %v, want 1 entry", got.Blocked)
	}
	if len(got.Warnings) != 1 || got.Warnings[0].PolicyRule != "budget" {
		t.Errorf("Warnings = %v, want 1 entry", got.Warnings)
	}
}

func TestGuardResultOmitEmpty(t *testing.T) {
	gr := GuardResult{}

	data, err := json.Marshal(gr)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	for _, key := range []string{"allowed", "blocked", "warnings"} {
		if _, ok := raw[key]; ok {
			t.Errorf("expected %q to be omitted from empty GuardResult", key)
		}
	}
}
