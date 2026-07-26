package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestRunRecord_InitialFetchFailureLeavesExistingFileUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	const original = "existing history\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := &options{
		Common: commonOptions{ConnectionOptions: ConnectionOptions{
			ClientOverride: testutil.NewFakeClient(),
		}},
	}

	err := runRecord(context.Background(), &bytes.Buffer{}, opts, "missing", time.Second, path)

	if err == nil {
		t.Fatal("expected initial HPA fetch failure")
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != original {
		t.Fatalf("existing record changed after initial fetch failure: %q", data)
	}
}

func TestRunRecord_ConnectionFailureLeavesExistingFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")
	const original = "existing history\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := &options{Common: commonOptions{ConnectionOptions: ConnectionOptions{
		Kubeconfig: filepath.Join(dir, "does-not-exist"),
	}}}

	err := runRecord(context.Background(), &bytes.Buffer{}, opts, "web", time.Second, path)

	if err == nil {
		t.Fatal("expected client creation failure")
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != original {
		t.Fatalf("existing record changed after connection failure: %q", data)
	}
}

func TestRunRecord_EmptyInitialResultLeavesExistingFileUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	const original = "existing history\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := &options{
		Common: commonOptions{ConnectionOptions: ConnectionOptions{
			ClientOverride: testutil.NewFakeClient(),
		}},
	}

	err := runRecord(context.Background(), &bytes.Buffer{}, opts, "", time.Second, path)

	if err == nil || !strings.Contains(err.Error(), "no HPAs matched") {
		t.Fatalf("expected empty-scope error, got %v", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != original {
		t.Fatalf("existing record changed after an empty initial result: %q", data)
	}
}

func TestRunRecord_CanceledInitialCollectionLeavesExistingFileUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	const original = "existing history\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	hpa := testutil.BuildHPA("default", "web")
	opts := &options{
		Common: commonOptions{ConnectionOptions: ConnectionOptions{
			ClientOverride: testutil.NewFakeClient(hpa),
		}},
		Status: statusOptions{Events: EventOption{Enabled: false}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runRecord(ctx, &bytes.Buffer{}, opts, "web", time.Second, path)

	if err == nil {
		t.Fatal("expected cancellation")
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != original {
		t.Fatalf("existing record changed after cancellation: %q", data)
	}
}

func TestInitializeRecordFile_AtomicallyReplacesAndNormalizesPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	trace := hpaanalysis.TimelineTrace{
		Namespace: "default",
		HPAName:   "web",
		Snapshots: []hpaanalysis.TimelineSnapshot{{Timestamp: time.Now(), Current: 2, Desired: 3}},
	}

	file, err := initializeRecordFile(path, []hpaanalysis.TimelineTrace{trace})
	if err != nil {
		t.Fatalf("initializeRecordFile: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("permissions = %o, want 600", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "old") || !strings.Contains(string(data), `"hpaName":"web"`) {
		t.Fatalf("unexpected published contents: %q", data)
	}
}

func TestInitializeRecordFile_RestoresOwnerReadWritePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	if err := os.WriteFile(path, []byte("old\n"), 0o400); err != nil {
		t.Fatal(err)
	}

	file, err := initializeRecordFile(path, []hpaanalysis.TimelineTrace{{HPAName: "web"}})
	if err != nil {
		t.Fatalf("initializeRecordFile: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("permissions = %o, want 600 even when the old file was read-only", got)
	}
}

func TestInitializeRecordFile_RejectsSymlinkWithoutChangingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.jsonl")
	link := filepath.Join(dir, "history.jsonl")
	const original = "do not overwrite\n"
	if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	file, err := initializeRecordFile(link, []hpaanalysis.TimelineTrace{{HPAName: "web"}})

	if file != nil {
		_ = file.Close()
		t.Fatal("expected no file for a symlink destination")
	}
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
	data, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != original {
		t.Fatalf("symlink target changed: %q", data)
	}
	info, lstatErr := os.Lstat(link)
	if lstatErr != nil {
		t.Fatal(lstatErr)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("record symlink should remain in place")
	}
}

func TestEnsurePublishedRecordFile_DetectsReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")
	file, err := initializeRecordFile(path, []hpaanalysis.TimelineTrace{{HPAName: "web"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	replacement := filepath.Join(dir, "replacement")
	if err := os.WriteFile(replacement, []byte("replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}

	err = ensurePublishedRecordFile(file, path)
	if err == nil || !strings.Contains(err.Error(), "was replaced") {
		t.Fatalf("expected detached-file protection, got %v", err)
	}
}

func TestEnsureRecordDestinationUnchanged_DetectsNewCompetitor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	if err := os.WriteFile(path, []byte("competitor\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := ensureRecordDestinationUnchanged(path, nil, false)

	if err == nil || !strings.Contains(err.Error(), "appeared") {
		t.Fatalf("expected competing destination detection, got %v", err)
	}
}
