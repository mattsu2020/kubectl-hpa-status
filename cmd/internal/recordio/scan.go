// Package recordio provides the shared streaming reader for timeline record
// files. It deliberately owns only JSONL decoding; command-specific filtering,
// merging, limits, and legacy single-JSON fallback stay with their callers.
package recordio

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

const maxJSONLineSize = 10 * 1024 * 1024

// ErrInvalidJSONLine lets legacy readers distinguish a JSONL decode failure
// from open, scan, and visitor errors before trying the single-JSON format.
var ErrInvalidJSONLine = errors.New("invalid JSONL record")

type invalidJSONLineError struct{ cause error }

func (e invalidJSONLineError) Error() string { return e.cause.Error() }
func (e invalidJSONLineError) Unwrap() error { return ErrInvalidJSONLine }

// ScanTraces streams non-empty JSONL records from path into visit. The returned
// line count includes empty lines, matching bufio.Scanner's input line count.
func ScanTraces(path string, visit func(hpaanalysis.TimelineTrace) error) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("failed to read record file: %w", err)
	}
	defer func() { _ = file.Close() }()

	lineCount := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxJSONLineSize)
	for scanner.Scan() {
		lineCount++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var trace hpaanalysis.TimelineTrace
		if err := json.Unmarshal(line, &trace); err != nil {
			return lineCount, fmt.Errorf("failed to parse JSONL record: %w", invalidJSONLineError{cause: err})
		}
		if err := visit(trace); err != nil {
			return lineCount, err
		}
	}
	if err := scanner.Err(); err != nil {
		return lineCount, fmt.Errorf("failed to scan record file: %w", err)
	}
	return lineCount, nil
}
