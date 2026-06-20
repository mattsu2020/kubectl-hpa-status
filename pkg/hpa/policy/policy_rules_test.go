package policy

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/suggestion"
)

// --- Test HPA builders -------------------------------------------------------

func policyTestHPA(opts ...func(*autoscalingv2.HorizontalPodAutoscaler)) *autoscalingv2.HorizontalPodAutoscaler {
	minReplicas := int32(1)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MinReplicas:    &minReplicas,
			MaxReplicas:    10,
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{CurrentReplicas: 2},
	}
	for _, opt := range opts {
		opt(hpa)
	}
	return hpa
}

func withStabilizationWindow(seconds int32) func(*autoscalingv2.HorizontalPodAutoscaler) {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		if hpa.Spec.Behavior == nil {
			hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{}
		}
		hpa.Spec.Behavior.ScaleDown = &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: &seconds}
	}
}

func withScaleUpPolicy() func(*autoscalingv2.HorizontalPodAutoscaler) {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		if hpa.Spec.Behavior == nil {
			hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{}
		}
		hpa.Spec.Behavior.ScaleUp = &autoscalingv2.HPAScalingRules{
			Policies: []autoscalingv2.HPAScalingPolicy{{Type: autoscalingv2.PercentScalingPolicy, Value: 100, PeriodSeconds: 15}},
		}
	}
}

func withScaleDownPolicy() func(*autoscalingv2.HorizontalPodAutoscaler) {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		if hpa.Spec.Behavior == nil {
			hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{}
		}
		hpa.Spec.Behavior.ScaleDown = &autoscalingv2.HPAScalingRules{
			Policies: []autoscalingv2.HPAScalingPolicy{{Type: autoscalingv2.PercentScalingPolicy, Value: 100, PeriodSeconds: 15}},
		}
	}
}

func withResourceMetric(name string, util int32) func(*autoscalingv2.HorizontalPodAutoscaler) {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		hpa.Spec.Metrics = append(hpa.Spec.Metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceName(name),
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: &util,
				},
			},
		})
	}
}

func withExternalMetric() func(*autoscalingv2.HorizontalPodAutoscaler) {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		hpa.Spec.Metrics = append(hpa.Spec.Metrics, autoscalingv2.MetricSpec{Type: autoscalingv2.ExternalMetricSourceType})
	}
}

func withMinReplicas(v int32) func(*autoscalingv2.HorizontalPodAutoscaler) {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		hpa.Spec.MinReplicas = &v
	}
}

// --- Params accessors --------------------------------------------------------

func TestParams_Int(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		params   Params
		key      string
		def      int
		expected int
	}{
		{"missing", Params{}, "x", 7, 7},
		{"int", Params{"x": 5}, "x", 0, 5},
		{"float64 from JSON", Params{"x": float64(5)}, "x", 0, 5},
		{"int64", Params{"x": int64(5)}, "x", 0, 5},
		{"wrong type", Params{"x": "5"}, "x", 9, 9},
	}
	for _, tc := range tests {
		if got := tc.params.Int(tc.key, tc.def); got != tc.expected {
			t.Errorf("%s: Int(%q,%d)=%d, want %d", tc.name, tc.key, tc.def, got, tc.expected)
		}
	}
}

func TestParams_String(t *testing.T) {
	t.Parallel()
	if got := (Params{}).String("missing", "def"); got != "def" {
		t.Errorf("missing key: got %q", got)
	}
	if got := (Params{"k": "v"}).String("k", "def"); got != "v" {
		t.Errorf("present key: got %q", got)
	}
	if got := (Params{"k": 42}).String("k", "def"); got != "def" {
		t.Errorf("wrong type falls back to default: got %q", got)
	}
}

func TestParams_Bool(t *testing.T) {
	t.Parallel()
	if got := (Params{}).Bool("missing", true); !got {
		t.Errorf("missing key should return default")
	}
	if got := (Params{"k": true}).Bool("k", false); !got {
		t.Errorf("present key should return value")
	}
	if got := (Params{"k": "true"}).Bool("k", false); got {
		t.Errorf("wrong type should return default")
	}
}

