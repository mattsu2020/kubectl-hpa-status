package cmd

// parseSimulateOverrides converts --simulate key=value flags into a map.
func parseSimulateOverrides(raw []string) (map[string]string, error) {
	return parseKeyValuePairs(raw, "--simulate")
}

// parseSimulateMetricOverrides parses --simulate-metric flag values into a map.
// Format: metricName=value (e.g. cpu=80%, memory=4Gi, http_requests=500)
func parseSimulateMetricOverrides(pairs []string) (map[string]string, error) {
	return parseKeyValuePairs(pairs, "--simulate-metric")
}
