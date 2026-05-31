package hpa

import (
	"fmt"
	"sort"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const limitation = "[confidence: high] This plugin uses existing HPA status, conditions, metrics, and events. It does not expose internal controller calculations."

type Analysis struct {
	Namespace         string             `json:"namespace" yaml:"namespace"`
	Name              string             `json:"name" yaml:"name"`
	Target            string             `json:"target" yaml:"target"`
	Current           int32              `json:"currentReplicas" yaml:"currentReplicas"`
	Desired           int32              `json:"desiredReplicas" yaml:"desiredReplicas"`
	Min               int32              `json:"minReplicas" yaml:"minReplicas"`
	Max               int32              `json:"maxReplicas" yaml:"maxReplicas"`
	Health            string             `json:"health" yaml:"health"`
	HealthScore       int                `json:"healthScore" yaml:"healthScore"`
	Summary           string             `json:"summary" yaml:"summary"`
	Conditions        []Condition        `json:"conditions" yaml:"conditions"`
	Metrics           []Metric           `json:"metrics" yaml:"metrics"`
	Behavior          []BehaviorRule     `json:"behavior,omitempty" yaml:"behavior,omitempty"`
	Actions           []string           `json:"recommendedActions,omitempty" yaml:"recommendedActions,omitempty"`
	Suggestions       []Suggestion       `json:"suggestions,omitempty" yaml:"suggestions,omitempty"`
	Interpretation    []string           `json:"interpretation,omitempty" yaml:"interpretation,omitempty"`
	ImpactMetric      *MetricImpactGuess `json:"impactMetric,omitempty" yaml:"impactMetric,omitempty"`
	CreationTimestamp metav1.Time        `json:"creationTimestamp,omitempty" yaml:"creationTimestamp,omitempty"`
}

type Condition struct {
	Type    string `json:"type" yaml:"type"`
	Status  string `json:"status" yaml:"status"`
	Reason  string `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

type Metric struct {
	Type    string   `json:"type" yaml:"type"`
	Name    string   `json:"name,omitempty" yaml:"name,omitempty"`
	Current string   `json:"current,omitempty" yaml:"current,omitempty"`
	Target  string   `json:"target,omitempty" yaml:"target,omitempty"`
	Ratio   *float64 `json:"ratio,omitempty" yaml:"ratio,omitempty"`
	Note    string   `json:"note,omitempty" yaml:"note,omitempty"`
	Text    string   `json:"text" yaml:"text"`
}

type MetricImpactGuess struct {
	Name  string  `json:"name" yaml:"name"`
	Ratio float64 `json:"ratio" yaml:"ratio"`
	Note  string  `json:"note" yaml:"note"`
}

type BehaviorRule struct {
	Direction                  string   `json:"direction" yaml:"direction"`
	StabilizationWindowSeconds *int32   `json:"stabilizationWindowSeconds,omitempty" yaml:"stabilizationWindowSeconds,omitempty"`
	SelectPolicy               string   `json:"selectPolicy,omitempty" yaml:"selectPolicy,omitempty"`
	Policies                   []string `json:"policies,omitempty" yaml:"policies,omitempty"`
	Text                       string   `json:"text" yaml:"text"`
}

type Suggestion struct {
	Title         string   `json:"title" yaml:"title"`
	Description   string   `json:"description" yaml:"description"`
	Command       string   `json:"command,omitempty" yaml:"command,omitempty"`
	Patch         string   `json:"patch,omitempty" yaml:"patch,omitempty"`
	Risk          string   `json:"risk,omitempty" yaml:"risk,omitempty"`
	Preconditions []string `json:"preconditions,omitempty" yaml:"preconditions,omitempty"`
	Warnings      []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	Apply         bool     `json:"apply,omitempty" yaml:"apply,omitempty"`
}

func Analyze(src *autoscalingv2.HorizontalPodAutoscaler, includeInterpretation bool) Analysis {
	minReplicas := int32(1)
	if src.Spec.MinReplicas != nil {
		minReplicas = *src.Spec.MinReplicas
	}

	analysis := Analysis{
		Namespace:         src.Namespace,
		Name:              src.Name,
		Target:            fmt.Sprintf("%s/%s", src.Spec.ScaleTargetRef.Kind, src.Spec.ScaleTargetRef.Name),
		Current:           src.Status.CurrentReplicas,
		Desired:           src.Status.DesiredReplicas,
		Min:               minReplicas,
		Max:               src.Spec.MaxReplicas,
		Summary:           SummarizeDirection(src, minReplicas),
		CreationTimestamp: src.CreationTimestamp,
	}

	for _, condition := range prioritizedConditions(src.Status.Conditions) {
		analysis.Conditions = append(analysis.Conditions, Condition{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		})
	}

	for _, metric := range src.Status.CurrentMetrics {
		analysis.Metrics = append(analysis.Metrics, FormatMetricStatus(src, metric))
	}

	analysis.Behavior = FormatBehavior(src)

	if guess, ok := MostInfluentialMetric(src); ok {
		analysis.ImpactMetric = &guess
	}

	if includeInterpretation {
		analysis.Actions = RecommendedActions(src, minReplicas)
		analysis.Suggestions = BuildSuggestions(src, minReplicas)
		analysis.Interpretation = Interpret(src, minReplicas)
	}
	analysis.Health, analysis.HealthScore = Health(src, minReplicas)

	return analysis
}

func Health(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) (string, int) {
	score := 100
	health := "OK"
	for _, condition := range hpa.Status.Conditions {
		switch {
		case condition.Type == "ScalingActive" && condition.Status != corev1.ConditionTrue:
			score -= 45
			health = "ERROR"
		case condition.Type == "AbleToScale" && condition.Status != corev1.ConditionTrue:
			score -= 35
			health = "ERROR"
		case condition.Type == "ScalingLimited" && condition.Status == corev1.ConditionTrue:
			score -= 25
			if health != "ERROR" {
				health = "LIMITED"
			}
		case condition.Type == "AbleToScale" && condition.Reason == "ScaleDownStabilized":
			score -= 10
			if health == "OK" {
				health = "STABILIZED"
			}
		}
	}
	if hpa.Status.CurrentReplicas == hpa.Status.DesiredReplicas && hpa.Status.CurrentReplicas == hpa.Spec.MaxReplicas {
		score -= 20
		if health == "OK" {
			health = "LIMITED"
		}
	}
	if hpa.Status.DesiredReplicas == minReplicas {
		score -= 5
	}
	if score < 0 {
		score = 0
	}
	return health, score
}

func SummarizeDirection(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) string {
	if condition := FindCondition(hpa, "ScalingActive"); condition != nil && condition.Status != corev1.ConditionTrue {
		return "HPA cannot currently compute a scaling recommendation from metrics."
	}
	if hpa.Status.DesiredReplicas == 0 && hpa.Status.CurrentReplicas > 0 {
		return "HPA has no visible desired replica recommendation in status."
	}

	current := hpa.Status.CurrentReplicas
	desired := hpa.Status.DesiredReplicas

	switch {
	case desired > current:
		return "HPA currently wants to scale up."
	case desired < current:
		return "HPA currently wants to scale down."
	case desired == hpa.Spec.MaxReplicas:
		return "HPA is at maxReplicas."
	case desired == minReplicas:
		return "HPA is at minReplicas."
	default:
		return "HPA currently keeps the replica count unchanged."
	}
}

func Interpret(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []string {
	var lines []string

	if hpa.Status.ObservedGeneration != nil && *hpa.Status.ObservedGeneration < hpa.Generation {
		lines = append(lines, fmt.Sprintf("[confidence: high] Warning: status.observedGeneration=%d is behind metadata.generation=%d; the status may not reflect the latest spec.", *hpa.Status.ObservedGeneration, hpa.Generation))
	}

	if condition := FindCondition(hpa, "ScalingActive"); condition != nil && condition.Status != corev1.ConditionTrue {
		lines = append(lines,
			fmt.Sprintf("[confidence: high] ScalingActive is %s: %s - %s", condition.Status, condition.Reason, condition.Message),
			"[confidence: high] The HPA is not reporting a reliable scale direction while metric evaluation is inactive.",
			"[confidence: high] This plugin avoids treating desiredReplicas=0 as a scale-down recommendation in this state.",
			limitation,
		)
		return lines
	}

	if condition := FindCondition(hpa, "AbleToScale"); condition != nil && condition.Status != corev1.ConditionTrue {
		lines = append(lines,
			fmt.Sprintf("[confidence: high] AbleToScale is %s: %s - %s", condition.Status, condition.Reason, condition.Message))
	} else if condition := FindCondition(hpa, "AbleToScale"); condition != nil && condition.Reason == "ScaleDownStabilized" {
		lines = append(lines,
			fmt.Sprintf("[confidence: medium] Scale down appears stabilized: %s", condition.Message))
	}

	if condition := FindCondition(hpa, "ScalingLimited"); condition != nil && condition.Status == corev1.ConditionTrue {
		switch hpa.Status.DesiredReplicas {
		case hpa.Spec.MaxReplicas:
			lines = append(lines, "[confidence: high] ScalingLimited reports that the visible desired replica count is constrained by maxReplicas.")
		case minReplicas:
			lines = append(lines, "[confidence: high] ScalingLimited reports that the visible desired replica count is constrained by minReplicas.")
		default:
			lines = append(lines, "[confidence: high] The recommendation is reported as limited.")
		}
	}

	if hpa.Status.DesiredReplicas > hpa.Status.CurrentReplicas {
		lines = append(lines, "[confidence: high] desiredReplicas is greater than currentReplicas, so the HPA is recommending scale up.")
	} else if hpa.Status.DesiredReplicas < hpa.Status.CurrentReplicas {
		lines = append(lines, "[confidence: high] desiredReplicas is less than currentReplicas, so the HPA is recommending scale down.")
	} else {
		lines = append(lines, "[confidence: high] desiredReplicas equals currentReplicas, so no immediate replica change is visible from status.")
		if hpa.Status.DesiredReplicas != hpa.Spec.MaxReplicas && hpa.Status.DesiredReplicas != minReplicas {
			if metric, ok := MetricOutsideTarget(hpa); ok {
				lines = append(lines, fmt.Sprintf("[confidence: medium] %s metric ratio is approximately %.3f, which is close to the target.", metric.Name, metric.Ratio))
				lines = append(lines, "[confidence: medium] This is consistent with tolerance-based no-scale. Kubernetes commonly uses a tolerance band around the target, but HPA status does not expose tolerance as an explicit reason.")
				lines = append(lines, "[confidence: high] The plugin avoids claiming the exact internal reason because rounding, stabilization, or conservative metric handling may also affect the final result.")
			}
		}
	}

	if guess, ok := MostInfluentialMetric(hpa); ok && len(hpa.Status.CurrentMetrics) > 1 {
		lines = append(lines, fmt.Sprintf("[confidence: medium] Among visible resource utilization metrics, %s has the largest distance from target (ratio %.3f).", guess.Name, guess.Ratio))
		lines = append(lines, "[confidence: high] This is only an impact estimate; the API does not expose per-metric replica recommendations or the final metric winner.")
	} else if len(hpa.Status.CurrentMetrics) > 1 {
		lines = append(lines, "[confidence: high] Multiple current metrics are reported, but the API does not expose per-metric replica recommendations or which metric would have selected the recommendation before replica limits were applied.")
		lines = append(lines, "[confidence: high] Events and human-readable messages can hint at the contributing metric, but they are not a stable decision record.")
	}

	lines = append(lines, limitation)

	return lines
}

func FindCondition(hpa *autoscalingv2.HorizontalPodAutoscaler, conditionType string) *autoscalingv2.HorizontalPodAutoscalerCondition {
	for i := range hpa.Status.Conditions {
		if string(hpa.Status.Conditions[i].Type) == conditionType {
			return &hpa.Status.Conditions[i]
		}
	}
	return nil
}

func calculateRatioAndNote(currentVal autoscalingv2.MetricValueStatus, targetVal autoscalingv2.MetricTarget, targetStr string) (*float64, string) {
	var ratio *float64
	var note string

	if currentVal.AverageUtilization != nil {
		ratio = utilizationRatio(currentVal.AverageUtilization, targetStr)
		note = CompareMetricToTarget(currentVal.AverageUtilization, targetStr)
	} else if currentVal.AverageValue != nil && targetVal.AverageValue != nil {
		ratio = quantityRatio(currentVal.AverageValue, targetVal.AverageValue)
		note = CompareQuantityToTarget(currentVal.AverageValue, targetVal.AverageValue)
	} else if currentVal.Value != nil && targetVal.Value != nil {
		ratio = quantityRatio(currentVal.Value, targetVal.Value)
		note = CompareQuantityToTarget(currentVal.Value, targetVal.Value)
	}
	return ratio, note
}

func FormatMetricStatus(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) Metric {
	switch metric.Type {
	case "":
		return Metric{Text: "Metric status is present, but details are unavailable"}
	case autoscalingv2.ResourceMetricSourceType:
		if metric.Resource == nil {
			return Metric{Type: "Resource", Text: "Resource metric: <missing status>"}
		}
		targetSpec := FindResourceTargetSpec(hpa, string(metric.Resource.Name))
		target := FormatMetricTarget(targetSpec)
		current := FormatMetricValue(metric.Resource.Current.AverageUtilization, metric.Resource.Current.AverageValue)
		ratio, note := calculateRatioAndNote(metric.Resource.Current, targetSpec, target)
		text := fmt.Sprintf("Resource %s current=%s target=%s", metric.Resource.Name, current, target)
		if ratio != nil {
			text = fmt.Sprintf("%s ratio=%.3f", text, *ratio)
		}
		if note != "" {
			text = fmt.Sprintf("%s note=%q", text, note)
		}
		return Metric{
			Type:    "Resource",
			Name:    string(metric.Resource.Name),
			Current: current,
			Target:  target,
			Ratio:   ratio,
			Note:    note,
			Text:    text,
		}
	case autoscalingv2.ContainerResourceMetricSourceType:
		if metric.ContainerResource == nil {
			return Metric{Type: "ContainerResource", Text: "ContainerResource metric: <missing status>"}
		}
		targetSpec := FindContainerResourceTargetSpec(hpa, string(metric.ContainerResource.Name), metric.ContainerResource.Container)
		target := FormatMetricTarget(targetSpec)
		current := FormatMetricValueStatus(metric.ContainerResource.Current)
		ratio, note := calculateRatioAndNote(metric.ContainerResource.Current, targetSpec, target)
		text := fmt.Sprintf("ContainerResource %s/%s current=%s target=%s", metric.ContainerResource.Container, metric.ContainerResource.Name, current, target)
		if ratio != nil {
			text = fmt.Sprintf("%s ratio=%.3f", text, *ratio)
		}
		if note != "" {
			text = fmt.Sprintf("%s note=%q", text, note)
		}
		return Metric{
			Type:    "ContainerResource",
			Name:    fmt.Sprintf("%s/%s", metric.ContainerResource.Container, metric.ContainerResource.Name),
			Current: current,
			Target:  target,
			Ratio:   ratio,
			Note:    note,
			Text:    text,
		}
	case autoscalingv2.PodsMetricSourceType:
		if metric.Pods == nil {
			return Metric{Type: "Pods", Text: "Pods metric: <missing status>"}
		}
		targetSpec := FindPodsTargetSpec(hpa, metric.Pods.Metric.Name)
		target := FormatMetricTarget(targetSpec)
		current := FormatMetricValueStatus(metric.Pods.Current)
		ratio, note := calculateRatioAndNote(metric.Pods.Current, targetSpec, target)
		text := fmt.Sprintf("Pods %s current=%s target=%s", metric.Pods.Metric.Name, current, target)
		if ratio != nil {
			text = fmt.Sprintf("%s ratio=%.3f", text, *ratio)
		}
		if note != "" {
			text = fmt.Sprintf("%s note=%q", text, note)
		}
		return Metric{
			Type:    "Pods",
			Name:    metric.Pods.Metric.Name,
			Current: current,
			Target:  target,
			Ratio:   ratio,
			Note:    note,
			Text:    text,
		}
	case autoscalingv2.ObjectMetricSourceType:
		if metric.Object == nil {
			return Metric{Type: "Object", Text: "Object metric: <missing status>"}
		}
		targetSpec := FindObjectTargetSpec(hpa, metric.Object.Metric.Name)
		target := FormatMetricTarget(targetSpec)
		current := FormatMetricValueStatus(metric.Object.Current)
		ratio, note := calculateRatioAndNote(metric.Object.Current, targetSpec, target)
		name := fmt.Sprintf("%s/%s", metric.Object.DescribedObject.Kind, metric.Object.DescribedObject.Name)
		text := fmt.Sprintf("Object %s %s current=%s target=%s", name, metric.Object.Metric.Name, current, target)
		if ratio != nil {
			text = fmt.Sprintf("%s ratio=%.3f", text, *ratio)
		}
		if note != "" {
			text = fmt.Sprintf("%s note=%q", text, note)
		}
		return Metric{
			Type:    "Object",
			Name:    metric.Object.Metric.Name,
			Current: current,
			Target:  target,
			Ratio:   ratio,
			Note:    note,
			Text:    text,
		}
	case autoscalingv2.ExternalMetricSourceType:
		if metric.External == nil {
			return Metric{Type: "External", Text: "External metric: <missing status>"}
		}
		targetSpec := FindExternalTargetSpec(hpa, metric.External.Metric.Name)
		target := FormatMetricTarget(targetSpec)
		current := FormatMetricValueStatus(metric.External.Current)
		ratio, note := calculateRatioAndNote(metric.External.Current, targetSpec, target)
		text := fmt.Sprintf("External %s current=%s target=%s", metric.External.Metric.Name, current, target)
		if ratio != nil {
			text = fmt.Sprintf("%s ratio=%.3f", text, *ratio)
		}
		if note != "" {
			text = fmt.Sprintf("%s note=%q", text, note)
		}
		return Metric{
			Type:    "External",
			Name:    metric.External.Metric.Name,
			Current: current,
			Target:  target,
			Ratio:   ratio,
			Note:    note,
			Text:    text,
		}
	default:
		return Metric{
			Type: string(metric.Type),
			Text: fmt.Sprintf("%s metric is present, but this plugin does not know how to format it in detail", metric.Type),
		}
	}
}

func FindResourceTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) autoscalingv2.MetricTarget {
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ResourceMetricSourceType &&
			spec.Resource != nil &&
			string(spec.Resource.Name) == name {
			return spec.Resource.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

func FindResourceTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) string {
	return FormatMetricTarget(FindResourceTargetSpec(hpa, name))
}

func FindContainerResourceTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name, container string) autoscalingv2.MetricTarget {
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ContainerResourceMetricSourceType &&
			spec.ContainerResource != nil &&
			string(spec.ContainerResource.Name) == name &&
			spec.ContainerResource.Container == container {
			return spec.ContainerResource.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

func FindContainerResourceTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name, container string) string {
	return FormatMetricTarget(FindContainerResourceTargetSpec(hpa, name, container))
}

func FindPodsTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) autoscalingv2.MetricTarget {
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.PodsMetricSourceType &&
			spec.Pods != nil &&
			spec.Pods.Metric.Name == name {
			return spec.Pods.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

func FindPodsTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) string {
	return FormatMetricTarget(FindPodsTargetSpec(hpa, name))
}

func FindObjectTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) autoscalingv2.MetricTarget {
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ObjectMetricSourceType &&
			spec.Object != nil &&
			spec.Object.Metric.Name == name {
			return spec.Object.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

func FindObjectTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) string {
	return FormatMetricTarget(FindObjectTargetSpec(hpa, name))
}

func FindExternalTargetSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) autoscalingv2.MetricTarget {
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ExternalMetricSourceType &&
			spec.External != nil &&
			spec.External.Metric.Name == name {
			return spec.External.Target
		}
	}
	return autoscalingv2.MetricTarget{}
}

func FindExternalTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) string {
	return FormatMetricTarget(FindExternalTargetSpec(hpa, name))
}

func FormatMetricTarget(target autoscalingv2.MetricTarget) string {
	switch target.Type {
	case autoscalingv2.UtilizationMetricType:
		if target.AverageUtilization != nil {
			return fmt.Sprintf("%d%%", *target.AverageUtilization)
		}
	case autoscalingv2.AverageValueMetricType:
		if target.AverageValue != nil {
			return target.AverageValue.String()
		}
	case autoscalingv2.ValueMetricType:
		if target.Value != nil {
			return target.Value.String()
		}
	}
	return "<unknown>"
}

func FormatMetricValue(utilization *int32, averageValue *resource.Quantity) string {
	if utilization != nil {
		return fmt.Sprintf("%d%%", *utilization)
	}
	if averageValue != nil && !averageValue.IsZero() {
		return averageValue.String()
	}
	return "<unknown>"
}

func FormatMetricValueStatus(value autoscalingv2.MetricValueStatus) string {
	if value.AverageUtilization != nil {
		return fmt.Sprintf("%d%%", *value.AverageUtilization)
	}
	if value.AverageValue != nil && !value.AverageValue.IsZero() {
		return value.AverageValue.String()
	}
	if value.Value != nil && !value.Value.IsZero() {
		return value.Value.String()
	}
	return "<unknown>"
}

func FormatBehavior(hpa *autoscalingv2.HorizontalPodAutoscaler) []BehaviorRule {
	if hpa.Spec.Behavior == nil {
		return nil
	}

	var out []BehaviorRule
	if rule := FormatBehaviorRule("scaleUp", hpa.Spec.Behavior.ScaleUp); rule != nil {
		out = append(out, *rule)
	}
	if rule := FormatBehaviorRule("scaleDown", hpa.Spec.Behavior.ScaleDown); rule != nil {
		out = append(out, *rule)
	}
	return out
}

func FormatBehaviorRule(direction string, rules *autoscalingv2.HPAScalingRules) *BehaviorRule {
	if rules == nil {
		return nil
	}

	rule := BehaviorRule{
		Direction:                  direction,
		StabilizationWindowSeconds: rules.StabilizationWindowSeconds,
	}
	if rules.SelectPolicy != nil {
		rule.SelectPolicy = string(*rules.SelectPolicy)
	}
	for _, policy := range rules.Policies {
		rule.Policies = append(rule.Policies, fmt.Sprintf("%s %d per %ds", policy.Type, policy.Value, policy.PeriodSeconds))
	}

	var parts []string
	if rule.StabilizationWindowSeconds != nil {
		parts = append(parts, fmt.Sprintf("stabilizationWindow=%ds", *rule.StabilizationWindowSeconds))
	}
	if rule.SelectPolicy != "" {
		parts = append(parts, "selectPolicy="+rule.SelectPolicy)
	}
	if len(rule.Policies) > 0 {
		parts = append(parts, "policies="+strings.Join(rule.Policies, ", "))
	}
	if len(parts) == 0 {
		parts = append(parts, "custom behavior is present")
	}
	rule.Text = direction + ": " + strings.Join(parts, "; ")
	return &rule
}

func RecommendedActions(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []string {
	var actions []string
	if hpa.Status.ObservedGeneration != nil && *hpa.Status.ObservedGeneration < hpa.Generation {
		actions = append(actions, "Wait for the HPA controller to observe the latest spec generation before trusting this status.")
	}
	if condition := FindCondition(hpa, "ScalingActive"); condition != nil && condition.Status != corev1.ConditionTrue {
		actions = append(actions, "Check metrics-server or custom/external metrics adapters; ScalingActive is not True.")
		return actions
	}
	if condition := FindCondition(hpa, "AbleToScale"); condition != nil && condition.Reason == "ScaleDownStabilized" {
		if window := scaleDownStabilizationWindow(hpa); window != nil {
			actions = append(actions, fmt.Sprintf("CPU or memory may already be low, but scale-down is stabilized; wait up to about %ds or review spec.behavior.scaleDown.stabilizationWindowSeconds.", *window))
		} else {
			actions = append(actions, "CPU or memory may already be low, but scale-down is stabilized; review HPA behavior and recent recommendations.")
		}
	}
	if condition := FindCondition(hpa, "ScalingLimited"); condition != nil && condition.Status == corev1.ConditionTrue {
		switch hpa.Status.DesiredReplicas {
		case hpa.Spec.MaxReplicas:
			actions = append(actions, "HPA is capped at maxReplicas; raise maxReplicas or reduce load/target utilization if more capacity is expected.")
		case minReplicas:
			actions = append(actions, "HPA is capped at minReplicas; lower minReplicas if scale-down below this point is expected.")
		}
	}
	if len(actions) == 0 && hpa.Status.DesiredReplicas == hpa.Status.CurrentReplicas {
		actions = append(actions, "No immediate action is visible from HPA status; inspect metrics and recent Events if behavior is unexpected.")
	}
	return actions
}

func scaleDownStabilizationWindow(hpa *autoscalingv2.HorizontalPodAutoscaler) *int32 {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return nil
	}
	return hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
}

func CompareMetricToTarget(utilization *int32, target string) string {
	if utilization == nil || !strings.HasSuffix(target, "%") {
		return ""
	}

	targetUtilization, ok := parsePercent(target)
	if !ok {
		return ""
	}

	switch {
	case *utilization > targetUtilization:
		return "current value is above target"
	case *utilization < targetUtilization:
		return "current value is below target"
	default:
		return "current value equals target"
	}
}

func MetricOutsideTarget(hpa *autoscalingv2.HorizontalPodAutoscaler) (MetricImpactGuess, bool) {
	for _, metric := range hpa.Status.CurrentMetrics {
		if metric.Type != autoscalingv2.ResourceMetricSourceType || metric.Resource == nil {
			continue
		}
		ratio := utilizationRatio(metric.Resource.Current.AverageUtilization, FindResourceTarget(hpa, string(metric.Resource.Name)))
		if ratio != nil && *ratio != 1 {
			return MetricImpactGuess{Name: string(metric.Resource.Name), Ratio: *ratio}, true
		}
	}

	return MetricImpactGuess{}, false
}

func MostInfluentialMetric(hpa *autoscalingv2.HorizontalPodAutoscaler) (MetricImpactGuess, bool) {
	var best MetricImpactGuess
	var bestDistance float64

	for _, metric := range hpa.Status.CurrentMetrics {
		if metric.Type != autoscalingv2.ResourceMetricSourceType || metric.Resource == nil {
			continue
		}
		ratio := utilizationRatio(metric.Resource.Current.AverageUtilization, FindResourceTarget(hpa, string(metric.Resource.Name)))
		if ratio == nil {
			continue
		}
		distance := *ratio - 1
		if distance < 0 {
			distance = -distance
		}
		if distance > bestDistance {
			bestDistance = distance
			best = MetricImpactGuess{
				Name:  string(metric.Resource.Name),
				Ratio: *ratio,
				Note:  "largest visible utilization ratio distance from target",
			}
		}
	}

	return best, bestDistance > 0
}

func prioritizedConditions(conditions []autoscalingv2.HorizontalPodAutoscalerCondition) []autoscalingv2.HorizontalPodAutoscalerCondition {
	out := append([]autoscalingv2.HorizontalPodAutoscalerCondition(nil), conditions...)
	priority := map[autoscalingv2.HorizontalPodAutoscalerConditionType]int{
		"ScalingActive":  0,
		"AbleToScale":    1,
		"ScalingLimited": 2,
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := priority[out[i].Type]
		right := priority[out[j].Type]
		if _, ok := priority[out[i].Type]; !ok {
			left = 100
		}
		if _, ok := priority[out[j].Type]; !ok {
			right = 100
		}
		return left < right
	})
	return out
}

func utilizationRatio(utilization *int32, target string) *float64 {
	if utilization == nil {
		return nil
	}
	targetUtilization, ok := parsePercent(target)
	if !ok || targetUtilization == 0 {
		return nil
	}
	ratio := float64(*utilization) / float64(targetUtilization)
	return &ratio
}

func parsePercent(value string) (int32, bool) {
	if !strings.HasSuffix(value, "%") {
		return 0, false
	}
	var percent int32
	if _, err := fmt.Sscanf(strings.TrimSuffix(value, "%"), "%d", &percent); err != nil {
		return 0, false
	}
	return percent, true
}

func quantityRatio(current, target *resource.Quantity) *float64 {
	if current == nil || target == nil || target.IsZero() {
		return nil
	}
	ratio := current.AsApproximateFloat64() / target.AsApproximateFloat64()
	return &ratio
}

func CompareQuantityToTarget(current, target *resource.Quantity) string {
	if current == nil || target == nil {
		return ""
	}
	cmp := current.Cmp(*target)
	switch {
	case cmp > 0:
		return "current value is above target"
	case cmp < 0:
		return "current value is below target"
	default:
		return "current value equals target"
	}
}
