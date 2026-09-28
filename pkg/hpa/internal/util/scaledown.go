// Package util holds small, dependency-free helpers shared across the pkg/hpa
// analysis domains.
package util

// ScaleDownBehaviorPatch builds the JSON merge patch that sets the given
// fields under spec.behavior.scaleDown (e.g. stabilizationWindowSeconds,
// selectPolicy, policies). Churn recommendations and the flapping fixes patch
// the same spec path; building it in one place keeps the patch shape — and
// therefore `kubectl patch` compatibility — identical across domains.
func ScaleDownBehaviorPatch(fields map[string]any) string {
	return MustMarshalJSON(map[string]any{
		"spec": map[string]any{
			"behavior": map[string]any{
				"scaleDown": fields,
			},
		},
	})
}
