package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Assumption represents a single detected HPA controller assumption with its
// source, confidence level, and impact description.
type Assumption struct {
	Name        string `json:"name" yaml:"name"`
	Value       string `json:"value" yaml:"value"`
	Source      string `json:"source" yaml:"source"`          // "hpa.spec" or "kubernetes-default"
	Confidence  string `json:"confidence" yaml:"confidence"`  // high / medium / low
	Impact      string `json:"impact" yaml:"impact"`
	Description string `json:"description" yaml:"description"`
}

// ControllerAssumptions holds all detected assumptions about the HPA controller
// configuration, including both explicitly set values and inferred defaults.
type ControllerAssumptions struct {
	Namespace               string      `json:"namespace" yaml:"namespace"`
	Name                    string      `json:"name" yaml:"name"`
	SyncPeriod              Assumption  `json:"syncPeriod" yaml:"syncPeriod"`
	GlobalTolerance         Assumption  `json:"globalTolerance" yaml:"globalTolerance"`
	CPUInitializationPeriod Assumption  `json:"cpuInitializationPeriod" yaml:"cpuInitializationPeriod"`
	InitialReadinessDelay   Assumption  `json:"initialReadinessDelay" yaml:"initialReadinessDelay"`
	DownscaleStabilization  Assumption  `json:"downscaleStabilization" yaml:"downscaleStabilization"`
	UpscaleStabilization    Assumption  `json:"upscaleStabilization" yaml:"upscaleStabilization"`
	Summary                 string      `json:"summary" yaml:"summary"`
	Warnings                []string    `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// AssumptionOverrides holds user-provided override values for controller assumptions.
// Each field is optional; nil values are ignored.
type AssumptionOverrides struct {
	Tolerance               *string `json:"tolerance,omitempty" yaml:"tolerance,omitempty"`
	SyncPeriod              *string `json:"syncPeriod,omitempty" yaml:"syncPeriod,omitempty"`
	CPUInitializationPeriod *string `json:"cpuInitializationPeriod,omitempty" yaml:"cpuInitializationPeriod,omitempty"`
	InitialReadinessDelay   *string `json:"initialReadinessDelay,omitempty" yaml:"initialReadinessDelay,omitempty"`
}

// DetectControllerAssumptions examines an HPA resource and reports the
// controller assumptions that affect its scaling behavior. It distinguishes
// between values explicitly set in the HPA spec (high confidence) and
// Kubernetes controller-manager defaults (medium or low confidence).
//
// Returns nil if hpa is nil.
func DetectControllerAssumptions(hpa *autoscalingv2.HorizontalPodAutoscaler) *ControllerAssumptions {
	return DetectControllerAssumptionsWithOverrides(hpa, AssumptionOverrides{}, nil)
}

// DetectControllerAssumptionsWithOverrides examines an HPA resource and reports
// controller assumptions, applying user-provided overrides and optionally
// integrating an observed controller-manager profile.
//
// Override values replace the detected default and are marked with
// source="overridden" and confidence="high". The observed profile upgrades
// fields still at "kubernetes-default" source to "observed" with
// confidence="medium".
//
// Returns nil if hpa is nil.
func DetectControllerAssumptionsWithOverrides(
	hpa *autoscalingv2.HorizontalPodAutoscaler,
	overrides AssumptionOverrides,
	observed *ControllerProfile,
) *ControllerAssumptions {
	if hpa == nil {
		return nil
	}

	result := &ControllerAssumptions{
		Namespace: hpa.Namespace,
		Name:      hpa.Name,
	}

	result.SyncPeriod = detectSyncPeriod()
	result.GlobalTolerance = detectTolerance(hpa)
	result.CPUInitializationPeriod = detectCPUInitializationPeriod()
	result.InitialReadinessDelay = detectInitialReadinessDelay()
	result.DownscaleStabilization = detectDownscaleStabilization(hpa)
	result.UpscaleStabilization = detectUpscaleStabilization(hpa)

	// Apply observed controller-manager profile values, upgrading confidence
	// for fields still using kubernetes-default source.
	if observed != nil {
		result.SyncPeriod = applyObservedProfile(result.SyncPeriod, observed.SyncPeriod, observed.Source)
		result.GlobalTolerance = applyObservedProfile(result.GlobalTolerance, observed.Tolerance, observed.Source)
		result.CPUInitializationPeriod = applyObservedProfile(result.CPUInitializationPeriod, observed.CPUInitializationPeriod, observed.Source)
		result.InitialReadinessDelay = applyObservedProfile(result.InitialReadinessDelay, observed.InitialReadinessDelay, observed.Source)
		result.DownscaleStabilization = applyObservedProfile(result.DownscaleStabilization, observed.DownscaleStabilization, observed.Source)
	}

	// Apply user-provided overrides last — they take highest priority.
	if overrides.Tolerance != nil {
		result.GlobalTolerance = applyOverride(result.GlobalTolerance, *overrides.Tolerance)
	}
	if overrides.SyncPeriod != nil {
		result.SyncPeriod = applyOverride(result.SyncPeriod, *overrides.SyncPeriod)
	}
	if overrides.CPUInitializationPeriod != nil {
		result.CPUInitializationPeriod = applyOverride(result.CPUInitializationPeriod, *overrides.CPUInitializationPeriod)
	}
	if overrides.InitialReadinessDelay != nil {
		result.InitialReadinessDelay = applyOverride(result.InitialReadinessDelay, *overrides.InitialReadinessDelay)
	}

	result.Summary = buildAssumptionsSummary(result)
	result.Warnings = buildAssumptionsWarnings(result)

	return result
}

// applyObservedProfile upgrades an assumption when its source is still
// "kubernetes-default" by replacing its value and source from the observed
// controller-manager profile. Confidence is set to "medium" since the value
// is observed but not guaranteed to be current.
func applyObservedProfile(a Assumption, observedValue string, observedSource string) Assumption {
	if a.Source != "kubernetes-default" || observedValue == "" {
		return a
	}
	return Assumption{
		Name:        a.Name,
		Value:       observedValue,
		Source:      observedSource,
		Confidence:  "medium",
		Impact:      a.Impact,
		Description: a.Description,
	}
}

// applyOverride replaces an assumption's value with a user-provided override,
// setting source="overridden" and confidence="high".
func applyOverride(a Assumption, overrideValue string) Assumption {
	return Assumption{
		Name:        a.Name,
		Value:       overrideValue,
		Source:      "overridden",
		Confidence:  "high",
		Impact:      a.Impact,
		Description: a.Description,
	}
}

// detectSyncPeriod returns the HPA controller sync period assumption.
// This is a controller-manager flag not observable via the HPA API.
func detectSyncPeriod() Assumption {
	return Assumption{
		Name:        "SyncPeriod",
		Value:       "15s",
		Source:      "kubernetes-default",
		Confidence:  "medium",
		Impact:      "HPA re-evaluates scaling decisions every sync period",
		Description: "The horizontal-pod-autoscaler-sync-period flag (default 15s) controls how often the HPA controller checks metrics and computes desired replicas.",
	}
}

// detectTolerance checks the HPA spec for explicitly configured tolerance
// values on scaleDown or scaleUp behavior, falling back to the Kubernetes
// default of 0.1 (10%).
func detectTolerance(hpa *autoscalingv2.HorizontalPodAutoscaler) Assumption {
	tolerance := extractSpecTolerance(hpa)
	if tolerance != nil {
		return Assumption{
			Name:        "GlobalTolerance",
			Value:       tolerance.String(),
			Source:      "hpa.spec",
			Confidence:  "high",
			Impact:      "Scaling is suppressed when the metric ratio is within the tolerance band around the target",
			Description: "The tolerance value defines the band around the target metric within which the HPA will not trigger scaling. Default is 0.1 (10%).",
		}
	}

	return Assumption{
		Name:        "GlobalTolerance",
		Value:       "0.1 (10%)",
		Source:      "kubernetes-default",
		Confidence:  "medium",
		Impact:      "Scaling is suppressed when the metric ratio is within the tolerance band around the target",
		Description: "The tolerance value defines the band around the target metric within which the HPA will not trigger scaling. Default is 0.1 (10%).",
	}
}

// detectCPUInitializationPeriod returns the CPU initialization period assumption.
// This is a controller-manager flag not observable via the HPA API.
func detectCPUInitializationPeriod() Assumption {
	return Assumption{
		Name:        "CPUInitializationPeriod",
		Value:       "5m0s",
		Source:      "kubernetes-default",
		Confidence:  "low",
		Impact:      "CPU metrics from pods younger than this period may be ignored during scaling calculations",
		Description: "The horizontal-pod-autoscaler-cpu-initialization-period flag (default 5m) specifies the period after pod start during which CPU metrics are not considered.",
	}
}

// detectInitialReadinessDelay returns the initial readiness delay assumption.
// This is a controller-manager flag not observable via the HPA API.
func detectInitialReadinessDelay() Assumption {
	return Assumption{
		Name:        "InitialReadinessDelay",
		Value:       "30s",
		Source:      "kubernetes-default",
		Confidence:  "low",
		Impact:      "Pods that become ready within this delay after start may be considered unready for scaling calculations",
		Description: "The horizontal-pod-autoscaler-initial-readiness-delay flag (default 30s) specifies the time after pod start during which readiness is not considered stable.",
	}
}

// detectDownscaleStabilization checks the HPA spec for the scaleDown
// stabilization window, falling back to the Kubernetes default of 300s.
func detectDownscaleStabilization(hpa *autoscalingv2.HorizontalPodAutoscaler) Assumption {
	if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleDown != nil &&
		hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != nil {
		val := *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
		return Assumption{
			Name:        "DownscaleStabilization",
			Value:       fmt.Sprintf("%ds", val),
			Source:      "hpa.spec",
			Confidence:  "high",
			Impact:      "Scale-down decisions are held for the stabilization window to prevent flapping",
			Description: "The scaleDown stabilizationWindowSeconds prevents repeated scale-down by remembering past recommendations. Default is 300s.",
		}
	}

	return Assumption{
		Name:        "DownscaleStabilization",
		Value:       "300s",
		Source:      "kubernetes-default",
		Confidence:  "medium",
		Impact:      "Scale-down decisions are held for the stabilization window to prevent flapping",
		Description: "The scaleDown stabilizationWindowSeconds prevents repeated scale-down by remembering past recommendations. Default is 300s.",
	}
}

// detectUpscaleStabilization checks the HPA spec for the scaleUp
// stabilization window, falling back to the Kubernetes default of 0s.
func detectUpscaleStabilization(hpa *autoscalingv2.HorizontalPodAutoscaler) Assumption {
	if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleUp != nil &&
		hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds != nil {
		val := *hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds
		return Assumption{
			Name:        "UpscaleStabilization",
			Value:       fmt.Sprintf("%ds", val),
			Source:      "hpa.spec",
			Confidence:  "high",
			Impact:      "Scale-up decisions can be delayed by this stabilization window",
			Description: "The scaleUp stabilizationWindowSeconds prevents repeated scale-up. Default is 0s (immediate).",
		}
	}

	return Assumption{
		Name:        "UpscaleStabilization",
		Value:       "0s",
		Source:      "kubernetes-default",
		Confidence:  "medium",
		Impact:      "Scale-up decisions can be delayed by this stabilization window",
		Description: "The scaleUp stabilizationWindowSeconds prevents repeated scale-up. Default is 0s (immediate).",
	}
}

// extractSpecTolerance looks for an explicitly configured tolerance in the HPA
// behavior spec. It checks scaleDown first, then scaleUp, returning the first
// non-nil, non-zero tolerance found.
func extractSpecTolerance(hpa *autoscalingv2.HorizontalPodAutoscaler) *resource.Quantity {
	if hpa.Spec.Behavior == nil {
		return nil
	}
	if hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.Tolerance != nil &&
		!hpa.Spec.Behavior.ScaleDown.Tolerance.IsZero() {
		return hpa.Spec.Behavior.ScaleDown.Tolerance
	}
	if hpa.Spec.Behavior.ScaleUp != nil && hpa.Spec.Behavior.ScaleUp.Tolerance != nil &&
		!hpa.Spec.Behavior.ScaleUp.Tolerance.IsZero() {
		return hpa.Spec.Behavior.ScaleUp.Tolerance
	}
	return nil
}

// buildAssumptionsSummary generates a one-line summary describing the overall
// confidence level of the detected assumptions.
func buildAssumptionsSummary(ca *ControllerAssumptions) string {
	lowCount := 0
	mediumCount := 0
	highCount := 0
	overriddenCount := 0

	assumptions := []Assumption{
		ca.SyncPeriod,
		ca.GlobalTolerance,
		ca.CPUInitializationPeriod,
		ca.InitialReadinessDelay,
		ca.DownscaleStabilization,
		ca.UpscaleStabilization,
	}

	for _, a := range assumptions {
		switch a.Confidence {
		case "low":
			lowCount++
		case "medium":
			mediumCount++
		case "high":
			highCount++
		case "overridden":
			overriddenCount++
		}
	}

	total := len(assumptions)
	explicit := highCount + overriddenCount

	switch {
	case explicit == total:
		return fmt.Sprintf("All %d assumptions are explicitly configured (high confidence).", total)
	case explicit > 0:
		return fmt.Sprintf("%d of %d assumptions are explicitly configured; %d use documented defaults; %d rely on unobservable controller-manager flags.",
			explicit, total, mediumCount, lowCount)
	default:
		return fmt.Sprintf("No assumptions are explicitly configured; %d use documented defaults; %d rely on unobservable controller-manager flags.",
			mediumCount, lowCount)
	}
}

// buildAssumptionsWarnings generates warning strings for assumptions that
// carry operational risk due to default or low-confidence values.
// Overridden values are excluded from warnings since they are explicitly set.
func buildAssumptionsWarnings(ca *ControllerAssumptions) []string {
	var warnings []string

	if ca.CPUInitializationPeriod.Source == "overridden" || ca.CPUInitializationPeriod.Source == "hpa.spec" {
		// No warning — value is explicitly known.
	} else if ca.CPUInitializationPeriod.Confidence == "low" {
		warnings = append(warnings, "CPU metrics from recently started pods may be ignored.")
	}

	if ca.GlobalTolerance.Source == "kubernetes-default" {
		warnings = append(warnings, "Ratio within 10% of target may not trigger scaling.")
	}

	if ca.InitialReadinessDelay.Source == "overridden" || ca.InitialReadinessDelay.Source == "hpa.spec" {
		// No warning — value is explicitly known.
	} else if ca.InitialReadinessDelay.Confidence == "low" {
		warnings = append(warnings, "Pods becoming ready shortly after start may not be counted.")
	}

	if ca.DownscaleStabilization.Source == "kubernetes-default" {
		warnings = append(warnings, "Scale-down may be delayed by default 300s stabilization.")
	}

	return warnings
}
