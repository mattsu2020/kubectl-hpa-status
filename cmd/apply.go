package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/patch"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func applySuggestions(ctx context.Context, out io.Writer, opts *options, name string, suggestions []hpaanalysis.Suggestion) ([]string, error) {
	return applySuggestionsInNamespace(ctx, out, opts, "", name, suggestions, false)
}

func applySuggestionsInNamespace(ctx context.Context, out io.Writer, opts *options, namespace string, name string, suggestions []hpaanalysis.Suggestion, skipConfirm bool) ([]string, error) {
	var patches []hpaanalysis.Suggestion
	for _, suggestion := range suggestions {
		if suggestion.Apply && suggestion.Patch != "" {
			patches = append(patches, suggestion)
		}
	}
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

	for _, suggestion := range patches {
		if _, err := fmt.Fprintf(out, "\nProposed patch: %s\n%s\n", suggestion.Title, hpaanalysis.SuggestionDiff(current.Spec.MinReplicas, current.Status.DesiredReplicas, current.Spec.MaxReplicas, suggestion.Patch)); err != nil {
			return nil, err
		}
	}

	// Phase 1: Pre-validate all patches with server-side dry-run before
	// asking for confirmation or applying anything. This catches validation
	// errors early and avoids partial application.
	dryRunOpts := metav1.PatchOptions{DryRun: []string{metav1.DryRunAll}}
	for _, suggestion := range patches {
		if _, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(namespace).
			Patch(ctx, name, types.MergePatchType, []byte(suggestion.Patch), dryRunOpts); err != nil {
			return nil, fmt.Errorf("pre-validation failed for patch %q: %w", suggestion.Title, err)
		}
	}
	if _, err := fmt.Fprintf(out, "All %d patch(es) passed server-side dry-run validation.\n", len(patches)); err != nil {
		return nil, err
	}

	if opts.dryRun {
		if _, err := fmt.Fprintln(out, "Dry-run mode is enabled; patches were validated but not persisted. Use --dry-run=false to apply changes."); err != nil {
			return nil, err
		}
		var results []string
		for _, suggestion := range patches {
			results = append(results, fmt.Sprintf("Dry-run validated: %s", suggestion.Title))
		}
		return results, nil
	}

	// Real apply path: confirm unless skipped.
	if !opts.yes && !skipConfirm {
		if _, err := fmt.Fprintf(out, "\nWARNING: About to apply %d patch(es) to HPA %s/%s. This will modify the live cluster.\n", len(patches), namespace, name); err != nil {
			return nil, err
		}
		if opts.in == nil {
			opts.in = os.Stdin
		}
		if _, err := fmt.Fprintf(out, "Apply %d patches to HPA %s/%s? [y/N]: ", len(patches), namespace, name); err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(opts.in)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return nil, err
			}
			return []string{"Apply skipped."}, nil
		}
		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if answer != "y" && answer != "yes" {
			return []string{"Apply skipped."}, nil
		}
	}

	// Phase 2: Merge all patches into a single JSON merge patch to avoid
	// partial application. If individual patch application is needed
	// (patches target different paths), fall back to sequential apply
	// but track which ones succeeded.
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
		var applied []string
		for _, suggestion := range patches {
			applied = append(applied, fmt.Sprintf("Applied: %s", suggestion.Title))
		}
		return applied, nil
	}

	// Fallback: sequential application. Report partial results on failure.
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
