package i18n

import (
	"strings"
	"testing"
)

func TestLoadEnglish(t *testing.T) {
	b := Load("en")
	if len(b) == 0 {
		t.Fatal("expected non-empty English bundle")
	}
	if b["label_target"] != "Target" {
		t.Errorf("expected label_target=Target, got %q", b["label_target"])
	}
}

func TestLoadJapanese(t *testing.T) {
	b := Load("ja")
	if len(b) == 0 {
		t.Fatal("expected non-empty Japanese bundle")
	}
	if b["label_target"] != "ターゲット" {
		t.Errorf("expected label_target=ターゲット, got %q", b["label_target"])
	}
}

func TestLoadUnknownFallsBackToEnglish(t *testing.T) {
	b := Load("unknown")
	if len(b) == 0 {
		t.Fatal("expected fallback to English bundle")
	}
	if b["label_target"] != "Target" {
		t.Errorf("expected English fallback for label_target, got %q", b["label_target"])
	}
}

func TestGetEnglish(t *testing.T) {
	got := Get("en", "label_target")
	if got != "Target" {
		t.Errorf("expected 'Target', got %q", got)
	}
}

func TestGetJapanese(t *testing.T) {
	got := Get("ja", "label_target")
	if got != "ターゲット" {
		t.Errorf("expected 'ターゲット', got %q", got)
	}
}

func TestGetUnknownKey(t *testing.T) {
	got := Get("en", "nonexistent_key")
	if got != "nonexistent_key" {
		t.Errorf("expected key as fallback, got %q", got)
	}
}

func TestHealthStatesPresent(t *testing.T) {
	for _, lang := range []string{"en", "ja"} {
		b := Load(lang)
		for _, key := range []string{"health_ok", "health_error", "health_limited", "health_stabilized"} {
			if _, ok := b[key]; !ok {
				t.Errorf("expected key %q in %s bundle", key, lang)
			}
		}
	}
}

func TestDirectionKeysPresent(t *testing.T) {
	b := Load("en")
	directions := []string{
		"dir_scale_up", "dir_scale_down", "dir_at_max",
		"dir_at_min", "dir_unchanged", "dir_no_recommendation", "dir_inactive",
	}
	for _, key := range directions {
		if _, ok := b[key]; !ok {
			t.Errorf("expected key %q in English bundle", key)
		}
		if !strings.HasSuffix(b[key], ".") && !strings.Contains(b[key], "maxReplicas") && !strings.Contains(b[key], "minReplicas") {
			t.Errorf("expected %q value to end with period or contain replicas info, got %q", key, b[key])
		}
	}
}
