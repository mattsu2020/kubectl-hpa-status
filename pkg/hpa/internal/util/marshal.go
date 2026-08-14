// Package util holds small, dependency-free helpers shared across the pkg/hpa
// analysis domains.
package util

import (
	"encoding/json"
	"fmt"
)

// MarshalJSON serialises an internal patch value and returns the JSON string.
// It returns an error if the value cannot be marshalled.
func MarshalJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal internal JSON patch: %w", err)
	}
	return string(data), nil
}

// MustMarshalJSON serialises an internal patch value. Patch builders only pass
// JSON-compatible maps; panicking on a programmer error is safer than silently
// returning a valid-looking no-op patch.
func MustMarshalJSON(value any) string {
	s, err := MarshalJSON(value)
	if err != nil {
		panic(err)
	}
	return s
}
