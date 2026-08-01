package cmd

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMapPerHPAPreservesInputOrder(t *testing.T) {
	names := []string{"c", "a", "b", "d"}

	// Sleep in reverse input order so completion order cannot match input
	// order by accident: "d" finishes first, "c" last.
	results := mapPerHPA(context.Background(), 4, names, func(_ context.Context, name string) (string, error) {
		delay := map[string]time.Duration{"c": 40, "a": 30, "b": 20, "d": 10}[name]
		time.Sleep(delay * time.Millisecond)
		return "report:" + name, nil
	})

	if len(results) != len(names) {
		t.Fatalf("expected %d results, got %d", len(names), len(results))
	}
	for i, name := range names {
		if results[i].name != name {
			t.Errorf("result %d: expected name %q, got %q", i, name, results[i].name)
		}
		if want := "report:" + name; results[i].value != want {
			t.Errorf("result %d: expected value %q, got %q", i, want, results[i].value)
		}
		if results[i].err != nil {
			t.Errorf("result %d: unexpected error %v", i, results[i].err)
		}
	}
}

func TestMapPerHPARunsEveryNameDespiteFailures(t *testing.T) {
	names := []string{"ok1", "bad", "ok2"}
	var attempts atomic.Int32

	results := mapPerHPA(context.Background(), 2, names, func(_ context.Context, name string) (int, error) {
		attempts.Add(1)
		if name == "bad" {
			return 0, errors.New("boom")
		}
		return len(name), nil
	})

	// A failure must not cancel its siblings: without this the reported error
	// would depend on goroutine scheduling.
	if got := attempts.Load(); got != int32(len(names)) {
		t.Errorf("expected all %d names attempted, got %d", len(names), got)
	}
	if results[0].err != nil || results[2].err != nil {
		t.Errorf("expected siblings to succeed, got %v and %v", results[0].err, results[2].err)
	}
	if results[1].err == nil {
		t.Error("expected the failing name to carry its error")
	}
}

func TestMapPerHPARespectsLimit(t *testing.T) {
	names := make([]string, 12)
	for i := range names {
		names[i] = fmt.Sprintf("hpa-%d", i)
	}

	var mu sync.Mutex
	inFlight, peak := 0, 0

	const limit = 3
	mapPerHPA(context.Background(), limit, names, func(_ context.Context, _ string) (struct{}, error) {
		mu.Lock()
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		mu.Unlock()

		time.Sleep(5 * time.Millisecond)

		mu.Lock()
		inFlight--
		mu.Unlock()
		return struct{}{}, nil
	})

	if peak > limit {
		t.Errorf("expected at most %d concurrent builds, peaked at %d", limit, peak)
	}
}

func TestMapPerHPAHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var called atomic.Int32
	results := mapPerHPA(ctx, 2, []string{"a", "b"}, func(_ context.Context, _ string) (int, error) {
		called.Add(1)
		return 1, nil
	})

	if called.Load() != 0 {
		t.Errorf("expected no builds on a cancelled context, got %d", called.Load())
	}
	for i, result := range results {
		if !errors.Is(result.err, context.Canceled) {
			t.Errorf("result %d: expected context.Canceled, got %v", i, result.err)
		}
	}
}

func TestCollectPerHPAReturnsFirstErrorInInputOrder(t *testing.T) {
	// "slow-fail" is first in input order but finishes last; "fast-fail"
	// fails immediately. The sequential loops this helper replaced reported
	// the first failure in input order, and that must not change.
	slowErr := errors.New("slow failure")
	fastErr := errors.New("fast failure")

	_, err := collectPerHPA(context.Background(), &options{}, []string{"slow-fail", "fast-fail"},
		func(_ context.Context, name string) (int, error) {
			if name == "slow-fail" {
				time.Sleep(30 * time.Millisecond)
				return 0, slowErr
			}
			return 0, fastErr
		})

	if !errors.Is(err, slowErr) {
		t.Errorf("expected the first error in input order (%v), got %v", slowErr, err)
	}
}

func TestCollectPerHPAReturnsValuesInInputOrder(t *testing.T) {
	names := []string{"web", "api", "worker"}

	values, err := collectPerHPA(context.Background(), &options{Common: commonOptions{
		ConnectionOptions: ConnectionOptions{Concurrency: 2},
	}}, names, func(_ context.Context, name string) (string, error) {
		return name + "!", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i, name := range names {
		if want := name + "!"; values[i] != want {
			t.Errorf("value %d: expected %q, got %q", i, want, values[i])
		}
	}
}

func TestPerHPAConcurrencyFallsBackToDefault(t *testing.T) {
	// validateEffectiveOptions rejects < 1 for real invocations, so this only
	// covers option structs assembled in-process.
	if got := perHPAConcurrency(nil); got != defaultConcurrency() {
		t.Errorf("nil options: expected %d, got %d", defaultConcurrency(), got)
	}
	if got := perHPAConcurrency(&options{}); got != defaultConcurrency() {
		t.Errorf("zero concurrency: expected %d, got %d", defaultConcurrency(), got)
	}

	opts := &options{Common: commonOptions{ConnectionOptions: ConnectionOptions{Concurrency: 4}}}
	if got := perHPAConcurrency(opts); got != 4 {
		t.Errorf("explicit concurrency: expected 4, got %d", got)
	}
}
