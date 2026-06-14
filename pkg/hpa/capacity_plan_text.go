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

	appendCapacityCurrentState(out, plan)
	appendCapacityProjectedState(out, plan)
	appendCapacityChecks(out, plan, theme)
	appendCapacitySchedulableNow(out, plan)
	appendCapacityNodeAutoscaler(out, plan)
	appendCapacityDryRun(out, plan)
	appendCapacityRecommendation(out, plan, theme)
	appendCapacityNextActions(out, plan)
}

func appendCapacityCurrentState(out *[]byte, plan *CapacityPlan) {
	*out = append(*out, "  Current:\n"...)
	*out = fmt.Appendf(*out, "    replicas: %d / maxReplicas: %d\n", plan.CurrentReplicas, plan.MaxReplicas)
	*out = fmt.Appendf(*out, "    issue: %s\n", plan.Issue)
}

func appendCapacityProjectedState(out *[]byte, plan *CapacityPlan) {
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
}

func appendCapacityChecks(out *[]byte, plan *CapacityPlan, theme style.Theme) {
	if len(plan.Checks) == 0 {
		return
	}
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

func appendCapacitySchedulableNow(out *[]byte, plan *CapacityPlan) {
	if plan.SchedulableNow <= 0 {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "  Schedulable now: likely %d replicas\n", plan.SchedulableNow)
}

func appendCapacityNodeAutoscaler(out *[]byte, plan *CapacityPlan) {
	*out = append(*out, '\n')
	if plan.NodeAutoscalerRequired {
		*out = fmt.Appendf(*out, "  Node autoscaler required: yes\n")
	} else {
		*out = fmt.Appendf(*out, "  Node autoscaler required: no\n")
	}
}

func appendCapacityDryRun(out *[]byte, plan *CapacityPlan) {
	if plan.DryRunCommand == "" {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "  Dry-run: %s\n", plan.DryRunCommand)
}

func appendCapacityRecommendation(out *[]byte, plan *CapacityPlan, theme style.Theme) {
	*out = append(*out, '\n')
	*out = append(*out, "  Recommendation:\n"...)
	for _, line := range wrapLines(plan.Recommendation, 76) {
		if plan.Safe {
			*out = fmt.Appendf(*out, "    %s\n", theme.OK.Render(line))
		} else {
			*out = fmt.Appendf(*out, "    %s\n", theme.Warning.Render(line))
		}
	}
}

func appendCapacityNextActions(out *[]byte, plan *CapacityPlan) {
	if len(plan.NextActions) == 0 {
		return
	}
	*out = append(*out, '\n')
	for _, action := range plan.NextActions {
		*out = fmt.Appendf(*out, "    - %s\n", action)
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

	lbls := defaultCapacityPlanLabels()
	AppendCapacityPlanText(&out, plan, theme, lbls)

	_, err := w.Write(out)
	return err
}

// defaultCapacityPlanLabels returns English labels for standalone capacity
// plan text output.
func defaultCapacityPlanLabels() labels {
	return labels{
		CapacityPlan: "Capacity Plan",
	}
}
