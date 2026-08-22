package hpa

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// formatMetaTimestamp renders a creation time as the second-precision
// RFC3339 string MetaView stores; the zero time yields the empty string so
// the field omits from serialized output.
func formatMetaTimestamp(t metav1.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// parseMetaTimestamp converts MetaView's RFC3339 creation-timestamp string
// back to metav1.Time for the few consumers (list rules) that need a
// time.Time. An empty or malformed string yields the zero time.
func parseMetaTimestamp(s string) metav1.Time {
	if s == "" {
		return metav1.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return metav1.Time{}
	}
	return metav1.NewTime(t)
}
