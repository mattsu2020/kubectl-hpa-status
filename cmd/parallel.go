package cmd

import (
	"context"
	"fmt"
	"io"

	"golang.org/x/sync/errgroup"

	"github.com/mattsu2020/kubectl-hpa-status/internal/cmdoptions"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

// perHPAResult is the outcome of one name's work in a multi-HPA command. It
// keeps the name alongside the value so callers can label partial output
// without re-deriving the input order.
type perHPAResult[T any] struct {
	name  string
	value T
	err   error
}

// runPerHPACommand is the shared pipeline for dedicated multi-HPA commands:
// it isolates execution from the shared root options (via preset when given,
// otherwise a plain copy), builds one client for the whole run, collects one
// output per name concurrently, and renders through the common one-vs-many
// envelope. Commands with a different rendering contract (assumptions,
// ownership, status) keep their own wiring on top of collectPerHPA.
func runPerHPACommand[T any](ctx context.Context, out io.Writer, opts *options, names []string,
	preset cmdoptions.CommandPreset,
	buildOne func(ctx context.Context, local *options, client *kube.Client, name string) (T, error),
	writeOne func(out io.Writer, local *options, o T) error) error {

	var local options
	if preset != "" {
		local = applyCommandPreset(opts, preset)
	} else {
		local = copyOptions(opts)
	}
	client, err := newClientOrDefault(&local)
	if err != nil {
		return writeErrorIfStructured(out, local.Output, err)
	}

	outputs, err := collectPerHPA(ctx, &local, names, func(ctx context.Context, name string) (T, error) {
		return buildOne(ctx, &local, client, name)
	})
	if err != nil {
		return writeErrorIfStructured(out, local.Output, err)
	}

	return renderPerHPA(out, &local, outputs, func(out io.Writer, o T) error {
		return writeOne(out, &local, o)
	})
}

// renderPerHPA centralizes the one-vs-many structured envelope and text
// separator contract shared by dedicated multi-HPA commands.
func renderPerHPA[T any](out io.Writer, opts *options, values []T, writeOne func(io.Writer, T) error) error {
	var value any = values
	if len(values) == 1 {
		value = values[0]
	}
	return renderWithOutput(out, opts, value, func(out io.Writer) error {
		for i, item := range values {
			if i > 0 {
				if _, err := fmt.Fprintln(out); err != nil {
					return fmt.Errorf("write report separator: %w", err)
				}
			}
			if err := writeOne(out, item); err != nil {
				return err
			}
		}
		return nil
	})
}

// perHPAConcurrency resolves the effective parallelism for a multi-HPA
// command. validateEffectiveOptions already rejects values below 1, so the
// guard here only covers option structs built in-process (tests, presets)
// that never went through flag validation.
func perHPAConcurrency(opts *options) int {
	if opts == nil || opts.Concurrency < 1 {
		return defaultConcurrency()
	}
	return opts.Concurrency
}

// mapPerHPA applies build to every name with bounded parallelism and returns
// one result per input, in input order.
//
// A per-name failure never cancels its siblings: every name is attempted and
// its error captured in the corresponding result. That keeps the outcome
// independent of goroutine scheduling, which is what lets callers pick a
// deterministic error (collectPerHPA) or render partial output (status).
// Cancellation of the parent context is still honored, so Ctrl+C unwinds
// instead of running the remaining names.
//
// build must be safe for concurrent use. That holds for the current callers:
// client-go clients are goroutine-safe, and the analysis helpers read the
// per-command options snapshot without mutating it.
func mapPerHPA[T any](ctx context.Context, limit int, names []string, build func(context.Context, string) (T, error)) []perHPAResult[T] {
	results := make([]perHPAResult[T], len(names))
	for i, name := range names {
		results[i].name = name
	}

	g, gctx := errgroup.WithContext(ctx)
	if limit < 1 {
		limit = defaultConcurrency()
	}
	g.SetLimit(limit)

	for i := range names {
		g.Go(func() error {
			if err := gctx.Err(); err != nil {
				results[i].err = err
				return nil // do not cancel the group; record and move on
			}
			value, err := build(gctx, results[i].name)
			if err != nil {
				results[i].err = err
				return nil // do not cancel the group; record and move on
			}
			results[i].value = value
			return nil
		})
	}
	// g.Wait() is intentionally discarded: every goroutine above returns nil
	// (per-HPA errors are captured into results[i].err instead of cancelling
	// the group), so by construction Wait() returns nil here. If a future
	// change makes a goroutine return a non-nil error, this discard would hide
	// it — keep the goroutines returning nil, or revisit this call site.
	_ = g.Wait()
	return results
}

// collectPerHPA runs build for every name concurrently and returns the values
// in input order, or the first error in input order.
//
// The error semantics deliberately match the sequential loops this helper
// replaced: `blockers a b` reports a's error even when b fails first in
// wall-clock time, and no partial output is rendered. The observable
// difference from a sequential loop is that names after the failing one are
// still fetched rather than skipped.
//
// Commands that need to render partial results instead (status) use mapPerHPA
// directly.
func collectPerHPA[T any](ctx context.Context, opts *options, names []string, build func(context.Context, string) (T, error)) ([]T, error) {
	results := mapPerHPA(ctx, perHPAConcurrency(opts), names, build)

	values := make([]T, 0, len(results))
	for _, result := range results {
		if result.err != nil {
			return nil, result.err
		}
		values = append(values, result.value)
	}
	return values, nil
}
