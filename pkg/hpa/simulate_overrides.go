package hpa

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
)

// applySimulationOverride modifies a single field on the HPA spec using dot-notation path.
func applySimulationOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, path, value string) error {
	normalizedPath := normalizeSimulationPath(path)
	if strings.HasPrefix(normalizedPath, "scaleup.policies") || strings.HasPrefix(normalizedPath, "scaledown.policies") {
		return fmt.Errorf("%w: rate-policy overrides need scaling-event history", ErrUnsupportedSimulationSemantics)
	}
	if strings.HasPrefix(normalizedPath, "metric.") && strings.HasSuffix(normalizedPath, ".target") {
		name := strings.TrimSuffix(strings.TrimPrefix(normalizedPath, "metric."), ".target")
		return applyMetricTargetOverride(hpa, name, value)
	}
	if strings.HasSuffix(normalizedPath, ".targetaverageutilization") {
		name := strings.TrimSuffix(normalizedPath, ".targetaverageutilization")
		return applyMetricTargetOverride(hpa, name, value)
	}

	if definition, ok := simulationOverrideDefinitions[normalizedPath]; ok {
		return definition.Apply(hpa, value)
	}
	return fmt.Errorf("%w %q; supported: %s, metric.<name>.target", ErrUnsupportedSimulationPath, path, strings.Join(supportedSimulationPaths(), ", "))
}

type simulationOverrideDefinition struct {
	Name     string
	Apply    func(*autoscalingv2.HorizontalPodAutoscaler, string) error
	Original func(*autoscalingv2.HorizontalPodAutoscaler) string
}

func replicaOverride(path string) func(*autoscalingv2.HorizontalPodAutoscaler, string) error {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler, value string) error {
		_, err := applyReplicaSimulationOverride(hpa, path, value)
		return err
	}
}

func behaviorOverride(path string) func(*autoscalingv2.HorizontalPodAutoscaler, string) error {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler, value string) error {
		_, err := applyBehaviorSimulationOverride(hpa, path, value)
		return err
	}
}

var simulationOverrideDefinitions = map[string]simulationOverrideDefinition{
	"maxreplicas": {Name: "maxReplicas", Apply: replicaOverride("maxreplicas"), Original: func(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
		return fmt.Sprintf("%d", hpa.Spec.MaxReplicas)
	}},
	"minreplicas":                          {Name: "minReplicas", Apply: replicaOverride("minreplicas"), Original: originalMinReplicas},
	"targetaverageutilization":             {Name: "targetAverageUtilization", Apply: replicaOverride("targetaverageutilization"), Original: originalTargetAverageUtilization},
	"scaledown.stabilizationwindowseconds": {Name: "scaleDown.stabilizationWindowSeconds", Apply: behaviorOverride("scaledown.stabilizationwindowseconds"), Original: originalScaleDownStabilizationWindow},
	"scaleup.stabilizationwindowseconds":   {Name: "scaleUp.stabilizationWindowSeconds", Apply: behaviorOverride("scaleup.stabilizationwindowseconds"), Original: originalScaleUpStabilizationWindow},
	"scaledown.selectpolicy": {Name: "scaleDown.selectPolicy", Apply: behaviorOverride("scaledown.selectpolicy"), Original: func(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
		if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.SelectPolicy != nil {
			return string(*hpa.Spec.Behavior.ScaleDown.SelectPolicy)
		}
		return "Max"
	}},
	"scaleup.selectpolicy": {Name: "scaleUp.selectPolicy", Apply: behaviorOverride("scaleup.selectpolicy"), Original: func(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
		if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleUp != nil && hpa.Spec.Behavior.ScaleUp.SelectPolicy != nil {
			return string(*hpa.Spec.Behavior.ScaleUp.SelectPolicy)
		}
		return "Max"
	}},
	"tolerance": {Name: "tolerance", Apply: func(hpa *autoscalingv2.HorizontalPodAutoscaler, value string) error {
		return applyToleranceOverride(hpa, "both", value)
	}, Original: func(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
		up, down := effectiveDirectionalTolerances(hpa)
		return fmt.Sprintf("scaleUp=%.3g,scaleDown=%.3g", up, down)
	}},
	"scaleup.tolerance": {Name: "scaleUp.tolerance", Apply: func(hpa *autoscalingv2.HorizontalPodAutoscaler, value string) error {
		return applyToleranceOverride(hpa, "up", value)
	}, Original: func(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
		return originalDirectionalTolerance(hpa, true)
	}},
	"scaledown.tolerance": {Name: "scaleDown.tolerance", Apply: func(hpa *autoscalingv2.HorizontalPodAutoscaler, value string) error {
		return applyToleranceOverride(hpa, "down", value)
	}, Original: func(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
		return originalDirectionalTolerance(hpa, false)
	}},
}

func supportedSimulationPaths() []string {
	paths := make([]string, 0, len(simulationOverrideDefinitions)+2)
	for _, definition := range simulationOverrideDefinitions {
		paths = append(paths, definition.Name)
	}
	paths = append(paths, "scaleDown.stabilizationWindow", "scaleUp.stabilizationWindow")
	sort.Strings(paths)
	return paths
}

func applyReplicaSimulationOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, normalizedPath, value string) (bool, error) {
	switch normalizedPath {
	case "maxreplicas":
		v, err := parseNonNegativeInt32(value, 1, "maxReplicas must be >= 1")
		if err != nil {
			return true, err
		}
		hpa.Spec.MaxReplicas = v
		return true, nil
	case "minreplicas":
		v, err := parseNonNegativeInt32(value, 0, "minReplicas must be >= 0")
		if err != nil {
			return true, err
		}
		hpa.Spec.MinReplicas = &v
		return true, nil
	case "targetaverageutilization":
		v, err := parsePositiveInt32(value)
		if err != nil {
			return true, err
		}
		applyAverageUtilizationToResourceMetrics(hpa, v)
		return true, nil
	default:
		return false, nil
	}
}

func applyBehaviorSimulationOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, normalizedPath, value string) (bool, error) {
	switch normalizedPath {
	case "scaledown.stabilizationwindowseconds":
		window, err := parseNonNegativeInt32(value, 0, "stabilizationWindowSeconds must be >= 0")
		if err != nil {
			return true, err
		}
		if window > 3600 {
			return true, fmt.Errorf("%w: stabilizationWindowSeconds must be <= 3600", ErrInvalidSimulationValue)
		}
		return true, fmt.Errorf("%w: scaleDown stabilization cannot be projected without recommendation history", ErrUnsupportedSimulationSemantics)
	case "scaleup.stabilizationwindowseconds":
		window, err := parseNonNegativeInt32(value, 0, "stabilizationWindowSeconds must be >= 0")
		if err != nil {
			return true, err
		}
		if window > 3600 {
			return true, fmt.Errorf("%w: stabilizationWindowSeconds must be <= 3600", ErrInvalidSimulationValue)
		}
		return true, fmt.Errorf("%w: scaleUp stabilization cannot be projected without recommendation history", ErrUnsupportedSimulationSemantics)
	case "scaledown.selectpolicy":
		p, err := selectPolicy(value)
		if err != nil {
			return true, err
		}
		ensureBehavior(hpa)
		hpa.Spec.Behavior.ScaleDown.SelectPolicy = &p
		return true, nil
	case "scaleup.selectpolicy":
		p, err := selectPolicy(value)
		if err != nil {
			return true, err
		}
		ensureBehavior(hpa)
		hpa.Spec.Behavior.ScaleUp.SelectPolicy = &p
		return true, nil
	default:
		return false, nil
	}
}

func validateSimulatedHPA(hpa *autoscalingv2.HorizontalPodAutoscaler) error {
	minReplicas := int32(1)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}
	if minReplicas < 0 {
		return fmt.Errorf("%w: minReplicas must be >= 0, got %d", ErrInvalidSimulationValue, minReplicas)
	}
	if hpa.Spec.MaxReplicas < 1 {
		return fmt.Errorf("%w: maxReplicas must be >= 1, got %d", ErrInvalidSimulationValue, hpa.Spec.MaxReplicas)
	}
	if hpa.Spec.MaxReplicas < minReplicas {
		return fmt.Errorf("%w: maxReplicas (%d) must be >= minReplicas (%d)",
			ErrInvalidSimulationValue, hpa.Spec.MaxReplicas, minReplicas)
	}
	if hpa.Spec.Behavior == nil {
		return nil
	}
	if err := validateSimulatedScalingRules("scaleUp", hpa.Spec.Behavior.ScaleUp); err != nil {
		return err
	}
	return validateSimulatedScalingRules("scaleDown", hpa.Spec.Behavior.ScaleDown)
}

func validateSimulatedScalingRules(direction string, rules *autoscalingv2.HPAScalingRules) error {
	if rules == nil {
		return nil
	}
	if rules.StabilizationWindowSeconds != nil && *rules.StabilizationWindowSeconds < 0 {
		return fmt.Errorf("%w: %s.stabilizationWindowSeconds must be >= 0", ErrInvalidSimulationValue, direction)
	}
	if rules.StabilizationWindowSeconds != nil && *rules.StabilizationWindowSeconds > 3600 {
		return fmt.Errorf("%w: %s.stabilizationWindowSeconds must be <= 3600", ErrInvalidSimulationValue, direction)
	}
	if rules.SelectPolicy != nil {
		if _, err := selectPolicy(string(*rules.SelectPolicy)); err != nil {
			return fmt.Errorf("%s: %w", direction, err)
		}
	}
	if rules.Policies != nil && len(rules.Policies) == 0 {
		return fmt.Errorf("%w: %s.policies must contain at least one policy", ErrInvalidSimulationValue, direction)
	}
	for i, policy := range rules.Policies {
		if err := validateSimulatedScalingPolicy(direction, i, policy); err != nil {
			return err
		}
	}
	return nil
}

