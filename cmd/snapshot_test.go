package cmd

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
)

// These tests complement the snapshot smoke coverage in
// diagnostic_commands_smoke_test.go: they pin the archive entry set and the
// default output-path naming contract.

func TestRunSnapshotZipEntries(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 90),
		testutil.WithScaleTargetRef("Deployment", "web"),
	)
	deployment := testutil.BuildDeployment("default", "web",
		testutil.WithSelector(map[string]string{"app": "web"}),
	)
	fakeClient := testutil.NewFakeClientWithObjects(hpa, deployment)

	outPath := filepath.Join(t.TempDir(), "snapshot.zip")
	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				Namespace:      "default",
				ClientOverride: fakeClient,
			},
			OutputOptions: OutputOptions{
				Color: "never",
			},
		},
	}

	if err := runSnapshot(context.Background(), &buf, opts, "web", outPath, true); err != nil {
		t.Fatalf("runSnapshot returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "Snapshot saved to "+outPath) {
		t.Fatalf("expected save confirmation, got: %q", buf.String())
	}

	reader, err := zip.OpenReader(outPath)
	if err != nil {
		t.Fatalf("failed to open snapshot zip: %v", err)
	}
	defer func() { _ = reader.Close() }()

	entries := map[string]string{}
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("failed to open zip entry %s: %v", file.Name, err)
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("failed to read zip entry %s: %v", file.Name, err)
		}
		entries[file.Name] = string(content)
	}

	for _, name := range []string{"hpa.yaml", "deployment.yaml", "analysis.json", "report.md", "metadata.txt"} {
		if _, ok := entries[name]; !ok {
			t.Fatalf("snapshot zip missing entry %s; entries: %v", name, entries)
		}
	}
	if !strings.Contains(entries["hpa.yaml"], "name: web") {
		t.Fatalf("hpa.yaml entry does not describe the HPA:\n%s", entries["hpa.yaml"])
	}
	if !strings.Contains(entries["metadata.txt"], "HPA: default/web") {
		t.Fatalf("metadata.txt entry missing HPA identity:\n%s", entries["metadata.txt"])
	}
}

func TestRunSnapshotDefaultPathUsesNameAndTimestamp(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 90),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	// Run from a scratch directory so the default-named zip lands in an
	// isolated place and can be cleaned up reliably.
	workDir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("failed to enter temp dir: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				Namespace:      "default",
				ClientOverride: fakeClient,
			},
			OutputOptions: OutputOptions{
				Color: "never",
			},
		},
	}

	if err := runSnapshot(context.Background(), &buf, opts, "web", "", true); err != nil {
		t.Fatalf("runSnapshot returned error: %v", err)
	}
	if !strings.HasPrefix(buf.String(), "Snapshot saved to hpa-snapshot-web-") {
		t.Fatalf("expected default snapshot path prefix, got: %q", buf.String())
	}
	matches, err := filepath.Glob(filepath.Join(workDir, "hpa-snapshot-web-*.zip"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected exactly one default-named snapshot zip, matches=%v err=%v", matches, err)
	}
}
