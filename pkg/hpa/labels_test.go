package hpa

import (
	"sort"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/i18n"
)

func TestDefaultLabels_Get(t *testing.T) {
	dl := DefaultLabels{}
	got := dl.Get("label_target")
	if got != "Target" {
		t.Errorf("expected 'Target', got %q", got)
	}
}

func TestDefaultLabels_UnknownKey(t *testing.T) {
	dl := DefaultLabels{}
	got := dl.Get("nonexistent_key")
	if got != "nonexistent_key" {
		t.Errorf("expected key as fallback, got %q", got)
	}
}

type mockLabelProvider struct {
	values map[string]string
}

func (m mockLabelProvider) Get(key string) string {
	if v, ok := m.values[key]; ok {
		return v
	}
	return key
}

func TestResolveLabels_NilProvider(t *testing.T) {
	labels := resolveLabels(nil)
	if labels.Target != "Target" {
		t.Errorf("expected default English 'Target', got %q", labels.Target)
	}
	if labels.Health != "Health score" {
		t.Errorf("expected default English 'Health score', got %q", labels.Health)
	}
}

func TestResolveLabels_CustomProvider(t *testing.T) {
	mock := mockLabelProvider{
		values: map[string]string{
			"label_target": "Ziel",
			"label_health": "Gesundheit",
		},
	}
	labels := resolveLabels(mock)
	if labels.Target != "Ziel" {
		t.Errorf("expected custom 'Ziel', got %q", labels.Target)
	}
	if labels.Health != "Gesundheit" {
		t.Errorf("expected custom 'Gesundheit', got %q", labels.Health)
	}
	// Keys not in mock should fall back to the key itself
	if !strings.Contains(labels.Replicas, "label_replicas") {
		// The mock returns the key itself for unknown keys, so resolveLabels
		// will pass that through
		t.Logf("Replicas label for unknown key: %q", labels.Replicas)
	}
}

func TestResolveLabels_JapaneseProvider(t *testing.T) {
	jaProvider := mockLabelProvider{
		values: map[string]string{
			"label_target":  "対象",
			"label_actions": "推奨アクション",
		},
	}
	labels := resolveLabels(jaProvider)
	if labels.Target != "対象" {
		t.Errorf("expected Japanese '対象', got %q", labels.Target)
	}
	if labels.Actions != "推奨アクション" {
		t.Errorf("expected Japanese '推奨アクション', got %q", labels.Actions)
	}
}

type recordingLabelProvider struct {
	keys []string
}

func (p *recordingLabelProvider) Get(key string) string {
	p.keys = append(p.keys, key)
	return key
}

// TestResolvedLabelCatalogIsSynchronized derives the required key catalog by
// exercising resolveLabels itself. This avoids maintaining a second hand-written
// key list and locks the renderer, English defaults, and every locale together.
func TestResolvedLabelCatalogIsSynchronized(t *testing.T) {
	recorder := &recordingLabelProvider{}
	resolveLabels(recorder)

	required := make(map[string]struct{}, len(recorder.keys))
	for _, key := range recorder.keys {
		if _, duplicate := required[key]; duplicate {
			t.Errorf("resolveLabels looks up %q more than once", key)
		}
		required[key] = struct{}{}
	}

	keys := make([]string, 0, len(required))
	for key := range required {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	defaults := DefaultLabels{}
	en := i18n.Load("en")
	ja := i18n.Load("ja")
	for _, key := range keys {
		defaultValue := defaults.Get(key)
		if defaultValue == "" || defaultValue == key {
			t.Errorf("DefaultLabels is missing required key %q", key)
		}
		if got, ok := en[key]; !ok || got == "" {
			t.Errorf("English locale is missing required key %q", key)
		} else if got != defaultValue {
			t.Errorf("English locale %q=%q differs from DefaultLabels value %q", key, got, defaultValue)
		}
		if got, ok := ja[key]; !ok || got == "" {
			t.Errorf("Japanese locale is missing required key %q", key)
		}
	}

	// DefaultLabels also carries a few standalone renderer labels that do not
	// live in the labels struct. They must remain localized as well.
	for key, defaultValue := range defaultLabelValues {
		if got, ok := en[key]; !ok || got == "" {
			t.Errorf("English locale is missing DefaultLabels key %q", key)
		} else if got != defaultValue {
			t.Errorf("English locale %q=%q differs from DefaultLabels value %q", key, got, defaultValue)
		}
		if got, ok := ja[key]; !ok || got == "" {
			t.Errorf("Japanese locale is missing DefaultLabels key %q", key)
		}
	}
}
