package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/patch"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func applySuggestions(ctx context.Context, out io.Writer, opts *options, name string, suggestions []hpaanalysis.Suggestion) ([]string, error) {
	return applySuggestionsInNamespace(ctx, out, opts, "", name, suggestions, false)
}

//nolint:gocyclo // Multi-phase apply workflow: collect, validate, confirm, merge/apply
func applySuggestionsInNamespace(ctx context.Context, out io.Writer, opts *options, namespace string, name string, suggestions []hpaanalysis.Suggestion, skipConfirm bool) ([]string, error) {
	patches := collectApplicablePatches(suggestions)
	if len(patches) == 0 {
		return []string{"No applicable HPA patch was suggested."}, nil
	}
	client, err := opts.newClient()
	if err != nil {
		return nil, err
	}
	if namespace == "" {
		namespace = client.Namespace
	}

	// Fetch current HPA state once for diff display.
	current, err := client.Interface.AutoscalingV2().
		HorizontalPodAutoscalers(namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get HPA %s/%s: %w", namespace, name, err)
	}

	guardedPatches, err := guardPatches(out, opts, current, patches)
	if err != nil {
		return nil, err
	}
	patches = guardedPatches
	if len(patches) == 0 {
		return []string{"No applicable HPA patch was allowed by policy guard."}, nil
	}

	if err := printProposedPatches(out, current, patches); err != nil {
		return nil, err
	}

	// Phase 1: Pre-validate all patches with server-side dry-run before
	// asking for confirmation or applying anything.
	if err := preValidatePatches(ctx, client, namespace, name, patches); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(out, "All %d patch(es) passed server-side dry-run validation.\n", len(patches)); err != nil {
		return nil, err
	}

	if opts.DryRun {
		if _, err := fmt.Fprintln(out, "Dry-run mode is enabled; patches were validated but not persisted. Use --dry-run=false to apply changes."); err != nil {
			return nil, err
		}
		return dryRunResults(patches), nil
	}

	// Real apply path: confirm unless skipped.
	if !opts.Yes && !skipConfirm {
		if err := confirmApply(out, opts, len(patches), namespace, name); err != nil {
			return nil, err
		}
	}

	return executePatches(ctx, out, client, namespace, name, patches, opts.AllowPartial)
}

func guardPatches(out io.Writer, opts *options, current *autoscalingv2.HorizontalPodAutoscaler, patches []hpaanalysis.Suggestion) ([]hpaanalysis.Suggestion, error) {
	if opts.PolicyGuard == "" {
		return patches, nil
	}
	policyFile, err := hpaanalysis.LoadPolicyFile(opts.PolicyGuard)
	if err != nil {
		return nil, err
	}
	result := hpaanalysis.GuardFix(patches, policyFile, current)
	if err := hpaanalysis.WritePolicyGuardText(out, result); err != nil {
		return nil, err
	}
	switch opts.PolicyGuardMode {
	case "", "block":
		if len(result.Blocked) > 0 {
			return nil, fmt.Errorf("policy guard blocked %d patch(es)", len(result.Blocked))
		}
	case "warn":
	default:
		return nil, fmt.Errorf("invalid --policy-guard-mode %q; use block or warn", opts.PolicyGuardMode)
	}
	return result.Allowed, nil
}

func collectApplicablePatches(suggestions []hpaanalysis.Suggestion) []hpaanalysis.Suggestion {
	var patches []hpaanalysis.Suggestion
	for _, suggestion := range suggestions {
		if suggestion.Apply && suggestion.Patch != "" {
			patches = append(patches, suggestion)
		}
	}
	return patches
}

func printProposedPatches(out io.Writer, current *autoscalingv2.HorizontalPodAutoscaler, patches []hpaanalysis.Suggestion) error {
	for _, suggestion := range patches {
		if _, err := fmt.Fprintf(out, "\nProposed patch: %s\n%s\n", suggestion.Title, hpaanalysis.SuggestionDiff(current.Spec.MinReplicas, current.Status.DesiredReplicas, current.Spec.MaxReplicas, suggestion.Patch)); err != nil {
			return err
		}
	}
	return nil
}

func preValidatePatches(ctx context.Context, client *kube.Client, namespace, name string, patches []hpaanalysis.Suggestion) error {
	dryRunOpts := metav1.PatchOptions{DryRun: []string{metav1.DryRunAll}}
	for _, suggestion := range patches {
		if _, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(namespace).
			Patch(ctx, name, types.MergePatchType, []byte(suggestion.Patch), dryRunOpts); err != nil {
			return fmt.Errorf("pre-validation failed for patch %q: %w", suggestion.Title, err)
		}
	}
	return nil
}

