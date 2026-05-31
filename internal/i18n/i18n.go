package i18n

import (
	"embed"
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
		bundles = make(map[string]map[string]string)
		bundles["en"] = loadYAML("locales/en.yaml")
		bundles["ja"] = loadYAML("locales/ja.yaml")
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

func loadYAML(path string) map[string]string {
	data, err := localeFS.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}

	var raw map[string]interface{}
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
