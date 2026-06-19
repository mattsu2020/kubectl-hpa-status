package i18n

import (
	"sort"
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
	if b["label_target"] != "対象" {
		t.Errorf("expected label_target=対象, got %q", b["label_target"])
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
	if got != "対象" {
		t.Errorf("expected '対象', got %q", got)
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
		"dir_at_min", "dir_at_min_scale_to_zero", "dir_unchanged",
		"dir_no_recommendation", "dir_inactive", "dir_scale_to_zero",
		"dir_scaled_to_zero", "dir_unavailable",
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

// TestLocaleKeyParity asserts every key present in en.yaml is also present in
// every other locale, and vice versa. This guards against silent
// key-return-on-missing (i18n.go Get fallback) regressions when a new key is
// added to one locale but not the others.
func TestLocaleKeyParity(t *testing.T) {
	en := Load("en")
	ja := Load("ja")
	enKeys := sortedKeys(en)
	jaKeys := sortedKeys(ja)
	if len(enKeys) != len(jaKeys) {
		t.Fatalf("locale key count mismatch: en=%d ja=%d", len(enKeys), len(jaKeys))
	}
	for i, k := range enKeys {
		if jaKeys[i] != k {
			t.Errorf("locale key mismatch at index %d: en=%q ja=%q", i, k, jaKeys[i])
		}
	}
}

// TestDirectionKeysExistInAllLocales asserts every dir_* key produced by
// pkg/hpa.SummarizeDirectionWithKey resolves in every loaded locale. This
// locks the contract between pkg/hpa (key producer) and the locale files
// (key consumer) so a key rename cannot silently fall back to the raw key
// string in user output.
func TestDirectionKeysExistInAllLocales(t *testing.T) {
	directionKeys := []string{
		"dir_scale_up", "dir_scale_down", "dir_at_max",
		"dir_at_min", "dir_at_min_scale_to_zero", "dir_unchanged",
		"dir_no_recommendation", "dir_inactive", "dir_scale_to_zero",
		"dir_scaled_to_zero", "dir_unavailable",
	}
	for _, lang := range []string{"en", "ja"} {
		b := Load(lang)
		for _, key := range directionKeys {
			v, ok := b[key]
			if !ok {
				t.Errorf("direction key %q missing from %s locale", key, lang)
				continue
			}
			if v == "" {
				t.Errorf("direction key %q is empty in %s locale", key, lang)
			}
			// Get must never return the key itself for a direction key, which
			// would indicate a missing-lookup regression.
			if got := Get(lang, key); got == key {
				t.Errorf("Get(%q, %q) returned the key unchanged; locale entry is missing", lang, key)
			}
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestAllLabelKeysExistInBothLocales(t *testing.T) {
	en := Load("en")
	ja := Load("ja")
	labelKeys := []string{
		"label_target", "label_replicas", "label_health", "label_summary",
		"label_conditions", "label_metrics", "label_behavior", "label_actions",
		"label_suggestions", "label_fix", "label_interpretation", "label_debug",
		"label_keda", "label_events", "label_risk", "label_precondition",
		"label_warning", "label_metrics_diagnostics",
	}
	for _, key := range labelKeys {
		if _, ok := en[key]; !ok {
			t.Errorf("expected key %q in English bundle", key)
		}
		if _, ok := ja[key]; !ok {
			t.Errorf("expected key %q in Japanese bundle", key)
		}
	}
}
