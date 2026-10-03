package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestRecordedHPASelectionRejectsAmbiguousNamespaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	var data bytes.Buffer
	for _, ns := range []string{"ns-a", "ns-b", "ns-a"} {
		trace := hpaanalysis.TimelineTrace{Namespace: ns, HPAName: "web", Snapshots: []hpaanalysis.TimelineSnapshot{{Current: 2}}}
		if err := json.NewEncoder(&data).Encode(trace); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRecordedTrace(path, "", "web"); err == nil || !strings.Contains(err.Error(), "--namespace") {
		t.Fatalf("ambiguity was accepted: %v", err)
	}
	if _, err := inferRecordedTraceName(path, ""); err == nil || !strings.Contains(err.Error(), "--namespace") {
		t.Fatalf("ambiguous inference: %v", err)
	}
	selected, err := loadRecordedTrace(path, "ns-a", "web")
	if err != nil || selected.Namespace != "ns-a" || len(selected.Snapshots) != 2 {
		t.Fatalf("selection: %+v, %v", selected, err)
	}
	for _, args := range [][]string{
		{"timeline", "web", "--from-record", path, "-o", "json"},
		{"replay", path, "--hpa", "web", "-o", "json"},
	} {
		cmd := NewRootCommandWithDeps(AppDeps{})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--namespace") {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestCommandOutputTemplatesReachRenderers(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web", testutil.WithMinMax(1, 10), testutil.WithReplicas(2, 2), testutil.WithResourceMetric("cpu", 60, 70))
	peer := hpa.DeepCopy()
	peer.Name = "peer"
	client := testutil.NewFakeClient(hpa, peer)
	for _, tc := range []struct {
		name, path string
		run        func(*bytes.Buffer, *options) error
	}{
		{"simulate", "{.name}", func(out *bytes.Buffer, opts *options) error {
			return runSimulate(context.Background(), out, opts, "web", []string{"cpu=80%"}, nil, "", false, 0)
		}},
		{"fleet", "{.risk}", func(out *bytes.Buffer, opts *options) error {
			return runFleet(context.Background(), out, opts, "max-surge")
		}},
		{"flap", "{.name}", func(out *bytes.Buffer, opts *options) error {
			return runFlapLive(context.Background(), out, opts, "web", time.Hour)
		}},
		{"conflicts", "{.items[0].namespace}", func(out *bytes.Buffer, opts *options) error { return runConflictScan(context.Background(), out, opts) }},
	} {
		for _, format := range []string{"go-template", "jsonpath", "named"} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				opts := defaultRootOptions()
				opts.ClientOverride = client
				opts.Namespace = "default"
				opts.Output = format
				opts.Template = "{{printf \"selected-template\"}}"
				if format == "jsonpath" {
					opts.Template = tc.path
				}
				if format == "named" {
					opts.OutputTemplates = map[string]outputTemplateConfig{"named": {Type: "go-template", Template: "{{printf \"selected-template\"}}"}}
				}
				opts.Normalize()
				var out bytes.Buffer
				if err := tc.run(&out, &opts); err != nil {
					t.Fatal(err)
				}
				got := strings.TrimSpace(out.String())
				if format != "jsonpath" && got != "selected-template" {
					t.Fatalf("template discarded: %q", got)
				}
				if format == "jsonpath" && (got == "" || strings.Contains(got, "\n")) {
					t.Fatalf("JSONPath discarded: %q", got)
				}
			})
		}
	}
}

func TestTemplateFlagsWorkThroughCommandTree(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web", testutil.WithMinMax(1, 10), testutil.WithReplicas(2, 2), testutil.WithResourceMetric("cpu", 60, 70))
	peer := hpa.DeepCopy()
	peer.Name = "peer"
	for _, args := range [][]string{
		{"simulate", "web", "--set-metric", "cpu=80%"},
		{"fleet"},
		{"alpha", "flap", "web"},
		{"scan", "--conflicts"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			command := NewRootCommandWithDeps(AppDeps{Kubernetes: testutil.NewFakeClient(hpa, peer)})
			var out bytes.Buffer
			command.SetOut(&out)
			command.SetErr(&bytes.Buffer{})
			flags := []string{"-n", "default", "--color", "never", "-o", "go-template", "--template", "{{printf \"cli-template\"}}"}
			command.SetArgs(append(append([]string{}, args...), flags...))
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(out.String()); got != "cli-template" {
				t.Fatalf("flag template lost: %q", got)
			}
		})
	}
}
