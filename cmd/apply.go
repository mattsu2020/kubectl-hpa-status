package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	hpapolicy "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/policy"

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

// applyPlan holds the validated state produced by the setup/guard phase of an
// apply run, ready for the confirm-and-execute phase.
type applyPlan struct {
	client    *kube.Client
	namespace string
	current   *autoscalingv2.HorizontalPodAutoscaler
	patches   []hpaanalysis.Suggestion
}

func applySuggestionsInNamespace(ctx context.Context, out io.Writer, opts *options, namespace string, name string, suggestions []hpaanalysis.Suggestion, skipConfirm bool) ([]string, error) {
	plan, done, err := prepareApplyPlan(ctx, out, opts, namespace, name, suggestions)
	if err != nil {
		return nil, err
	}
	if done != nil {
		return done, nil
	}

	validated, done, err := validateApplyPlan(ctx, out, opts, plan)
	if err != nil {
		return nil, err
	}
	if done != nil {
		return done, nil
	}

	// Real apply path: confirm unless skipped.
	if !opts.Yes && !skipConfirm {
		if err := confirmApply(out, opts, len(validated.patches), validated.namespace, name); err != nil {
			return nil, err
		}
	}

	return executePatches(ctx, out, validated.client, validated.namespace, name, validated.patches, opts.AllowPartial, validated.current.ResourceVersion)
}

// prepareApplyPlan collects applicable patches, creates the client, fetches the
// current HPA, and runs the policy guard. A non-nil []string return is a
// short-circuit result (nothing to apply) that the caller should return as-is.
func prepareApplyPlan(ctx context.Context, out io.Writer, opts *options, namespace, name string, suggestions []hpaanalysis.Suggestion) (*applyPlan, []string, error) {
	patches := collectApplicablePatches(suggestions)
	if len(patches) == 0 {
		return nil, []string{"No applicable HPA patch was suggested."}, nil
	}
	// applySuggestions returns (messages, err) rather than a single error,
	// so it surfaces the raw client-creation error to the caller instead of
	// the standard wrapper. The caller wraps it for display.
	client, err := opts.NewClient()
	if err != nil {
		return nil, nil, err
	}
	if namespace == "" {
		namespace = client.Namespace
	}

	// Fetch current HPA state once for diff display.
	current, err := client.Interface.AutoscalingV2().
		HorizontalPodAutoscalers(namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, wrapHPALookupError(namespace, name, err)
	}

	patches, err = guardPatches(out, opts, current, patches)
	if err != nil {
		return nil, nil, err
	}
	if len(patches) == 0 {
		return nil, []string{"No applicable HPA patch was allowed by policy guard."}, nil
	}

	return &applyPlan{client: client, namespace: namespace, current: current, patches: patches}, nil, nil
}

// validateApplyPlan guards the merged patch, prints the proposed diffs, and
// runs the server-side dry-run pre-validation. A non-nil []string return is a
// short-circuit result (dry-run mode) that the caller should return as-is.
func validateApplyPlan(ctx context.Context, out io.Writer, opts *options, plan *applyPlan) (*applyPlan, []string, error) {
	mergedPatch, mergeErr := mergeSuggestionPatches(plan.patches)
	if mergeErr == nil {
		if err := guardMergedPatch(out, opts, plan.current, mergedPatch); err != nil {
			return nil, nil, err
		}
	}

	if err := printProposedPatches(out, plan.current, plan.patches); err != nil {
		return nil, nil, err
	}

	// Pre-validate all patches with server-side dry-run before asking for
	// confirmation or applying anything.
	if err := preValidatePatches(ctx, plan.client, plan.namespace, plan.current.Name, plan.patches, mergedPatch, mergeErr); err != nil {
		return nil, nil, err
	}
	validationMessage := fmt.Sprintf("All %d patch(es) passed server-side dry-run validation.", len(plan.patches))
	if mergeErr == nil {
		validationMessage = fmt.Sprintf("All %d patch(es) and their combined final state passed server-side dry-run validation.", len(plan.patches))
	}
	if _, err := fmt.Fprintln(out, validationMessage); err != nil {
		return nil, nil, err
	}

	if opts.DryRun {
		if _, err := fmt.Fprintln(out, "Dry-run mode is enabled; patches were validated but not persisted. Use --dry-run=false to apply changes."); err != nil {
			return nil, nil, err
		}
		return nil, dryRunResults(plan.patches), nil
	}

	return plan, nil, nil
}

