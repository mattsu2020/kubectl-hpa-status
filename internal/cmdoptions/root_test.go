package cmdoptions

import (
	"reflect"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// TestCopy_DeepCopiesMutableDataFields verifies that mutating slice/map/*int
// fields on the copy does not leak back into the original. This is the
// regression guard for the silent-aliasing bug class that a struct value copy
// would otherwise introduce.
func TestCopy_DeepCopiesMutableDataFields(t *testing.T) {
	orig := DefaultRoot()
	orig.HealthWeightOverrides = []string{"a", "b"}
	orig.Simulate = []string{"maxReplicas=20"}
	orig.SimulateMetric = []string{"cpu=80%"}
	orig.OutputTemplates = map[string]OutputTemplateConfig{
		"tmpl": {Type: "go-template", Template: "{{.Name}}"},
	}
	orig.HealthWeights = hpaanalysis.HealthWeights{
		ScalingInactive: hpaanalysis.IntWeight(50),
	}.Clone()

	clone := orig.Copy()

	// Mutate every data field on the clone.
	clone.HealthWeightOverrides = append(clone.HealthWeightOverrides, "c")
	clone.Simulate[0] = "maxReplicas=999"
	clone.SimulateMetric = append(clone.SimulateMetric, "memory=4Gi")
	clone.OutputTemplates["extra"] = OutputTemplateConfig{Type: "jsonpath"}
	if clone.HealthWeights.ScalingInactive != nil {
		*clone.HealthWeights.ScalingInactive = 1
	}

	// Original must be untouched.
	if !reflect.DeepEqual(orig.HealthWeightOverrides, []string{"a", "b"}) {
		t.Errorf("HealthWeightOverrides aliased: orig=%v", orig.HealthWeightOverrides)
	}
	if orig.Simulate[0] != "maxReplicas=20" {
		t.Errorf("Simulate aliased: orig=%v", orig.Simulate)
	}
	if len(orig.SimulateMetric) != 1 || orig.SimulateMetric[0] != "cpu=80%" {
		t.Errorf("SimulateMetric aliased: orig=%v", orig.SimulateMetric)
	}
	if _, exists := orig.OutputTemplates["extra"]; exists {
		t.Errorf("OutputTemplates map aliased: orig=%v", orig.OutputTemplates)
	}
	if orig.HealthWeights.ScalingInactive == nil || *orig.HealthWeights.ScalingInactive != 50 {
		t.Errorf("HealthWeights.ScalingInactive aliased: orig=%v", orig.HealthWeights.ScalingInactive)
	}
}

// TestCopy_PreservesInputPortsShared verifies ClientOverride and In remain
// shared (intentional) rather than niled out.
func TestCopy_PreservesInputPortsShared(t *testing.T) {
	orig := DefaultRoot()
	orig.In = nopReader{}

	clone := orig.Copy()
	if clone.In != orig.In {
		t.Error("In should be shared between original and copy")
	}
	// ClientOverride defaults to nil; the copy must preserve that rather than
	// zeroing it. (We compare against nil here because the test would otherwise
	// need a full kubernetes.Interface stub just for identity.)
	if clone.ClientOverride != orig.ClientOverride {
		t.Error("ClientOverride should be shared between original and copy")
	}
}

// TestCopy_EmptySlicesAndMapsAreNil verifies that an unset field copies as nil
// rather than an empty non-nil container, so len()/append behavior is
// indistinguishable from the original.
func TestCopy_EmptySlicesAndMapsAreNil(t *testing.T) {
	orig := DefaultRoot() // all slices/maps are nil by default
	clone := orig.Copy()
	if clone.HealthWeightOverrides != nil {
		t.Errorf("unset HealthWeightOverrides should stay nil, got %v", clone.HealthWeightOverrides)
	}
	if clone.Simulate != nil {
		t.Errorf("unset Simulate should stay nil, got %v", clone.Simulate)
	}
	if clone.OutputTemplates != nil {
		t.Errorf("unset OutputTemplates should stay nil, got %v", clone.OutputTemplates)
	}
}

// nopReader is a minimal io.Reader for the shared-In assertion.
type nopReader struct{}

func (nopReader) Read([]byte) (int, error) { return 0, nil }
