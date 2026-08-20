package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// writeSeasonalRecord writes a JSONL record whose desiredReplicas ramps from
// base to peak between 09:00 and 17:00 UTC on each of the given days.
func writeSeasonalRecord(t *testing.T, days int, base, peak int32) string {
	t.Helper()

	tmp, err := os.CreateTemp(t.TempDir(), "hpa-seasonal-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tmp.Close() }()

	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC) // a Monday
	for d := range days {
		day := start.AddDate(0, 0, d)
		var snapshots []hpaanalysis.TimelineSnapshot
		for minute := 0; minute < 24*60; minute += 15 {
			desired := base
			if minute >= 9*60 && minute < 17*60 {
				desired = peak
			}
			snapshots = append(snapshots, hpaanalysis.TimelineSnapshot{
				Timestamp: day.Add(time.Duration(minute) * time.Minute),
				Current:   desired,
				Desired:   desired,
			})
		}
		trace := hpaanalysis.TimelineTrace{
			Namespace: "prod",
			HPAName:   "web",
			Snapshots: snapshots,
		}
		if err := writeRecordLine(tmp, trace); err != nil {
			t.Fatal(err)
		}
	}
	return tmp.Name()
}

func TestRunAnalyzeRecordDetectsSeasonality(t *testing.T) {
	path := writeSeasonalRecord(t, 5, 3, 12)

	var buf bytes.Buffer
	params := analyzeRecordOptions{detect: detectSeasonality, timezone: "UTC"}
	if err := runAnalyzeRecord(&buf, &options{}, path, params); err != nil {
		t.Fatalf("runAnalyzeRecord returned error: %v", err)
	}

	output := buf.String()
	for _, want := range []string{
		"prod/web: recurring daily pattern detected",
		"09:00-17:00",
		"raise minReplicas to 12 at 08:45",
		"cron:    45 8 * * *",
		"type: cron",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}
}

func TestRunAnalyzeRecordSeasonalityJSON(t *testing.T) {
	path := writeSeasonalRecord(t, 5, 3, 12)

	var buf bytes.Buffer
	opts := &options{}
	opts.Output = "json"
	params := analyzeRecordOptions{detect: detectSeasonality, timezone: "UTC"}
	if err := runAnalyzeRecord(&buf, opts, path, params); err != nil {
		t.Fatalf("runAnalyzeRecord returned error: %v", err)
	}

	var decoded recordSeasonality
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if len(decoded.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(decoded.Items))
	}
	item := decoded.Items[0]
	if !item.Analysis.Detected {
		t.Fatalf("expected Detected=true, notes=%v", item.Analysis.Notes)
	}
	if item.Analysis.Recommendation.MinReplicas != 12 {
		t.Errorf("MinReplicas = %d, want 12", item.Analysis.Recommendation.MinReplicas)
	}
	if item.Snapshots == 0 {
		t.Error("expected the snapshot count to be reported")
	}
}

func TestRunAnalyzeRecordSeasonalityInsufficientData(t *testing.T) {
	path := writeSeasonalRecord(t, 1, 3, 12)

	var buf bytes.Buffer
	params := analyzeRecordOptions{detect: detectSeasonality, timezone: "UTC"}
	if err := runAnalyzeRecord(&buf, &options{}, path, params); err != nil {
		t.Fatalf("runAnalyzeRecord returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "insufficient data") {
		t.Errorf("expected an insufficient-data report, got:\n%s", output)
	}
	if !strings.Contains(output, "record for at least") {
		t.Errorf("expected guidance on how much data is needed, got:\n%s", output)
	}
}

func TestRunAnalyzeRecordUnsupportedDetector(t *testing.T) {
	path := writeSeasonalRecord(t, 2, 3, 12)

	err := runAnalyzeRecord(&bytes.Buffer{}, &options{}, path, analyzeRecordOptions{detect: "bogus"})
	if err == nil {
		t.Fatal("expected an error for an unsupported detector")
	}
	if !strings.Contains(err.Error(), "seasonality") {
		t.Errorf("error should list the supported detectors, got: %v", err)
	}
}

func TestRunAnalyzeRecordSeasonalityInvalidTimezone(t *testing.T) {
	path := writeSeasonalRecord(t, 2, 3, 12)

	params := analyzeRecordOptions{detect: detectSeasonality, timezone: "Not/AZone"}
	err := runAnalyzeRecord(&bytes.Buffer{}, &options{}, path, params)
	if err == nil {
		t.Fatal("expected an error for an invalid timezone")
	}
	if !strings.Contains(err.Error(), "--timezone") {
		t.Errorf("error should name the offending flag, got: %v", err)
	}
}

func TestObservationsFromTraceDropsUndatedSnapshots(t *testing.T) {
	trace := hpaanalysis.TimelineTrace{
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{Desired: 3}, // no timestamp: unusable for time-of-day placement
			{Timestamp: time.Now(), Desired: 5},
		},
	}

	got := observationsFromTrace(trace)
	if len(got) != 1 {
		t.Fatalf("expected 1 usable observation, got %d", len(got))
	}
	if got[0].Desired != 5 {
		t.Errorf("Desired = %d, want 5", got[0].Desired)
	}
}

