package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	hpakeda "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"
	hpavpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"

	tea "charm.land/bubbletea/v2"
	"github.com/mattsu2020/kubectl-hpa-status/internal/tui"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/audit"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	cmd.Flags().StringVar(&opts.PolicyGuard, "policy-guard", opts.PolicyGuard, "path to a policy file used to guard TUI apply patches")
	cmd.Flags().StringVar(&opts.PolicyGuardMode, "policy-guard-mode", opts.PolicyGuardMode, "policy guard mode for TUI apply: block or warn")
	return cmd
}

func runTUI(ctx context.Context, out io.Writer, opts *options, initialName string, startInDetail bool) error {
	if !isInteractiveTerminal(out) {
		return fmt.Errorf("tui requires an interactive terminal; use 'watch' for non-interactive streaming")
	}

	if opts.WatchTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.WatchTimeout)
		defer cancel()
	}

	interval := opts.WatchInterval
	if interval < time.Second {
		interval = time.Second
	}

	client, err := newClientOrDefault(opts)
	if err != nil {
		return err
	}

	namespace := client.Namespace
	if opts.AllNamespaces {
		namespace = ""
	}

	// Create enrichment context and build a callback for batched enrichment.
	ec := newEnrichmentContext(ctx, opts)
	var enrichFn func(context.Context, []autoscalingv2.HorizontalPodAutoscaler) (map[string]*hpakeda.Analysis, map[string]*hpavpa.ConflictInfo)
	if ec != nil {
		enrichFn = func(enrichCtx context.Context, hpas []autoscalingv2.HorizontalPodAutoscaler) (map[string]*hpakeda.Analysis, map[string]*hpavpa.ConflictInfo) {
			keda, _ := enrichListKEDA(enrichCtx, ec, hpas)
			vpa, _ := enrichListVPA(enrichCtx, ec, hpas)
			return keda, vpa
		}
	}

	liveApplyFn, dryRunFn := newTUIApplyCallbacks(opts)

	model := tui.NewModel(client.Interface, namespace, tui.Options{
		AllNamespaces: opts.AllNamespaces,
		Debug:         opts.Debug,
		ChunkSize:     opts.ChunkSize,
		Interval:      interval,
		InitialName:   initialName,
		InitialNS:     client.Namespace,
		StartInDetail: startInDetail,
		EnrichHPAs:    enrichFn,
		HealthWeights: opts.HealthWeights,
		ApplyFn:       liveApplyFn,
		DryRunFn:      dryRunFn,
		AuditFn: func(auditCtx context.Context, ns, name string) (*audit.Report, error) {
			hpa, err := client.Interface.AutoscalingV2().
				HorizontalPodAutoscalers(ns).
				Get(auditCtx, name, metav1.GetOptions{})
			if err != nil {
				return nil, wrapHPALookupError(client.Namespace, name, err)
			}
			minReplicas := hpaanalysis.DefaultMinReplicas
			if hpa.Spec.MinReplicas != nil {
				minReplicas = *hpa.Spec.MinReplicas
			}
			return audit.Run(hpa, minReplicas), nil
		},
	})
	model = model.WithContext(ctx)

	_, err = tea.NewProgram(model, tea.WithContext(ctx), tea.WithOutput(out)).Run()
	return err
}

func newTUIApplyCallbacks(opts *options) (liveApplyFn, dryRunFn tui.ApplyFunc) {
	dryRunFn = newTUIApplyFunc(opts, true)
	if opts.Apply && !opts.DryRun {
		liveApplyFn = newTUIApplyFunc(opts, false)
	}
	return liveApplyFn, dryRunFn
}

// newTUIApplyFunc adapts the TUI to the same validation, policy, confirmation,
// merge, and optimistic-concurrency workflow used by CLI apply. Persistent
// callbacks are only installed by runTUI after both --apply and
// --dry-run=false were explicitly selected; dry-run callbacks always force
// DryRun=true regardless of the caller's options.
func newTUIApplyFunc(opts *options, dryRun bool) tui.ApplyFunc {
	return func(applyCtx context.Context, namespace, name string, suggestions []hpaanalysis.Suggestion) error {
		applyOpts := copyOptions(opts)
		applyOpts.Apply = true
		applyOpts.DryRun = dryRun
		applyOpts.Yes = true
		_, err := applySuggestionsInNamespace(applyCtx, io.Discard, &applyOpts, namespace, name, suggestions, true)
		return err
	}
}

func isInteractiveTerminal(out io.Writer) bool {
	file, ok := out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}
