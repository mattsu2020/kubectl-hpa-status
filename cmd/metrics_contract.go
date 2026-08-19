package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/restmapper"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func newMetricsContractCommand(opts *options) *cobra.Command {
	var generate string

	cmd := &cobra.Command{
		Use:               "contract NAME",
		Short:             "Validate HPA metric API contract and optionally generate test artifacts",
		Long:              "Check that each HPA metric has a reachable API service and current data. Use --generate to produce YAML, Markdown, JUnit XML, or kubectl verification commands.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateMode("--generate", generate, "", "yaml", "markdown", "junit", "commands"); err != nil {
				return err
			}
			return runMetricsContract(cmd.Context(), cmd.OutOrStdout(), opts, args[0], generate)
		},
	}

	cmd.Flags().StringVar(&generate, "generate", "",
		"generate test artifacts: yaml, markdown, junit, commands")

	return cmd
}

func runMetricsContract(ctx context.Context, out io.Writer, opts *options, name string, generate string) error {
	client, hpa, err := lookupHPA(ctx, opts, name)
	if err != nil {
		return err
	}

	input := buildMetricContractInput(ctx, client, hpa)
	report := hpaanalysis.AnalyzeMetricContract(input)

	switch generate {
	case "yaml":
		data, err := hpaanalysis.GenerateContractYAML(report)
		if err != nil {
			return fmt.Errorf("failed to generate YAML: %w", err)
		}
		_, err = out.Write(data)
		return err
	case "markdown":
		data, err := hpaanalysis.GenerateContractMarkdown(report)
		if err != nil {
			return fmt.Errorf("failed to generate Markdown: %w", err)
		}
		_, err = out.Write(data)
		return err
	case "junit":
		data, err := hpaanalysis.GenerateContractJUnit(report)
		if err != nil {
			return fmt.Errorf("failed to generate JUnit XML: %w", err)
		}
		_, err = out.Write(data)
		return err
	case "commands":
		commands := hpaanalysis.GenerateContractCommands(report)
		for _, cmd := range commands {
			_, _ = fmt.Fprintln(out, cmd)
		}
		return nil
	default:
		// Standard output (text, JSON, YAML via --output flag)
		output := metricsContractOutput{
			Namespace: report.Namespace,
			Name:      report.Name,
			Target:    report.Target,
			Contract:  report,
		}

		return renderWithOutput(out, opts, output, func(out io.Writer) error {
			return hpaanalysis.WriteMetricContractText(out, report)
		})

	}
}

// metricsContractOutput wraps the contract report for structured output.
type metricsContractOutput struct {
	Namespace string                            `json:"namespace" yaml:"namespace"`
	Name      string                            `json:"name" yaml:"name"`
	Target    string                            `json:"target" yaml:"target"`
	Contract  *hpaanalysis.MetricContractReport `json:"contract" yaml:"contract"`
}

// buildMetricContractInput builds the input for metrics contract analysis.
func buildMetricContractInput(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) hpaanalysis.MetricContractInput {
	input := hpaanalysis.MetricContractInput{
		Namespace:   hpa.Namespace,
		HPAName:     hpa.Name,
		Target:      fmt.Sprintf("%s/%s", hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name),
		Metrics:     make([]hpaanalysis.MetricContractMetric, 0, len(hpa.Spec.Metrics)),
		APIServices: make(map[string]hpaanalysis.APIServiceStatus),
	}
	resourceAPI := selectMetricAPIService(ctx, client, input.APIServices, "metrics.k8s.io/v1beta1")
	customAPI := selectMetricAPIService(
		ctx,
		client,
		input.APIServices,
		"custom.metrics.k8s.io/v1beta2",
		"custom.metrics.k8s.io/v1beta1",
	)
	externalAPI := selectMetricAPIService(ctx, client, input.APIServices, "external.metrics.k8s.io/v1beta1")

	currentMetricMap := buildCurrentMetricDataMap(hpa)
	objectResourceCache := make(map[string]customMetricResourceResolution)
	var objectResourceResolver *customMetricResourceResolver
	targetSelector := metricTargetSelectorResolution{}
	if hasPodScopedContractMetric(hpa.Spec.Metrics) {
		targetSelector = resolveMetricTargetSelector(ctx, client, hpa)
	}

	for _, m := range hpa.Spec.Metrics {
		var objectResource *customMetricResourceResolution
		if m.Object != nil {
			if objectResourceResolver == nil {
				objectResourceResolver = newCustomMetricResourceResolver(client)
			}
			key := m.Object.DescribedObject.APIVersion + "\x00" + m.Object.DescribedObject.Kind
			resolution, ok := objectResourceCache[key]
			if !ok {
				resolution = objectResourceResolver.Resolve(m.Object.DescribedObject)
				objectResourceCache[key] = resolution
			}
			objectResource = &resolution
		}
		metric := buildMetricContractMetric(m, currentMetricMap, objectResource)
		switch m.Type {
		case autoscalingv2.ResourceMetricSourceType,
			autoscalingv2.ContainerResourceMetricSourceType,
			autoscalingv2.PodsMetricSourceType:
			if m.Type == autoscalingv2.PodsMetricSourceType {
				metric.APIGroup = customAPI
			} else {
				metric.APIGroup = resourceAPI
			}
			metric.TargetSelector = targetSelector.Selector
			if targetSelector.Err != nil {
				metric.TargetSelectorResolutionError = targetSelector.Err.Error()
			}
		case autoscalingv2.ObjectMetricSourceType:
			metric.APIGroup = customAPI
		case autoscalingv2.ExternalMetricSourceType:
			metric.APIGroup = externalAPI
		}
		input.Metrics = append(input.Metrics, metric)
	}

	return input
}

