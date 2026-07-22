package util

// PendingPodInfo describes a single pending pod for a scale target, used by
// capacity, scale-path, and autoscaler-map analysis to explain why pods are
// stuck in Pending. It lives here (rather than in pkg/hpa) so that leaf
// sub-packages such as autoscalermap can reference it without importing
// pkg/hpa, which would create an import cycle back through hpa's own
// white-box test files.
type PendingPodInfo struct {
	Name          string   `json:"name" yaml:"name"`
	Phase         string   `json:"phase" yaml:"phase"`
	Unschedulable bool     `json:"unschedulable" yaml:"unschedulable"`
	Reasons       []string `json:"reasons,omitempty" yaml:"reasons,omitempty"`
}
