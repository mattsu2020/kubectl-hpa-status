package alerts

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestRulesDefaultsToPrometheus(t *testing.T) {
	def, err := Rules("")
	if err != nil {
		t.Fatalf("Rules(\"\") returned error: %v", err)
	}
	explicit, err := Rules(string(FormatPrometheus))
	if err != nil {
		t.Fatalf("Rules(prometheus) returned error: %v", err)
	}
	if def != explicit {
		t.Error("the empty format must produce the same rules as \"prometheus\"")
	}
}

func TestPrometheusRulesAreValidYAML(t *testing.T) {
	rules, err := Rules(string(FormatPrometheus))
	if err != nil {
		t.Fatalf("Rules returned error: %v", err)
	}

	// The output is pasted straight into a Prometheus rule file, so a
	// malformed document is a user-facing break.
	var doc struct {
		Groups []struct {
			Name  string `json:"name"`
			Rules []struct {
				Alert string `json:"alert"`
				Expr  string `json:"expr"`
				For   string `json:"for"`
			} `json:"rules"`
		} `json:"groups"`
	}
	if err := yaml.Unmarshal([]byte(rules), &doc); err != nil {
		t.Fatalf("Prometheus rules are not valid YAML: %v", err)
	}
	if len(doc.Groups) != 1 {
		t.Fatalf("expected one rule group, got %d", len(doc.Groups))
	}
	if len(doc.Groups[0].Rules) == 0 {
		t.Fatal("expected at least one alert rule")
	}
	for _, rule := range doc.Groups[0].Rules {
		if rule.Alert == "" || rule.Expr == "" {
			t.Errorf("rule is missing alert/expr: %+v", rule)
		}
	}
}

func TestDatadogRulesAreValidYAML(t *testing.T) {
	rules, err := Rules(string(FormatDatadog))
	if err != nil {
		t.Fatalf("Rules returned error: %v", err)
	}

	var monitors []struct {
		Name    string   `json:"name"`
		Query   string   `json:"query"`
		Message string   `json:"message"`
		Tags    []string `json:"tags"`
	}
	if err := yaml.Unmarshal([]byte(rules), &monitors); err != nil {
		t.Fatalf("Datadog rules are not valid YAML: %v", err)
	}
	if len(monitors) == 0 {
		t.Fatal("expected at least one monitor")
	}
	for _, monitor := range monitors {
		if monitor.Name == "" || monitor.Query == "" {
			t.Errorf("monitor is missing name/query: %+v", monitor)
		}
	}
}

func TestRulesReferenceTheHealthScoreMetric(t *testing.T) {
	// Both dialects encode the same health-score semantics; if the metric name
	// ever changes, both documents must change together.
	for _, format := range []Format{FormatPrometheus, FormatDatadog} {
		rules, err := Rules(string(format))
		if err != nil {
			t.Fatalf("Rules(%s) returned error: %v", format, err)
		}
		if !strings.Contains(rules, "hpa_status_health_score") {
			t.Errorf("%s rules do not reference hpa_status_health_score", format)
		}
	}
}

func TestRulesRejectsUnknownFormat(t *testing.T) {
	_, err := Rules("pagerduty")
	if err == nil {
		t.Fatal("expected an error for an unsupported format")
	}
	for _, want := range []string{"pagerduty", "prometheus", "datadog"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q in the error, got: %v", want, err)
		}
	}
}
