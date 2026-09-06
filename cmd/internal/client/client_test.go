package client

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/mattsu2020/kubectl-hpa-status/cmd/internal/errs"
	"github.com/mattsu2020/kubectl-hpa-status/internal/cmdoptions"
)

func TestWrapClientError(t *testing.T) {
	cause := errors.New("dial tcp: connection refused")
	wrapped := WrapClientError(cause)
	if !strings.Contains(wrapped.Error(), "failed to create Kubernetes client") {
		t.Fatalf("expected canonical prefix, got: %v", wrapped)
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("expected underlying cause to be reachable via errors.Is")
	}
}

func TestWrapHPALookupError(t *testing.T) {
	t.Run("with namespace formats ns/name", func(t *testing.T) {
		cause := apierrors.NewNotFound(autoscalingv2.Resource("horizontalpodautoscalers"), "web")
		wrapped := WrapHPALookupError("production", "web", cause)
		msg := wrapped.Error()
		if !strings.Contains(msg, "failed to get HPA production/web") {
			t.Fatalf("expected ns/name format, got: %s", msg)
		}
		if !errors.Is(wrapped, errs.ErrHPANotFound) {
			t.Fatal("expected ErrHPANotFound to be reachable via errors.Is")
		}
		if !errors.Is(wrapped, cause) {
			t.Fatal("expected underlying cause to be reachable via errors.Is")
		}
	})

	t.Run("empty namespace formats bare name", func(t *testing.T) {
		cause := apierrors.NewNotFound(autoscalingv2.Resource("horizontalpodautoscalers"), "web")
		wrapped := WrapHPALookupError("", "web", cause)
		msg := wrapped.Error()
		if !strings.Contains(msg, "failed to get HPA web") {
			t.Fatalf("expected bare name format, got: %s", msg)
		}
		if strings.Contains(msg, "/web") {
			t.Fatalf("did not expect ns/ prefix for empty namespace, got: %s", msg)
		}
		if !errors.Is(wrapped, errs.ErrHPANotFound) {
			t.Fatal("expected ErrHPANotFound for empty-namespace case too")
		}
	})

	t.Run("non-NotFound API failure does not carry the not-found sentinel", func(t *testing.T) {
		// Permission denials and server errors must classify as generic
		// failures (exit 1), not as "the HPA does not exist" (exit 3).
		cause := apierrors.NewForbidden(autoscalingv2.Resource("horizontalpodautoscalers"), "web", errors.New("rbac"))
		wrapped := WrapHPALookupError("production", "web", cause)
		if !strings.Contains(wrapped.Error(), "failed to get HPA production/web") {
			t.Fatalf("expected canonical prefix, got: %s", wrapped.Error())
		}
		if errors.Is(wrapped, errs.ErrHPANotFound) {
			t.Fatal("ErrHPANotFound must only attach to genuine NotFound failures")
		}
		if !errors.Is(wrapped, cause) {
			t.Fatal("expected underlying cause to be reachable via errors.Is")
		}
	})
}

func TestNewClientOrDefault(t *testing.T) {
	t.Run("client override succeeds", func(t *testing.T) {
		opts := &cmdoptions.Root{}
		opts.ClientOverride = fake.NewClientset()
		c, err := NewClientOrDefault(opts)
		if err != nil {
			t.Fatalf("NewClientOrDefault with override: %v", err)
		}
		if c == nil || c.Interface == nil {
			t.Fatal("expected usable client")
		}
	})

	t.Run("invalid kubeconfig wraps error", func(t *testing.T) {
		opts := &cmdoptions.Root{}
		opts.Kubeconfig = filepath.Join(t.TempDir(), "does-not-exist")
		_, err := NewClientOrDefault(opts)
		if err == nil {
			t.Fatal("expected error for missing kubeconfig")
		}
		if !strings.Contains(err.Error(), "failed to create Kubernetes client") {
			t.Fatalf("expected canonical wrap, got: %v", err)
		}
	})
}

func TestLookupHPA(t *testing.T) {
	t.Run("missing HPA wraps ErrHPANotFound", func(t *testing.T) {
		opts := &cmdoptions.Root{}
		opts.ClientOverride = fake.NewClientset()
		opts.Namespace = "default"
		_, _, err := LookupHPA(t.Context(), opts, "missing-hpa")
		if err == nil {
			t.Fatal("expected not-found error")
		}
		if !errors.Is(err, errs.ErrHPANotFound) {
			t.Fatalf("expected ErrHPANotFound, got: %v", err)
		}
		if !strings.Contains(err.Error(), "failed to get HPA") {
			t.Fatalf("expected canonical lookup prefix, got: %v", err)
		}
	})

	t.Run("client failure uses standard wrap", func(t *testing.T) {
		opts := &cmdoptions.Root{}
		opts.Kubeconfig = filepath.Join(t.TempDir(), "does-not-exist")
		_, _, err := LookupHPA(t.Context(), opts, "web")
		if err == nil {
			t.Fatal("expected client error")
		}
		if !strings.Contains(err.Error(), "failed to create Kubernetes client") {
			t.Fatalf("expected client wrap, got: %v", err)
		}
	})
}
