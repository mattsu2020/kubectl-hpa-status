package hpa

import (
	"fmt"
	"io"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
)

// AppendCapacityPlanText appends the capacity plan section to out. It renders
// the current state, projected state, check results, and recommendation.
func AppendCapacityPlanText(out *[]byte, plan *CapacityPlan, theme style.Theme, lbls labels) {
	if plan == nil {
		return
	}

	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", lbls.CapacityPlan)

	// Current state.
	*out = append(*out, "  Current:\n"...)
	*out = fmt.Appendf(*out, "    replicas: %d / maxReplicas: %d\n", plan.CurrentReplicas, plan.MaxReplicas)
	*out = fmt.Appendf(*out, "    issue: %s\n", plan.Issue)

	// Projected state.
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "  If maxReplicas is raised to %d:\n", plan.TargetMaxReplicas)
	*out = fmt.Appendf(*out, "    additional pods: %d\n", plan.AdditionalPods)

	parts := []string{}
	if plan.RequiredCPU != "0" && plan.RequiredCPU != "" {
		parts = append(parts, fmt.Sprintf("cpu %s", plan.RequiredCPU))
	}
	if plan.RequiredMemory != "0" && plan.RequiredMemory != "" {
		parts = append(parts, fmt.Sprintf("memory %s", plan.RequiredMemory))
	}
	if len(parts) > 0 {
		*out = fmt.Appendf(*out, "    required: %s\n", strings.Join(parts, ", "))
	}

	// Checks.
	if len(plan.Checks) > 0 {
		*out = append(*out, '\n')
		*out = append(*out, "  Checks:\n"...)
		for _, c := range plan.Checks {
			indicator := theme.OK.Render("✓")
			if !c.Pass {
				indicator = theme.Error.Render("!")
			}
			*out = fmt.Appendf(*out, "    %s %s\n", indicator, c.Message)
		}
	}

	// Schedulable now estimate.
	if plan.SchedulableNow > 0 {
		*out = append(*out, '\n')
		*out = fmt.Appendf(*out, "  Schedulable now: likely %d replicas\n", plan.SchedulableNow)
	}

	// Node autoscaler required.
	*out = append(*out, '\n')
	if plan.NodeAutoscalerRequired {
		*out = fmt.Appendf(*out, "  Node autoscaler required: yes\n")
	} else {
		*out = fmt.Appendf(*out, "  Node autoscaler required: no\n")
	}

	// Dry-run command.
	if plan.DryRunCommand != "" {
		*out = append(*out, '\n')
		*out = fmt.Appendf(*out, "  Dry-run: %s\n", plan.DryRunCommand)
	}

	// Recommendation.
	*out = append(*out, '\n')
	*out = append(*out, "  Recommendation:\n"...)
	if plan.Safe {
		for _, line := range wrapLines(plan.Recommendation, 76) {
			*out = fmt.Appendf(*out, "    %s\n", theme.OK.Render(line))
		}
	} else {
		for _, line := range wrapLines(plan.Recommendation, 76) {
			*out = fmt.Appendf(*out, "    %s\n", theme.Warning.Render(line))
		}
	}

	// Next actions.
	if len(plan.NextActions) > 0 {
		*out = append(*out, '\n')
		for _, action := range plan.NextActions {
			*out = fmt.Appendf(*out, "    - %s\n", action)
		}
	}
}

// WriteCapacityPlanText writes a standalone capacity plan report (used by the
// capacity subcommand) with an HPA header line.
func WriteCapacityPlanText(w io.Writer, plan *CapacityPlan, theme style.Theme) error {
	if plan == nil {
		return nil
	}

	var out []byte
	out = fmt.Appendf(out, "Capacity plan for %s/%s\n\n", plan.Namespace, plan.Target)
	out = fmt.Appendf(out, "HPA: %s\n", plan.Name)
	out = fmt.Appendf(out, "replicas: %d / maxReplicas: %d\n\n", plan.CurrentReplicas, plan.MaxReplicas)

	lbls := DefaultCapacityPlanLabels()
	AppendCapacityPlanText(&out, plan, theme, lbls)

	_, err := w.Write(out)
	return err
}

// DefaultCapacityPlanLabels returns English labels for standalone capacity
// plan text output.
func DefaultCapacityPlanLabels() labels {
	return labels{
		CapacityPlan: "Capacity Plan",
	}
}
