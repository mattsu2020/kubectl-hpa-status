// Loading recorded timeline traces from JSONL or single-JSON files. These
// helpers read the durable record written by the record command and return the
// matching trace, falling back to a whole-file JSON trace when the JSONL parse
// fails. Split from timeline.go so the live command stays focused.
package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func loadRecordedTrace(path, namespace, name string) (*hpaanalysis.TimelineTrace, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read record file: %w", err)
	}
	defer func() { _ = file.Close() }()

	var combined hpaanalysis.TimelineTrace
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var trace hpaanalysis.TimelineTrace
		if err := json.Unmarshal(line, &trace); err != nil {
			return loadRecordedJSONTrace(path, namespace, name)
		}
		if trace.HPAName != name {
			continue
		}
		if namespace != "" && trace.Namespace != namespace {
			continue
		}
		mergeRecordedTrace(&combined, trace)
		if len(combined.Snapshots) > maxSnapshotsPerTrace {
			return nil, snapshotLimitError(path)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan record file: %w", err)
	}
	if len(combined.Snapshots) == 0 {
		if lineNo == 0 {
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