func hasPodScopedContractMetric(metrics []autoscalingv2.MetricSpec) bool {
	for _, metric := range metrics {
		switch metric.Type {
		case autoscalingv2.ResourceMetricSourceType,
			autoscalingv2.ContainerResourceMetricSourceType,
			autoscalingv2.PodsMetricSourceType:
			return true
		}
	}
	return false
}

type metricTargetSelectorResolution struct {
	Selector string
	Err      error
}

func resolveMetricTargetSelector(
	ctx context.Context,
	client *kube.Client,
	hpa *autoscalingv2.HorizontalPodAutoscaler,
) metricTargetSelectorResolution {
	if client == nil || client.Interface == nil {
		return metricTargetSelectorResolution{Err: fmt.Errorf("kubernetes client is unavailable")}
	}
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	if err != nil {
		return metricTargetSelectorResolution{Err: fmt.Errorf("resolve scale target pod selector: %w", err)}
	}
	if info == nil {
		return metricTargetSelectorResolution{
			Err: fmt.Errorf("scale target kind %q is not supported for exact pod-selector verification", hpa.Spec.ScaleTargetRef.Kind),
		}
	}
	if info.Selector == nil {
		return metricTargetSelectorResolution{Err: fmt.Errorf("scale target has no pod selector")}
	}
	parsedSelector, err := metav1.LabelSelectorAsSelector(info.Selector)
	if err != nil {
		return metricTargetSelectorResolution{Err: fmt.Errorf("scale target has an invalid pod selector: %w", err)}
	}
	if parsedSelector.Empty() {
		return metricTargetSelectorResolution{Err: fmt.Errorf("scale target has an empty pod selector")}
	}
	return metricTargetSelectorResolution{Selector: parsedSelector.String()}
}

// buildCurrentMetricDataMap builds a set of "Type/Name" keys for metrics that
// have current data in the HPA status.
func buildCurrentMetricDataMap(hpa *autoscalingv2.HorizontalPodAutoscaler) map[string]bool {
	currentMetricMap := make(map[string]bool)
	for _, m := range hpa.Status.CurrentMetrics {
		value, available := contractMetricStatusValue(m)
		if !available || !contractMetricValueAvailable(value) {
			continue
		}
		key := currentMetricContractIdentity(m)
		if key != "" {
			currentMetricMap[key] = true
		}
	}
	return currentMetricMap
}

func currentMetricContractIdentity(m autoscalingv2.MetricStatus) string {
	switch {
	case m.Resource != nil:
		return fmt.Sprintf("Resource/%s", m.Resource.Name)
	case m.ContainerResource != nil:
		return fmt.Sprintf("ContainerResource/%s/%s", m.ContainerResource.Container, m.ContainerResource.Name)
	case m.Pods != nil:
		return metricContractIdentity("Pods", m.Pods.Metric.Name,
			canonicalMetricSelector(m.Pods.Metric.Selector))
	case m.Object != nil:
		return objectMetricContractIdentity(
			m.Object.Metric.Name,
			canonicalMetricSelector(m.Object.Metric.Selector),
			m.Object.DescribedObject,
		)
	case m.External != nil:
		return metricContractIdentity("External", m.External.Metric.Name,
			canonicalMetricSelector(m.External.Metric.Selector))
	default:
		return ""
	}
}

