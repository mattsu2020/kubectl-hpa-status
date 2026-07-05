package lint

import (
	"encoding/json"
	"strings"
)

// FormatLintSARIF formats lint results as SARIF JSON for CI integration.
func FormatLintSARIF(result *Result, filePath string) string {
	// Collect unique rules preserving first-appearance order so the output is
	// deterministic (a map would randomize rule ordering across runs).
	var rules []map[string]any
	seenRules := make(map[string]struct{})
	for _, f := range result.Findings {
		if _, ok := seenRules[f.Rule]; ok {
			continue
		}
		seenRules[f.Rule] = struct{}{}
		rules = append(rules, map[string]any{
			"id": f.Rule,
			"shortDescription": map[string]any{
				"text": f.Rule,
			},
		})
	}

	results := make([]map[string]any, 0, len(result.Findings))
	for _, f := range result.Findings {
		level := sarifLevel(f.Severity)
		results = append(results, map[string]any{
			"ruleId": f.Rule,
			"level":  level,
			"message": map[string]any{
				"text": f.Message,
			},
			"locations": []map[string]any{
				{
					"physicalLocation": map[string]any{
						"artifactLocation": map[string]any{
							"uri": filePath,
						},
					},
				},
			},
		})
	}

	doc := map[string]any{
		"$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		"version": "2.1.0",
		"runs": []map[string]any{
			{
				"tool": map[string]any{
					"driver": map[string]any{
						"name":           "kubectl-hpa-status",
						"informationUri": "https://github.com/mattsu2020/kubectl-hpa-status",
						"rules":          rules,
					},
				},
				"results": results,
			},
		},
	}

	var sb strings.Builder
	encoder := json.NewEncoder(&sb)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(doc); err != nil {
		// Encoding can only fail on unsupported types; the values above are
		// all primitives/maps/slices, so this is unreachable in practice.
		// Fall back to a minimal valid SARIF document to keep the contract.
		return `{"version":"2.1.0","runs":[]}`
	}
	return strings.TrimRight(sb.String(), "\n")
}

// sarifLevel maps a Severity to a SARIF level string.
func sarifLevel(severity Severity) string {
	switch severity {
	case Error:
		return "error"
	case Warning:
		return "warning"
	default:
		return "note"
	}
}
