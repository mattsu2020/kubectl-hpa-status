package healthtrend

import (
	"strings"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/flapping"
)

func trendResult() Result {
	return Result{
		Snapshots: []HealthSnapshot{
			{Timestamp: time.Now(), HealthScore: 90, HealthState: "OK"},
			{Timestamp: time.Now().Add(time.Hour), HealthScore: 60, HealthState: "LIMITED"},
		},
		Variance:         2.5,
		MinScore:         60,
		MaxScore:         90,
		MeanScore:        75,
		DegradationRate:  -10,
		FlappingDetected: true,
		FlappingSeverity: "HIGH",
		Sparkline:        "▁▂▃",
		Anomalies: []flapping.AnomalyDetection{
			{
				Type:          flapping.AnomalySuddenDegradation,
				Severity:      "critical",
				ScoreBefore:   90,
				ScoreAfter:    60,
				Duration:      "1h",
				CauseEstimate: "load spike",
				Remediation:   "scale up",
			},
		},
	}
}

func TestFormatTrendText(t *testing.T) {
	got := FormatTrendText(trendResult())
	for _, want := range []string{"Health Trend:", "mean=75", "min=60", "max=90", "variance=2.5", "degrading (-10.0 score/hour)", "FLAPPING(HIGH)", "▁▂▃"} {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatTrendText missing %q:\n%s", want, got)
		}
	}
	if got := FormatTrendText(Result{}); got != "" {
		t.Fatalf("FormatTrendText(empty) = %q, want empty", got)
	}
}

func TestFormatTrendText_Improving(t *testing.T) {
	r := trendResult()
	r.DegradationRate = 10
	if got := FormatTrendText(r); !strings.Contains(got, "improving") {
		t.Fatalf("expected improving direction, got %q", got)
	}
}

func TestFormatTrendAnomalyText(t *testing.T) {
	got := FormatTrendAnomalyText(trendResult())
	for _, want := range []string{"Anomalies: 1 detected", "[1]", "sudden-degradation", "critical", "90->60", "duration=1h", "cause: load spike", "fix:   scale up"} {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatTrendAnomalyText missing %q:\n%s", want, got)
		}
	}
	if got := FormatTrendAnomalyText(Result{}); got != "" {
		t.Fatalf("FormatTrendAnomalyText(empty) = %q, want empty", got)
	}
}

func TestFormatTrendAnomalyGraph(t *testing.T) {
	got := FormatTrendAnomalyGraph(trendResult(), 40)
	for _, want := range []string{"Health Trend:", "Anomalies:", "▁▂▃"} {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatTrendAnomalyGraph missing %q:\n%s", want, got)
		}
	}
	if got := FormatTrendAnomalyGraph(Result{}, 40); got != "" {
		t.Fatalf("FormatTrendAnomalyGraph(empty) = %q, want empty", got)
	}
}

func TestFormatTrendListRow(t *testing.T) {
	got := FormatTrendListRow(trendResult())
	for _, want := range []string{"▁▂▃", "down", "FLAP:HIGH"} {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatTrendListRow missing %q:\n%s", want, got)
		}
	}
	// Empty result -> empty string.
	if got := FormatTrendListRow(Result{}); got != "" {
		t.Fatalf("FormatTrendListRow(empty) = %q, want empty", got)
	}
	// Upward degradation yields "up".
	r := trendResult()
	r.DegradationRate = 20
	if got := FormatTrendListRow(r); !strings.Contains(got, "up") {
		t.Fatalf("expected up marker, got %q", got)
	}
}