func contractMetricStatusValue(m autoscalingv2.MetricStatus) (autoscalingv2.MetricValueStatus, bool) {
	switch {
	case m.Resource != nil:
		return m.Resource.Current, true
	case m.ContainerResource != nil:
		return m.ContainerResource.Current, true
	case m.Pods != nil:
		return m.Pods.Current, true
	case m.Object != nil:
		return m.Object.Current, true
	case m.External != nil:
		return m.External.Current, true
	default:
		return autoscalingv2.MetricValueStatus{}, false
	}
}

func contractMetricValueAvailable(value autoscalingv2.MetricValueStatus) bool {
	return value.AverageUtilization != nil || value.AverageValue != nil || value.Value != nil
}

func canonicalMetricSelector(selector *metav1.LabelSelector) string {
	if selector == nil {
		return ""
	}
	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return ""
	}
	return parsed.String()
}

func metricContractIdentity(metricType, name, selector string) string {
	return fmt.Sprintf("%s/%s|selector=%s", metricType, name, selector)
}

func objectMetricContractIdentity(name, selector string, ref autoscalingv2.CrossVersionObjectReference) string {
	return fmt.Sprintf("Object/%s|selector=%s|object=%s/%s/%s",
		name, selector, ref.APIVersion, ref.Kind, ref.Name)
}

type customMetricResourceResolution struct {
	Resource   string
	Namespaced bool
	Err        error
}

type customMetricResourceResolver struct {
	mapper meta.RESTMapper
	err    error
}

// newCustomMetricResourceResolver builds the same discovery-backed RESTMapper
// used by Kubernetes clients. This avoids deriving CRD resource names from
// Kind, which is incorrect for irregular plural forms.
func newCustomMetricResourceResolver(client *kube.Client) *customMetricResourceResolver {
	if client == nil || client.Interface == nil {
		return &customMetricResourceResolver{err: fmt.Errorf("kubernetes discovery client is unavailable")}
	}
	groupResources, err := restmapper.GetAPIGroupResources(client.Interface.Discovery())
	if err != nil {
		return &customMetricResourceResolver{err: fmt.Errorf("build discovery RESTMapper: %w", err)}
	}
	return &customMetricResourceResolver{mapper: restmapper.NewDiscoveryRESTMapper(groupResources)}
}

func (resolver *customMetricResourceResolver) Resolve(ref autoscalingv2.CrossVersionObjectReference) customMetricResourceResolution {
	if resolver == nil || resolver.err != nil {
		err := fmt.Errorf("kubernetes discovery RESTMapper is unavailable")
		if resolver != nil && resolver.err != nil {
			err = resolver.err
		}
		return customMetricResourceResolution{Err: err}
	}
	if strings.TrimSpace(ref.APIVersion) == "" || strings.TrimSpace(ref.Kind) == "" {
		return customMetricResourceResolution{Err: fmt.Errorf("describedObject apiVersion and kind are required")}
	}

	groupVersion, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		return customMetricResourceResolution{Err: fmt.Errorf("invalid describedObject apiVersion %q: %w", ref.APIVersion, err)}
	}
	mapping, err := resolver.mapper.RESTMapping(groupVersion.WithKind(ref.Kind).GroupKind(), groupVersion.Version)
	if err != nil {
		return customMetricResourceResolution{
			Err: fmt.Errorf("map describedObject %s %q to a resource: %w", ref.APIVersion, ref.Kind, err),
		}
	}
	if mapping.Scope == nil {
		return customMetricResourceResolution{
			Err: fmt.Errorf("discovery mapping for %s %q has no resource scope", ref.APIVersion, ref.Kind),
		}
	}
	switch mapping.Scope.Name() {
	case meta.RESTScopeNameNamespace, meta.RESTScopeNameRoot:
	default:
		return customMetricResourceResolution{
			Err: fmt.Errorf("discovery mapping for %s %q has unknown scope %q", ref.APIVersion, ref.Kind, mapping.Scope.Name()),
		}
	}
	return customMetricResourceResolution{
		Resource:   mapping.Resource.GroupResource().String(),
		Namespaced: mapping.Scope.Name() == meta.RESTScopeNameNamespace,
	}
}

