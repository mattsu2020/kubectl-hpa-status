package cmd

import (
	"fmt"
	"strings"
)

// parseKeyValuePairs parses name=value pairs for the flag identified by
// flagLabel, without normalizing case or surrounding whitespace; values may
// be empty. Commands whose keys are case-insensitive or whose values must be
// non-empty use parseNormalizedPairs instead.
func parseKeyValuePairs(pairs []string, flagLabel string) (map[string]string, error) {
	overrides := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("invalid %s %q: expected name=value format", flagLabel, pair)
		}
		if key == "" {
			return nil, fmt.Errorf("empty name in %s %q", flagLabel, pair)
		}
		overrides[key] = value
	}
	return overrides, nil
}

// parseNormalizedPairs parses strict name=value pairs for the flag identified
// by flagLabel: names are lower-cased and trimmed, values are trimmed, and
// both must be non-empty.
func parseNormalizedPairs(pairs []string, flagLabel string) (map[string]string, error) {
	overrides := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("invalid %s %q: expected name=value format", flagLabel, pair)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return nil, fmt.Errorf("invalid %s %q: name and value must be non-empty", flagLabel, pair)
		}
		overrides[key] = value
	}
	return overrides, nil
}