func guardPatches(out io.Writer, opts *options, current *autoscalingv2.HorizontalPodAutoscaler, patches []hpaanalysis.Suggestion) ([]hpaanalysis.Suggestion, error) {
	if opts.PolicyGuard == "" {
		return patches, nil
	}
	policyFile, err := hpapolicy.LoadPolicyFile(opts.PolicyGuard)
	if err != nil {
		return nil, err
	}
	result := hpapolicy.GuardFix(patches, policyFile, current)
	if err := hpaanalysis.WritePolicyGuardText(out, result); err != nil {
		return nil, err
	}
	switch opts.PolicyGuardMode {
	case "", "block":
		if len(result.Blocked) > 0 {
			return nil, fmt.Errorf("policy guard blocked %d patch(es): %w", len(result.Blocked), ErrPolicyGuardBlocked)
		}
	case "warn":
	default:
		return nil, fmt.Errorf("invalid --policy-guard-mode %q; use block or warn", opts.PolicyGuardMode)
	}
	return result.Allowed, nil
}

// guardMergedPatch evaluates the complete state produced by all allowed
// suggestions. Evaluating suggestions independently is insufficient because
// two individually valid changes can violate a policy only when combined.
func guardMergedPatch(out io.Writer, opts *options, current *autoscalingv2.HorizontalPodAutoscaler, mergedPatch string) error {
	if opts.PolicyGuard == "" {
		return nil
	}
	policyFile, err := hpapolicy.LoadPolicyFile(opts.PolicyGuard)
	if err != nil {
		return err
	}
	report, err := hpapolicy.EvaluateMergePatch(current, mergedPatch, policyFile)
	if err != nil {
		return err
	}

	combined := hpaanalysis.Suggestion{
		Title: "Combined final HPA state",
		Patch: mergedPatch,
		Apply: true,
	}
	result := &hpaanalysis.GuardResult{Allowed: []hpaanalysis.Suggestion{combined}}
	for _, violation := range report.Violations {
		switch violation.Severity {
		case "critical":
			result.Blocked = append(result.Blocked, hpaanalysis.GuardBlocked{
				Suggestion: combined,
				Reason:     violation.Description,
				PolicyRule: violation.RuleID,
			})
		case "warning":
			result.Warnings = append(result.Warnings, hpaanalysis.GuardWarning{
				Suggestion: combined,
				Reason:     violation.Description,
				PolicyRule: violation.RuleID,
			})
		}
	}
	if len(result.Blocked) > 0 || len(result.Warnings) > 0 {
		if err := hpaanalysis.WritePolicyGuardText(out, result); err != nil {
			return err
		}
	}

	switch opts.PolicyGuardMode {
	case "", "block":
		if len(result.Blocked) > 0 {
			return fmt.Errorf("policy guard blocked the combined final HPA state: %w", ErrPolicyGuardBlocked)
		}
	case "warn":
	default:
		return fmt.Errorf("invalid --policy-guard-mode %q; use block or warn", opts.PolicyGuardMode)
	}
	return nil
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
			return fmt.Errorf("write proposed patch: %w", err)
		}
	}
	return nil
}