func TestRule_IsEnabled(t *testing.T) {
	t.Parallel()
	enabled := true
	disabled := false
	tests := []struct {
		name string
		rule Rule
		want bool
	}{
		{"nil defaults to enabled", Rule{}, true},
		{"explicit true", Rule{Enabled: &enabled}, true},
		{"explicit false", Rule{Enabled: &disabled}, false},
	}
	for _, tc := range tests {
		if got := tc.rule.IsEnabled(); got != tc.want {
			t.Errorf("%s: IsEnabled()=%v, want %v", tc.name, got, tc.want)
		}
	}
}

// --- File.Validate -----------------------------------------------------------

func TestFile_Validate(t *testing.T) {
	t.Parallel()
	t.Run("empty file is valid", func(t *testing.T) {
		if err := (File{}).Validate(); err != nil {
			t.Errorf("empty file should be valid, got %v", err)
		}
	})
	t.Run("valid apiVersion", func(t *testing.T) {
		f := File{APIVersion: "hpa-status/v1", Rules: []Rule{{ID: "r", Name: "R", Type: "stabilizationWindowSeconds"}}}
		if err := f.Validate(); err != nil {
			t.Errorf("valid file: %v", err)
		}
	})
	t.Run("unsupported apiVersion", func(t *testing.T) {
		f := File{APIVersion: "v2"}
		if err := f.Validate(); err == nil {
			t.Error("expected error for unsupported apiVersion")
		}
	})
	t.Run("rule missing id", func(t *testing.T) {
		f := File{Rules: []Rule{{Name: "no id"}}}
		if err := f.Validate(); err == nil {
			t.Error("expected error for missing id")
		}
	})
	t.Run("rule missing name on unknown id", func(t *testing.T) {
		// Unknown id -> normalize cannot fill a default Name, so an empty Name
		// is a genuine validation error.
		f := File{Rules: []Rule{{ID: "totally-unknown-id"}}}
		if err := f.Validate(); err == nil {
			t.Error("expected error for missing name on unknown id")
		}
	})
	t.Run("known id fills default name", func(t *testing.T) {
		// A known id supplies a default Name via normalize, so empty input Name
		// should still validate successfully.
		f := File{Rules: []Rule{{ID: "stabilizationWindowSeconds"}}}
		if err := f.Validate(); err != nil {
			t.Errorf("known id should fill default name: %v", err)
		}
	})
	t.Run("invalid severity", func(t *testing.T) {
		f := File{Rules: []Rule{{ID: "r", Name: "R", Severity: "boom"}}}
		if err := f.Validate(); err == nil {
			t.Error("expected error for invalid severity")
		}
	})
	t.Run("rules in policies validated too", func(t *testing.T) {
		f := File{
			Policies: []Set{{
				Name:  "p",
				Rules: []Rule{{ID: "r", Name: "R", Severity: "boom"}},
			}},
		}
		if err := f.Validate(); err == nil {
			t.Error("expected error for invalid severity in policy set")
		}
	})
}

// --- allRules / normalizePolicyRule -----------------------------------------

func TestFile_allRules(t *testing.T) {
	t.Parallel()
	f := File{
		Rules: []Rule{{ID: "top1"}, {ID: "top2"}},
		Policies: []Set{{
			Name:  "set",
			Rules: []Rule{{ID: "set1"}},
		}},
	}
	got := f.allRules()
	if len(got) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(got))
	}
}

