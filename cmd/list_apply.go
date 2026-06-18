package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// This file holds the list/scan batch-apply and patch-export helpers, split
// from list.go so list.go stays focused on listing, filtering, and sorting.

// batchEntry holds a single HPA suggestion for batch patch application.
type batchEntry struct {
	Namespace  string
	Name       string
	Suggestion hpaanalysis.Suggestion
}

func applyListSuggestions(ctx context.Context, out io.Writer, opts *options, hpas []autoscalingv2.HorizontalPodAutoscaler, items []hpaanalysis.ListItem) error {
	selected := map[string]bool{}
	for _, item := range items {
		selected[item.Namespace+"/"+item.Name] = true
	}

	entries := collectBatchEntries(opts, hpas, selected)

	if len(entries) == 0 {
		if _, err := fmt.Fprintln(out, "No applicable HPA patches found."); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	}

	if err := printBatchSummary(out, entries); err != nil {
		return fmt.Errorf("write batch summary: %w", err)
	}

	confirmed, err := confirmBatchApply(out, opts, len(entries))
	if err != nil {
		return fmt.Errorf("confirm batch apply: %w", err)
	}
	if !confirmed {
		return nil
	}

	return executeBatchPatches(ctx, out, opts, entries)
}

func exportListPatchesDirectory(out io.Writer, opts *options, hpas []autoscalingv2.HorizontalPodAutoscaler, items []hpaanalysis.ListItem) error {
	selected := map[string]bool{}
	for _, item := range items {
		selected[item.Namespace+"/"+item.Name] = true
	}
	if len(selected) == 0 {
		if _, err := fmt.Fprintln(out, "No HPAs selected for patch export."); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	}
	dir := "hpa-patches"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create patch export directory %s: %w", dir, err)
	}
	written := 0
	for i := range hpas {
		hpa := &hpas[i]
		if !selected[hpa.Namespace+"/"+hpa.Name] {
			continue
		}
		analysis := hpaanalysis.AnalyzeWithOptions(hpa, true, analysisOptions(opts.HealthWeights, opts.Debug))
		report := hpaanalysis.StatusReport{APIVersion: hpaanalysis.SchemaVersion, Analysis: analysis}
		var buf strings.Builder
		if err := writeGitOpsExport(&buf, "yaml", report); err != nil {
			return fmt.Errorf("render patch for %s/%s: %w", hpa.Namespace, hpa.Name, err)
		}
		if strings.Contains(buf.String(), "no applicable") {
			continue
		}
		path := fmt.Sprintf("%s/%s-%s-hpa-patch.yaml", dir, hpa.Namespace, hpa.Name)
		if err := os.WriteFile(path, []byte(buf.String()), 0o644); err != nil {
			return fmt.Errorf("write patch file %s: %w", path, err)
		}
		written++
	}
	if _, err := fmt.Fprintf(out, "Exported %d HPA patch file(s) to %s\n", written, dir); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

// collectBatchEntries gathers applicable suggestions from selected HPAs.
func collectBatchEntries(opts *options, hpas []autoscalingv2.HorizontalPodAutoscaler, selected map[string]bool) []batchEntry {
	var entries []batchEntry
	for i := range hpas {
		hpa := &hpas[i]
		if !selected[hpa.Namespace+"/"+hpa.Name] {
			continue
		}
		analysis := hpaanalysis.AnalyzeWithOptions(hpa, true, analysisOptions(opts.HealthWeights, opts.Debug))
		for _, s := range analysis.Suggestions {
			if s.Apply && s.Patch != "" {
				entries = append(entries, batchEntry{
					Namespace:  hpa.Namespace,
					Name:       hpa.Name,
					Suggestion: s,
				})
			}
		}
	}
	return entries
}

// printBatchSummary displays a summary table of all patches to apply.
func printBatchSummary(out io.Writer, entries []batchEntry) error {
	seenHPAs := make(map[string]bool)
	for _, e := range entries {
		seenHPAs[e.Namespace+"/"+e.Name] = true
	}
	if _, err := fmt.Fprintf(out, "\nBatch patch summary (%d patches across %d HPA(s)):\n", len(entries), len(seenHPAs)); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	if _, err := fmt.Fprintln(out, "  NAMESPACE/NAME                    PATCH                           RISK"); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	for _, e := range entries {
		if _, err := fmt.Fprintf(out, "  %-35s %-30s %s\n", e.Namespace+"/"+e.Name, e.Suggestion.Title, e.Suggestion.Risk); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

// confirmBatchApply prompts the user to confirm the batch operation.
func confirmBatchApply(out io.Writer, opts *options, count int) (bool, error) {
	if opts.Yes {
		return true, nil
	}
	action := "dry-run"
	if !opts.DryRun {
		action = "apply"
	}
	// Resolve the reader locally without mutating opts: when stdin was not
	// explicitly wired (e.g. by tests or an embedding caller) and the process
	// stdin is not an interactive terminal, a prompt would either block forever
	// or silently consume non-confirmation input. Require an explicit --yes/-y
	// in that case. A caller that deliberately sets opts.In is allowed to drive
	// the prompt programmatically.
	in := opts.In
	if in == nil {
		if !stdinIsTerminal(os.Stdin) {
			return false, fmt.Errorf("cannot prompt for confirmation (stdin is not a terminal); pass --yes/-y to apply non-interactively")
		}
		in = os.Stdin
	}
	if _, err := fmt.Fprintf(out, "%s %d patches? [y/N]: ", action, count); err != nil {
		return false, fmt.Errorf("write output: %w", err)
	}
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return false, nil
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	if answer != "y" && answer != "yes" {
		if _, err := fmt.Fprintln(out, "Batch apply skipped."); err != nil {
			return false, fmt.Errorf("write output: %w", err)
		}
		return false, nil
	}
	return true, nil
}

// executeBatchPatches applies each patch entry and reports results.
func executeBatchPatches(ctx context.Context, out io.Writer, opts *options, entries []batchEntry) error {
	var succeeded, failed int
	for _, e := range entries {
		results, err := applySuggestionsInNamespace(ctx, out, opts, e.Namespace, e.Name, []hpaanalysis.Suggestion{e.Suggestion}, true)
		if err != nil {
			if _, ferr := fmt.Fprintf(out, "  FAILED %s/%s: %v\n", e.Namespace, e.Name, err); ferr != nil {
				return fmt.Errorf("write output: %w", ferr)
			}
			failed++
			continue
		}
		for _, line := range results {
			if _, err := fmt.Fprintf(out, "%s/%s: %s\n", e.Namespace, e.Name, line); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
		}
		succeeded++
	}

	if _, err := fmt.Fprintf(out, "\nBatch complete: %d succeeded, %d failed\n", succeeded, failed); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}
