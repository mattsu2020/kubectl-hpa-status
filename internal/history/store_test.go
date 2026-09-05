package history

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
)

func testKey(cluster, namespace, name string) SnapshotKey {
	return SnapshotKey{Cluster: cluster, Namespace: namespace, Name: name, UID: cluster + "-" + name + "-uid"}
}

func TestHealthStoreAppendAndLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHealthStoreWithDir(dir)
	if err != nil {
		t.Fatalf("NewHealthStoreWithDir() error: %v", err)
	}
	key := testKey("prod", "default", "my-app")

	now := time.Now()
	snapshots := []healthtrend.HealthSnapshot{
		{Timestamp: now.Add(-2 * time.Hour), HealthScore: 100, HealthState: "OK", DesiredReplicas: 5, CurrentReplicas: 5},
		{Timestamp: now.Add(-1 * time.Hour), HealthScore: 80, HealthState: "LIMITED", DesiredReplicas: 8, CurrentReplicas: 6},
		{Timestamp: now, HealthScore: 90, HealthState: "OK", DesiredReplicas: 7, CurrentReplicas: 7},
	}

	// Append all snapshots.
	for _, snap := range snapshots {
		if err := store.Append(key, snap); err != nil {
			t.Fatalf("Append() error: %v", err)
		}
	}

	// Load with 3-hour window — should get all 3.
	loaded, err := store.Load(key, 3*time.Hour)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(loaded) != 3 {
		t.Errorf("Load() returned %d snapshots, want 3", len(loaded))
	}

	// Load with 90-minute window — should get only 2.
	loaded, err = store.Load(key, 90*time.Minute)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("Load() returned %d snapshots, want 2", len(loaded))
	}
}