func TestNormalizePolicyRule(t *testing.T) {
	t.Parallel()
	t.Run("stabilization aliases collapse to canonical id", func(t *testing.T) {
		for _, alias := range []string{"stabilizationWindowSeconds", "stabilizationWindow", "stabilization-window"} {
			r := normalizePolicyRule(Rule{ID: alias})
			if r.ID != "stabilization-window" {
				t.Errorf("alias %q -> %q, want stabilization-window", alias, r.ID)
			}
			if r.Name == "" {
				t.Errorf("alias %q did not set default Name", alias)
			}
		}
	})
	t.Run("maxReplicas with multiplier maps to multiplier variant", func(t *testing.T) {
		m := 3
		r := normalizePolicyRule(Rule{ID: "maxReplicas", Multiplier: &m})
		if r.ID != "max-replicas-multiplier" {
			t.Errorf("got %q", r.ID)
		}
		if r.Parameters["multiplier"] != 3 {
			t.Errorf("multiplier not copied into Parameters: %v", r.Parameters["multiplier"])
		}
	})
	t.Run("maxReplicas with maxMultiplierFromCurrent maps to from-current variant", func(t *testing.T) {
		mc := 5
		r := normalizePolicyRule(Rule{ID: "maxReplicas", MaxMultiplierFromCurrent: &mc})
		if r.ID != "max-replicas-from-current" {
			t.Errorf("got %q", r.ID)
		}
	})
	t.Run("unknown id is preserved", func(t *testing.T) {
		r := normalizePolicyRule(Rule{ID: "weird", Name: "Weird"})
		if r.ID != "weird" {
			t.Errorf("got %q", r.ID)
		}
	})
	t.Run("scalar fields copied into Parameters", func(t *testing.T) {
		minV, maxV, ratio := 5, 100, 10
		r := normalizePolicyRule(Rule{ID: "replica-range", Min: &minV, Max: &maxV, MaxRatio: &ratio})
		if r.Parameters["min"] != 5 || r.Parameters["max"] != 100 || r.Parameters["maxRatio"] != 10 {
			t.Errorf("parameters not copied: %v", r.Parameters)
		}
		if r.Parameters == nil {
			t.Error("Parameters should be non-nil")
		}
	})
	t.Run("empty ID falls back to Type", func(t *testing.T) {
		r := normalizePolicyRule(Rule{Type: "metricCoverage"})
		if r.ID != "metric-coverage" {
			t.Errorf("got %q", r.ID)
		}
	})
}

// --- buildPolicySummary ------------------------------------------------------

func TestBuildPolicySummary(t *testing.T) {
	t.Parallel()
	t.Run("no violations", func(t *testing.T) {
		s := buildPolicySummary(&Report{Score: 100})
		if s == "" {
			t.Error("expected non-empty summary")
		}
	})
	t.Run("mixed severities", func(t *testing.T) {
		s := buildPolicySummary(&Report{
			Score: 70,
			Violations: []Violation{
				{Severity: "critical"},
				{Severity: "warning"}, {Severity: "warning"},
				{Severity: "info"},
			},
		})
		// Should mention all three severity buckets.
		for _, want := range []string{"critical", "warnings", "informational"} {
			if !contains(s, want) {
				t.Errorf("summary %q missing %q", s, want)
			}
		}
	})
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0) }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// --- EvaluateRule / EvaluatePolicies ----------------------------------------

func TestEvaluateRule_UnknownRuleIsInfo(t *testing.T) {
	t.Parallel()
	hpa := policyTestHPA()
	vs := EvaluateRule(hpa, Rule{ID: "unknown-rule", Name: "Unknown"})
	if len(vs) != 1 || vs[0].Severity != "info" {
		t.Fatalf("expected single info violation for unknown rule, got %#v", vs)
	}
}

func TestEvaluatePolicies_SeverityOverride(t *testing.T) {
	t.Parallel()
	// stabilization-window defaults to warning; the rule overrides it to critical.
	hpa := policyTestHPA(withStabilizationWindow(5)) // below default min 60
	f := File{Rules: []Rule{{
		ID: "stabilization-window", Name: "Stab", Severity: "critical", Parameters: Params{},
	}}}
	report := EvaluatePolicies(hpa, f)
	if len(report.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(report.Violations))
	}
	if report.Violations[0].Severity != "critical" {
		t.Errorf("severity override failed: %q", report.Violations[0].Severity)
	}
	if report.Score != 80 {
		t.Errorf("critical deduction: got score %d, want 80", report.Score)
	}
}

func TestEvaluatePolicies_DisabledRuleSkipped(t *testing.T) {
	t.Parallel()
	disabled := false
	hpa := policyTestHPA(withStabilizationWindow(5))
	f := File{Rules: []Rule{{
		ID: "stabilization-window", Name: "Stab", Enabled: &disabled, Parameters: Params{},
	}}}
	report := EvaluatePolicies(hpa, f)
	if len(report.Violations) != 0 {
		t.Fatalf("disabled rule should be skipped, got %#v", report.Violations)
	}
	if report.Score != 100 {
		t.Errorf("no violations -> score 100, got %d", report.Score)
	}
}