func validateSimulatedScalingPolicy(direction string, index int, policy autoscalingv2.HPAScalingPolicy) error {
	if policy.Type != autoscalingv2.PodsScalingPolicy && policy.Type != autoscalingv2.PercentScalingPolicy {
		return fmt.Errorf("%w: %s.policies[%d].type must be Pods or Percent, got %q",
			ErrInvalidSimulationValue, direction, index, policy.Type)
	}
	if policy.Value <= 0 {
		return fmt.Errorf("%w: %s.policies[%d].value must be > 0", ErrInvalidSimulationValue, direction, index)
	}
	if policy.PeriodSeconds <= 0 {
		return fmt.Errorf("%w: %s.policies[%d].periodSeconds must be > 0", ErrInvalidSimulationValue, direction, index)
	}
	if policy.PeriodSeconds > 1800 {
		return fmt.Errorf("%w: %s.policies[%d].periodSeconds must be <= 1800", ErrInvalidSimulationValue, direction, index)
	}
	return nil
}

func applyToleranceOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, direction, value string) error {
	tolerance, err := resource.ParseQuantity(value)
	if err != nil {
		return fmt.Errorf("invalid tolerance %q: %w", value, err)
	}
	floatValue := tolerance.AsApproximateFloat64()
	if math.IsNaN(floatValue) || math.IsInf(floatValue, 0) || floatValue < 0 {
		return fmt.Errorf("%w: tolerance must be non-negative, got %q", ErrInvalidSimulationValue, value)
	}
	ensureBehavior(hpa)
	if direction == "both" || direction == "up" {
		up := tolerance.DeepCopy()
		hpa.Spec.Behavior.ScaleUp.Tolerance = &up
	}
	if direction == "both" || direction == "down" {
		down := tolerance.DeepCopy()
		hpa.Spec.Behavior.ScaleDown.Tolerance = &down
	}
	return nil
}

func applyMetricTargetOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, name, value string) error {
	for i := range hpa.Spec.Metrics {
		spec := &hpa.Spec.Metrics[i]
		if !metricSpecNameMatches(*spec, name) {
			continue
		}
		target := metricTargetPointer(spec)
		if target == nil {
			return fmt.Errorf("metric %q has no target", name)
		}
		if target.Type == autoscalingv2.UtilizationMetricType || target.AverageUtilization != nil {
			parsed := strings.TrimSuffix(value, "%")
			utilization, err := parsePositiveInt32(parsed)
			if err != nil {
				return fmt.Errorf("invalid utilization target %q for metric %q: %w", value, name, err)
			}
			target.Type = autoscalingv2.UtilizationMetricType
			target.AverageUtilization = &utilization
			target.AverageValue = nil
			target.Value = nil
			return nil
		}

		quantity, err := resource.ParseQuantity(value)
		if err != nil || quantity.Sign() <= 0 {
			if err == nil {
				err = errors.New("target must be greater than zero")
			}
			return fmt.Errorf("invalid quantity target %q for metric %q: %w", value, name, err)
		}
		if target.Type == autoscalingv2.ValueMetricType || target.Value != nil {
			target.Type = autoscalingv2.ValueMetricType
			target.Value = &quantity
			target.AverageValue = nil
		} else {
			target.Type = autoscalingv2.AverageValueMetricType
			target.AverageValue = &quantity
			target.Value = nil
		}
		target.AverageUtilization = nil
		return nil
	}
	return fmt.Errorf("metric %q: %w", name, ErrMetricNotFound)
}

func metricSpecNameMatches(spec autoscalingv2.MetricSpec, name string) bool {
	wanted := strings.ToLower(strings.TrimSpace(name))
	switch spec.Type {
	case autoscalingv2.ResourceMetricSourceType:
		return spec.Resource != nil && strings.ToLower(string(spec.Resource.Name)) == wanted
	case autoscalingv2.ContainerResourceMetricSourceType:
		return spec.ContainerResource != nil && strings.ToLower(string(spec.ContainerResource.Name)) == wanted
	case autoscalingv2.PodsMetricSourceType:
		return spec.Pods != nil && strings.ToLower(spec.Pods.Metric.Name) == wanted
	case autoscalingv2.ObjectMetricSourceType:
		return spec.Object != nil && strings.ToLower(spec.Object.Metric.Name) == wanted
	case autoscalingv2.ExternalMetricSourceType:
		return spec.External != nil && strings.ToLower(spec.External.Metric.Name) == wanted
	default:
		return false
	}
}

func metricTargetPointer(spec *autoscalingv2.MetricSpec) *autoscalingv2.MetricTarget {
	switch spec.Type {
	case autoscalingv2.ResourceMetricSourceType:
		if spec.Resource != nil {
			return &spec.Resource.Target
		}
	case autoscalingv2.ContainerResourceMetricSourceType:
		if spec.ContainerResource != nil {
			return &spec.ContainerResource.Target
		}
	case autoscalingv2.PodsMetricSourceType:
		if spec.Pods != nil {
			return &spec.Pods.Target
		}
	case autoscalingv2.ObjectMetricSourceType:
		if spec.Object != nil {
			return &spec.Object.Target
		}
	case autoscalingv2.ExternalMetricSourceType:
		if spec.External != nil {
			return &spec.External.Target
		}
	}
	return nil
}