// resolveCustomMetricResource is the one-shot form used by focused tests.
func resolveCustomMetricResource(client *kube.Client, ref autoscalingv2.CrossVersionObjectReference) customMetricResourceResolution {
	return newCustomMetricResourceResolver(client).Resolve(ref)
}

// buildMetricContractMetric converts a single HPA spec metric into a contract
// metric and records its hasCurrentData flag against the provided map.
func buildMetricContractMetric(
	m autoscalingv2.MetricSpec,
	currentMetricMap map[string]bool,
	objectResource *customMetricResourceResolution,
) hpaanalysis.MetricContractMetric {
	metric := hpaanalysis.MetricContractMetric{
		Type: string(m.Type),
	}

	switch {
	case m.Resource != nil:
		metric.Name = string(m.Resource.Name)
		metric.APIGroup = "metrics.k8s.io/v1beta1"
	case m.ContainerResource != nil:
		metric.Name = string(m.ContainerResource.Name)
		metric.APIGroup = "metrics.k8s.io/v1beta1"
	case m.Pods != nil:
		metric.Name = m.Pods.Metric.Name
		metric.APIGroup = "custom.metrics.k8s.io/v1beta1"
		metric.Resource = "pods"
		metric.ResourceName = "*"
		metric.ResourceNamespaced = boolPointer(true)
		metric.Selector = canonicalMetricSelector(m.Pods.Metric.Selector)
	case m.Object != nil:
		metric.Name = m.Object.Metric.Name
		metric.APIGroup = "custom.metrics.k8s.io/v1beta1"
		metric.ResourceName = m.Object.DescribedObject.Name
		metric.Selector = canonicalMetricSelector(m.Object.Metric.Selector)
		switch {
		case objectResource == nil:
			metric.ResourceResolutionError = "describedObject resource was not resolved"
		case objectResource.Err != nil:
			metric.ResourceResolutionError = objectResource.Err.Error()
		default:
			metric.Resource = objectResource.Resource
			metric.ResourceNamespaced = boolPointer(objectResource.Namespaced)
		}
	case m.External != nil:
		metric.Name = m.External.Metric.Name
		metric.APIGroup = "external.metrics.k8s.io/v1beta1"
		metric.Selector = canonicalMetricSelector(m.External.Metric.Selector)
	}

	// Check if current data exists
	metricKey := fmt.Sprintf("%s/%s", metric.Type, metric.Name)
	switch {
	case metric.Type == "ContainerResource" && m.ContainerResource != nil:
		metricKey = fmt.Sprintf("%s/%s/%s", metric.Type, m.ContainerResource.Container, metric.Name)
	case m.Pods != nil, m.External != nil:
		metricKey = metricContractIdentity(metric.Type, metric.Name, metric.Selector)
	case m.Object != nil:
		metricKey = objectMetricContractIdentity(metric.Name, metric.Selector, m.Object.DescribedObject)
	}
	metric.HasCurrentData = currentMetricMap[metricKey]
	if identity, err := hpaanalysis.MetricIDFromSpec(m); err == nil {
		metric = hpaanalysis.WithMetricContractIdentity(metric, identity)
	}

	return metric
}

func boolPointer(value bool) *bool {
	return &value
}

// selectMetricAPIService chooses the first served API version and records each
// attempted discovery result. Custom metrics candidates are ordered with
// v1beta2 first, matching the versions supported by the Kubernetes client.
func selectMetricAPIService(
	ctx context.Context,
	client *kube.Client,
	statuses map[string]hpaanalysis.APIServiceStatus,
	candidates ...string,
) string {
	for _, candidate := range candidates {
		status := checkAPIServiceAvailability(ctx, client, candidate)
		statuses[candidate] = status
		if status.Available {
			return candidate
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

// checkAPIServiceAvailability checks if a metrics API is available via discovery.
func checkAPIServiceAvailability(ctx context.Context, client *kube.Client, groupVersion string) hpaanalysis.APIServiceStatus {
	// Respect context cancellation for long-running discovery calls.
	select {
	case <-ctx.Done():
		return hpaanalysis.APIServiceStatus{
			Available: false,
			Message:   "discovery cancelled",
		}
	default:
	}
	_, err := client.Interface.Discovery().ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		return hpaanalysis.APIServiceStatus{
			Available: false,
			Message:   err.Error(),
		}
	}
	return hpaanalysis.APIServiceStatus{
		Available: true,
		Message:   groupVersion,
	}
}