func TestSeasonalityOptionsDefaultsAreLeftToDetector(t *testing.T) {
	got, err := seasonalityOptions(analyzeRecordOptions{detect: detectSeasonality})
	if err != nil {
		t.Fatalf("seasonalityOptions returned error: %v", err)
	}
	if got.Location != nil {
		t.Error("Location should stay nil so the detector applies its own default")
	}
	if got.LeadTime != 0 || got.BucketMinutes != 0 || got.MinDays != 0 {
		t.Errorf("unset flags must stay zero, got %+v", got)
	}
}

func TestRunAnalyzeRecordSeasonalityFlatTraffic(t *testing.T) {
	path := writeSeasonalRecord(t, 4, 5, 5) // base == peak: no ramp at all

	var buf bytes.Buffer
	params := analyzeRecordOptions{detect: detectSeasonality, timezone: "UTC"}
	if err := runAnalyzeRecord(&buf, &options{}, path, params); err != nil {
		t.Fatalf("runAnalyzeRecord returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "no recurring pattern detected") {
		t.Errorf("expected no detection for flat traffic, got:\n%s", output)
	}
}

// writeSeasonalRecordWithMetrics writes a JSONL record whose desiredReplicas
// ramp from base to peak between 09:00 and 17:00 UTC and whose cpu
// utilization ramps alongside it, exercising the typed metric signal path.
func writeSeasonalRecordWithMetrics(t *testing.T, days int, base, peak int32, cpuBase, cpuPeak float64) string {
	t.Helper()

	tmp, err := os.CreateTemp(t.TempDir(), "hpa-seasonal-metric-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tmp.Close() }()

	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC) // a Monday
	for d := range days {
		day := start.AddDate(0, 0, d)
		var snapshots []hpaanalysis.TimelineSnapshot
		for minute := 0; minute < 24*60; minute += 15 {
			desired := base
			cpu := cpuBase
			if minute >= 9*60 && minute < 17*60 {
				desired = peak
				cpu = cpuPeak
			}
			snapshots = append(snapshots, hpaanalysis.TimelineSnapshot{
				Timestamp: day.Add(time.Duration(minute) * time.Minute),
				Current:   desired,
				Desired:   desired,
				MetricValues: []hpaanalysis.MetricReading{
					{Type: "Resource", Name: "cpu", Value: cpu, Target: 60, Unit: "%"},
				},
			})
		}
		trace := hpaanalysis.TimelineTrace{
			Namespace: "prod",
			HPAName:   "web",
			Snapshots: snapshots,
		}
		if err := writeRecordLine(tmp, trace); err != nil {
			t.Fatal(err)
		}
	}
	return tmp.Name()
}

func TestRunAnalyzeRecordSeasonalityAutoUsesMetricSignal(t *testing.T) {
	path := writeSeasonalRecordWithMetrics(t, 5, 3, 12, 20, 80)

	var buf bytes.Buffer
	params := analyzeRecordOptions{detect: detectSeasonality, timezone: "UTC"}
	if err := runAnalyzeRecord(&buf, &options{}, path, params); err != nil {
		t.Fatalf("runAnalyzeRecord returned error: %v", err)
	}

	output := buf.String()
	for _, want := range []string{
		"signal:  metric:cpu (%)",
		"demand:   baseline 20.0% -> peak 80.0%",
		"raise minReplicas to 12",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}
}

func TestRunAnalyzeRecordSeasonalitySignalReplicasOverridesMetric(t *testing.T) {
	path := writeSeasonalRecordWithMetrics(t, 5, 3, 12, 20, 80)

	var buf bytes.Buffer
	params := analyzeRecordOptions{detect: detectSeasonality, timezone: "UTC", signal: "replicas"}
	if err := runAnalyzeRecord(&buf, &options{}, path, params); err != nil {
		t.Fatalf("runAnalyzeRecord returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "signal:  replicas") {
		t.Errorf("expected the replica signal to win over recorded metrics, got:\n%s", output)
	}
	if !strings.Contains(output, "demand:   baseline 3.0 -> peak 12 replicas") {
		t.Errorf("expected replica-space demand line, got:\n%s", output)
	}
}

func TestRunAnalyzeRecordSeasonalityMetricSignalJSON(t *testing.T) {
	path := writeSeasonalRecordWithMetrics(t, 5, 3, 12, 20, 80)

	var buf bytes.Buffer
	opts := &options{}
	opts.Output = "json"
	params := analyzeRecordOptions{detect: detectSeasonality, timezone: "UTC"}
	if err := runAnalyzeRecord(&buf, opts, path, params); err != nil {
		t.Fatalf("runAnalyzeRecord returned error: %v", err)
	}

	var decoded recordSeasonality
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if len(decoded.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(decoded.Items))
	}
	a := decoded.Items[0].Analysis
	if !a.Detected {
		t.Fatalf("expected Detected=true, notes=%v", a.Notes)
	}
	if a.Signal != "metric:cpu" {
		t.Errorf("Signal = %q, want metric:cpu", a.Signal)
	}
	if a.SignalUnit != "%" {
		t.Errorf("SignalUnit = %q, want %%", a.SignalUnit)
	}
	if a.Peak == nil || a.Peak.PeakSignal != 80 {
		t.Errorf("PeakSignal = %+v, want 80", a.Peak)
	}
}

func TestRunAnalyzeRecordSeasonalityForcedMetricOnLegacyRecord(t *testing.T) {
	path := writeSeasonalRecord(t, 5, 3, 12) // no metricValues recorded

	var buf bytes.Buffer
	params := analyzeRecordOptions{detect: detectSeasonality, timezone: "UTC", signal: "metric"}
	if err := runAnalyzeRecord(&buf, &options{}, path, params); err != nil {
		t.Fatalf("runAnalyzeRecord returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "no typed metric values") {
		t.Errorf("expected an explanation instead of a silent fallback, got:\n%s", output)
	}
	if !strings.Contains(output, "no recurring pattern detected") {
		t.Errorf("forced metric on a legacy record must not report a detection, got:\n%s", output)
	}
	if strings.Contains(output, "recurring daily") || strings.Contains(output, "recurring weekly") {
		t.Errorf("forced metric on a legacy record must not report a detection, got:\n%s", output)
	}
}

func TestRunAnalyzeRecordSeasonalityInvalidSignal(t *testing.T) {
	path := writeSeasonalRecord(t, 2, 3, 12)

	params := analyzeRecordOptions{detect: detectSeasonality, timezone: "UTC", signal: "queue"}
	err := runAnalyzeRecord(&bytes.Buffer{}, &options{}, path, params)
	if err == nil {
		t.Fatal("expected an error for an invalid signal")
	}
	if !strings.Contains(err.Error(), "--signal") {
		t.Errorf("error should name the offending flag, got: %v", err)
	}
}

func TestObservationsFromTraceCarriesMetricSamples(t *testing.T) {
	trace := hpaanalysis.TimelineTrace{
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{
				Timestamp: time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC),
				Desired:   5,
				MetricValues: []hpaanalysis.MetricReading{
					{Type: "Resource", Name: "cpu", Value: 80, Target: 60, Unit: "%"},
					{Type: "External", Name: "queue_depth", Value: 120},
				},
			},
		},
	}

	got := observationsFromTrace(trace)
	if len(got) != 1 {
		t.Fatalf("expected 1 observation, got %d", len(got))
	}
	if len(got[0].Metrics) != 2 {
		t.Fatalf("expected both metric samples to ride along, got %d", len(got[0].Metrics))
	}
	if got[0].Metrics[0].Name != "cpu" || got[0].Metrics[0].Value != 80 || got[0].Metrics[0].Unit != "%" {
		t.Errorf("cpu sample = %+v", got[0].Metrics[0])
	}
}
