package cmd

import (
	"testing"
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestIsKnownOutputFormat(t *testing.T) {
	tests := []struct {
		format string
		want   bool
	}{
		{"", true},
		{"table", true},
		{"wide", true},
		{"ja", true},
		{"json", true},
		{"yaml", true},
		{"markdown", true},
		{"md", true},
		{"html", true},
		{"incident", true},
		{"prometheus", true},
		{"jsonpath={.items}", true},
		{"template=my.tmpl", true},
		{"go-template", true},
		{"bogus", false},
		{"csv", false},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			if got := isKnownOutputFormat(tt.format); got != tt.want {
				t.Fatalf("isKnownOutputFormat(%q) = %v, want %v", tt.format, got, tt.want)
			}
		})
	}
}

func TestMergeRecordedTrace(t *testing.T) {
	t.Run("adopts identity from first source when dst empty", func(t *testing.T) {
		dst := &hpaanalysis.TimelineTrace{}
		src := hpaanalysis.TimelineTrace{
			HPAName:   "web",
			Namespace: "default",
			Interval:  5 * time.Second,
			Start:     time.Unix(1000, 0),
			End:       time.Unix(1100, 0),
			Snapshots: []hpaanalysis.TimelineSnapshot{{HealthScore: 80}},
		}
		mergeRecordedTrace(dst, src)
		if dst.HPAName != "web" || dst.Namespace != "default" {
			t.Fatalf("identity not adopted: name=%s ns=%s", dst.HPAName, dst.Namespace)
		}
		if dst.Interval != 5*time.Second {
			t.Fatalf("interval = %v, want 5s", dst.Interval)
		}
		if !dst.Start.Equal(time.Unix(1000, 0)) {
			t.Fatalf("start = %v, want 1000", dst.Start)
		}
		if len(dst.Snapshots) != 1 {
			t.Fatalf("snapshots len = %d, want 1", len(dst.Snapshots))
		}
	})

	t.Run("does not overwrite identity when dst already set", func(t *testing.T) {
		dst := &hpaanalysis.TimelineTrace{
			HPAName:   "original",
			Namespace: "orig-ns",
			Interval:  10 * time.Second,
			Start:     time.Unix(500, 0),
		}
		src := hpaanalysis.TimelineTrace{
			HPAName:   "other",
			Namespace: "other-ns",
			End:       time.Unix(1200, 0),
			Snapshots: []hpaanalysis.TimelineSnapshot{{HealthScore: 90}},
		}
		mergeRecordedTrace(dst, src)
		// Identity stays from dst.
		if dst.HPAName != "original" || dst.Namespace != "orig-ns" {
			t.Fatalf("identity overwritten: name=%s ns=%s", dst.HPAName, dst.Namespace)
		}
		if dst.Interval != 10*time.Second {
			t.Fatalf("interval = %v, want 10s", dst.Interval)
		}
		// End is always taken from the latest source.
		if !dst.End.Equal(time.Unix(1200, 0)) {
			t.Fatalf("end = %v, want 1200", dst.End)
		}
	})

	t.Run("accumulates snapshots across merges", func(t *testing.T) {
		dst := &hpaanalysis.TimelineTrace{HPAName: "web"}
		mergeRecordedTrace(dst, hpaanalysis.TimelineTrace{
			Snapshots: []hpaanalysis.TimelineSnapshot{{HealthScore: 70}, {HealthScore: 75}},
		})
		mergeRecordedTrace(dst, hpaanalysis.TimelineTrace{
			Snapshots: []hpaanalysis.TimelineSnapshot{{HealthScore: 80}},
		})
		if len(dst.Snapshots) != 3 {
			t.Fatalf("snapshots len = %d, want 3", len(dst.Snapshots))
		}
	})
}