func TestEvaluatePolicies_PolicySetSelectorMismatch(t *testing.T) {
	t.Parallel()
	hpa := policyTestHPA(withStabilizationWindow(5))
	f := File{
		Policies: []Set{{
			Name: "prod", Selector: map[string]string{"env": "prod"},
			Rules: []Rule{{ID: "stabilization-window", Name: "Stab", Parameters: Params{}}},
		}},
	}
	report := EvaluatePolicies(hpa, f)
	if len(report.Violations) != 0 {
		t.Fatalf("non-matching selector should skip policy set, got %#v", report.Violations)
	}
}

func TestEvaluatePolicies_ScoreClampedToZero(t *testing.T) {
	t.Parallel()
	hpa := policyTestHPA(withStabilizationWindow(5))
	// Many critical rules -> score should clamp at 0.
	rules := []Rule{}
	for i := 0; i < 10; i++ {
		rules = append(rules, Rule{ID: "stabilization-window", Name: "Stab", Severity: "critical", Parameters: Params{}})
	}
	f := File{Rules: rules}
	report := EvaluatePolicies(hpa, f)
	if report.Score != 0 {
		t.Errorf("score should clamp at 0, got %d", report.Score)
	}
}

func TestPolicySetMatches(t *testing.T) {
	t.Parallel()
	hpa := policyTestHPA()
	hpa.Labels = map[string]string{"env": "prod", "team": "payments"}
	tests := []struct {
		name string
		set  Set
		want bool
	}{
		{"empty selector matches all", Set{}, true},
		{"single match", Set{Selector: map[string]string{"env": "prod"}}, true},
		{"multi match", Set{Selector: map[string]string{"env": "prod", "team": "payments"}}, true},
		{"missing key", Set{Selector: map[string]string{"env": "staging"}}, false},
		{"partial mismatch", Set{Selector: map[string]string{"env": "prod", "team": "other"}}, false},
	}
	for _, tc := range tests {
		if got := policySetMatches(hpa, tc.set); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// --- Individual builtin rule functions --------------------------------------

func TestStabilizationWindowPolicy(t *testing.T) {
	t.Parallel()
	t.Run("in range via explicit value", func(t *testing.T) {
		hpa := policyTestHPA(withStabilizationWindow(120))
		if got := stabilizationWindowPolicy(hpa, Params{}); got != nil {
			t.Errorf("120s is in [60,3600]: got %#v", got)
		}
	})
	t.Run("below min", func(t *testing.T) {
		hpa := policyTestHPA(withStabilizationWindow(10))
		got := stabilizationWindowPolicy(hpa, Params{})
		if len(got) != 1 {
			t.Fatalf("expected 1 violation, got %d", len(got))
		}
	})
	t.Run("uses kubernetes default 300s when unset", func(t *testing.T) {
		hpa := policyTestHPA() // no behavior
		if got := stabilizationWindowPolicy(hpa, Params{}); got != nil {
			t.Errorf("default 300s is in range: got %#v", got)
		}
	})
	t.Run("custom params", func(t *testing.T) {
		hpa := policyTestHPA(withStabilizationWindow(120))
		// Tighten range so 120s is now out of bounds.
		got := stabilizationWindowPolicy(hpa, Params{"min": 200, "max": 400})
		if len(got) != 1 {
			t.Fatalf("120s should violate [200,400]: got %d", len(got))
		}
	})
}

func TestMaxReplicasMultiplierPolicy(t *testing.T) {
	t.Parallel()
	t.Run("passes when ratio satisfied", func(t *testing.T) {
		hpa := policyTestHPA(withMinReplicas(1)) // max 10, min 1 -> ratio 10 >= 3
		if got := maxReplicasMultiplierPolicy(hpa, Params{}); got != nil {
			t.Errorf("expected pass: %#v", got)
		}
	})
	t.Run("violates when ratio not met", func(t *testing.T) {
		hpa := policyTestHPA(withMinReplicas(5)) // max 10, min 5 -> ratio 2 < 3
		got := maxReplicasMultiplierPolicy(hpa, Params{})
		if len(got) != 1 {
			t.Fatalf("expected violation, got %d", len(got))
		}
		if got[0].Required == "" {
			t.Error("Required field should be populated")
		}
	})
	t.Run("nil min defaults to 1", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{MaxReplicas: 3},
		}
		if got := maxReplicasMultiplierPolicy(hpa, Params{}); got != nil {
			t.Errorf("max=3 min=1 -> ratio 3 should pass: %#v", got)
		}
	})
}

