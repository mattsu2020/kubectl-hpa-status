package cmd

import (
	"fmt"
	"io"
	"slices"
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// Bound change history per recorded HPA independently of recording duration.
// The JSONL output retains every snapshot; only the terminal summary is capped.
const recordSummaryChangeLimit = 100

type recordSummary struct {
	counts   map[string]int
	previous map[string]hpaanalysis.TimelineSnapshot
	changes  map[string][]string
	omitted  map[string]int
}

func newRecordSummary() *recordSummary {
	return &recordSummary{
		counts:   make(map[string]int),
		previous: make(map[string]hpaanalysis.TimelineSnapshot),
		changes:  make(map[string][]string),
		omitted:  make(map[string]int),
	}
}

func (s *recordSummary) track(records []hpaanalysis.TimelineTrace) {
	active := make(map[string]bool, len(records))
	for _, record := range records {
		key := record.Namespace + "/" + record.HPAName
		active[key] = true
		s.counts[key]++
		if len(record.Snapshots) == 0 {
			continue
		}
		snapshot := record.Snapshots[0]
		if prev, ok := s.previous[key]; ok {
			for _, change := range hpaanalysis.DiffSnapshots(prev, snapshot) {
				s.addChange(key, fmt.Sprintf("%s %s", snapshot.Timestamp.Format("15:04"), change))
			}
		}
		s.previous[key] = snapshot
	}
	// Forget inactive snapshots so absent/re-created HPAs do not retain stale
	// comparison data. Counters and bounded changes remain for the final summary.
	for key := range s.previous {
		if !active[key] {
			delete(s.previous, key)
		}
	}
}

func (s *recordSummary) addChange(key, change string) {
	entries := s.changes[key]
	if len(entries) == recordSummaryChangeLimit {
		copy(entries, entries[1:])
		entries[len(entries)-1] = change
		s.omitted[key]++
	} else {
		entries = append(entries, change)
	}
	s.changes[key] = entries
}

func (s *recordSummary) write(out io.Writer, path string, elapsed time.Duration) error {
	total := 0
	for _, count := range s.counts {
		total += count
	}
	if _, err := fmt.Fprintf(out, "Recorded %d snapshots for %d HPAs to %s in %s\n", total, len(s.counts), path, elapsed.Round(time.Second)); err != nil {
		return err
	}
	if len(s.changes) == 0 {
		_, err := fmt.Fprintln(out, "\nInteresting changes: none")
		return err
	}
	if _, err := fmt.Fprintln(out, "\nInteresting changes:"); err != nil {
		return err
	}
	keys := make([]string, 0, len(s.changes))
	for key := range s.changes {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if _, err := fmt.Fprintf(out, "- %s\n", key); err != nil {
			return err
		}
		if s.omitted[key] > 0 {
			if _, err := fmt.Fprintf(out, "  %d earlier change(s) omitted from summary; all snapshots are in %s\n", s.omitted[key], path); err != nil {
				return err
			}
		}
		for _, entry := range s.changes[key] {
			if _, err := fmt.Fprintf(out, "  %s\n", entry); err != nil {
				return err
			}
		}
	}
	return nil
}
