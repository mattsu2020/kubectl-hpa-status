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

// parseSimulateMetricOverrides parses --simulate-metric flag values into a map.
// Format: metricName=value (e.g. cpu=80%, memory=4Gi, http_requests=500)
func parseSimulateMetricOverrides(pairs []string) (map[string]string, error) {
	overrides := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid metric override %q: expected name=value format", pair)
		}
		if parts[0] == "" {
			return nil, fmt.Errorf("empty metric name in override %q", pair)
		}
		overrides[parts[0]] = parts[1]
	}
	return overrides, nil
}
