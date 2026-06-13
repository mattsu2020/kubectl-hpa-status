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
	"golang.org/x/term"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
		ApplyFn: func(applyCtx context.Context, ns, name, patch string) error {
			_, err := client.Interface.AutoscalingV2().
				HorizontalPodAutoscalers(ns).
				Patch(applyCtx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
			if err != nil {
				return fmt.Errorf("patch failed: %w", err)
			}
			return nil
		},
		AuditFn: func(auditCtx context.Context, ns, name string) (*hpaanalysis.AuditReport, error) {
			hpa, err := client.Interface.AutoscalingV2().
				HorizontalPodAutoscalers(ns).
				Get(auditCtx, name, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("getting HPA %s: %w", name, err)
			}
			minReplicas := hpaanalysis.DefaultMinReplicas
			if hpa.Spec.MinReplicas != nil {
				minReplicas = *hpa.Spec.MinReplicas
			}
			return hpaanalysis.AuditHPA(hpa, minReplicas), nil
		},
	})
	model = model.WithContext(ctx)

	_, err = tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx), tea.WithOutput(out)).Run()
	return err
}

func isInteractiveTerminal(out io.Writer) bool {
	file, ok := out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}
