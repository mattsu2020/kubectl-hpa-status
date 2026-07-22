// Package behavioradvisor evaluates HPA scaling-behavior configuration
// (stabilization windows, tolerance, policies) and suggests tuning. It is a
// pure, dependency-free analysis package; the behavior_advisor_text.go
// renderer stays in pkg/hpa because it shares the labels machinery.
package behavioradvisor

import (
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/confidence"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Input aggregates signals for behavior tuning analysis.
type Input struct {
	// HasExplicitBehavior is true when spec.behavior is set.
	HasExplicitBehavior bool
	// ScaleDownWindow is the configured scaleDown.stabilizationWindowSeconds, nil if unset.
	ScaleDownWindow *int32
	// ScaleUpWindow is the configured scaleUp.stabilizationWindowSeconds, nil if unset.
	ScaleUpWindow *int32
	// ScaleDownPolicies lists scaleDown policy strings.
	ScaleDownPolicies []string
	// ScaleUpPolicies lists scaleUp policy strings.
	ScaleUpPolicies []string
	// ScaleDownSelectPolicy is the scaleDown selectPolicy (Min, Max, Disabled).
	ScaleDownSelectPolicy string
	// ScaleUpSelectPolicy is the scaleUp selectPolicy.
	ScaleUpSelectPolicy string
	// ScaleDownTolerance is the configured scaleDown tolerance, nil if unset.
	ScaleDownTolerance *resource.Quantity
	// ScaleUpTolerance is the configured scaleUp tolerance, nil if unset.
	ScaleUpTolerance *resource.Quantity
	// CurrentReplicas is the current replica count.
	CurrentReplicas int32
	// DesiredReplicas is the desired replica count.
	DesiredReplicas int32
}

// Result holds the behavior tuning advisor analysis.
type Result struct {
	// Findings lists all behavior tuning findings.
	Findings []Finding `json:"findings" yaml:"findings"`
	// Summary is a one-line summary of the behavior tuning analysis.
	Summary string `json:"summary" yaml:"summary"`
}

// Finding represents a single behavior tuning finding.
type Finding struct {
	// ID is a unique identifier for the finding.
	ID string `json:"id" yaml:"id"`
	// Category classifies the finding: "stabilization", "tolerance", "policy".
	Category string `json:"category" yaml:"category"`
	// Severity is the finding severity: info, warning, error.
	Severity confidence.Severity `json:"severity" yaml:"severity"`
	// Message is a human-readable description of the finding.
	Message string `json:"message" yaml:"message"`
	// Current shows the current configuration value.
	Current string `json:"current,omitempty" yaml:"current,omitempty"`
	// Recommended shows the recommended configuration value.
	Recommended string `json:"recommended,omitempty" yaml:"recommended,omitempty"`
	// Patch is a kubectl patch command to fix the finding.
	Patch string `json:"patch,omitempty" yaml:"patch,omitempty"`
}

// Analyze evaluates HPA behavior configuration and suggests tuning.
// This is a pure function with no Kubernetes API dependencies.
func Analyze(hpa *autoscalingv2.HorizontalPodAutoscaler) *Result {
	if hpa == nil {
		return nil
	}

	input := extractBehaviorAdvisorInput(hpa)
	var findings []Finding

	// Check stabilization windows.
	findings = append(findings, checkStabilizationWindow(input)...)

	// Check tolerance.
	findings = append(findings, checkTolerance(input)...)

	// Check policies.
	findings = append(findings, checkPolicies(input)...)

	if len(findings) == 0 {
		return &Result{
			Findings: nil,
			Summary:  "Behavior configuration looks reasonable. No tuning recommendations.",
		}
	}

	summary := buildBehaviorAdvisorSummary(findings)
	return &Result{
		Findings: findings,
		Summary:  summary,
	}
}

// extractBehaviorAdvisorInput extracts behavior advisor input from an HPA.
func extractBehaviorAdvisorInput(hpa *autoscalingv2.HorizontalPodAutoscaler) Input {
	input := Input{
		HasExplicitBehavior: hpa.Spec.Behavior != nil,
		CurrentReplicas:     hpa.Status.CurrentReplicas,
		DesiredReplicas:     hpa.Status.DesiredReplicas,
	}

	if hpa.Spec.Behavior == nil {
		return input
	}

	if hpa.Spec.Behavior.ScaleDown != nil {
		input.ScaleDownWindow = hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
		if hpa.Spec.Behavior.ScaleDown.SelectPolicy != nil {
			input.ScaleDownSelectPolicy = string(*hpa.Spec.Behavior.ScaleDown.SelectPolicy)
		}
		if hpa.Spec.Behavior.ScaleDown.Tolerance != nil {
			input.ScaleDownTolerance = hpa.Spec.Behavior.ScaleDown.Tolerance
		}
		for _, p := range hpa.Spec.Behavior.ScaleDown.Policies {
			input.ScaleDownPolicies = append(input.ScaleDownPolicies,
				fmt.Sprintf("%s:%d/%ds", string(p.Type), p.Value, p.PeriodSeconds))
		}
	}

	if hpa.Spec.Behavior.ScaleUp != nil {
		input.ScaleUpWindow = hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds
		if hpa.Spec.Behavior.ScaleUp.SelectPolicy != nil {
			input.ScaleUpSelectPolicy = string(*hpa.Spec.Behavior.ScaleUp.SelectPolicy)
		}
		if hpa.Spec.Behavior.ScaleUp.Tolerance != nil {
			input.ScaleUpTolerance = hpa.Spec.Behavior.ScaleUp.Tolerance
		}
		for _, p := range hpa.Spec.Behavior.ScaleUp.Policies {
			input.ScaleUpPolicies = append(input.ScaleUpPolicies,
				fmt.Sprintf("%s:%d/%ds", string(p.Type), p.Value, p.PeriodSeconds))
		}
	}

	return input
}

// checkStabilizationWindow checks stabilization window settings.
func checkStabilizationWindow(input Input) []Finding {
	var findings []Finding

	// Scale-down stabilization window analysis.
	if input.ScaleDownWindow != nil {
		window := *input.ScaleDownWindow
		if window > 600 {
			minutes := window / 60
			findings = append(findings, Finding{
				ID:       "behavior-scaledown-window-long",
				Category: "stabilization",
				Severity: confidence.Warning,
				Message: fmt.Sprintf(
					"Scale-down stabilization window is %d seconds (%d minutes). "+
						"Scale-down may remain suppressed for up to %d minutes after load drops.",
					window, minutes, minutes),
				Current:     fmt.Sprintf("%ds", window),
				Recommended: "For latency-sensitive: ≤120s. For cost-sensitive: ≤300s.",
				Patch:       `kubectl patch hpa <name> --type=merge -p '{"spec":{"behavior":{"scaleDown":{"stabilizationWindowSeconds":300}}}}'`,
			})
		}
	} else {
		findings = append(findings, Finding{
			ID:       "behavior-scaledown-window-default",
			Category: "stabilization",
			Severity: confidence.Info,
			Message: "Scale-down stabilization window is not explicitly set. " +
				"The controller-manager default (300s) applies implicitly. " +
				"Explicit configuration prevents surprise behavior changes across Kubernetes upgrades.",
			Current:     "unset (default 300s)",
			Recommended: "Set stabilizationWindowSeconds explicitly",
			Patch:       `kubectl patch hpa <name> --type=merge -p '{"spec":{"behavior":{"scaleDown":{"stabilizationWindowSeconds":300}}}}'`,
		})
	}

	// Scale-up stabilization window analysis.
	if input.ScaleUpWindow != nil && *input.ScaleUpWindow > 120 {
		findings = append(findings, Finding{
			ID:       "behavior-scaleup-window-long",
			Category: "stabilization",
			Severity: confidence.Warning,
			Message: fmt.Sprintf(
				"Scale-up stabilization window is %d seconds. "+
					"This delays adding capacity under load. For bursty workloads, consider ≤60s.",
				*input.ScaleUpWindow),
			Current:     fmt.Sprintf("%ds", *input.ScaleUpWindow),
			Recommended: "≤60s for latency-sensitive workloads",
		})
	}

	return findings
}

// checkTolerance checks tolerance settings.
func checkTolerance(input Input) []Finding {
	var findings []Finding

	defaultTolerance := 0.1

	scaleDownTol := defaultTolerance
	if input.ScaleDownTolerance != nil {
		scaleDownTol = input.ScaleDownTolerance.AsApproximateFloat64()
	}

	scaleUpTol := defaultTolerance
	if input.ScaleUpTolerance != nil {
		scaleUpTol = input.ScaleUpTolerance.AsApproximateFloat64()
	}

	if scaleDownTol > 0.2 {
		findings = append(findings, Finding{
			ID:       "behavior-tolerance-scaledown-loose",
			Category: "tolerance",
			Severity: confidence.Info,
			Message: fmt.Sprintf(
				"Scale-down tolerance is %.0f%%. A loose tolerance means small metric fluctuations "+
					"won't trigger scale-down, which can delay releasing unused capacity.",
				scaleDownTol*100),
			Current:     fmt.Sprintf("%.0f%%", scaleDownTol*100),
			Recommended: "10% (default) for most workloads",
		})
	}

	if scaleUpTol > 0.15 && scaleUpTol != defaultTolerance {
		findings = append(findings, Finding{
			ID:       "behavior-tolerance-scaleup-loose",
			Category: "tolerance",
			Severity: confidence.Warning,
			Message: fmt.Sprintf(
				"Scale-up tolerance is %.0f%%. A loose scale-up tolerance may delay adding capacity "+
					"under load, leading to latency spikes.",
				scaleUpTol*100),
			Current:     fmt.Sprintf("%.0f%%", scaleUpTol*100),
			Recommended: "≤10% for latency-sensitive workloads",
		})
	}

	if input.ScaleUpTolerance == nil && input.ScaleDownTolerance == nil && !input.HasExplicitBehavior {
		findings = append(findings, Finding{
			ID:       "behavior-tolerance-default",
			Category: "tolerance",
			Severity: confidence.Info,
			Message: "Tolerance is not explicitly set. The default 0.1 (10%) applies. " +
				"Workloads with tight scaling requirements may benefit from a narrower tolerance.",
			Current:     "default (0.1 / 10%)",
			Recommended: "Consider setting an explicit tolerance if the default is too loose",
		})
	}

	return findings
}

// checkPolicies checks scaling policy settings.
func checkPolicies(input Input) []Finding {
	var findings []Finding

	if len(input.ScaleDownPolicies) == 0 && input.HasExplicitBehavior {
		findings = append(findings, Finding{
			ID:       "behavior-scaledown-no-policies",
			Category: "policy",
			Severity: confidence.Info,
			Message: "No explicit scaleDown policies configured. " +
				"The HPA controller uses default behavior which may cause aggressive downscaling.",
			Current:     "default scaleDown behavior",
			Recommended: "Add explicit scaleDown policies with bounded rates",
		})
	}

	if len(input.ScaleUpPolicies) == 0 && input.HasExplicitBehavior {
		findings = append(findings, Finding{
			ID:       "behavior-scaleup-no-policies",
			Category: "policy",
			Severity: confidence.Info,
			Message: "No explicit scaleUp policies configured. " +
				"The HPA controller uses default behavior which may not match your workload's scaling needs.",
			Current:     "default scaleUp behavior",
			Recommended: "Add explicit scaleUp policies with bounded rates",
		})
	}

	return findings
}

// buildBehaviorAdvisorSummary creates a one-line summary.
func buildBehaviorAdvisorSummary(findings []Finding) string {
	warnings := 0
	infos := 0
	for _, f := range findings {
		switch f.Severity {
		case confidence.Warning, confidence.Error:
			warnings++
		default:
			infos++
		}
	}
	if warnings > 0 {
		return fmt.Sprintf("Found %d warnings and %d informational behavior tuning recommendations.", warnings, infos)
	}
	return fmt.Sprintf("Found %d informational behavior tuning recommendations.", infos)
}
