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
		return nil, fmt.Errorf("failed to create Kubernetes client from kubeconfig/context flags: %w", err)
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

	if opts.dryRun {
		if _, err := fmt.Fprintln(out, "Dry-run mode is enabled; patches were validated but not persisted. Use --dry-run=false to apply changes."); err != nil {
			return nil, err
		}
		return dryRunResults(patches), nil
	}

	// Real apply path: confirm unless skipped.
	if !opts.yes && !skipConfirm {
		if err := confirmApply(out, opts, len(patches), namespace, name); err != nil {
			return nil, err
		}
	}

	return executePatches(ctx, client, namespace, name, patches)
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
	if opts.in == nil {
		opts.in = os.Stdin
	}
	if _, err := fmt.Fprintf(out, "Apply %d patches to HPA %s/%s? [y/N]: ", count, namespace, name); err != nil {
		return err
	}
	scanner := bufio.NewScanner(opts.in)
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

func executePatches(ctx context.Context, client *kube.Client, namespace, name string, patches []hpaanalysis.Suggestion) ([]string, error) {
	patchItems := make([]patch.Patch, len(patches))
	for i, s := range patches {
		patchItems[i] = patch.Patch{Title: s.Title, JSON: s.Patch}
	}
	merged, mergeErr := patch.MergePatches(patchItems)
	if mergeErr == nil {
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

	// Fallback: sequential application.
	var applied []string
	for _, suggestion := range patches {
		_, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(namespace).
			Patch(ctx, name, types.MergePatchType, []byte(suggestion.Patch), metav1.PatchOptions{})
		if err != nil {
			return applied, fmt.Errorf("partial apply: %d/%d succeeded, then failed on %q: %w", len(applied), len(patches), suggestion.Title, err)
		}
		applied = append(applied, fmt.Sprintf("Applied: %s", suggestion.Title))
	}
	return applied, nil
}
