package hpa

// MetricHintFix represents a single troubleshooting step with an optional
// copy-pasteable command and expected output description.
type MetricHintFix struct {
	StepNumber     int    `json:"stepNumber" yaml:"stepNumber"`
	Description    string `json:"description" yaml:"description"`
	Command        string `json:"command,omitempty" yaml:"command,omitempty"`
	ExpectedOutput string `json:"expectedOutput,omitempty" yaml:"expectedOutput,omitempty"`
	DocsLink       string `json:"docsLink,omitempty" yaml:"docsLink,omitempty"`
}

// MetricHintTroubleshooting holds a structured troubleshooting flow for a
// single metric hint pattern, with step-by-step diagnostic commands.
type MetricHintTroubleshooting struct {
	Pattern    string          `json:"pattern" yaml:"pattern"`
	Severity   string          `json:"severity" yaml:"severity"`
	Title      string          `json:"title" yaml:"title"`
	Steps      []MetricHintFix `json:"steps" yaml:"steps"`
	MetricType string          `json:"metricType" yaml:"metricType"`
	MetricName string          `json:"metricName" yaml:"metricName"`
}

// BuildTroubleshootingFlows converts a slice of MetricHint values into
// structured troubleshooting flows with copy-pasteable diagnostic commands.
// Each hint pattern maps to a predefined set of steps.
func BuildTroubleshootingFlows(hints []MetricHint) []MetricHintTroubleshooting {
	if len(hints) == 0 {
		return nil
	}
	flows := make([]MetricHintTroubleshooting, 0, len(hints))
	for _, hint := range hints {
		flow := buildFlow(hint)
		if flow != nil {
			flows = append(flows, *flow)
		}
	}
	if len(flows) == 0 {
		return nil
	}
	return flows
}

