// Package patch provides utilities for merging JSON merge patches.
// It supports deep merging of nested maps while using last-value-wins
// for scalar conflicts at the same path.
package patch

import (
	"encoding/json"
)

// Patch represents a single JSON merge patch with its title for identification.
type Patch struct {
	Title string // Human-readable title for the patch (e.g., "Set minReplicas to 3")
	JSON  string // Raw JSON merge patch (RFC 7396)
}

// MergePatches combines multiple JSON merge patches into a single patch.
// It performs deep merging of nested maps (e.g., spec.behavior.scaleDown),
// while using last-value-wins for scalar conflicts at the same path.
//
// Merge behavior:
// - Nested maps are merged recursively (deep merge)
// - Scalar values at the same path: last value wins (no error returned)
// - Invalid JSON in any patch causes an error
//
// Parameters:
//   - patches: Slice of patches to merge. Order matters for scalar conflicts (last wins).
//
// Returns:
//   - A single JSON merge patch string that combines all inputs.
//   - An error if any patch contains invalid JSON.
//
// Example:
//
//	patches := []patch.Patch{
//	    {Title: "set min", JSON: `{"spec": {"minReplicas": 2}}`},
//	    {Title: "set max", JSON: `{"spec": {"maxReplicas": 10}}`},
//	}
//	merged, err := patch.MergePatches(patches)
//	// merged == `{"spec":{"maxReplicas":10,"minReplicas":2}}`
func MergePatches(patches []Patch) (string, error) {
	if len(patches) == 0 {
		return "{}", nil
	}
	if len(patches) == 1 {
		return patches[0].JSON, nil
	}

	merged := map[string]any{}
	for _, p := range patches {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(p.JSON), &parsed); err != nil {
			return "", err
		}
		for k, v := range parsed {
			existing, exists := merged[k]
			if !exists {
				merged[k] = v
				continue
			}
			// Deep merge nested maps (e.g., spec.behavior.scaleDown).
			existingMap, ok1 := existing.(map[string]any)
			vMap, ok2 := v.(map[string]any)
			if ok1 && ok2 {
				merged[k] = DeepMerge(existingMap, vMap)
				continue
			}
			// Last value wins for scalar conflicts.
			merged[k] = v
		}
	}
	data, err := json.Marshal(merged)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// DeepMerge recursively merges two maps. Both input maps remain unchanged.
// For conflicting keys, values from map 'b' take precedence (last-value-wins).
// Nested maps are merged recursively; all other values use 'b's value.
//
// Parameters:
//   - a: First map (base values)
//   - b: Second map (override values)
//
// Returns:
//   - A new map containing the merged result. Neither 'a' nor 'b' is modified.
//
// Example:
//
//	a := map[string]any{"spec": map[string]any{"minReplicas": 1}}
//	b := map[string]any{"spec": map[string]any{"maxReplicas": 10}}
//	result := patch.DeepMerge(a, b)
//	// result == map[string]any{"spec": map[string]any{"maxReplicas":10,"minReplicas":1}}
func DeepMerge(a, b map[string]any) map[string]any {
	result := make(map[string]any, len(a)+len(b))
	// Copy all keys from 'a' (immutability: don't modify 'a')
	for k, v := range a {
		result[k] = v
	}
	// Merge keys from 'b', with deep merge for nested maps
	for k, v := range b {
		existing, exists := result[k]
		if !exists {
			result[k] = v
			continue
		}
		// Both are maps: deep merge recursively
		aMap, ok1 := existing.(map[string]any)
		bMap, ok2 := v.(map[string]any)
		if ok1 && ok2 {
			result[k] = DeepMerge(aMap, bMap)
			continue
		}
		// Scalar or type mismatch: 'b' wins (last-value-wins)
		result[k] = v
	}
	return result
}
