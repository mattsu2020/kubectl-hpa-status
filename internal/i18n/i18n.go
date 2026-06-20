// Package i18n provides internationalization support using embedded locale files.
// Package i18n provides internationalization support for kubectl-hpa-status
// using embedded locale files.
package i18n

import (
	"embed"
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
// Falls back to "en" if the language is not found.
func Load(lang string) map[string]string {
	once.Do(func() {
		bundles = loadAllBundles()
	})
	if b, ok := bundles[lang]; ok {
		return b
	}
	return bundles["en"]
}

// Get returns a message for the given language and key.
func Get(lang, key string) string {
	b := Load(lang)
	if msg, ok := b[key]; ok {
		return msg
	}
	return key
}

// loadAllBundles scans the embedded locales directory and builds a bundle per
// language file. The filename (without extension) is used as the language code,
// so new locales dropped into locales/*.yaml are picked up automatically.
// "en" is always available as the fallback.
func loadAllBundles() map[string]map[string]string {
	result := make(map[string]map[string]string)

	entries, err := localeFS.ReadDir("locales")
	if err != nil {
		// Ensure the fallback bundle exists even on read failure.
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

	// Guarantee the fallback bundle is present.
	if _, ok := result["en"]; !ok {
		result["en"] = map[string]string{}
	}
	return result
}

func loadYAML(locPath string) map[string]string {
	data, err := localeFS.ReadFile(locPath)
	if err != nil {
		return map[string]string{}
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return map[string]string{}
	}

	result := make(map[string]string, len(raw))
	for k, v := range raw {
		if s, ok := v.(string); ok {
			result[k] = strings.TrimSpace(s)
		}
	}
	return result
}
