package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func TestRunWatch_InformerReactsToUpdateAndUntilCondition(t *testing.T) {
	origDebounce := informerDebounce
	informerDebounce = 10 * time.Millisecond
	defer func() { informerDebounce = origDebounce }()

	hpa := testutil.BuildHPA("default", "web", testutil.WithReplicas(2, 2))
	fakeClient := testutil.NewFakeClient(hpa)

	var out syncBuffer
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: fakeClient,
				Namespace:      "default",
			},
			OutputOptions: OutputOptions{Color: "never"},
		},
		Watch: watchOptions{
			Watch:          true,
			WatchMode:      "informer",
			WatchTimeout:   2 * time.Second,
			UntilCondition: "ScalingLimited",
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runWatch(ctx, &out, opts, "web", false)
	}()

	// Wait for initial frame before updating HPA condition.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), "web") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	updated := hpa.DeepCopy()
	updated.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: autoscalingv2.ScalingLimited, Status: corev1.ConditionTrue, Reason: "TooManyReplicas"},
	}
	if _, err := fakeClient.AutoscalingV2().HorizontalPodAutoscalers("default").UpdateStatus(ctx, updated, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runWatch informer returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for UntilCondition stop; output:\n%s", out.String())
	}

	if !strings.Contains(out.String(), `Stopped: condition "ScalingLimited" is present.`) {
		t.Errorf("expected ScalingLimited stop notice in output:\n%s", out.String())
	}
}

func TestRunWatchList_InformerReactsToAddedHPA(t *testing.T) {
	origDebounce := informerDebounce
	informerDebounce = 10 * time.Millisecond
	defer func() { informerDebounce = origDebounce }()

	hpa1 := testutil.BuildHPA("default", "web", testutil.WithReplicas(2, 2))
	fakeClient := testutil.NewFakeClient(hpa1)

	var out syncBuffer
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: fakeClient,
				Namespace:      "default",
			},
			OutputOptions: OutputOptions{Output: "jsonl", Color: "never"},
		},
		Watch: watchOptions{
			Watch:        true,
			WatchMode:    "informer",
			WatchTimeout: 300 * time.Millisecond,
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- runWatchList(context.Background(), &out, opts)
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), `"web"`) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	hpa2 := testutil.BuildHPA("default", "api", testutil.WithReplicas(3, 4))
	if _, err := fakeClient.AutoscalingV2().HorizontalPodAutoscalers("default").Create(context.Background(), hpa2, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("runWatchList informer returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runWatchList timeout")
	}

	if !strings.Contains(out.String(), `"api"`) {
		t.Errorf("expected newly created api HPA in informer output:\n%s", out.String())
	}
}

func TestValidateOptions_WatchMode(t *testing.T) {
	opts := &options{
		Watch: watchOptions{
			WatchMode:     "bogus",
			WatchInterval: defaultPollInterval,
		},
	}
	if err := validateAnalysisRanges(opts); err == nil {
		t.Fatal("expected invalid --watch-mode error")
	}
}
