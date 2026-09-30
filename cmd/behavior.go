package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/scaling"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

type behaviorOutput struct {
	Namespace string              `json:"namespace" yaml:"namespace"`
	Name      string              `json:"name" yaml:"name"`
	Current   int32               `json:"currentReplicas" yaml:"currentReplicas"`
	Desired   int32               `json:"desiredReplicas" yaml:"desiredReplicas"`
	ScaleUp   behaviorDirection   `json:"scaleUp" yaml:"scaleUp"`
	ScaleDown behaviorDirection   `json:"scaleDown" yaml:"scaleDown"`
	Path      []behaviorPathPoint `json:"estimatedPath,omitempty" yaml:"estimatedPath,omitempty"`
}

type behaviorDirection struct {
	StabilizationWindowSeconds int32                  `json:"stabilizationWindowSeconds" yaml:"stabilizationWindowSeconds"`
	SelectPolicy               string                 `json:"selectPolicy" yaml:"selectPolicy"`
	Policies                   []behaviorPolicyOutput `json:"policies,omitempty" yaml:"policies,omitempty"`
}

type behaviorPolicyOutput struct {
	Type          string `json:"type" yaml:"type"`
	Value         int32  `json:"value" yaml:"value"`
	PeriodSeconds int32  `json:"periodSeconds" yaml:"periodSeconds"`
	Summary       string `json:"summary" yaml:"summary"`
}

type behaviorPathPoint struct {
	AfterSeconds int32 `json:"afterSeconds" yaml:"afterSeconds"`
	Replicas     int32 `json:"replicas" yaml:"replicas"`
}

func newBehaviorCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "behavior NAME",
		Short:             "Visualize HPA scaleUp and scaleDown behavior policies",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBehavior(cmd.Context(), cmd.OutOrStdout(), opts, args[0])
		},
	}
}

func runBehavior(ctx context.Context, out io.Writer, opts *options, name string) error {
	_, hpa, err := lookupHPA(ctx, opts, name)
	if err != nil {
		return err
	}

	result := buildBehaviorOutput(hpa)
	return renderWithOutput(out, opts, result, func(out io.Writer) error {
		return writeBehaviorText(out, result)
	})

}

func buildBehaviorOutput(hpa *autoscalingv2.HorizontalPodAutoscaler) behaviorOutput {
	out := behaviorOutput{
		Namespace: hpa.Namespace,
		Name:      hpa.Name,
		Current:   hpa.Status.CurrentReplicas,
		Desired:   hpa.Status.DesiredReplicas,
	}
	if hpa.Spec.Behavior != nil {
		out.ScaleUp = describeBehaviorDirection(hpa.Spec.Behavior.ScaleUp, "scaleUp")
		out.ScaleDown = describeBehaviorDirection(hpa.Spec.Behavior.ScaleDown, "scaleDown")
	} else {
		out.ScaleUp = describeBehaviorDirection(nil, "scaleUp")
		out.ScaleDown = describeBehaviorDirection(nil, "scaleDown")
	}
	out.Path = estimateBehaviorPath(out.Current, out.Desired, out.ScaleUp, out.ScaleDown)
	return out
}

func describeBehaviorDirection(rules *autoscalingv2.HPAScalingRules, direction string) behaviorDirection {
	result := behaviorDirection{SelectPolicy: "Max"}
	if rules == nil {
		return result
	}
	if rules.StabilizationWindowSeconds != nil {
		result.StabilizationWindowSeconds = *rules.StabilizationWindowSeconds
	}
	if rules.SelectPolicy != nil {
		result.SelectPolicy = string(*rules.SelectPolicy)
	}
	for _, policy := range rules.Policies {
		summary := fmt.Sprintf("%s%d per %ds", policyPrefix(direction), policy.Value, policy.PeriodSeconds)
		if policy.Type == autoscalingv2.PercentScalingPolicy {
			summary = fmt.Sprintf("%s%d%% per %ds", policyPrefix(direction), policy.Value, policy.PeriodSeconds)
		}
		result.Policies = append(result.Policies, behaviorPolicyOutput{
			Type:          string(policy.Type),
			Value:         policy.Value,
			PeriodSeconds: policy.PeriodSeconds,
			Summary:       summary,
		})
	}
	return result
}

func policyPrefix(direction string) string {
	if direction == "scaleDown" {
		return "-"
	}
	return "+"
}

