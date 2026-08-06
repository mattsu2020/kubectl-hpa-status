package compare

import (
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
)

func diffFields(r Report) []string {
	fields := make([]string, 0, len(r.Differences))
	for _, d := range r.Differences {
		fields = append(fields, d.Field)
	}
	return fields
}

func TestBuildReport_IdenticalHPAsHaveNoDifferences(t *testing.T) {
	from := testutil.BuildHPA("staging", "web", testutil.WithMinMax(2, 10))
	to := testutil.BuildHPA("prod", "web", testutil.WithMinMax(2, 10))

	report := BuildReport("staging/web", "prod/web", from, to)

	if report.From != "staging/web" || report.To != "prod/web" {
		t.Errorf("labels = %q -> %q, want staging/web -> prod/web", report.From, report.To)
	}
	if len(report.Differences) != 0 {
		t.Errorf("identical HPAs produced differences: %+v", report.Differences)
	}
	if len(report.Risks) != 0 {
		t.Errorf("identical HPAs produced risks: %v", report.Risks)
	}
}

func TestBuildReport_ReportsSpecDifferencesAndRisk(t *testing.T) {
	from := testutil.BuildHPA("staging", "web",
		testutil.WithMinMax(2, 20),
		testutil.WithResourceMetric("cpu", 70, 50),
	)
	to := testutil.BuildHPA("prod", "web",
		testutil.WithMinMax(4, 8),
		testutil.WithResourceMetric("cpu", 80, 50),
	)

	report := BuildReport("staging/web", "prod/web", from, to)

	fields := strings.Join(diffFields(report), ",")
	for _, want := range []string{"minReplicas", "maxReplicas", "metrics"} {
		if !strings.Contains(fields, want) {
			t.Errorf("expected a %s difference, got fields: %s", want, fields)
		}
	}
	for _, diff := range report.Differences {
		switch diff.Field {
		case "minReplicas":
			if diff.From != "2" || diff.To != "4" {
				t.Errorf("minReplicas diff = %s -> %s, want 2 -> 4", diff.From, diff.To)
			}
		case "maxReplicas":
			if diff.From != "20" || diff.To != "8" {
				t.Errorf("maxReplicas diff = %s -> %s, want 20 -> 8", diff.From, diff.To)
			}
		}
	}

	// A lower maxReplicas downstream is the risk this domain exists to catch.
	if len(report.Risks) == 0 || !strings.Contains(report.Risks[0], "lower maxReplicas") {
		t.Errorf("expected a lower-maxReplicas risk, got: %v", report.Risks)
	}
}

func TestBuildReport_FlagsRemovedStabilization(t *testing.T) {
	from := testutil.BuildHPA("staging", "web", testutil.WithScaleDownStabilizationWindow(300))
	to := testutil.BuildHPA("prod", "web")

	report := BuildReport("staging/web", "prod/web", from, to)

	joined := strings.Join(diffFields(report), ",")
	if !strings.Contains(joined, "behavior.scaleDown.stabilizationWindowSeconds") {
		t.Errorf("expected a stabilization-window difference, got fields: %s", joined)
	}
}

func TestMetricSummary_AllMetricTypes(t *testing.T) {
	hpa := testutil.BuildHPA("prod", "web",
		testutil.WithResourceMetric("cpu", 70, 50),
		testutil.WithContainerResourceMetric("app", "memory", 80, 40),
		testutil.WithExternalMetric("queue_depth", "100"),
		testutil.WithPodsMetric("rps", "500", "300"),
		testutil.WithObjectMetric("requests", "1k", "800"),
	)

	got := MetricSummary(hpa)
	for _, want := range []string{
		"Resource/cpu=70%",
		"ContainerResource/app/memory=80%",
		"External/queue_depth=100",
		"Pods/rps=500",
		"Object/requests=1k",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("MetricSummary missing %q, got: %s", want, got)
		}
	}

	if empty := MetricSummary(testutil.BuildHPA("prod", "bare")); empty != "" {
		t.Errorf("MetricSummary of an HPA without metrics = %q, want empty", empty)
	}
}

func TestStabilizationWindow(t *testing.T) {
	if got := StabilizationWindow(testutil.BuildHPA("prod", "web")); got != "<default>" {
		t.Errorf("StabilizationWindow without behavior = %q, want <default>", got)
	}
	if got := StabilizationWindow(testutil.BuildHPA("prod", "web", testutil.WithScaleDownStabilizationWindow(120))); got != "120" {
		t.Errorf("StabilizationWindow with explicit window = %q, want 120", got)
	}
}
