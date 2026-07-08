package bundle

import (
	"archive/zip"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func fullZipFixture() *Data {
	return &Data{
		Namespace:   "production",
		HPAName:     "web",
		Timestamp:   time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
		HPA:         []byte("# hpa"),
		ScaleTarget: []byte("# target"),
		ReplicaSets: []byte("# rs"),
		Pods:        []byte("# pods"),
		Events:      []byte("events"),
		MetricsAPI:  []byte("metrics"),
		ContainerStatuses: []kube.ContainerStatusDetail{
			{Pod: "web-1", Container: "app", Waiting: true, WaitingReason: "CrashLoopBackOff", RestartCount: 3},
		},
		ResourceQuotas: []kube.QuotaInfo{{Name: "quota", Resource: "cpu", Used: "1", Hard: "2", Ratio: 0.5}},
		LimitRanges:    []kube.LimitRangeInfo{{Name: "lr", Type: "Container", Resource: "cpu", Min: "10m", Max: "2"}},
		PDBs:           []kube.PDBInfo{{Name: "pdb", MinAvailable: "1"}},
		NodeCapacity:   &kube.NodeCapacityInfo{TotalNodes: 3, TaintedNodes: 1},
		StatusReport:   hpaanalysis.StatusReport{},
	}
}

func TestWriteZip_AllEntries(t *testing.T) {
	t.Parallel()
	out := filepath.Join(t.TempDir(), "bundle.zip")
	if err := WriteZip(fullZipFixture(), out); err != nil {
		t.Fatalf("WriteZip: %v", err)
	}

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("opening zip: %v", err)
	}
	defer func() { _ = zr.Close() }()

	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	want := []string{
		"report.md", "hpa.yaml", "scale-target.yaml", "replicasets.yaml",
		"pods.yaml", "events.txt", "metrics-api.txt", "analysis.json",
		"container-statuses.json", "resourcequotas.json", "limitranges.json",
		"pdbs.json", "node-capacity.json", "metadata.txt",
	}
	for _, name := range want {
		if !names[name] {
			t.Errorf("expected zip entry %q, got entries %v", name, names)
		}
	}
}

func TestWriteZip_SkipsEmptySections(t *testing.T) {
	t.Parallel()
	data := &Data{Namespace: "default", HPAName: "web", Timestamp: time.Now()}
	out := filepath.Join(t.TempDir(), "minimal.zip")
	if err := WriteZip(data, out); err != nil {
		t.Fatalf("WriteZip: %v", err)
	}

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("opening zip: %v", err)
	}
	defer func() { _ = zr.Close() }()

	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	// Optional sections with no data must be omitted, not written empty.
	for _, absent := range []string{"hpa.yaml", "container-statuses.json", "pdbs.json", "node-capacity.json"} {
		if names[absent] {
			t.Errorf("expected entry %q to be omitted for empty data", absent)
		}
	}
	// The report and metadata are always present.
	for _, present := range []string{"report.md", "metadata.txt", "analysis.json"} {
		if !names[present] {
			t.Errorf("expected entry %q even for minimal data", present)
		}
	}
}

func TestWriteZip_CreateError(t *testing.T) {
	t.Parallel()
	err := WriteZip(fullZipFixture(), filepath.Join(t.TempDir(), "missing-dir", "bundle.zip"))
	if err == nil {
		t.Fatal("expected error for uncreatable output path, got nil")
	}
}