func preValidatePatches(ctx context.Context, client *kube.Client, namespace, name string, patches []hpaanalysis.Suggestion, mergedPatch string, mergeErr error) error {
	dryRunOpts := metav1.PatchOptions{DryRun: []string{metav1.DryRunAll}}
	for _, suggestion := range patches {
		if _, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(namespace).
			Patch(ctx, name, types.MergePatchType, []byte(suggestion.Patch), dryRunOpts); err != nil {
			return fmt.Errorf("pre-validation failed for patch %q: %w", suggestion.Title, err)
		}
	}
	if mergeErr == nil && len(patches) > 1 {
		if _, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(namespace).
			Patch(ctx, name, types.MergePatchType, []byte(mergedPatch), dryRunOpts); err != nil {
			return fmt.Errorf("combined final patch failed server-side dry-run validation: %w", err)
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

func mergeSuggestionPatches(patches []hpaanalysis.Suggestion) (string, error) {
	patchItems := make([]patch.Patch, len(patches))
	for i, suggestion := range patches {
		patchItems[i] = patch.Patch{Title: suggestion.Title, JSON: suggestion.Patch}
	}
	return patch.MergePatches(patchItems)
}

func confirmApply(out io.Writer, opts *options, count int, namespace, name string) error {
	if _, err := fmt.Fprintf(out, "\nWARNING: About to apply %d patch(es) to HPA %s/%s. This will modify the live cluster.\n", count, namespace, name); err != nil {
		return fmt.Errorf("write apply warning: %w", err)
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
		return fmt.Errorf("write apply prompt: %w", err)
	}
	scanner := bufio.NewScanner(opts.In)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("read confirmation input: %w", err)
		}
		return fmt.Errorf("apply skipped")
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	if answer != "y" && answer != "yes" {
		return fmt.Errorf("apply skipped")
	}
	return nil
}

func executePatches(ctx context.Context, out io.Writer, client *kube.Client, namespace, name string, patches []hpaanalysis.Suggestion, allowPartial bool, expectedResourceVersion string) ([]string, error) {
	current, err := client.Interface.AutoscalingV2().
		HorizontalPodAutoscalers(namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, wrapHPALookupError(namespace, name, err)
	}
	if current.ResourceVersion != expectedResourceVersion {
		return nil, fmt.Errorf("HPA %s/%s changed after it was reviewed (resourceVersion %q -> %q); no changes were applied, review the updated HPA and retry", namespace, name, expectedResourceVersion, current.ResourceVersion)
	}

	merged, mergeErr := mergeSuggestionPatches(patches)
	if mergeErr == nil {
		merged, err = mergePatchWithResourceVersion(merged, expectedResourceVersion)
		if err != nil {
			return nil, fmt.Errorf("adding resourceVersion precondition: %w", err)
		}
		// Validate the merged patch with a server-side dry-run before applying,
		// so an invalid merge is rejected without touching the HPA.
		if _, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(namespace).
			Patch(ctx, name, types.MergePatchType, []byte(merged), metav1.PatchOptions{DryRun: []string{metav1.DryRunAll}}); err != nil {
			return nil, fmt.Errorf("merged patch failed server-side dry-run validation: %w", err)
		}
		// Single merged patch — atomic application.
		_, err = client.Interface.AutoscalingV2().
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
		return nil, fmt.Errorf("patches could not be merged into one atomic patch (%w); pass --allow-partial to apply them sequentially at the risk of a partial modification", mergeErr)
	}

	_, _ = fmt.Fprintf(out, "WARNING: patches could not be merged (%v); falling back to sequential, non-atomic apply.\n", mergeErr)
	_, _ = fmt.Fprintf(out, "WARNING: if a later patch fails, the HPA %s/%s will be left partially modified — inspect it with `kubectl describe hpa %s -n %s` and reconcile manually.\n", namespace, name, name, namespace)

	var applied []string
	resourceVersion := expectedResourceVersion
	for _, suggestion := range patches {
		patchJSON, err := mergePatchWithResourceVersion(suggestion.Patch, resourceVersion)
		if err != nil {
			return applied, fmt.Errorf("adding resourceVersion precondition to %q: %w", suggestion.Title, err)
		}
		updated, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(namespace).
			Patch(ctx, name, types.MergePatchType, []byte(patchJSON), metav1.PatchOptions{})
		if err != nil {
			return applied, fmt.Errorf("partial apply: %d/%d succeeded, then failed on %q: %w (HPA %s/%s is partially modified; re-run apply or reconcile manually with `kubectl describe hpa %s -n %s`)", len(applied), len(patches), suggestion.Title, err, namespace, name, name, namespace)
		}
		resourceVersion = updated.ResourceVersion
		applied = append(applied, fmt.Sprintf("Applied: %s", suggestion.Title))
	}
	return applied, nil
}

// mergePatchWithResourceVersion adds a Kubernetes optimistic-concurrency
// precondition without discarding any metadata fields already in the patch.
// An empty resourceVersion is omitted for fake/embedded clients that do not
// model Kubernetes object versions.
func mergePatchWithResourceVersion(mergePatch, resourceVersion string) (string, error) {
	var patchMap map[string]any
	if err := json.Unmarshal([]byte(mergePatch), &patchMap); err != nil {
		return "", err
	}
	metadata, ok := patchMap["metadata"].(map[string]any)
	if !ok {
		metadata = map[string]any{}
		patchMap["metadata"] = metadata
	}
	if resourceVersion != "" {
		metadata["resourceVersion"] = resourceVersion
	}
	encoded, err := json.Marshal(patchMap)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
