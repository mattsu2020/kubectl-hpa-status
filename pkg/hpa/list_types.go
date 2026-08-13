package hpa

import (
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ListItem is a compact row representation for list output.
type ListItem struct {
	Namespace string `json:"namespace" yaml:"namespace"`
	Name      string `json:"name" yaml:"name"`
	Target    string `json:"target" yaml:"target"`
	Current   int32  `json:"currentReplicas" yaml:"currentReplicas"`
	Desired   int32  `json:"desiredReplicas" yaml:"desiredReplicas"`
	Min       int32  `json:"minReplicas" yaml:"minReplicas"`
	Max       int32  `json:"maxReplicas" yaml:"maxReplicas"`
	Summary   string `json:"summary" yaml:"summary"`
	// SummaryKey mirrors Analysis.SummaryKey for locale lookup; empty when
	// Summary was overwritten outside SummarizeDirection.
	SummaryKey        string      `json:"summaryKey,omitempty" yaml:"summaryKey,omitempty"`
	Health            string      `json:"health" yaml:"health"`
	HealthScore       int         `json:"healthScore" yaml:"healthScore"`
	Issue             string      `json:"issue,omitempty" yaml:"issue,omitempty"`
	Metrics           string      `json:"metrics,omitempty" yaml:"metrics,omitempty"`
	Behavior          string      `json:"behavior,omitempty" yaml:"behavior,omitempty"`
	Conditions        string      `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	CreationTimestamp metav1.Time `json:"creationTimestamp,omitempty" yaml:"creationTimestamp,omitempty"`
	// Stabilizing is true when StabilizationRemaining > 0.
	Stabilizing bool `json:"stabilizing,omitempty" yaml:"stabilizing,omitempty"`
	// StabilizationLabel is a human-readable countdown like "4m12s".
	StabilizationLabel string `json:"stabilizationLabel,omitempty" yaml:"stabilizationLabel,omitempty"`
	// ChurnLevel is the churn severity (LOW/MEDIUM/HIGH/CRITICAL) if churn was detected.
	ChurnLevel string `json:"churnLevel,omitempty" yaml:"churnLevel,omitempty"`
	// ChurnScore is the numeric churn score 0-100.
	ChurnScore int `json:"churnScore,omitempty" yaml:"churnScore,omitempty"`
	// TrendSparkline is a pre-formatted sparkline showing health score trend.
	TrendSparkline string `json:"trendSparkline,omitempty" yaml:"trendSparkline,omitempty"`
	// TrendFlapping indicates whether flapping was detected in health history.
	TrendFlapping bool `json:"trendFlapping,omitempty" yaml:"trendFlapping,omitempty"`
}

// ListReport holds the list of HPA items for table output.
type ListReport struct {
	// APIVersion identifies the JSON/YAML schema version (see SchemaVersion).
	APIVersion  string              `json:"apiVersion" yaml:"apiVersion"`
	Items       []ListItem          `json:"items" yaml:"items"`
	GitOpsDrift []GitOpsDriftSignal `json:"gitOpsDrift,omitempty" yaml:"gitOpsDrift,omitempty"`
}

// GitOpsDriftSignal describes an HPA that appears to be GitOps-managed and
// should be compared against the declared Git manifest.
type GitOpsDriftSignal struct {
	Namespace string   `json:"namespace" yaml:"namespace"`
	Name      string   `json:"name" yaml:"name"`
	Tool      string   `json:"tool" yaml:"tool"`
	Evidence  []string `json:"evidence,omitempty" yaml:"evidence,omitempty"`
	Advice    string   `json:"advice" yaml:"advice"`
}

// ListTextOptions configures list output with wide, color, language, and theme.
type ListTextOptions struct {
	Wide   bool
	Color  bool
	Lang   string
	Labels LabelProvider // When nil, English defaults are used. Takes precedence over Lang.
	// Theme takes precedence over Color. When Theme is set, Color is ignored.
	Theme style.Theme
	// SummaryTranslator, when non-nil, localises the per-row Summary line
	// (populated from Analysis.Summary via NewListItem). It receives the
	// English summary text and the stable SummaryKey (see
	// StatusTextOptions.SummaryTranslator for the contract). pkg/hpa cannot
	// import internal/i18n, so the cmd layer injects i18n.Get here, mirroring
	// StatusTextOptions.SummaryTranslator. When nil, Summary is rendered
	// verbatim (English).
	SummaryTranslator func(summary, key string) string
}

// translateSummary applies opts.SummaryTranslator when configured, returning
// the original string otherwise.
func (o ListTextOptions) translateSummary(s, key string) string {
	if o.SummaryTranslator != nil {
		return o.SummaryTranslator(s, key)
	}
	return s
}

func (o ListTextOptions) theme() style.Theme {
	if o.Theme.Enabled() || !o.Color {
		return o.Theme
	}
	return style.NewTheme(true)
}
