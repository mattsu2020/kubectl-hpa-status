package simulate

import (
	"errors"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// TestDependencyMissingReturnsErrorNotPanic pins the explicit-dependency
// contract: running a simulate entry point without the hpa root package's
// init() registration must surface errors.ErrDependencyMissing, never panic.
// The injected globals are saved and restored because pkg/hpa's init installs
// the production dependencies in this test binary.
func TestDependencyMissingReturnsErrorNotPanic(t *testing.T) {
	savedAnalyze := analyzeFuncInstance
	savedRatio := metricImpactRatioFuncImpl
	t.Cleanup(func() {
		analyzeFuncInstance = savedAnalyze
		metricImpactRatioFuncImpl = savedRatio
	})
	analyzeFuncInstance = nil
	metricImpactRatioFuncImpl = nil

	if _, err := AnalysisFuncInvoker(nil, false, AnalysisOptions{}); !errors.Is(err, ErrDependencyMissing) {
		t.Fatalf("AnalysisFuncInvoker error = %v, want ErrDependencyMissing", err)
	}
	if _, _, err := metricImpactRatioInvoker(nil, autoscalingv2.MetricStatus{}); !errors.Is(err, ErrDependencyMissing) {
		t.Fatalf("metricImpactRatioInvoker error = %v, want ErrDependencyMissing", err)
	}
}
