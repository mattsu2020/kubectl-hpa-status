// Package cmd provides loading of recorded timeline traces from JSONL or single-JSON files. These
// helpers read the durable record written by the record command and return the
// matching trace, falling back to a whole-file JSON trace when the JSONL parse
// fails. Split from timeline.go so the live command stays focused.
package cmd

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/cmd/internal/recordio"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func loadRecordedTrace(path, namespace, name string) (*hpaanalysis.TimelineTrace, error) {
	var combined hpaanalysis.TimelineTrace
	lineCount, err := recordio.ScanTraces(path, func(trace hpaanalysis.TimelineTrace) error {
		if trace.HPAName != name {
			return nil
		}
		if namespace != "" && trace.Namespace != namespace {
			return nil
		}
		mergeRecordedTrace(&combined, trace)
		if len(combined.Snapshots) > maxSnapshotsPerTrace {
			return snapshotLimitError(path)
		}
		return nil
	})
	if errors.Is(err, recordio.ErrInvalidJSONLine) {
		return loadRecordedJSONTrace(path, namespace, name)
	}
	if err != nil {
		return nil, err
	}
	if len(combined.Snapshots) == 0 {
		if lineCount == 0 {
			return loadRecordedJSONTrace(path, namespace, name)
		}
		return nil, noSnapshotsError(namespace, name)
	}
	return &combined, nil
}

func loadRecordedJSONTrace(path, namespace, name string) (*hpaanalysis.TimelineTrace, error) {
	data, err := readFileBounded(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read record file: %w", err)
	}
	var trace hpaanalysis.TimelineTrace
	if err := json.Unmarshal(data, &trace); err != nil {
		return nil, fmt.Errorf("failed to parse record file as JSONL or JSON trace: %w", err)
	}
	if trace.HPAName != name || (namespace != "" && trace.Namespace != namespace) {
		return nil, noSnapshotsError(namespace, name)
	}
	return &trace, nil
}

func mergeRecordedTrace(dst *hpaanalysis.TimelineTrace, src hpaanalysis.TimelineTrace) {
	if dst.HPAName == "" {
		dst.HPAName = src.HPAName
		dst.Namespace = src.Namespace
		dst.Interval = src.Interval
		dst.Start = src.Start
	}
	dst.End = src.End
	dst.Snapshots = append(dst.Snapshots, src.Snapshots...)
}
