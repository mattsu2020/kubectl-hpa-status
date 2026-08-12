// Package metricsapi contains the canonical Kubernetes metrics API vocabulary
// shared by discovery, diagnostics, snapshots, and metric collectors.
package metricsapi

const (
	// Resource identifies the resource metrics API.
	Resource = "metrics.k8s.io"
	// Custom identifies the custom metrics API.
	Custom = "custom.metrics.k8s.io"
	// External identifies the external metrics API.
	External = "external.metrics.k8s.io"
)

// GroupVersions returns supported group versions in preference order.
func GroupVersions(source string) []string {
	switch source {
	case Resource:
		return []string{Resource + "/v1beta1"}
	case Custom:
		return []string{Custom + "/v1beta2", Custom + "/v1beta1"}
	case External:
		return []string{External + "/v1beta1"}
	default:
		return nil
	}
}

// Sources returns every supported metrics API source.
func Sources() []string { return []string{Resource, Custom, External} }
