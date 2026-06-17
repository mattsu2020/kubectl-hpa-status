package cmd

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
)

func TestNewBundleCommand(t *testing.T) {
	opts := &options{}
	cmd := newBundleCommand(opts)

	if cmd.Use != "bundle NAME" {
		t.Fatalf("unexpected Use: %q", cmd.Use)
	}
	if len(cmd.Aliases) != 1 || cmd.Aliases[0] != "collect" {
		t.Fatalf("expected alias 'collect', got: %v", cmd.Aliases)
	}
	if !strings.Contains(cmd.Short, "Bundle all HPA investigation data") {
		t.Fatalf("unexpected Short: %q", cmd.Short)
	}

	// Verify flags.
	format, _ := cmd.Flags().GetString("format")
	if format != "markdown" {
		t.Fatalf("expected default format 'markdown', got %q", format)
	}
	redact, _ := cmd.Flags().GetBool("redact")
	if redact {
		t.Fatal("expected default redact to be false")
	}
}

func TestBundleMarkdownOutput(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		ClientOverride: fakeClient,
		Namespace:      "default",
		Events:         EventOption{Enabled: false},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "test-bundle.md")

	err := runBundle(context.Background(), &buf, opts, "web", "markdown", outputPath, false)
	if err != nil {
		t.Fatalf("runBundle returned error: %v", err)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	// Verify key sections are present.
	sections := []string{
		"# HPA Investigation Bundle",
		"## HPA Status Summary",
		"## HPA Resource (YAML)",
		"## Scale Target Resource",
		"## Pod Status",
		"## Container Status",
		"## Resource Requests/Limits",
		"## Events",
		"## Metrics API Status",
		"## Metrics Diagnostics",
		"## Capacity Context",
		"## Scale Path",
		"## Blocker Analysis",
		"## ResourceQuotas",
		"## LimitRanges",
		"## PodDisruptionBudgets",
		"## Node Capacity",
		"## Recommendations",
		"## Full Analysis Report",
		"## Table of Contents",
	}
	for _, section := range sections {
		if !strings.Contains(string(content), section) {
			t.Errorf("expected bundle to contain %q", section)
		}
	}

	// Verify metadata header.
	if !strings.Contains(string(content), "**Plugin:** kubectl-hpa-status bundle") {
		t.Error("expected metadata header in output")
	}
}

func TestBundleZipOutput(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		ClientOverride: fakeClient,
		Namespace:      "default",
		Events:         EventOption{Enabled: false},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "test-bundle.zip")

	err := runBundle(context.Background(), &buf, opts, "web", "zip", outputPath, false)
	if err != nil {
		t.Fatalf("runBundle returned error: %v", err)
	}

	// Verify zip file can be opened and contains expected files.
	reader, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatalf("failed to open zip: %v", err)
	}
	defer func() { _ = reader.Close() }()

	expectedFiles := map[string]bool{
		"report.md":     false,
		"hpa.yaml":      false,
		"metadata.txt":  false,
		"analysis.json": false,
	}
	for _, f := range reader.File {
		if _, ok := expectedFiles[f.Name]; ok {
			expectedFiles[f.Name] = true
		}
	}
	for name, found := range expectedFiles {
		if !found {
			t.Errorf("expected zip to contain %s", name)
		}
	}

	// Verify analysis.json is valid JSON.
	for _, f := range reader.File {
		if f.Name == "analysis.json" {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("failed to open analysis.json: %v", err)
			}
			data, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				t.Fatalf("failed to read analysis.json: %v", err)
			}
			var parsed map[string]interface{}
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Errorf("analysis.json is not valid JSON: %v", err)
			}
		}
	}
}

func TestBundleRedact(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		ClientOverride: fakeClient,
		Namespace:      "default",
		Events:         EventOption{Enabled: false},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "test-bundle-redact.md")

	err := runBundle(context.Background(), &buf, opts, "web", "markdown", outputPath, true)
	if err != nil {
		t.Fatalf("runBundle with redact returned error: %v", err)
	}

	// The redaction should have been applied to byte-slice fields.
	// We can't easily assert redaction of specific content without injecting
	// known sensitive data, but we verify the file was created successfully.
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected non-empty redacted output")
	}

	// Verify sections are still present after redaction.
	if !strings.Contains(string(content), "## HPA Status Summary") {
		t.Error("expected sections to remain after redaction")
	}
}

func TestBundleDefaultFormat(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		ClientOverride: fakeClient,
		Namespace:      "default",
		Events:         EventOption{Enabled: false},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "test-default.md")

	// Use "md" as format to verify alias.
	err := runBundle(context.Background(), &buf, opts, "web", "md", outputPath, false)
	if err != nil {
		t.Fatalf("runBundle with format=md returned error: %v", err)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected non-empty markdown output")
	}
}

func TestBundleUnsupportedFormat(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		ClientOverride: fakeClient,
		Namespace:      "default",
		Events:         EventOption{Enabled: false},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "test-bundle.csv")

	err := runBundle(context.Background(), &buf, opts, "web", "csv", outputPath, false)
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBundleIncludesDoctorAnalysis(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		ClientOverride: fakeClient,
		Namespace:      "default",
		Events:         EventOption{Enabled: false},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "test-doctor.md")

	err := runBundle(context.Background(), &buf, opts, "web", "markdown", outputPath, false)
	if err != nil {
		t.Fatalf("runBundle returned error: %v", err)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	// Doctor-level analysis should be present in the JSON details section.
	// The full analysis report should include metrics diagnostics.
	if !strings.Contains(string(content), "metricsDiagnostics") {
		t.Error("expected MetricsDiagnostics in full analysis JSON")
	}
}

func TestBundleDefaultOutputPath(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		ClientOverride: fakeClient,
		Namespace:      "default",
		Events:         EventOption{Enabled: false},
	}

	// Change to temp dir so default output file is created there.
	originalDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(originalDir) }()

	err := runBundle(context.Background(), &buf, opts, "web", "markdown", "", false)
	if err != nil {
		t.Fatalf("runBundle returned error: %v", err)
	}

	// Verify the default output path message.
	if !strings.Contains(buf.String(), "hpa-bundle-web-") {
		t.Errorf("expected default output path in stdout, got: %s", buf.String())
	}
	if !strings.Contains(buf.String(), ".md") {
		t.Errorf("expected .md extension in default path, got: %s", buf.String())
	}
}

func TestBundleHPANotFound(t *testing.T) {
	fakeClient := testutil.NewFakeClient() // No HPAs.

	var buf bytes.Buffer
	opts := &options{
		ClientOverride: fakeClient,
		Namespace:      "default",
		Events:         EventOption{Enabled: false},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "test-notfound.md")

	err := runBundle(context.Background(), &buf, opts, "nonexistent", "markdown", outputPath, false)
	if err == nil {
		t.Fatal("expected error for missing HPA")
	}
	if !strings.Contains(err.Error(), "getting HPA") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRootHelpIncludesBundleCommand(t *testing.T) {
	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("root help returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "bundle") {
		t.Fatalf("expected root help to include bundle command, got:\n%s", buf.String())
	}
}