func dryRunResults(patches []hpaanalysis.Suggestion) []string {
	results := make([]string, len(patches))
	for i, suggestion := range patches {
		results[i] = fmt.Sprintf("Dry-run validated: %s", suggestion.Title)
	}
	return results
}

func confirmApply(out io.Writer, opts *options, count int, namespace, name string) error {
	if _, err := fmt.Fprintf(out, "\nWARNING: About to apply %d patch(es) to HPA %s/%s. This will modify the live cluster.\n", count, namespace, name); err != nil {
		return err
	}
	// When stdin was not explicitly wired (e.g. by tests or an embedding
	// caller) and the process stdin is not an interactive terminal, a prompt
	// would either block forever or silently consume non-confirmation input.
	// Require an explicit --yes/-y in that case. A caller that deliberately
	// sets opts.In is allowed to drive the prompt programmatically.
	if opts.In == nil {
		if !stdinIsTerminal(os.Stdin) {
			return fmt.Errorf("cannot prompt for confirmation (stdin is not a terminal); pass --yes/-y to apply non-interactively")
		}
		opts.In = os.Stdin
	}
	if _, err := fmt.Fprintf(out, "Apply %d patches to HPA %s/%s? [y/N]: ", count, namespace, name); err != nil {
		return err
	}
	scanner := bufio.NewScanner(opts.In)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return err
		}
		return fmt.Errorf("apply skipped")
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	if answer != "y" && answer != "yes" {
		return fmt.Errorf("apply skipped")
	}
	return nil
}

func executePatches(ctx context.Context, out io.Writer, client *kube.Client, namespace, name string, patches []hpaanalysis.Suggestion, allowPartial bool) ([]string, error) {
	patchItems := make([]patch.Patch, len(patches))
	for i, s := range patches {
		patchItems[i] = patch.Patch{Title: s.Title, JSON: s.Patch}
	}
	merged, mergeErr := patch.MergePatches(patchItems)
	if mergeErr == nil {
		// Validate the merged patch with a server-side dry-run before applying,
		// so an invalid merge is rejected without touching the HPA.
		if _, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(namespace).
			Patch(ctx, name, types.MergePatchType, []byte(merged), metav1.PatchOptions{DryRun: []string{"All"}}); err != nil {
			return nil, fmt.Errorf("merged patch failed server-side dry-run validation: %w", err)
		}
		// Single merged patch — atomic application.
		_, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(namespace).
			Patch(ctx, name, types.MergePatchType, []byte(merged), metav1.PatchOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to apply merged patch: %w", err)
		}
		applied := make([]string, len(patches))
		for i, suggestion := range patches {
			applied[i] = fmt.Sprintf("Applied: %s", suggestion.Title)
		}
		return applied, nil
	}

	// Merge failed: refuse unless the operator explicitly opted into a
	// non-atomic sequential apply. Without --allow-partial we return an error
	// so a merge failure can never silently leave the HPA partially modified.
	if !allowPartial {
		return nil, fmt.Errorf("patches could not be merged into one atomic patch (%v); pass --allow-partial to apply them sequentially at the risk of a partial modification", mergeErr)
	}

	_, _ = fmt.Fprintf(out, "WARNING: patches could not be merged (%v); falling back to sequential, non-atomic apply.\n", mergeErr)
	_, _ = fmt.Fprintf(out, "WARNING: if a later patch fails, the HPA %s/%s will be left partially modified — inspect it with `kubectl describe hpa %s -n %s` and reconcile manually.\n", namespace, name, name, namespace)

	var applied []string
	for _, suggestion := range patches {
		_, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(namespace).
			Patch(ctx, name, types.MergePatchType, []byte(suggestion.Patch), metav1.PatchOptions{})
		if err != nil {
			return applied, fmt.Errorf("partial apply: %d/%d succeeded, then failed on %q: %w (HPA %s/%s is partially modified; re-run apply or reconcile manually with `kubectl describe hpa %s -n %s`)", len(applied), len(patches), suggestion.Title, err, namespace, name, name, namespace)
		}
		applied = append(applied, fmt.Sprintf("Applied: %s", suggestion.Title))
	}
	return applied, nil
}
