package cmd

import (
	"bytes"
	"context"
	"errors"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
)

// TestHPAFetchErrorClassification pins the error-sentinel contract of
// hpaFetchError: only a genuine NotFound classifies as ExitNotFound (exit 3).
// Permission denials, server failures, and unsupported API versions are
// generic errors (exit 1) — an exit code of 3 means "the HPA does not exist"
// to scripts, and a misclassified RBAC denial or outage would make them treat
// a broken cluster as an absent HPA.
func TestHPAFetchErrorClassification(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantCode     int
		wantSentinel bool
	}{
		{
			name:         "not found keeps the not-found sentinel",
			err:          apierrors.NewNotFound(autoscalingv2.Resource("horizontalpodautoscalers"), "web"),
			wantCode:     ExitNotFound,
			wantSentinel: true,
		},
		{
			name:         "forbidden is a generic error",
			err:          apierrors.NewForbidden(autoscalingv2.Resource("horizontalpodautoscalers"), "web", errors.New("rbac")),
			wantCode:     ExitError,
			wantSentinel: false,
		},
		{
			name:         "service unavailable is a generic error",
			err:          apierrors.NewServiceUnavailable("apiserver down"),
			wantCode:     ExitError,
			wantSentinel: false,
		},
		{
			name:         "method not supported is a generic error",
			err:          apierrors.NewMethodNotSupported(autoscalingv2.Resource("horizontalpodautoscalers"), "get"),
			wantCode:     ExitError,
			wantSentinel: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := hpaFetchError(tc.err, "web", "default")
			if got := errors.Is(wrapped, ErrHPANotFound); got != tc.wantSentinel {
				t.Fatalf("errors.Is(ErrHPANotFound) = %v, want %v (error: %v)", got, tc.wantSentinel, wrapped)
			}
			if code := exitCodeForError(wrapped); code != tc.wantCode {
				t.Fatalf("exit code = %d, want %d (error: %v)", code, tc.wantCode, wrapped)
			}
		})
	}
}

// TestRunStatusAPIFailureToExitCode is the cross-cutting failure-path check:
// a status run against an API that denies the read must surface an error
// whose process exit code is ExitError, and a genuinely absent HPA must stay
// ExitNotFound. This walks the full fetch -> wrap -> classify chain rather
// than the wrapper in isolation.
func TestRunStatusAPIFailureToExitCode(t *testing.T) {
	tests := []struct {
		name     string
		reactor  func() (runtime.Object, error)
		wantCode int
	}{
		{
			name: "forbidden read exits ExitError",
			reactor: func() (runtime.Object, error) {
				return nil, apierrors.NewForbidden(autoscalingv2.Resource("horizontalpodautoscalers"), "web", errors.New("rbac"))
			},
			wantCode: ExitError,
		},
		{
			name: "server timeout exits ExitError",
			reactor: func() (runtime.Object, error) {
				return nil, apierrors.NewTimeoutError("list timed out", 30)
			},
			wantCode: ExitError,
		},
		{
			name: "absent HPA exits ExitNotFound",
			reactor: func() (runtime.Object, error) {
				return nil, apierrors.NewNotFound(autoscalingv2.Resource("horizontalpodautoscalers"), "web")
			},
			wantCode: ExitNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hpa := testutil.BuildHPA("default", "web",
				testutil.WithReplicas(3, 5),
				testutil.WithResourceMetric("cpu", 80, 70),
			)
			fakeClient := testutil.NewFakeClient(hpa)
			fakeClient.PrependReactor("get", "horizontalpodautoscalers", func(_ ktesting.Action) (bool, runtime.Object, error) {
				obj, err := tc.reactor()
				return true, obj, err
			})

			opts := &options{
				Common: commonOptions{
					ConnectionOptions: ConnectionOptions{
						ClientOverride: fakeClient,
						Namespace:      "default",
					},
				},
				Status: statusOptions{
					Events: EventOption{Enabled: false},
				},
			}

			var buf bytes.Buffer
			err := runStatus(context.Background(), &buf, opts, "web", false)
			if err == nil {
				t.Fatal("expected error from runStatus, got nil")
			}
			if code := ExitCodeForMain(err); code != tc.wantCode {
				t.Fatalf("ExitCodeForMain = %d, want %d (error: %v)", code, tc.wantCode, err)
			}
		})
	}
}