// TestHealthStoreKeyIsolation locks in the stream separation guarantees: the
// same namespace/name on a different cluster, and a recreated HPA (new UID),
// must never read or overwrite another stream's history.
func TestHealthStoreKeyIsolation(t *testing.T) {
	store, err := NewHealthStoreWithDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	prod := testKey("prod", "default", "web")
	dev := testKey("dev", "default", "web")
	recreated := prod
	recreated.UID = "new-uid"

	if err := store.Append(prod, healthtrend.HealthSnapshot{Timestamp: now, HealthScore: 90, HealthState: "OK"}); err != nil {
		t.Fatal(err)
	}

	for _, other := range []SnapshotKey{dev, recreated} {
		loaded, err := store.Load(other, time.Hour)
		if err != nil {
			t.Fatalf("Load(%s): %v", other, err)
		}
		if len(loaded) != 0 {
			t.Fatalf("Load(%s) returned %d snapshots, want 0: streams must be isolated", other, len(loaded))
		}
	}

	// Appending to the second stream must not touch the first.
	if err := store.Append(dev, healthtrend.HealthSnapshot{Timestamp: now, HealthScore: 40, HealthState: "ERROR"}); err != nil {
		t.Fatal(err)
	}
	prodLoaded, err := store.Load(prod, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(prodLoaded) != 1 || prodLoaded[0].HealthScore != 90 {
		t.Fatalf("prod history was mutated by the dev stream: %#v", prodLoaded)
	}
	if got := len(store.filePath(prod)); got > maxHistoryFilenameLength {
		t.Fatalf("filename length = %d, want <= %d", got, maxHistoryFilenameLength)
	}
}

// countHistoryLines counts the non-empty lines of a history file.
func countHistoryLines(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	lines := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			lines++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

// TestRecordAndLoadAppendOnlyBetweenCompactions verifies that a record only
// rewrites the file when enough history has expired; below the compaction
// thresholds the record is a single append and expired lines linger until a
// later compaction pass collects them.
func TestRecordAndLoadAppendOnlyBetweenCompactions(t *testing.T) {
	store, err := NewHealthStoreWithDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := testKey("prod", "default", "web")
	path := store.filePath(key)
	now := time.Now()

	// Seed 90 fresh lines plus 10 expired ones: expired=10 is below both
	// compaction thresholds relative to total=100.
	for i := 0; i < 90; i++ {
		if err := appendSnapshotLine(path, healthtrend.HealthSnapshot{Timestamp: now.Add(-time.Duration(i+1) * time.Minute), HealthScore: 80}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 10; i++ {
		if err := appendSnapshotLine(path, healthtrend.HealthSnapshot{Timestamp: now.Add(-48*time.Hour - time.Duration(i)*time.Minute), HealthScore: 70}); err != nil {
			t.Fatal(err)
		}
	}

	window, err := store.RecordAndLoad(key, healthtrend.HealthSnapshot{Timestamp: now, HealthScore: 90}, 24*time.Hour, 2*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	// The returned window must exclude the expired lines.
	if len(window) != 91 {
		t.Fatalf("window = %d snapshots, want 91 (90 fresh + 1 new)", len(window))
	}
	if got := countHistoryLines(t, path); got != 101 {
		t.Fatalf("file has %d lines after record, want 101: record must append, not rewrite", got)
	}

	// Now expire the majority: compaction must reclaim the file.
	for i := 0; i < 92; i++ {
		if err := appendSnapshotLine(path, healthtrend.HealthSnapshot{Timestamp: now.Add(-48 * time.Hour), HealthScore: 60}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.RecordAndLoad(key, healthtrend.HealthSnapshot{Timestamp: now, HealthScore: 95}, 24*time.Hour, time.Hour, now); err != nil {
		t.Fatal(err)
	}
	if got := countHistoryLines(t, path); got > 100 {
		t.Fatalf("file has %d lines after compaction-triggering record, want <= 100", got)
	}
	loaded, err := store.Load(key, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) == 0 || loaded[len(loaded)-1].HealthScore != 95 {
		t.Fatalf("latest snapshot missing after compaction: %#v", loaded)
	}
}

func TestShouldCompactThresholds(t *testing.T) {
	tests := []struct {
		name           string
		total, expired int
		want           bool
	}{
		{name: "nothing expired", total: 100, expired: 0, want: false},
		{name: "small minority expired", total: 100, expired: 10, want: false},
		{name: "absolute threshold reached", total: 10000, expired: compactionMinExpired, want: true},
		{name: "half expired", total: 10, expired: 5, want: true},
		{name: "majority expired", total: 10, expired: 9, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldCompact(tt.total, tt.expired); got != tt.want {
				t.Fatalf("shouldCompact(%d, %d) = %v, want %v", tt.total, tt.expired, got, tt.want)
			}
		})
	}
}

func TestHistoryLockWaitHonorsContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	release, err := acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := acquireLockContext(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquireLockContext error = %v, want context.Canceled", err)
	}
}

// childRole names the subprocess modes used by the multi-process regression
// tests. The parent re-invokes the test binary with the same test name plus an
// env var; the child detects the env var and runs its role.
const (
	childEnvRole        = "HPA_HISTORY_CHILD_ROLE"
	roleLockMustWait    = "lock-must-wait"
	roleLockMayAcquire  = "lock-may-acquire"
	roleRecordHammer    = "record-hammer"
	roleLockTargetEnv   = "HPA_HISTORY_LOCK_TARGET"
	roleRecordDirEnv    = "HPA_HISTORY_RECORD_DIR"
	roleRecordCluster   = "hammer-cluster"
	roleRecordPerChild  = 5
	hammerChildTimeout  = 30 * time.Second
	hammerChildWaitSpan = 500 * time.Millisecond
)

// TestHistoryLockProcessLevelExclusion guards the OS-lock design at the
// process boundary. A lock held by a live process must not be stolen even
// when its sidecar file looks long-abandoned (the reclaim race of the old
// O_EXCL+mtime design), and it must become acasurable right after release.
func TestHistoryLockProcessLevelExclusion(t *testing.T) {
	if role := os.Getenv(childEnvRole); role != "" {
		runLockChildRole(t, role)
		return
	}
	path := filepath.Join(t.TempDir(), "history.jsonl")

	release, err := acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	// Forge the exact signal the old design used to detect abandonment.
	stale := time.Now().Add(-10 * lockTimeout)
	if err := os.Chtimes(path+".lock", stale, stale); err != nil {
		t.Fatal(err)
	}

	if code := runLockChild(t, path, roleLockMustWait); code != 0 {
		t.Fatalf("child acquired a lock held by a live process (exit %d): stale mtime must not trigger reclamation", code)
	}

	release()
	if code := runLockChild(t, path, roleLockMayAcquire); code != 0 {
		t.Fatalf("child could not acquire the released lock (exit %d)", code)
	}
}

func runLockChild(t *testing.T, path, role string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestHistoryLockProcessLevelExclusion", "-test.timeout=60s") // #nosec G204 -- test binary re-invocation
	cmd.Env = append(os.Environ(),
		childEnvRole+"="+role,
		roleLockTargetEnv+"="+path,
	)
	out, err := cmd.CombinedOutput()
	t.Logf("child (%s) output:\n%s", role, out)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		t.Fatalf("running child: %v", err)
	}
	return 0
}

func runLockChildRole(t *testing.T, role string) {
	target := os.Getenv(roleLockTargetEnv)
	if target == "" {
		os.Exit(4)
	}
	ctx, cancel := context.WithTimeout(context.Background(), hammerChildWaitSpan)
	defer cancel()
	release, err := acquireLockContext(ctx, target)
	switch role {
	case roleLockMustWait:
		if err == nil {
			release()
			fmt.Println("CHILD-ERROR: acquired lock that must be held")
			os.Exit(3)
		}
		fmt.Println("CHILD-OK: lock held by live process was not stolen")
	case roleLockMayAcquire:
		if err != nil {
			fmt.Printf("CHILD-ERROR: released lock not acquirable: %v\n", err)
			os.Exit(3)
		}
		release()
		fmt.Println("CHILD-OK: acquired released lock")
	default:
		os.Exit(4)
	}
	os.Exit(0)
}

// TestHistoryMultiProcessRecordDoesNotLoseUpdates hammers one history file
// from several OS processes to prove the lock + append design loses no
// records — the failure mode the stale-reclamation race could cause.
func TestHistoryMultiProcessRecordDoesNotLoseUpdates(t *testing.T) {
	if role := os.Getenv(childEnvRole); role == roleRecordHammer {
		runRecordHammerChild(t)
		return
	}
	dir := t.TempDir()
	store, err := NewHealthStoreWithDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := testKey(roleRecordCluster, "default", "web")
	const children = 4
	now := time.Now()

	cmds := make([]*exec.Cmd, children)
	for i := range cmds {
		cmds[i] = exec.Command(os.Args[0], "-test.run=TestHistoryMultiProcessRecordDoesNotLoseUpdates", "-test.timeout=60s") // #nosec G204 -- test binary re-invocation
		cmds[i].Env = append(os.Environ(),
			childEnvRole+"="+roleRecordHammer,
			roleRecordDirEnv+"="+dir,
			"HPA_HISTORY_CHILD_INDEX="+fmt.Sprint(i),
		)
	}
	var wg sync.WaitGroup
	results := make(chan error, children)
	for _, cmd := range cmds {
		wg.Add(1)
		go func(cmd *exec.Cmd) {
			defer wg.Done()
			_, err := cmd.CombinedOutput()
			results <- err
		}(cmd)
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("record child failed: %v", err)
		}
	}

	loaded, err := store.LoadAt(key, 24*time.Hour, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if want := children * roleRecordPerChild; len(loaded) != want {
		t.Fatalf("loaded %d snapshots after %d children x %d records, want %d (lost updates?)", len(loaded), children, roleRecordPerChild, want)
	}
}

func runRecordHammerChild(t *testing.T) {
	dir := os.Getenv(roleRecordDirEnv)
	if dir == "" {
		os.Exit(4)
	}
	store, err := NewHealthStoreWithDir(dir)
	if err != nil {
		fmt.Printf("CHILD-ERROR: %v\n", err)
		os.Exit(3)
	}
	key := testKey(roleRecordCluster, "default", "web")
	base := time.Now().Add(-time.Duration(os.Getpid()%1000) * time.Second)
	for i := 0; i < roleRecordPerChild; i++ {
		snapshot := healthtrend.HealthSnapshot{
			Timestamp:   base.Add(time.Duration(i) * time.Millisecond),
			HealthScore: 80,
			HealthState: "OK",
		}
		if _, err := store.RecordAndLoad(key, snapshot, 24*time.Hour, time.Hour, time.Now()); err != nil {
			fmt.Printf("CHILD-ERROR: %v\n", err)
			os.Exit(3)
		}
	}
	os.Exit(0)
}

func TestHealthStoreLongNamesAreBoundedAndCollisionSafe(t *testing.T) {
	store, err := NewHealthStoreWithDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prefix := strings.Repeat("a", maxFilenameSegmentLength)
	first := store.filePath(SnapshotKey{Namespace: "default", Name: prefix + "-first"})
	second := store.filePath(SnapshotKey{Namespace: "default", Name: prefix + "-second"})
	if first == second {
		t.Fatalf("long names collided: %s", first)
	}
	if got := len(filepath.Base(first)); got > maxHistoryFilenameLength {
		t.Fatalf("filename length = %d, want <= %d", got, maxHistoryFilenameLength)
	}
}

func TestHealthStorePermissionsSortingAndCorruption(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "history")
	store, err := NewHealthStoreWithDir(dir)
	if err != nil {
		t.Fatalf("NewHealthStoreWithDir: %v", err)
	}
	if info, statErr := os.Stat(dir); statErr != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("history directory permissions: info=%v err=%v", info, statErr)
	}

	key := testKey("prod", "default", "app")
	now := time.Now()
	for _, snap := range []healthtrend.HealthSnapshot{
		{Timestamp: now, HealthScore: 90},
		{Timestamp: now.Add(-time.Hour), HealthScore: 80},
	} {
		if err := store.Append(key, snap); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	path := store.filePath(key)
	if info, statErr := os.Stat(path); statErr != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("history file permissions: info=%v err=%v", info, statErr)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open corrupt fixture: %v", err)
	}
	if _, err := f.WriteString("not-json\n"); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close corrupt fixture: %v", err)
	}

	loaded, loadErr := store.Load(key, 2*time.Hour)
	var corrupt *CorruptLinesError
	if !errors.As(loadErr, &corrupt) {
		t.Fatalf("Load error = %v, want CorruptLinesError", loadErr)
	}
	if len(loaded) != 2 || !loaded[0].Timestamp.Before(loaded[1].Timestamp) {
		t.Fatalf("valid snapshots were not returned sorted: %#v", loaded)
	}
}

func TestHealthStoreConcurrentAppend(t *testing.T) {
	store, err := NewHealthStoreWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewHealthStoreWithDir: %v", err)
	}
	key := testKey("prod", "default", "concurrent")
	const count = 20
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(score int) {
			defer wg.Done()
			errs <- store.Append(key, healthtrend.HealthSnapshot{
				Timestamp:   time.Now().Add(time.Duration(score) * time.Millisecond),
				HealthScore: score,
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Append: %v", err)
		}
	}
	loaded, err := store.Load(key, time.Hour)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != count {
		t.Fatalf("loaded %d snapshots, want %d", len(loaded), count)
	}
}

func TestHealthStoreLoadNonExistent(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHealthStoreWithDir(dir)
	if err != nil {
		t.Fatalf("NewHealthStoreWithDir() error: %v", err)
	}

	loaded, err := store.Load(SnapshotKey{Namespace: "default", Name: "nonexistent"}, 24*time.Hour)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("Load() returned %d snapshots for nonexistent HPA, want 0", len(loaded))
	}
}

