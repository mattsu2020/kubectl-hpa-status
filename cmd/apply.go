package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func applySuggestions(ctx context.Context, out io.Writer, opts *options, name string, suggestions []hpaanalysis.Suggestion) ([]string, error) {
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
	for _, suggestion := range patches {
		current, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(client.Namespace).
			Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get HPA before applying suggested patch %q: %w", suggestion.Title, err)
		}
		if _, err := fmt.Fprintf(out, "\nProposed patch: %s\n%s\n", suggestion.Title, hpaanalysis.SuggestionDiff(current.Spec.MinReplicas, current.Status.DesiredReplicas, current.Spec.MaxReplicas, suggestion.Patch)); err != nil {
			return nil, err
		}
	}
	if opts.dryRun {
		if _, err := fmt.Fprintln(out, "Dry-run mode is enabled; Kubernetes will validate patches without persisting them. Use --dry-run=false to apply changes."); err != nil {
			return nil, err
		}
	}
	if !opts.yes {
		if opts.in == nil {
			opts.in = os.Stdin
		}
		action := "dry-run"
		if !opts.dryRun {
			action = "apply"
		}
		if _, err := fmt.Fprintf(out, "%s %d suggested patch(es) to HPA %s? [y/N]: ", action, len(patches), name); err != nil {
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

	var applied []string
	patchOptions := metav1.PatchOptions{}
	if opts.dryRun {
		patchOptions.DryRun = []string{metav1.DryRunAll}
	}
	for _, suggestion := range patches {
		_, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(client.Namespace).
			Patch(ctx, name, types.MergePatchType, []byte(suggestion.Patch), patchOptions)
		if err != nil {
			return applied, fmt.Errorf("failed to apply suggested patch %q: %w", suggestion.Title, err)
		}
		if opts.dryRun {
			applied = append(applied, fmt.Sprintf("Dry-run validated: %s", suggestion.Title))
		} else {
			applied = append(applied, fmt.Sprintf("Applied: %s", suggestion.Title))
		}
	}
	return applied, nil
}
