package replaylab

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestWriteReportPropagatesWriterError(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"text", "markdown"} {
		if err := WriteReport(failingWriter{}, format, Report{Name: "test"}); err == nil {
			t.Fatalf("format %s: expected writer error", format)
		}
	}
}

func fullReport() Report {
	return Report{
		Namespace: "default",
		Name:      "web",
		Record:    "trace.json",
		Candidate: "candidate.yaml",
		ProposedConfig: map[string]string{
			"maxReplicas": "20",
		},
		Current: Summary{
			Snapshots: 12, ScaleEvents: 6, DirectionFlips: 3,
			PeakReplicas: 9, MaxReplicas: 10, MaxReplicasReached: 2,
			CappedDuration: "10m", EstimatedUnderProvision: 1,
			PodHours: 4.5, FlappingScore: "42", FlappingLabel: "moderate",
		},
		CandidateResult: &Summary{
			Snapshots: 12, ScaleEvents: 3, DirectionFlips: 1,
			PeakReplicas: 8, FlappingScore: "12", FlappingLabel: "low",
		},
		// Two candidates: the text renderer switches to the policy comparison
		// table only when more than one candidate was simulated.
		Candidates: []CandidateResult{
			{Name: "wider-window", Candidate: "a.yaml",
				ProposedConfig: map[string]string{"scaleDownStabilization": "600"},
				Summary:        Summary{ScaleEvents: 2, FlappingScore: "8"},
				Recommendation: "adopt"},
			{Name: "higher-max", Candidate: "b.yaml",
				Summary: Summary{ScaleEvents: 4, FlappingScore: "20"}},
		},
		Impact: &Impact{
			ScaleEventReductionPct: 50,
			PodHoursChangePct:      -10,
			UnderProvisionFixed:    true,
		},
		Recommendation:  "increase stabilization window",
		Recommendations: []string{"increase stabilization window"},
		Limitations:     []string{"synthetic demand model"},
	}
}

func TestWriteReport_JSON(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := WriteReport(&buf, "json", fullReport()); err != nil {
		t.Fatalf("WriteReport json: %v", err)
	}
	var got Report
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if got.Name != "web" || got.Impact == nil {
		t.Errorf("unexpected round-trip result: %+v", got)
	}
}

func TestWriteReport_YAML(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := WriteReport(&buf, "yaml", fullReport()); err != nil {
		t.Fatalf("WriteReport yaml: %v", err)
	}
	var got Report
	if err := yaml.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if got.Namespace != "default" {
		t.Errorf("unexpected namespace %q", got.Namespace)
	}
}

func TestWriteReport_TextAndMarkdown(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"", "markdown", "md"} {
		var buf bytes.Buffer
		if err := WriteReport(&buf, format, fullReport()); err != nil {
			t.Fatalf("WriteReport %q: %v", format, err)
		}
		out := buf.String()
		for _, want := range []string{"web", "increase stabilization window", "wider-window"} {
			if !strings.Contains(out, want) {
				t.Errorf("format %q: expected %q in output:\n%s", format, want, out)
			}
		}
	}
}

func TestReplaySLORisk(t *testing.T) {
	t.Parallel()
	if got := replaySLORisk(Summary{MaxReplicasReached: 6}); got != "high" {
		t.Errorf("expected high, got %s", got)
	}
	if got := replaySLORisk(Summary{EstimatedUnderProvision: 1}); got != "medium" {
		t.Errorf("expected medium, got %s", got)
	}
	if got := replaySLORisk(Summary{}); got != "low" {
		t.Errorf("expected low, got %s", got)
	}
}

func TestTruncateReplayColumn(t *testing.T) {
	t.Parallel()
	if got := truncateReplayColumn("short", 10); got != "short" {
		t.Errorf("no-op truncation broken: %q", got)
	}
	if got := truncateReplayColumn("longvaluehere", 8); got != "longv..." {
		t.Errorf("truncation = %q", got)
	}
	if got := truncateReplayColumn("abcdef", 3); got != "abc" {
		t.Errorf("tiny width = %q", got)
	}
}
