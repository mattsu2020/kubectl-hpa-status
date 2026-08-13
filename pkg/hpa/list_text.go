package hpa

import (
	"fmt"
	"io"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/rendutil"
)

func padRight(s string, width int) string {
	return rendutil.PadDisplayWidth(s, width)
}

// WriteListText writes a table-formatted list of HPA items.
func WriteListText(w io.Writer, report ListReport, opts ListTextOptions) error {
	return WriteListTextStreaming(w, report, opts, true)
}

func listHasTrend(items []ListItem) bool {
	for _, item := range items {
		if item.TrendSparkline != "" || item.TrendFlapping {
			return true
		}
	}
	return false
}

// WriteListTextStreaming writes a table-formatted list in the same shape as
// WriteListText, but split across multiple calls so a caller can stream pages
// incrementally without buffering the whole cluster. With first=true the
// header row is written; with first=false only data rows are emitted, so
// consecutive calls produce one continuous table. The trend column is shown
// when any item in the current page carries trend data; because trend is a
// per-page decision, callers streaming with --trend should prefer the
// accumulated path (canStreamList already excludes it).
func WriteListTextStreaming(w io.Writer, report ListReport, opts ListTextOptions, first bool) error {
	t := opts.theme()
	showTrend := listHasTrend(report.Items)
	var out []byte
	writeHeader := func(format string, args ...any) {
		out = fmt.Appendf(out, format, args...)
	}
	writeRow := func(format string, args ...any) {
		out = fmt.Appendf(out, format, args...)
	}
	if opts.Wide {
		if first {
			header := fmt.Sprintf("%s %s %s %s %s %s %s %s %s %s %s %s %s %s",
				padRight("NAMESPACE", 20),
				padRight("NAME", 32),
				padRight("TARGET", 28),
				padRight("CURRENT", 8),
				padRight("DESIRED", 8),
				padRight("DIFF", 8),
				padRight("MIN", 8),
				padRight("MAX", 8),
				padRight("HEALTH", 12),
				padRight("SCORE", 8),
				padRight("METRICS", 20),
				padRight("BEHAVIOR", 28),
				padRight("ISSUE", 32),
				padRight("CONDITIONS", 36))
			if showTrend {
				header = fmt.Sprintf("%s %s", header, padRight("TREND", 20))
			}
			writeHeader("%s %s\n", header, "SUMMARY")
		}
		for _, item := range report.Items {
			row := fmt.Sprintf("%s %s %s %s %s %s %s %s %s %s %s %s %s %s",
				padRight(item.Namespace, 20),
				padRight(item.Name, 32),
				padRight(item.Target, 28),
				padRight(fmt.Sprintf("%d", item.Current), 8),
				padRight(fmt.Sprintf("%d", item.Desired), 8),
				padRight(formatReplicaDiff(item.Desired-item.Current), 8),
				padRight(fmt.Sprintf("%d", item.Min), 8),
				padRight(fmt.Sprintf("%d", item.Max), 8),
				padRight(t.HealthLabel(item.Health, item.HealthScore), 12),
				padRight(fmt.Sprintf("%d", item.HealthScore), 8),
				padRight(item.Metrics, 20),
				padRight(item.Behavior, 28),
				padRight(t.Issue(item.Issue, item.Health), 32),
				padRight(item.Conditions, 36))
			if showTrend {
				row = fmt.Sprintf("%s %s", row, padRight(item.TrendSparkline, 20))
			}
			writeRow("%s %s\n", row, opts.translateSummary(item.Summary, item.SummaryKey))
		}
		_, err := w.Write(out)
		return err
	}

	if first {
		header := fmt.Sprintf("%s %s %s %s %s %s %s",
			padRight("NAMESPACE", 20),
			padRight("NAME", 32),
			padRight("CURRENT", 8),
			padRight("DESIRED", 8),
			padRight("HEALTH", 12),
			padRight("SCORE", 8),
			padRight("ISSUE", 32))
		if showTrend {
			header = fmt.Sprintf("%s %s", header, padRight("TREND", 20))
		}
		writeHeader("%s %s\n", header, "SUMMARY")
	}
	for _, item := range report.Items {
		row := fmt.Sprintf("%s %s %s %s %s %s %s",
			padRight(item.Namespace, 20),
			padRight(item.Name, 32),
			padRight(fmt.Sprintf("%d", item.Current), 8),
			padRight(fmt.Sprintf("%d", item.Desired), 8),
			padRight(t.HealthLabel(item.Health, item.HealthScore), 12),
			padRight(fmt.Sprintf("%d", item.HealthScore), 8),
			padRight(t.Issue(item.Issue, item.Health), 32))
		if showTrend {
			row = fmt.Sprintf("%s %s", row, padRight(item.TrendSparkline, 20))
		}
		writeRow("%s %s\n", row, opts.translateSummary(item.Summary, item.SummaryKey))
	}
	_, err := w.Write(out)
	return err
}

func formatReplicaDiff(diff int32) string {
	if diff > 0 {
		return fmt.Sprintf("+%d", diff)
	}
	return fmt.Sprintf("%d", diff)
}
