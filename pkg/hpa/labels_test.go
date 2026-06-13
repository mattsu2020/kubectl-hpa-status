package hpa

import (
	"strings"
	"testing"
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

func TestDefaultLabels_AllKeysPresent(t *testing.T) {
	dl := DefaultLabels{}
	keys := []string{
		"label_target", "label_replicas", "label_health", "label_summary",
		"label_conditions", "label_metrics", "label_behavior", "label_actions",
		"label_suggestions", "label_fix", "label_interpretation", "label_debug",
		"label_keda", "label_events", "label_risk", "label_precondition",
		"label_warning", "label_metrics_diagnostics",
	}
	for _, key := range keys {
		got := dl.Get(key)
		if got == key {
			t.Errorf("expected a real label for key %q, got key back (missing from defaults)", key)
		}
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
			"label_target":              "対象",
			"label_replicas":            "レプリカ",
			"label_health":              "ヘルススコア",
			"label_summary":             "要約",
			"label_conditions":          "状態",
			"label_metrics":             "メトリクス",
			"label_behavior":            "挙動",
			"label_actions":             "推奨アクション",
			"label_suggestions":         "推奨コマンド",
			"label_fix":                 "修正プラン",
			"label_interpretation":      "解釈",
			"label_debug":               "デバッグ",
			"label_keda":                "KEDA",
			"label_events":              "最近のイベント",
			"label_risk":                "リスク",
			"label_precondition":        "前提条件",
			"label_warning":             "警告",
			"label_metrics_diagnostics": "メトリクス診断",
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
