package simulate

import (
	"errors"
	"math"
	"testing"
)

func TestProjectionLimits(t *testing.T) {
	hpa := buildTestHPAWithResourceMetric(5, 5, 1, 20, 50, 80)
	for _, opts := range []SimulationExtendedOptions{{DurationSeconds: math.MaxInt32, StepSeconds: 1}, {DurationSeconds: math.MaxInt32}, {DurationSeconds: 10000, StepSeconds: 1}} {
		if _, err := ProjectReplicaTrajectory(hpa, hpa, opts); !errors.Is(err, ErrInvalidSimulationValue) {
			t.Fatalf("expected bounded projection error for %+v, got %v", opts, err)
		}
	}
	states, err := ProjectReplicaTrajectory(hpa, hpa, SimulationExtendedOptions{DurationSeconds: math.MaxInt32, StepSeconds: math.MaxInt32})
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 || states[1].TimeOffset != math.MaxInt32 {
		t.Fatalf("overflow boundary: %+v", states)
	}
	states, err = ProjectReplicaTrajectory(hpa, hpa, SimulationExtendedOptions{DurationSeconds: 9999, StepSeconds: 1})
	if err != nil || len(states) != 10000 {
		t.Fatalf("maximum accepted point count: len=%d err=%v", len(states), err)
	}
	states, err = ProjectReplicaTrajectory(hpa, hpa, SimulationExtendedOptions{DurationSeconds: 31, StepSeconds: 30})
	if err != nil || len(states) != 3 || states[2].TimeOffset != 31 {
		t.Fatalf("final partial interval: %+v, %v", states, err)
	}
	if _, err = ProjectReplicaTrajectory(nil, hpa, SimulationExtendedOptions{}); !errors.Is(err, ErrNilHPA) {
		t.Fatalf("nil original: %v", err)
	}
	if _, err = ProjectReplicaTrajectory(hpa, nil, SimulationExtendedOptions{}); !errors.Is(err, ErrNilHPA) {
		t.Fatalf("nil modified: %v", err)
	}
}
