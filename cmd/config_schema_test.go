package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// TestConfigExampleCoversAllConfigFields is the config/CLI-flag diff detector.
// It loads docs/config-example.yaml and asserts every field of configFile has
// a matching (commented or active) YAML key in the example. This catches the
// "flag exists in code but is absent from the documented config" drift without
// a golden-file dependency. When a new config field is added, this test fails
// until the example documents it.
//
// Fields explicitly excluded from the example are listed in configExampleSkip
// (none currently; the example aims to be exhaustive).
func TestConfigExampleCoversAllConfigFields(t *testing.T) {
	examplePath := configExamplePath(t)
	data, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("read %s: %v", examplePath, err)
	}
	example := string(data)

	skip := configExampleSkip()
	typ := reflect.TypeOf(configFile{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		yamlKey := field.Tag.Get("yaml")
		if yamlKey == "" || yamlKey == "-" {
			continue
		}
		// The yaml tag may carry omitempty; take the key before any comma.
		if idx := strings.Index(yamlKey, ","); idx >= 0 {
			yamlKey = yamlKey[:idx]
		}
		if skip[yamlKey] {
			continue
		}
		// The example documents most keys commented out; match the key with a
		// trailing colon so "keda" does not match "kedaFoo".
		needle := yamlKey + ":"
		if !strings.Contains(example, needle) {
			t.Errorf("config field %q (yaml key %q) is not documented in %s; add it to the example so the config schema stays in sync with the code", field.Name, yamlKey, examplePath)
		}
	}
}

// configExampleSkip returns config-file keys intentionally absent from the
// example. Keep this empty; the example should be exhaustive. It exists so a
// deliberate omission is explicit rather than silent.
func configExampleSkip() map[string]bool {
	return map[string]bool{}
}

// configExamplePath resolves the repo-relative docs/config-example.yaml path
// regardless of the test working directory, using runtime.Caller to find the
// source file's package directory.
func configExamplePath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate test source file")
	}
	// thisFile is .../cmd/config_schema_test.go; the repo root is two parents up.
	repoRoot := filepath.Dir(filepath.Dir(thisFile))
	return filepath.Join(repoRoot, "docs", "config-example.yaml")
}
