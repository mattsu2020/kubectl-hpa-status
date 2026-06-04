package cmd

import (
	"fmt"
	"strings"
)

// parseSimulateOverrides converts --simulate key=value flags into a map.
func parseSimulateOverrides(raw []string) (map[string]string, error) {
	overrides := make(map[string]string, len(raw))
	for _, r := range raw {
		key, value, ok := strings.Cut(r, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --simulate %q; expected key=value format (e.g. maxReplicas=20)", r)
		}
		if key == "" {
			return nil, fmt.Errorf("empty key in --simulate %q", r)
		}
		overrides[key] = value
	}
	return overrides, nil
}