func TestHealthStorePrune(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHealthStoreWithDir(dir)
	if err != nil {
		t.Fatalf("NewHealthStoreWithDir() error: %v", err)
	}
	key := testKey("prod", "default", "my-app")

	now := time.Now()
	old := healthtrend.HealthSnapshot{Timestamp: now.Add(-48 * time.Hour), HealthScore: 50, HealthState: "ERROR"}
	recent := healthtrend.HealthSnapshot{Timestamp: now.Add(-1 * time.Hour), HealthScore: 100, HealthState: "OK"}

	if err := store.Append(key, old); err != nil {
		t.Fatalf("Append() error: %v", err)
	}
	if err := store.Append(key, recent); err != nil {
		t.Fatalf("Append() error: %v", err)
	}

	// Prune entries older than 24 hours.
	if err := store.Prune(key, 24*time.Hour); err != nil {
		t.Fatalf("Prune() error: %v", err)
	}

	loaded, err := store.Load(key, 72*time.Hour)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(loaded) != 1 {
		t.Errorf("after Prune(), Load() returned %d snapshots, want 1", len(loaded))
	}
	if loaded[0].HealthScore != 100 {
		t.Errorf("remaining snapshot HealthScore = %d, want 100", loaded[0].HealthScore)
	}
}

func TestHealthStoreEmptyNamespaceRejected(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHealthStoreWithDir(dir)
	if err != nil {
		t.Fatalf("NewHealthStoreWithDir() error: %v", err)
	}

	snap := healthtrend.HealthSnapshot{Timestamp: time.Now(), HealthScore: 100, HealthState: "OK"}
	if err := store.Append(SnapshotKey{Namespace: "", Name: "my-app"}, snap); err == nil {
		t.Error("expected error for empty namespace")
	}
	if err := store.Append(SnapshotKey{Namespace: "default", Name: ""}, snap); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestHealthStoreLoadMultiple(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHealthStoreWithDir(dir)
	if err != nil {
		t.Fatalf("NewHealthStoreWithDir() error: %v", err)
	}

	now := time.Now()
	appA := testKey("prod", "default", "app-a")
	appB := testKey("prod", "default", "app-b")
	_ = store.Append(appA, healthtrend.HealthSnapshot{Timestamp: now, HealthScore: 90, HealthState: "OK"})
	_ = store.Append(appB, healthtrend.HealthSnapshot{Timestamp: now, HealthScore: 80, HealthState: "OK"})

	keys := []SnapshotKey{
		appA,
		appB,
		testKey("prod", "default", "app-c"), // nonexistent
	}

	result, err := store.LoadMultiple(keys, 1*time.Hour)
	if err != nil {
		t.Fatalf("LoadMultiple() error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("LoadMultiple() returned %d entries, want 2", len(result))
	}
	if _, ok := result[appA.String()]; !ok {
		t.Errorf("LoadMultiple() result missing key %q: %v", appA, result)
	}
}

func TestSanitizeFilename(t *testing.T) {
	long := strings.Repeat("a", maxFilenameSegmentLength+50)
	tests := []struct {
		input string
		want  string
	}{
		{input: "default", want: "default"},
		{input: "kube-system", want: "kube-system"},
		{input: "../../../etc/passwd", want: "_.._.._.._etc_passwd"},
		{input: "", want: "_"},
		{input: ".", want: "_."},
		{input: "..", want: "_.."},
		{input: "a\x00b\x1fc", want: "a_b_c"},
		{input: long, want: strings.Repeat("a", maxFilenameSegmentLength)},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
