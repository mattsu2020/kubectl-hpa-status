package enrichment

import (
	"errors"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestCRDReason(t *testing.T) {
	t.Run("nil error yields not-found message", func(t *testing.T) {
		if got := crdReason(nil); got != "CRD not found in API discovery" {
			t.Fatalf("crdReason(nil) = %q", got)
		}
	})
	t.Run("non-nil error surfaces verbatim", func(t *testing.T) {
		err := errors.New("forbidden: cannot list")
		if got := crdReason(err); got != "forbidden: cannot list" {
			t.Fatalf("crdReason = %q, want verbatim message", got)
		}
	})
}

func TestToAnalysisStatus(t *testing.T) {
	t.Run("empty status yields zero-value pointer", func(t *testing.T) {
		out := Status{}.ToAnalysisStatus()
		if out == nil {
			t.Fatal("expected non-nil EnrichmentStatus")
		}
		if out.KEDA != nil || out.VPA != nil {
			t.Fatalf("expected nil entries for empty status, got KEDA=%v VPA=%v", out.KEDA, out.VPA)
		}
	})

	t.Run("KEDA-only status maps to KEDA entry", func(t *testing.T) {
		s := Status{
			KEDA: &Entry{Source: "keda", State: StateError, Reason: "CRD missing"},
		}
		out := s.ToAnalysisStatus()
		if out.KEDA == nil {
			t.Fatal("expected non-nil KEDA entry")
		}
		if out.VPA != nil {
			t.Fatal("expected nil VPA entry for KEDA-only status")
		}
		if out.KEDA.Source != hpaanalysis.EnrichmentSource("keda") {
			t.Fatalf("Source = %v", out.KEDA.Source)
		}
		if out.KEDA.State != hpaanalysis.EnrichmentState(StateError) {
			t.Fatalf("State = %v", out.KEDA.State)
		}
		if out.KEDA.Reason != "CRD missing" {
			t.Fatalf("Reason = %q", out.KEDA.Reason)
		}
	})

	t.Run("VPA-only status maps to VPA entry", func(t *testing.T) {
		s := Status{
			VPA: &Entry{Source: "vpa", State: StateActive, Reason: ""},
		}
		out := s.ToAnalysisStatus()
		if out.VPA == nil {
			t.Fatal("expected non-nil VPA entry")
		}
		if out.KEDA != nil {
			t.Fatal("expected nil KEDA entry for VPA-only status")
		}
		if out.VPA.Source != hpaanalysis.EnrichmentSource("vpa") {
			t.Fatalf("Source = %v", out.VPA.Source)
		}
	})

	t.Run("both entries map independently", func(t *testing.T) {
		s := Status{
			KEDA: &Entry{Source: "keda", State: StateActive, Reason: "ready"},
			VPA:  &Entry{Source: "vpa", State: StateError, Reason: "conflict"},
		}
		out := s.ToAnalysisStatus()
		if out.KEDA == nil || out.VPA == nil {
			t.Fatal("expected both entries mapped")
		}
		if out.KEDA.Reason != "ready" || out.VPA.Reason != "conflict" {
			t.Fatalf("reasons not preserved: KEDA=%q VPA=%q", out.KEDA.Reason, out.VPA.Reason)
		}
	})
}