func estimateBehaviorPath(current, desired int32, scaleUp, scaleDown behaviorDirection) []behaviorPathPoint {
	if current <= 0 || desired <= 0 || current == desired {
		return nil
	}
	rules := scaleUp
	direction := int32(1)
	if desired < current {
		rules = scaleDown
		direction = -1
	}
	if strings.EqualFold(rules.SelectPolicy, string(autoscalingv2.DisabledPolicySelect)) {
		return []behaviorPathPoint{{AfterSeconds: 0, Replicas: current}}
	}
	stepSeconds := minPositivePeriod(rules.Policies)
	if stepSeconds == 0 {
		return []behaviorPathPoint{{AfterSeconds: 0, Replicas: desired}}
	}
	// Evaluate at the greatest common divisor so different periods expire at
	// their own boundaries rather than inheriting the shortest policy's cadence.
	tick := stepSeconds
	policies := make([]autoscalingv2.HPAScalingPolicy, 0, len(rules.Policies))
	for _, policy := range rules.Policies {
		if policy.PeriodSeconds <= 0 {
			continue
		}
		tick = behaviorPeriodGCD(tick, policy.PeriodSeconds)
		policies = append(policies, autoscalingv2.HPAScalingPolicy{Type: autoscalingv2.HPAScalingPolicyType(policy.Type), Value: policy.Value, PeriodSeconds: policy.PeriodSeconds})
	}
	replicas := current
	var path []behaviorPathPoint
	var history []scaling.Event
	delay := int64(max(int32(0), rules.StabilizationWindowSeconds))
	for elapsed := delay + int64(stepSeconds); elapsed <= delay+int64(stepSeconds)*20 && replicas != desired; elapsed += int64(tick) {
		limit, ok := scaling.Limit(replicas, direction > 0, autoscalingv2.ScalingPolicySelect(rules.SelectPolicy), policies, elapsed, history)
		if !ok {
			break
		}
		next := max(desired, limit)
		if direction > 0 {
			next = min(desired, limit)
		}
		if next == replicas {
			continue
		}
		history = append(history, scaling.Event{TimeSeconds: elapsed, Change: next - replicas})
		replicas = next
		if elapsed > int64(1<<31-1) {
			break
		}
		path = append(path, behaviorPathPoint{AfterSeconds: int32(elapsed), Replicas: replicas}) // #nosec G115 -- bounded above.
	}
	return path
}

func behaviorPeriodGCD(a, b int32) int32 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func minPositivePeriod(policies []behaviorPolicyOutput) int32 {
	var minPeriod int32
	for _, policy := range policies {
		if policy.PeriodSeconds <= 0 {
			continue
		}
		if minPeriod == 0 || policy.PeriodSeconds < minPeriod {
			minPeriod = policy.PeriodSeconds
		}
	}
	return minPeriod
}

func writeBehaviorText(out io.Writer, result behaviorOutput) error {
	var buffer strings.Builder
	writeBehaviorTextTo(&buffer, result)
	_, err := io.WriteString(out, buffer.String())
	return err
}

func writeBehaviorTextTo(out io.Writer, result behaviorOutput) {
	_, _ = fmt.Fprintf(out, "HPA behavior: %s/%s\n\n", result.Namespace, result.Name)
	_, _ = fmt.Fprintf(out, "current=%d desired=%d\n\n", result.Current, result.Desired)
	writeBehaviorDirectionText(out, "ScaleUp", result.ScaleUp)
	writeBehaviorDirectionText(out, "ScaleDown", result.ScaleDown)
	if len(result.Path) > 0 {
		_, _ = fmt.Fprintln(out, "\nEstimated path:")
		for _, point := range result.Path {
			_, _ = fmt.Fprintf(out, "  t+%ds: %d\n", point.AfterSeconds, point.Replicas)
		}
	}
}

func writeBehaviorDirectionText(out io.Writer, title string, direction behaviorDirection) {
	_, _ = fmt.Fprintf(out, "%s behavior:\n", title)
	_, _ = fmt.Fprintf(out, "  stabilizationWindowSeconds: %d\n", direction.StabilizationWindowSeconds)
	_, _ = fmt.Fprintf(out, "  selectPolicy: %s\n", direction.SelectPolicy)
	if len(direction.Policies) == 0 {
		_, _ = fmt.Fprintln(out, "  policies: <default controller behavior>")
		return
	}
	_, _ = fmt.Fprintln(out, "  policies:")
	for _, policy := range direction.Policies {
		_, _ = fmt.Fprintf(out, "    - %s\n", policy.Summary)
	}
}
