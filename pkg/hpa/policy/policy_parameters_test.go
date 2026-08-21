package policy

import (
	"math"
	"strings"
	"testing"
)

func TestParameterInt64AcceptsAllNumericKinds(t *testing.T) {
	t.Parallel()
	params := Params{
		"int":    7,
		"int32":  int32(8),
		"int64":  int64(9),
		"float":  float64(10),
		"string": "11",
	}
	want := map[string]int64{"int": 7, "int32": 8, "int64": 9, "float": 10}
	for key, expected := range want {
		got, err := parameterInt64(params, key, 1)
		if err != nil || got != expected {
			t.Errorf("parameterInt64(%q) = (%d, %v), want (%d, nil)", key, got, err, expected)
		}
	}
}

func TestParameterInt64RejectsNonIntegerValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		value  any
		reason string
	}{
		{"fractional", float64(1.5), "fractional"},
		{"NaN", math.NaN(), "NaN"},
		{"infinity", math.Inf(1), "infinity"},
		{"out of int64 range", math.MaxFloat64, "range"},
		{"string", "7", "string"},
		{"bool", true, "bool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parameterInt64(Params{"k": tt.value}, "k", 1); err == nil {
				t.Fatalf("expected %s value to be rejected", tt.reason)
			}
		})
	}
}

func TestValidateIntRangePairBounds(t *testing.T) {
	t.Parallel()
	t.Run("below lower bound", func(t *testing.T) {
		err := validateIntRangePair(Params{"min": 0}, 30, 90, 1, 100)
		if err == nil || !strings.Contains(err.Error(), "between 1 and 100") {
			t.Fatalf("expected range error, got %v", err)
		}
	})
	t.Run("above upper bound", func(t *testing.T) {
		err := validateIntRangePair(Params{"max": 101}, 30, 90, 1, 100)
		if err == nil || !strings.Contains(err.Error(), "between 1 and 100") {
			t.Fatalf("expected range error, got %v", err)
		}
	})
	t.Run("min exceeds max", func(t *testing.T) {
		err := validateIntRangePair(Params{"min": 80, "max": 40}, 30, 90, 1, 100)
		if err == nil || !strings.Contains(err.Error(), `must not exceed`) {
			t.Fatalf("expected ordering error, got %v", err)
		}
	})
	t.Run("defaults are valid", func(t *testing.T) {
		if err := validateIntRangePair(Params{}, 30, 90, 1, 100); err != nil {
			t.Fatalf("expected defaults to validate, got %v", err)
		}
	})
}

func TestValidateRuleParameterMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		rule Rule
		want string // empty means the rule must validate
	}{
		{"stabilization window valid", Rule{ID: "stabilization-window", Parameters: Params{"min": 60, "max": 600}}, ""},
		{"stabilization window invalid", Rule{ID: "stabilization-window", Parameters: Params{"min": "soon"}}, "must be an integer"},
		{"multiplier valid", Rule{ID: "max-replicas-multiplier", Parameters: Params{"multiplier": 2}}, ""},
		{"multiplier zero invalid", Rule{ID: "max-replicas-multiplier", Parameters: Params{"multiplier": 0}}, "between 1 and"},
		{"multiplier from current valid", Rule{ID: "max-replicas-from-current", Parameters: Params{"maxMultiplierFromCurrent": 4}}, ""},
		{"behavior flags valid", Rule{ID: "behavior-policy-required", Parameters: Params{"requireScaleUp": true, "requireScaleDown": false}}, ""},
		{"behavior flag invalid", Rule{ID: "behavior-policy-required", Parameters: Params{"requireScaleUp": "yes"}}, "must be a boolean"},
		{"metric coverage valid", Rule{ID: "metric-coverage", Parameters: Params{"requireResource": true, "minMetrics": 2}}, ""},
		{"metric coverage minMetrics invalid", Rule{ID: "metric-coverage", Parameters: Params{"minMetrics": -1}}, "between 1 and"},
		{"utilization range valid", Rule{ID: "target-utilization-range", Parameters: Params{"min": 40, "max": 80}}, ""},
		{"utilization range inverted", Rule{ID: "target-utilization-range", Parameters: Params{"min": 95, "max": 50}}, "must not exceed"},
		{"replica ratio valid", Rule{ID: "replica-range", Parameters: Params{"maxRatio": 3}}, ""},
		{"unknown parameter rejected", Rule{ID: "replica-range", Parameters: Params{"maxRATIO": 3}}, `unknown parameter`},
		{"unknown rule passes through", Rule{ID: "future-rule", Parameters: Params{"anything": 1}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRuleParameters(tt.rule)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("expected valid parameters, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}
