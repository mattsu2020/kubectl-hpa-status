package cmd

import (
	"fmt"
	"io"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func writeAIContext(out io.Writer, report hpaanalysis.StatusReport, question string) error {
	return writeAIContextMany(out, []hpaanalysis.StatusReport{report}, question)
}

func writeAIContextMany(out io.Writer, reports []hpaanalysis.StatusReport, question string) error {
	if _, err := fmt.Fprintln(out, "# HPA AI Context"); err != nil {
		return err
	}
	if question != "" {
		if _, err := fmt.Fprintf(out, "\nQuestion: %s\n", question); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(out, "\nUse this as local LLM context. Do not assume hidden HPA controller internals beyond the evidence listed here."); err != nil {
		return err
	}
	for _, report := range reports {
		a := report.Analysis
		if _, err := fmt.Fprintf(out, "\n## %s/%s\n", a.Namespace, a.Name); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "- target: %s\n- replicas: current=%d desired=%d min=%d max=%d\n- health: %s (%d/100)\n- summary: %s\n",
			a.Target, a.Current, a.Desired, a.Min, a.Max, a.Health, a.HealthScore, a.Summary); err != nil {
			return err
		}
		if len(a.Conditions) > 0 {
			if _, err := fmt.Fprintln(out, "- conditions:"); err != nil {
				return err
			}
			for _, condition := range a.Conditions {
				if _, err := fmt.Fprintf(out, "  - %s=%s reason=%s message=%s\n", condition.Type, condition.Status, condition.Reason, condition.Message); err != nil {
					return err
				}
			}
		}
		if len(a.Metrics) > 0 {
			if _, err := fmt.Fprintln(out, "- metrics:"); err != nil {
				return err
			}
			for _, metric := range a.Metrics {
				if _, err := fmt.Fprintf(out, "  - %s/%s current=%s target=%s note=%s\n", metric.Type, metric.Name, metric.Current, metric.Target, metric.Note); err != nil {
					return err
				}
			}
		}
		if len(a.HiddenFactors) > 0 {
			if _, err := fmt.Fprintln(out, "- hidden decision factors:"); err != nil {
				return err
			}
			for _, factor := range a.HiddenFactors {
				if _, err := fmt.Fprintf(out, "  - %s: %s impact=%s confidence=%s\n", factor.Name, factor.Status, factor.Impact, factor.Confidence); err != nil {
					return err
				}
			}
		}
		if len(a.Suggestions) > 0 {
			if _, err := fmt.Fprintln(out, "- safe suggestions:"); err != nil {
				return err
			}
			for _, suggestion := range a.Suggestions {
				if _, err := fmt.Fprintf(out, "  - %s: %s\n", suggestion.Title, suggestion.Description); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
