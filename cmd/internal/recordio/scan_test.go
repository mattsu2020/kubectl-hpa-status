package recordio

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestScanTraces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record.jsonl")
	data := []byte("\n{\"namespace\":\"default\",\"hpaName\":\"web\"}\n{\"namespace\":\"ops\",\"hpaName\":\"api\"}\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	var names []string
	lines, err := ScanTraces(path, func(trace hpaanalysis.TimelineTrace) error {
		names = append(names, trace.Namespace+"/"+trace.HPAName)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if lines != 3 {
		t.Fatalf("lines = %d, want 3", lines)
	}
	if len(names) != 2 || names[0] != "default/web" || names[1] != "ops/api" {
		t.Fatalf("unexpected traces: %v", names)
	}
}

func TestScanTracesClassifiesInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record.jsonl")
	if err := os.WriteFile(path, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := ScanTraces(path, func(_ hpaanalysis.TimelineTrace) error { return nil })
	if !errors.Is(err, ErrInvalidJSONLine) {
		t.Fatalf("error = %v, want ErrInvalidJSONLine", err)
	}
}
