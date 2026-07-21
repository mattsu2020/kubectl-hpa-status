package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
)

func TestRunWatch_TimeoutExpires(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 3))
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: fakeClient,
			},
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
		Watch: watchOptions{
			WatchInterval: 100 * time.Millisecond,
			WatchTimeout:  250 * time.Millisecond,
		},
	}
	err := runWatch(context.Background(), &buf, opts, "web", false)
	if err == nil {
		t.Fatal("expected context deadline exceeded error, got nil")
	}
	output := buf.String()
	if !strings.Contains(output, "Updated:") {
		t.Errorf("expected at least one watch update, got:\n%s", output)
	}
	if !strings.Contains(output, "HPA default/web") {
		t.Errorf("expected HPA header in watch output, got:\n%s", output)
	}
}

func TestRunWatch_UntilCondition(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(10, 10),
		testutil.WithMinMax(2, 10),
		testutil.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: fakeClient,
			},
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
		Watch: watchOptions{
			WatchInterval:  100 * time.Millisecond,
			UntilCondition: "scaling-limited",
		},
	}
	err := runWatch(context.Background(), &buf, opts, "web", false)
	if err != nil {
		t.Fatalf("runWatch returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Stopped") {
		t.Errorf("expected 'Stopped' message when condition found, got:\n%s", output)
	}
}

// --------------------------------------------------------------------------
// Exit code integration tests
// --------------------------------------------------------------------------