func TestMaxReplicasFromCurrentPolicy(t *testing.T) {
	t.Parallel()
	t.Run("passes when within multiplier", func(t *testing.T) {
		hpa := policyTestHPA() // current 2, max 10, 5*2=10 -> allowed
		if got := maxReplicasFromCurrentPolicy(hpa, Params{}); got != nil {
			t.Errorf("expected pass: %#v", got)
		}
	})
	t.Run("violates when exceeding", func(t *testing.T) {
		hpa := policyTestHPA(func(h *autoscalingv2.HorizontalPodAutoscaler) {
			h.Status.CurrentReplicas = 1 // 5*1=5, max 10 > 5
		})
		got := maxReplicasFromCurrentPolicy(hpa, Params{})
		if len(got) != 1 {
			t.Fatalf("expected violation, got %d", len(got))
		}
	})
	t.Run("zero current clamps to 1", func(t *testing.T) {
		hpa := policyTestHPA(func(h *autoscalingv2.HorizontalPodAutoscaler) {
			h.Status.CurrentReplicas = 0
		})
		// 5*1=5, max 10 > 5 -> violation
		if got := maxReplicasFromCurrentPolicy(hpa, Params{}); len(got) != 1 {
			t.Fatalf("zero current clamps to 1 -> violation expected, got %d", len(got))
		}
	})
}

func TestBehaviorPolicyRequiredPolicy(t *testing.T) {
	t.Parallel()
	t.Run("no behavior -> two info violations", func(t *testing.T) {
		hpa := policyTestHPA()
		got := behaviorPolicyRequiredPolicy(hpa, Params{})
		if len(got) != 2 {
			t.Fatalf("expected 2 violations (scaleUp + scaleDown), got %d", len(got))
		}
	})
	t.Run("only scaleUp configured -> one violation", func(t *testing.T) {
		hpa := policyTestHPA(withScaleUpPolicy())
		got := behaviorPolicyRequiredPolicy(hpa, Params{})
		if len(got) != 1 || got[0].Description == "" {
			t.Fatalf("expected single scaleDown violation, got %#v", got)
		}
	})
	t.Run("both configured -> pass", func(t *testing.T) {
		hpa := policyTestHPA(withScaleUpPolicy(), withScaleDownPolicy())
		if got := behaviorPolicyRequiredPolicy(hpa, Params{}); got != nil {
			t.Errorf("expected pass: %#v", got)
		}
	})
	t.Run("disable scaleUp requirement", func(t *testing.T) {
		hpa := policyTestHPA() // nothing configured
		got := behaviorPolicyRequiredPolicy(hpa, Params{"requireScaleUp": false})
		if len(got) != 1 {
			t.Fatalf("only scaleDown should be required, got %d", len(got))
		}
	})
}

func TestMetricCoveragePolicy(t *testing.T) {
	t.Parallel()
	t.Run("below minimum metrics", func(t *testing.T) {
		hpa := policyTestHPA()
		got := metricCoveragePolicy(hpa, Params{"minMetrics": 2})
		if len(got) != 1 {
			t.Fatalf("expected violation, got %d", len(got))
		}
	})
	t.Run("has external but no resource -> info violation", func(t *testing.T) {
		hpa := policyTestHPA(withExternalMetric())
		got := metricCoveragePolicy(hpa, Params{})
		if len(got) != 1 || got[0].Severity != "info" {
			t.Fatalf("expected info violation for missing resource metric, got %#v", got)
		}
	})
	t.Run("has resource metric -> pass", func(t *testing.T) {
		hpa := policyTestHPA(withResourceMetric("cpu", 70))
		if got := metricCoveragePolicy(hpa, Params{}); got != nil {
			t.Errorf("expected pass: %#v", got)
		}
	})
	t.Run("requireResource disabled tolerates external-only", func(t *testing.T) {
		hpa := policyTestHPA(withExternalMetric())
		if got := metricCoveragePolicy(hpa, Params{"requireResource": false}); got != nil {
			t.Errorf("expected pass when requireResource=false: %#v", got)
		}
	})
}

