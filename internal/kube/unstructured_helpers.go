package kube

// This file provides small, reusable helpers for traversing the nested
// map[string]any values produced by dynamic/unstructured CRD access. The
// repeated `m["k"].(map[string]any)` / `.([]any)` two-step with ok-checks is
// verbose and error-prone; these helpers centralize the casts.

// nestedMap returns the child map at m[key] and true when present and of the
// right type. Returns nil, false otherwise.
func nestedMap(m map[string]any, key string) (map[string]any, bool) {
	raw, ok := m[key]
	if !ok {
		return nil, false
	}
	child, ok := raw.(map[string]any)
	return child, ok
}

// nestedSlice returns the child []any at m[key] and true when present and of
// the right type. Returns nil, false otherwise.
func nestedSlice(m map[string]any, key string) ([]any, bool) {
	raw, ok := m[key]
	if !ok {
		return nil, false
	}
	child, ok := raw.([]any)
	return child, ok
}

// nestedString returns the string at m[key] when present and a string.
func nestedString(m map[string]any, key string) (string, bool) {
	raw, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := raw.(string)
	return s, ok
}

// mapAt safely type-asserts an arbitrary any to map[string]any.
func mapAt(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}
