package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/cmd/internal/promhistory"
	"github.com/spf13/cobra"
)

var promLoadTrace = promhistory.LoadTrace

type prometheusImportFlags struct {
	Enabled     bool
	URL         string
	Step        time.Duration
	Since       time.Duration
	MetricQuery string
	MetricName  string
	Token       string
}

func addPrometheusImportFlags(cmd *cobra.Command, p *prometheusImportFlags, includeSince bool) {
	cmd.Flags().BoolVar(&p.Enabled, "from-prometheus", false, "import HPA history from the Prometheus HTTP API instead of Kubernetes events or a record file")
	cmd.Flags().StringVar(&p.URL, "prometheus-url", "", "Prometheus base URL for history import (or set PROMETHEUS_URL)")
	cmd.Flags().DurationVar(&p.Step, "prometheus-step", time.Minute, "query step resolution for Prometheus range queries")
	cmd.Flags().StringVar(&p.MetricQuery, "prometheus-metric-query", "", "optional PromQL expression to populate metricValues on imported snapshots")
	cmd.Flags().StringVar(&p.MetricName, "prometheus-metric-name", "custom", "metric name label for --prometheus-metric-query samples")
	cmd.Flags().StringVar(&p.Token, "prometheus-token", "", "optional bearer token for Prometheus HTTP API authentication")
	if includeSince {
		cmd.Flags().DurationVar(&p.Since, "since", 6*time.Hour, "lookback window when importing history from Prometheus")
	}
}

func (p prometheusImportFlags) requested() bool {
	return p.Enabled || strings.TrimSpace(p.URL) != ""
}

func loadTraceFromPrometheus(
	ctx context.Context,
	opts *options,
	name string,
	since time.Duration,
	p prometheusImportFlags,
) (*promhistory.ImportResult, error) {
	baseURL := strings.TrimSpace(p.URL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("PROMETHEUS_URL"))
	}
	if baseURL == "" {
		return nil, fmt.Errorf("--prometheus-url (or PROMETHEUS_URL) is required when using --from-prometheus")
	}
	if since <= 0 {
		since = 6 * time.Hour
	}
	end := opts.CurrentTime().UTC()
	ns := opts.Namespace
	if ns == "" {
		ns = "default"
	}
	token := strings.TrimSpace(p.Token)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("PROMETHEUS_TOKEN"))
	}
	return promLoadTrace(ctx, promhistory.QueryConfig{
		BaseURL:     baseURL,
		BearerToken: token,
		Namespace:   ns,
		HPAName:     name,
		Start:       end.Add(-since),
		End:         end,
		Step:        p.Step,
		MetricQuery: p.MetricQuery,
		MetricName:  p.MetricName,
	})
}
