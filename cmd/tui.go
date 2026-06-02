package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattsu2020/kubectl-hpa-status/internal/tui"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"golang.org/x/term"
)

func newTUICommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Interactive TUI dashboard for HPA status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTUI(cmd.Context(), cmd.OutOrStdout(), opts, "", false)
		},
	}
	return cmd
}

func runTUI(ctx context.Context, out io.Writer, opts *options, initialName string, startInDetail bool) error {
	if !isInteractiveTerminal(out) {
		return fmt.Errorf("tui requires an interactive terminal; use 'watch' for non-interactive streaming")
	}

	if opts.watchTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.watchTimeout)
		defer cancel()
	}

	interval := opts.watchInterval
	if interval < time.Second {
		interval = time.Second
	}

	client, err := opts.newClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	namespace := client.Namespace
	if opts.allNamespaces {
		namespace = ""
	}

	// Create enrichment context and build a callback for batched enrichment.
	ec := newEnrichmentContext(ctx, opts)
	var enrichFn func(context.Context, []autoscalingv2.HorizontalPodAutoscaler) (map[string]*hpaanalysis.KEDAAnalysis, map[string]*hpaanalysis.VPAConflictInfo)
	if ec != nil {
		enrichFn = func(enrichCtx context.Context, hpas []autoscalingv2.HorizontalPodAutoscaler) (map[string]*hpaanalysis.KEDAAnalysis, map[string]*hpaanalysis.VPAConflictInfo) {
			return enrichListKEDA(enrichCtx, ec, hpas), enrichListVPA(enrichCtx, ec, hpas)
		}
	}

	model := tui.NewModel(client.Interface, namespace, tui.Options{
		AllNamespaces: opts.allNamespaces,
		Debug:         opts.debug,
		ChunkSize:     opts.chunkSize,
		Interval:      interval,
		InitialName:   initialName,
		InitialNS:     client.Namespace,
		StartInDetail: startInDetail,
		EnrichHPAs:    enrichFn,
		HealthWeights: opts.healthWeights,
	})

	_, err = tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx), tea.WithOutput(out)).Run()
	return err
}

func isInteractiveTerminal(out io.Writer) bool {
	file, ok := out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}