func TestTargetUtilizationRangePolicy(t *testing.T) {
	t.Parallel()
	t.Run("in range", func(t *testing.T) {
		hpa := policyTestHPA(withResourceMetric("cpu", 70))
		if got := targetUtilizationRangePolicy(hpa, Params{}); got != nil {
			t.Errorf("70%% is in [30,90]: %#v", got)
		}
	})
	t.Run("too high", func(t *testing.T) {
		hpa := policyTestHPA(withResourceMetric("cpu", 95))
		if got := targetUtilizationRangePolicy(hpa, Params{}); len(got) != 1 {
			t.Fatalf("95%% out of range: got %d", len(got))
		}
	})
	t.Run("too low", func(t *testing.T) {
		hpa := policyTestHPA(withResourceMetric("memory", 10))
		if got := targetUtilizationRangePolicy(hpa, Params{}); len(got) != 1 {
			t.Fatalf("10%% out of range: got %d", len(got))
		}
	})
	t.Run("non-utilization metric ignored", func(t *testing.T) {
		hpa := policyTestHPA()
		hpa.Spec.Metrics = append(hpa.Spec.Metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name:   corev1.ResourceCPU,
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType},
			},
		})
		if got := targetUtilizationRangePolicy(hpa, Params{}); got != nil {
			t.Errorf("non-utilization metric should be skipped: %#v", got)
		}
	})
}

func TestReplicaRangePolicy(t *testing.T) {
	t.Parallel()
	t.Run("passes when ratio ok", func(t *testing.T) {
		hpa := policyTestHPA(withMinReplicas(1)) // ratio 10/1 = 10
		if got := replicaRangePolicy(hpa, Params{}); got != nil {
			t.Errorf("ratio 10 <= default 10: %#v", got)
		}
	})
	t.Run("violates when ratio too high", func(t *testing.T) {
		hpa := policyTestHPA(withMinReplicas(1)) // ratio 10
		got := replicaRangePolicy(hpa, Params{"maxRatio": 5})
		if len(got) != 1 {
			t.Fatalf("ratio 10 > 5 should violate, got %d", len(got))
		}
	})
	t.Run("zero minReplicas returns nil", func(t *testing.T) {
		hpa := policyTestHPA(withMinReplicas(0))
		if got := replicaRangePolicy(hpa, Params{}); got != nil {
			t.Errorf("zero min should be skipped: %#v", got)
		}
	})
}

// --- LoadPolicyFile ----------------------------------------------------------

func TestLoadPolicyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	t.Run("valid file", func(t *testing.T) {
		path := filepath.Join(dir, "valid.yaml")
		content := `
apiVersion: hpa-status/v1
rules:
  - id: stabilization-window
    name: Stab
    type: stabilizationWindowSeconds
    min: 60
    max: 3600
`
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		f, err := LoadPolicyFile(path)
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if len(f.Rules) != 1 {
			t.Fatalf("expected 1 rule, got %d", len(f.Rules))
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := LoadPolicyFile(filepath.Join(dir, "does-not-exist.yaml"))
		if err == nil {
			t.Error("expected error for missing file")
		}
	})

	t.Run("invalid YAML", func(t *testing.T) {
		path := filepath.Join(dir, "bad.yaml")
		if err := os.WriteFile(path, []byte("apiVersion: [oops\n  not: valid"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPolicyFile(path); err == nil {
			t.Error("expected parse error")
		}
	})

	t.Run("validation failure", func(t *testing.T) {
		path := filepath.Join(dir, "invalid.yaml")
		content := "apiVersion: hpa-status/v1\nrules:\n  - id: x\n    name: X\n    severity: bogus\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPolicyFile(path); err == nil {
			t.Error("expected validation error for invalid severity")
		}
	})
}

// --- mergeJSONMap / applySuggestionPatch ------------------------------------

func TestMergeJSONMap(t *testing.T) {
	t.Parallel()
	dst := map[string]any{"a": "1", "nested": map[string]any{"x": "1"}}
	src := map[string]any{
		"b":      "2",
		"nested": map[string]any{"y": "2"},
		"a":      "updated",
	}
	mergeJSONMap(dst, src)
	if dst["a"] != "updated" {
		t.Errorf("scalar overwrite failed: %v", dst["a"])
	}
	if dst["b"] != "2" {
		t.Errorf("new key not added: %v", dst["b"])
	}
	nested, ok := dst["nested"].(map[string]any)
	if !ok {
		t.Fatal("nested should remain a map")
	}
	if nested["x"] != "1" || nested["y"] != "2" {
		t.Errorf("deep merge failed: %#v", nested)
	}
}

func TestMergeJSONMap_NilDeletesKey(t *testing.T) {
	t.Parallel()
	dst := map[string]any{"a": "1", "b": "2"}
	src := map[string]any{"a": nil}
	mergeJSONMap(dst, src)
	if _, exists := dst["a"]; exists {
		t.Error("nil value should delete key")
	}
	if dst["b"] != "2" {
		t.Error("unrelated key should survive")
	}
}

func TestApplySuggestionPatch(t *testing.T) {
	t.Parallel()
	hpa := policyTestHPA() // maxReplicas 10
	patch := `{"spec":{"maxReplicas":50}}`
	patched, err := applySuggestionPatch(hpa, patch)
	if err != nil {
		t.Fatalf("applySuggestionPatch: %v", err)
	}
	if patched.Spec.MaxReplicas != 50 {
		t.Errorf("maxReplicas not patched: %d", patched.Spec.MaxReplicas)
	}
	// Original HPA must be unchanged (deep copy via marshal round-trip).
	if hpa.Spec.MaxReplicas != 10 {
		t.Errorf("source HPA was mutated: %d", hpa.Spec.MaxReplicas)
	}
}

func TestApplySuggestionPatch_InvalidJSON(t *testing.T) {
	t.Parallel()
	hpa := policyTestHPA()
	if _, err := applySuggestionPatch(hpa, `{not json`); err == nil {
		t.Error("expected error for invalid JSON patch")
	}
}

// --- firstViolationWithSeverity ---------------------------------------------

func TestFirstViolationWithSeverity(t *testing.T) {
	t.Parallel()
	vs := []Violation{
		{Severity: "info"},
		{Severity: "warning"},
		{Severity: "critical"},
	}
	if got := firstViolationWithSeverity(vs, "critical"); got == nil || got.Severity != "critical" {
		t.Error("expected critical violation")
	}
	if got := firstViolationWithSeverity(vs, "warning"); got == nil {
		t.Error("expected warning violation")
	}
	if got := firstViolationWithSeverity(vs, "nonexistent"); got != nil {
		t.Error("expected nil for missing severity")
	}
	if got := firstViolationWithSeverity(nil, "critical"); got != nil {
		t.Error("expected nil for empty list")
	}
}

// --- GuardFix ---------------------------------------------------------------

func TestGuardFix_NilHPABlocksAll(t *testing.T) {
	t.Parallel()
	sugs := []suggestion.Suggestion{
		{Apply: true, Patch: `{"spec":{"maxReplicas":50}}`},
	}
	result := GuardFix(sugs, File{}, nil)
	if len(result.Blocked) != 1 {
		t.Fatalf("expected 1 blocked, got %d", len(result.Blocked))
	}
	if len(result.Allowed) != 0 {
		t.Errorf("expected 0 allowed, got %d", len(result.Allowed))
	}
}

func TestGuardFix_NonApplySuggestionAllowed(t *testing.T) {
	t.Parallel()
	hpa := policyTestHPA()
	sugs := []suggestion.Suggestion{
		{Apply: false, Patch: ""}, // not an apply suggestion
		{Apply: true, Patch: ""},  // apply but empty patch
	}
	result := GuardFix(sugs, File{}, hpa)
	if len(result.Allowed) != 2 {
		t.Fatalf("expected both allowed, got %d", len(result.Allowed))
	}
	if len(result.Blocked) != 0 {
		t.Errorf("expected no blocks, got %d", len(result.Blocked))
	}
}

func TestGuardFix_CriticalViolationBlocks(t *testing.T) {
	t.Parallel()
	hpa := policyTestHPA()
	// A patch that introduces a max-replicas-from-current violation.
	patch := `{"spec":{"maxReplicas":1000}}`
	sugs := []suggestion.Suggestion{{Apply: true, Patch: patch}}
	f := File{Rules: []Rule{{
		ID: "max-replicas-from-current", Name: "Max", Severity: "critical", Parameters: Params{},
	}}}
	result := GuardFix(sugs, f, hpa)
	if len(result.Blocked) != 1 {
		t.Fatalf("expected 1 blocked, got %d (allowed=%d, warnings=%d)",
			len(result.Blocked), len(result.Allowed), len(result.Warnings))
	}
}

func TestGuardFix_WarningProducesWarningButAllowed(t *testing.T) {
	t.Parallel()
	hpa := policyTestHPA()
	// Patch the stabilization window to a violating value (warning severity).
	patch := `{"spec":{"behavior":{"scaleDown":{"stabilizationWindowSeconds":1}}}}`
	sugs := []suggestion.Suggestion{{Apply: true, Patch: patch}}
	f := File{Rules: []Rule{{
		ID: "stabilization-window", Name: "Stab", Parameters: Params{},
	}}}
	result := GuardFix(sugs, f, hpa)
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(result.Warnings))
	}
	if len(result.Allowed) != 1 {
		t.Errorf("warning should still allow the suggestion, got allowed=%d", len(result.Allowed))
	}
}

