package cmd

import (
	"fmt"
	"io"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// writeAIContext renders a compact, LLM-friendly summary of one HPA report.
// All write failures are wrapped with a "write ai context" prefix to match the
// project's convention (see cmd/list_apply.go) and aid debugging.
func writeAIContext(out io.Writer, report hpaanalysis.StatusReport, question string) error {
	return writeAIContextMany(out, []hpaanalysis.StatusReport{report}, question)
}

func writeAIContextMany(out io.Writer, reports []hpaanalysis.StatusReport, question string) error {
	if _, err := fmt.Fprintln(out, "# HPA AI Context"); err != nil {
		return fmt.Errorf("write ai context header: %w", err)
	}
	if question != "" {
		if _, err := fmt.Fprintf(out, "\nQuestion: %s\n", question); err != nil {
			return fmt.Errorf("write ai context question: %w", err)
		}
	}
	if _, err := fmt.Fprintln(out, "\nUse this as local LLM context. Do not assume hidden HPA controller internals beyond the evidence listed here."); err != nil {
		return fmt.Errorf("write ai context preamble: %w", err)
	}
	for _, report := range reports {
		if err := writeAIContextReport(out, report.Analysis); err != nil {
			return err
		}
	}
	return nil
}

func writeAIContextReport(out io.Writer, a hpaanalysis.Analysis) error {
	if _, err := fmt.Fprintf(out, "\n## %s/%s\n", a.Namespace, a.Name); err != nil {
		return fmt.Errorf("write ai context heading: %w", err)
	}
	if _, err := fmt.Fprintf(out, "- target: %s\n- replicas: current=%d desired=%d min=%d max=%d\n- health: %s (%d/100)\n- summary: %s\n",
		a.Target, a.Current, a.Desired, a.Min, a.Max, a.Health, a.HealthScore, a.Summary); err != nil {
		return fmt.Errorf("write ai context summary: %w", err)
	}
	if err := writeAIContextConditions(out, a.Conditions); err != nil {
		return err
	}
	if err := writeAIContextMetrics(out, a.Metrics); err != nil {
		return err
	}
	if err := writeAIContextHiddenFactors(out, a.HiddenFactors); err != nil {
		return err
	}
	return writeAIContextSuggestions(out, a.Suggestions)
}

func writeAIContextConditions(out io.Writer, conditions []hpaanalysis.Condition) error {
	if len(conditions) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "- conditions:"); err != nil {
		return fmt.Errorf("write ai context conditions header: %w", err)
	}
	for _, condition := range conditions {
		if _, err := fmt.Fprintf(out, "  - %s=%s reason=%s message=%s\n", condition.Type, condition.Status, condition.Reason, condition.Message); err != nil {
			return fmt.Errorf("write ai context conditions: %w", err)
		}
	}
	return nil
}

func writeAIContextMetrics(out io.Writer, metrics []hpaanalysis.Metric) error {
	if len(metrics) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "- metrics:"); err != nil {
		return fmt.Errorf("write ai context metrics header: %w", err)
	}
	for _, metric := range metrics {
		if _, err := fmt.Fprintf(out, "  - %s/%s current=%s target=%s note=%s\n", metric.Type, metric.Name, metric.Current, metric.Target, metric.Note); err != nil {
			return fmt.Errorf("write ai context metrics: %w", err)
		}
	}
	return nil
}

func writeAIContextHiddenFactors(out io.Writer, factors []hpaanalysis.HiddenDecisionFactor) error {
	if len(factors) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "- hidden decision factors:"); err != nil {
		return fmt.Errorf("write ai context hidden factors header: %w", err)
	}
	for _, factor := range factors {
		if _, err := fmt.Fprintf(out, "  - %s: %s impact=%s confidence=%s\n", factor.Name, factor.Status, factor.Impact, factor.Confidence); err != nil {
			return fmt.Errorf("write ai context hidden factors: %w", err)
		}
	}
	return nil
}

func writeAIContextSuggestions(out io.Writer, suggestions []hpaanalysis.Suggestion) error {
	if len(suggestions) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "- safe suggestions:"); err != nil {
		return fmt.Errorf("write ai context suggestions header: %w", err)
	}
	for _, suggestion := range suggestions {
		if _, err := fmt.Fprintf(out, "  - %s: %s\n", suggestion.Title, suggestion.Description); err != nil {
			return fmt.Errorf("write ai context suggestions: %w", err)
		}
	}
	return nil
}
