package hpa

import (
	"errors"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/simulate"
)

// TestSentinelErrorsAliasAcrossPackages pins the shared-sentinel contract:
// an error wrapping simulate.ErrMetricNotFound must match the identically
// named pkg/hpa sentinel (and vice versa), because both are aliases of the
// single pkg/hpa/internal/errs value. Before the shared package existed the
// two declarations were distinct errors with identical messages.
func TestSentinelErrorsAliasAcrossPackages(t *testing.T) {
	pairs := []struct {
		root     error
		simulate error
	}{
		{ErrNilHPA, simulate.ErrNilHPA},
		{ErrMetricNotFound, simulate.ErrMetricNotFound},
		{ErrMetricAmbiguous, simulate.ErrMetricAmbiguous},
	}
	for _, pair := range pairs {
		if !errors.Is(pair.root, pair.simulate) {
			t.Errorf("root sentinel %v does not match simulate sentinel %v", pair.root, pair.simulate)
		}
		if !errors.Is(pair.simulate, pair.root) {
			t.Errorf("simulate sentinel %v does not match root sentinel %v", pair.simulate, pair.root)
		}
	}

	wrapped := errors.Join(simulate.ErrMetricNotFound)
	if !errors.Is(wrapped, ErrMetricNotFound) {
		t.Errorf("wrapped simulate error %v must match root ErrMetricNotFound", wrapped)
	}
}
