// Package i18n provides internationalization support for kubectl-hpa-status
// using embedded locale files.
package i18n

import (
	"embed"
	"log"
	"path"
	"strings"
	"sync"

	"sigs.k8s.io/yaml"
)

//go:embed locales/*.yaml
var localeFS embed.FS

var (
	once    sync.Once
	bundles map[string]map[string]string
)

// Load returns the message bundle for the given language (e.g., "en", "ja").
// Falls back to "en" if the language is not found. The returned map is a
// shallow copy: mutating it cannot corrupt the process-wide bundle other
// goroutines and the alt-screen TUI read from.
func Load(lang string) map[string]string {
	b := bundleFor(lang)
	if len(b) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(b))
	for k, v := range b {
		out[k] = v
	}
	return out
}

// Get returns a message for the given language and key without allocating.
func Get(lang, key string) string {
	if msg, ok := bundleFor(lang)[key]; ok {
		return msg
	}
	if msg, ok := bundles["en"][key]; ok {
		return msg
	}
	return key
}

func bundleFor(lang string) map[string]string {
	once.Do(func() {
		bundles = loadAllBundles()
	})
	normalized := normalizeLangCode(lang)
	if candidate, ok := bundles[normalized]; ok {
		return candidate
	}
	if base, _, ok := strings.Cut(normalized, "-"); ok {
		if candidate, ok := bundles[base]; ok {
			return candidate
		}
	}
	return bundles["en"]
}

func normalizeLangCode(lang string) string {
	lang = strings.TrimSpace(lang)
	if base, _, ok := strings.Cut(lang, "."); ok {
		lang = base
	}
	lang = strings.ReplaceAll(lang, "_", "-")
	return strings.ToLower(lang)
}

// loadAllBundles scans the embedded locales directory and builds a bundle per
// language file. The filename (without extension) is used as the language code,
// so new locales dropped into locales/*.yaml are picked up automatically.
// "en" is always available as the fallback.
func loadAllBundles() map[string]map[string]string {
	result := make(map[string]map[string]string)

	entries, err := localeFS.ReadDir("locales")
	if err != nil {
		// Ensure the fallback bundle exists even on read failure. This cannot
		// happen with a valid embed directive, but stay defensive.
		log.Printf("i18n: reading embedded locales directory: %v", err)
		result["en"] = map[string]string{}
		return result
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") {
			continue
		}
		lang := strings.TrimSuffix(name, ".yaml")
		if lang == "" {
			continue
		}
		result[lang] = loadYAML(path.Join("locales", name))
	}

	// Guarantee the fallback bundle is present and backfill missing keys in non-en locales.
	enBundle, ok := result["en"]
	if !ok {
		log.Printf("i18n: no en.yaml locale found; all messages will fall back to their keys")
		enBundle = map[string]string{}
		result["en"] = enBundle
	}
	for lang, bundle := range result {
		if lang == "en" {
			continue
		}
		for k, v := range enBundle {
			if _, exists := bundle[k]; !exists {
				bundle[k] = v
			}
		}
	}
	return result
}

// loadYAML parses one locale file. A corrupt file degrades to an empty bundle
// (keys fall back to themselves) but emits a diagnostic on stderr so the
// breakage is visible instead of silently stripping every translation.
// Non-string values are reported and skipped rather than dropped quietly.
func loadYAML(locPath string) map[string]string {
	data, err := localeFS.ReadFile(locPath)
	if err != nil {
		log.Printf("i18n: reading locale %s: %v", locPath, err)
		return map[string]string{}
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		log.Printf("i18n: parsing locale %s: %v (all %s messages will fall back to their keys)", locPath, err, strings.TrimSuffix(path.Base(locPath), ".yaml"))
		return map[string]string{}
	}

	result := make(map[string]string, len(raw))
	for k, v := range raw {
		if s, ok := v.(string); ok {
			result[k] = strings.TrimSpace(s)
			continue
		}
		log.Printf("i18n: locale %s: key %q has non-string value %T; skipping", locPath, k, v)
	}
	return result
}
