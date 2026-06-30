package client

import (
	"errors"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/cmd/internal/errs"
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
	cause := errors.New("not found")

	t.Run("with namespace formats ns/name", func(t *testing.T) {
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
}
