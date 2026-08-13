package hpa

// MetricHint holds a troubleshooting hint for a custom or external metric issue.
type MetricHint struct {
	// identity distinguishes selector/container/object variants internally.
	// It stays out of JSON/YAML to preserve the established wire format.
	identity     MetricID
	MetricType   string   `json:"metricType" yaml:"metricType"`
	MetricName   string   `json:"metricName" yaml:"metricName"`
	Pattern      string   `json:"pattern" yaml:"pattern"`
	Severity     string   `json:"severity" yaml:"severity"`
	Title        string   `json:"title" yaml:"title"`
	Description  string   `json:"description" yaml:"description"`
	Checks       []string `json:"checks,omitempty" yaml:"checks,omitempty"`
	CommonCauses []string `json:"commonCauses,omitempty" yaml:"commonCauses,omitempty"`
	FixSteps     []string `json:"fixSteps,omitempty" yaml:"fixSteps,omitempty"`
}

// MetricHintsReport holds the complete metric hints analysis for an HPA.
type MetricHintsReport struct {
	Namespace            string                      `json:"namespace" yaml:"namespace"`
	Name                 string                      `json:"name" yaml:"name"`
	Hints                []MetricHint                `json:"hints,omitempty" yaml:"hints,omitempty"`
	Summary              string                      `json:"summary" yaml:"summary"`
	TroubleshootingFlows []MetricHintTroubleshooting `json:"troubleshootingFlows,omitempty" yaml:"troubleshootingFlows,omitempty"`
}
