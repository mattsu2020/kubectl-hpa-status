package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
)

func TestRunFleet_UnsupportedRiskRejected(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web")
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: fakeClient,
			},
		},
	}
	err := runFleet(context.Background(), &buf, opts, "bogus-risk")
	if err == nil {
		t.Fatal("expected error for unsupported --risk, got nil")
	}
	if !strings.Contains(err.Error(), "bogus-risk") {
		t.Fatalf("expected error to mention the risk value, got: %v", err)
	}
}

func TestRunFleet_DefaultRiskIsMaxSurge(t *testing.T) {
	// Passing empty risk defaults to "max-surge" and must not error.
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithResourceMetric("cpu", 70, 65),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: fakeClient,
			},
		},
	}
	err := runFleet(context.Background(), &buf, opts, "")
	if err != nil {
		t.Fatalf("unexpected error with default risk: %v", err)
	}
}

// Ensure runFleet surfaces list failures rather than silently emitting an
// empty report.
func TestRunFleet_ListErrorPropagates(t *testing.T) {
	fakeClient := testutil.NewFakeClient()
	fakeClient.PrependReactor("list", "horizontalpodautoscalers", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected list failure")
	})
	opts := &options{Common: commonOptions{
		ConnectionOptions: ConnectionOptions{
			ClientOverride: fakeClient,
		},
	}}
	var buf bytes.Buffer
	err := runFleet(context.Background(), &buf, opts, "max-surge")
	if err == nil {
		t.Fatal("expected list error")
	}
	if !strings.Contains(err.Error(), "injected list failure") {
		t.Fatalf("expected wrapped list error, got %v", err)
	}
}