// troubleshootingSteps is the data table behind buildFlow: each supported hint
// pattern maps to its ordered diagnostic steps. Keeping the steps in a table
// (rather than a 7-arm switch of struct literals) makes the steps editable in
// isolation and the supported-pattern set a one-line change.
var troubleshootingSteps = map[string][]MetricHintFix{
	"external-metric-missing": {
		{
			StepNumber:     1,
			Description:    "Check if the external metrics APIService is registered and available",
			Command:        "kubectl get apiservice v1beta1.external.metrics.k8s.io",
			ExpectedOutput: "Status should be Available; check for error messages in the output",
		},
		{
			StepNumber:     2,
			Description:    "Verify the metrics adapter pods are running",
			Command:        "kubectl get pods -n <adapter-namespace> -l app=prometheus-adapter",
			ExpectedOutput: "All pods should show Running status with recent ready timestamps",
		},
		{
			StepNumber:  3,
			Description: "Query Prometheus directly to confirm the metric exists",
			Command:     "kubectl get --raw /apis/external.metrics.k8s.io/v1beta1",
			DocsLink:    "https://github.com/kubernetes-sigs/prometheus-adapter",
		},
		{
			StepNumber:     4,
			Description:    "Check the adapter relabel configuration for the metric name",
			Command:        "kubectl get configmap prometheus-adapter-config -n <adapter-namespace> -o yaml",
			ExpectedOutput: "The seriesQuery or rules should match the metric name used in the HPA spec",
		},
	},
	"external-metric-stale": {
		{
			StepNumber:     1,
			Description:    "Check the metrics adapter logs for scrape or query errors",
			Command:        "kubectl logs -n <adapter-namespace> -l app=prometheus-adapter --tail=50",
			ExpectedOutput: "Look for repeated error lines, connection refused, or timeout messages",
		},
		{
			StepNumber:     2,
			Description:    "Verify the upstream metric source (Prometheus) is healthy",
			Command:        "kubectl get pods -n <prometheus-namespace> -l app=prometheus",
			ExpectedOutput: "Prometheus pods should be Running and Ready",
		},
		{
			StepNumber:     3,
			Description:    "Check the adapter polling interval configuration",
			Command:        "kubectl get deploy prometheus-adapter -n <adapter-namespace> -o jsonpath='{.spec.template.spec.containers[0].args}'",
			ExpectedOutput: "Look for --metrics-relist-interval and --prometheus-url flags",
		},
	},
	"custom-api-service-unavailable": {
		{
			StepNumber:     1,
			Description:    "Install the prometheus-adapter if not already present",
			Command:        "kubectl get deploy -A | grep prometheus-adapter",
			ExpectedOutput: "If empty, install prometheus-adapter via Helm or manifest",
			DocsLink:       "https://github.com/kubernetes-sigs/prometheus-adapter#installation",
		},
		{
			StepNumber:     2,
			Description:    "Check the APIService registration and status",
			Command:        "kubectl get apiservice v1beta1.custom.metrics.k8s.io -o yaml",
			ExpectedOutput: "Status.conditions should show Available=True",
		},
		{
			StepNumber:     3,
			Description:    "Verify the adapter has correct RBAC permissions",
			Command:        "kubectl get clusterrolebinding -o wide | grep prometheus-adapter",
			ExpectedOutput: "The adapter service account should be bound to the system:auth-delegator role",
		},
	},
	"external-api-service-unavailable": {
		{
			StepNumber:     1,
			Description:    "Install a metrics adapter that serves external.metrics.k8s.io",
			Command:        "kubectl get deploy -A | grep -E 'prometheus-adapter|keda'",
			ExpectedOutput: "If empty, install prometheus-adapter or KEDA with external metrics enabled",
			DocsLink:       "https://github.com/kubernetes-sigs/prometheus-adapter#installation",
		},
		{
			StepNumber:     2,
			Description:    "Check the APIService registration and status",
			Command:        "kubectl get apiservice v1beta1.external.metrics.k8s.io -o yaml",
			ExpectedOutput: "Status.conditions should show Available=True; check message for errors",
		},
		{
			StepNumber:     3,
			Description:    "Verify the adapter has correct RBAC permissions for external metrics",
			Command:        "kubectl get clusterrole external-metrics-server-resources -o yaml",
			ExpectedOutput: "Should include rules for external.metrics.k8s.io resources",
		},
	},
	"metric-value-zero": {
		{
			StepNumber:     1,
			Description:    "Verify the application exports the expected metric",
			Command:        "kubectl port-forward <pod-name> <metrics-port> && curl localhost:<metrics-port>/metrics",
			ExpectedOutput: "Look for the metric name with a non-zero value in the output",
		},
		{
			StepNumber:     2,
			Description:    "Check the metric source (e.g., Prometheus) for the metric data",
			Command:        "kubectl get --raw /apis/custom.metrics.k8s.io/v1beta1",
			ExpectedOutput: "The metric should appear with a non-zero value for the matching labels",
		},
		{
			StepNumber:     3,
			Description:    "Verify label selectors match between the HPA spec and the metric source",
			Command:        "kubectl get hpa <hpa-name> -n <namespace> -o jsonpath='{.spec.metrics[*].pods.metric.selector}'",
			ExpectedOutput: "The selector labels must match the labels on the metric series in the source",
		},
	},
	"object-metric-target-not-found": {
		{
			StepNumber:     1,
			Description:    "Verify the referenced object exists in the expected namespace",
			Command:        "kubectl get <kind> <name> -n <namespace>",
			ExpectedOutput: "The object should be found; NotFound indicates it was deleted or misspelled",
		},
		{
			StepNumber:     2,
			Description:    "Check the object kind and name in the HPA spec for typos",
			Command:        "kubectl get hpa <hpa-name> -n <namespace> -o jsonpath='{.spec.metrics[*].object.describedObject}'",
			ExpectedOutput: "Verify kind, name, and apiVersion match an actual cluster resource",
		},
		{
			StepNumber:     3,
			Description:    "Check for cross-namespace references which are not supported",
			Command:        "kubectl get hpa <hpa-name> -n <namespace> -o yaml | grep -A5 describedObject",
			ExpectedOutput: "HPA object metrics must reference objects in the same namespace as the HPA",
		},
	},
	"missing-metric-in-status": {
		{
			StepNumber:     1,
			Description:    "Check if the metrics adapter is running and healthy",
			Command:        "kubectl get pods -n <adapter-namespace> -l app=prometheus-adapter",
			ExpectedOutput: "All adapter pods should be Running and Ready with recent restart counts",
		},
		{
			StepNumber:     2,
			Description:    "Verify the metric name matches the adapter configuration",
			Command:        "kubectl get configmap prometheus-adapter-config -n <adapter-namespace> -o yaml",
			ExpectedOutput: "The rules.seriesQuery or rules.name.matches should produce the metric name used in the HPA",
		},
		{
			StepNumber:     3,
			Description:    "Wait a few reconciliation cycles and re-check HPA status",
			Command:        "kubectl get hpa <hpa-name> -n <namespace> -o jsonpath='{.status.currentMetrics}'",
			ExpectedOutput: "After 1-2 minutes the metric should appear in currentMetrics if the adapter is working",
		},
	},
}

func buildFlow(h MetricHint) *MetricHintTroubleshooting {
	steps, ok := troubleshootingSteps[h.Pattern]
	if !ok {
		return nil
	}
	return &MetricHintTroubleshooting{
		Pattern:    h.Pattern,
		Severity:   h.Severity,
		Title:      h.Title,
		MetricType: h.MetricType,
		MetricName: h.MetricName,
		Steps:      steps,
	}
}