func TestGuardFix_InvalidPatchBlocks(t *testing.T) {
	t.Parallel()
	hpa := policyTestHPA()
	sugs := []suggestion.Suggestion{{Apply: true, Patch: `{invalid`}}
	result := GuardFix(sugs, File{}, hpa)
	if len(result.Blocked) != 1 {
		t.Fatalf("expected invalid patch blocked, got %d", len(result.Blocked))
	}
}

func TestGuardFix_CompliantPatchAllowed(t *testing.T) {
	t.Parallel()
	hpa := policyTestHPA()
	// A patch that does not trigger any rule -> allowed.
	patch := `{"spec":{"maxReplicas":20}}`
	sugs := []suggestion.Suggestion{{Apply: true, Patch: patch}}
	f := File{Rules: []Rule{{
		ID: "stabilization-window", Name: "Stab", Parameters: Params{},
	}}}
	result := GuardFix(sugs, f, hpa)
	if len(result.Allowed) != 1 {
		t.Fatalf("expected compliant patch allowed, got allowed=%d blocked=%d",
			len(result.Allowed), len(result.Blocked))
	}
	if len(result.Warnings) != 0 || len(result.Blocked) != 0 {
		t.Errorf("expected no warnings/blocks: warnings=%d blocked=%d", len(result.Warnings), len(result.Blocked))
	}
}

// Smoke test: ensure builtinRules registry references all the expected ids.
func TestBuiltinRules_Registry(t *testing.T) {
	t.Parallel()
	want := []string{
		"stabilization-window",
		"max-replicas-multiplier",
		"max-replicas-from-current",
		"behavior-policy-required",
		"metric-coverage",
		"target-utilization-range",
		"replica-range",
	}
	got := make([]string, 0, len(want))
	for k := range builtinRules {
		got = append(got, k)
	}
	if !sameSet(got, want) {
		t.Errorf("builtinRules registry mismatch: got %v, want %v", got, want)
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]struct{}, len(b))
	for _, s := range b {
		set[s] = struct{}{}
	}
	for _, s := range a {
		if _, ok := set[s]; !ok {
			return false
		}
	}
	return true
}

// Sanity guard: detect accidental changes to Violation fields.
func TestViolationShape(t *testing.T) {
	t.Parallel()
	v := Violation{}
	typ := reflect.TypeOf(v)
	expectedFields := []string{"RuleID", "RuleName", "Severity", "Description", "Current", "Required", "FixPatch", "FixCommand"}
	if typ.NumField() != len(expectedFields) {
		t.Fatalf("Violation field count changed: got %d, please update this test", typ.NumField())
	}
	for i, name := range expectedFields {
		if typ.Field(i).Name != name {
			t.Errorf("field %d: got %s, want %s", i, typ.Field(i).Name, name)
		}
	}
}
