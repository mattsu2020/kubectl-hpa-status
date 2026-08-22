// Package cmd provides durable recording for the timeline command and writes an HPA's desired-replica
// trace to a JSONL file with atomic publication, symlink protection, and
// per-line sync. Split from timeline.go so the live-polling command stays
// focused on its run loop.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func runRecord(ctx context.Context, out io.Writer, opts *options, name string, interval time.Duration, outputPath string) error {
	if interval < time.Second {
		_, _ = fmt.Fprintf(out, "Warning: interval %s is below 1s; clamping to 1s to reduce API server load.\n", interval)
		interval = time.Second
	}

	client, err := newClientOrDefault(opts)
	if err != nil {
		return err
	}
	ec := newEnrichmentContext(ctx, opts)
	start := opts.CurrentTime()
	initialRecords, err := recordOnce(ctx, opts, client, name, interval, ec)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(initialRecords) == 0 {
		return fmt.Errorf("no HPAs matched the recording scope; record file was not modified")
	}

	file, err := initializeRecordFile(outputPath, initialRecords)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	counts := map[string]int{}
	previous := map[string]hpaanalysis.TimelineSnapshot{}
	interestingChanges := map[string][]string{}
	trackRecordedSnapshots(initialRecords, counts, previous, interestingChanges)
	_, _ = fmt.Fprintf(out, "Recorded %d snapshot(s) at %s\n", len(initialRecords), opts.CurrentTime().Format(time.RFC3339))

	for {
		select {
		case <-ctx.Done():
			return syncAndWriteRecordSummary(file, out, outputPath, counts, interestingChanges, opts.CurrentTime().Sub(start))
		case <-ticker.C:
		}

		records, err := recordOnce(ctx, opts, client, name, interval, ec)
		if err != nil {
			return err
		}
		if err := ensurePublishedRecordFile(file, outputPath); err != nil {
			return err
		}
		for _, record := range records {
			if err := writeRecordLine(file, record); err != nil {
				return err
			}
		}
		if err := file.Sync(); err != nil {
			return fmt.Errorf("failed to sync record file: %w", err)
		}
		trackRecordedSnapshots(records, counts, previous, interestingChanges)
		_, _ = fmt.Fprintf(out, "Recorded %d snapshot(s) at %s\n", len(records), opts.CurrentTime().Format(time.RFC3339))
	}
}

func syncAndWriteRecordSummary(
	file *os.File,
	out io.Writer,
	outputPath string,
	counts map[string]int,
	interestingChanges map[string][]string,
	elapsed time.Duration,
) error {
	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync record file: %w", err)
	}
	return writeRecordSummary(out, outputPath, counts, interestingChanges, elapsed)
}

// initializeRecordFile publishes the first successfully fetched batch
// atomically. Existing files are untouched until the batch has been serialized
// and synced. The published file always uses mode 0600. A final-path symlink
// is rejected so recording never truncates the symlink target.
func initializeRecordFile(outputPath string, records []hpaanalysis.TimelineTrace) (_ *os.File, retErr error) {
	mode := os.FileMode(0o600)
	var originalInfo os.FileInfo
	targetExisted := false
	info, err := os.Lstat(outputPath)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("refusing to replace symlink record file %q", outputPath)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("record file %q is not a regular file", outputPath)
		}
		originalInfo = info
		targetExisted = true
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("failed to inspect record file: %w", err)
	}

	dir := filepath.Dir(outputPath)
	base := filepath.Base(outputPath)
	file, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary record file: %w", err)
	}
	tempPath := file.Name()
	published := false
	defer func() {
		if retErr != nil {
			_ = file.Close()
		}
		if !published {
			_ = os.Remove(tempPath)
		}
	}()

	if err := file.Chmod(mode); err != nil {
		return nil, fmt.Errorf("failed to preserve record file permissions: %w", err)
	}
	for _, record := range records {
		if err := writeRecordLine(file, record); err != nil {
			return nil, err
		}
	}
	if err := file.Sync(); err != nil {
		return nil, fmt.Errorf("failed to sync initial record file: %w", err)
	}
	if err := ensureRecordDestinationUnchanged(outputPath, originalInfo, targetExisted); err != nil {
		return nil, err
	}
	if err := os.Rename(tempPath, outputPath); err != nil {
		return nil, fmt.Errorf("failed to publish record file: %w", err)
	}
	published = true
	if err := syncRecordDirectory(dir); err != nil {
		return nil, fmt.Errorf("record file was published but its directory could not be synced: %w", err)
	}
	return file, nil
}

func ensureRecordDestinationUnchanged(path string, originalInfo os.FileInfo, originallyExisted bool) error {
	currentInfo, err := os.Lstat(path)
	if !originallyExisted {
		switch {
		case os.IsNotExist(err):
			return nil
		case err != nil:
			return fmt.Errorf("failed to recheck record file: %w", err)
		default:
			return fmt.Errorf("record file %q appeared while preparing the recording; refusing to overwrite it", path)
		}
	}
	if err != nil {
		return fmt.Errorf("record file %q changed while preparing the recording: %w", path, err)
	}
	if currentInfo.Mode()&os.ModeSymlink != 0 || !currentInfo.Mode().IsRegular() ||
		!os.SameFile(originalInfo, currentInfo) ||
		currentInfo.Size() != originalInfo.Size() ||
		!currentInfo.ModTime().Equal(originalInfo.ModTime()) ||
		currentInfo.Mode().Perm() != originalInfo.Mode().Perm() {
		return fmt.Errorf("record file %q changed while preparing the recording; refusing to overwrite it", path)
	}
	return nil
}

func ensurePublishedRecordFile(file *os.File, path string) error {
	openInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to inspect open record file: %w", err)
	}
	currentInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("record file %q is no longer available: %w", path, err)
	}
	if currentInfo.Mode()&os.ModeSymlink != 0 || !currentInfo.Mode().IsRegular() || !os.SameFile(openInfo, currentInfo) {
		return fmt.Errorf("record file %q was replaced while recording; refusing to write to a detached file", path)
	}
	return nil
}

func trackRecordedSnapshots(records []hpaanalysis.TimelineTrace, counts map[string]int, previous map[string]hpaanalysis.TimelineSnapshot, interestingChanges map[string][]string) {
	for _, record := range records {
		key := record.Namespace + "/" + record.HPAName
		counts[key]++
		if len(record.Snapshots) == 0 {
			continue
		}
		snapshot := record.Snapshots[0]
		if prev, ok := previous[key]; ok {
			for _, change := range hpaanalysis.DiffSnapshots(prev, snapshot) {
				interestingChanges[key] = append(interestingChanges[key],
					fmt.Sprintf("%s %s", snapshot.Timestamp.Format("15:04"), change))
			}
		}
		previous[key] = snapshot
	}
}

func recordOnce(ctx context.Context, opts *options, client *kube.Client, name string, interval time.Duration, ec *enrichmentContext) ([]hpaanalysis.TimelineTrace, error) {
	if name != "" {
		report, err := buildStatusReport(ctx, opts, client, name, true, ec)
		if err != nil {
			return nil, err
		}
		return []hpaanalysis.TimelineTrace{traceFromReport(report, interval)}, nil
	}

	namespace := client.Namespace
	if opts.AllNamespaces {
		namespace = metav1.NamespaceAll
	}
	var records []hpaanalysis.TimelineTrace
	err := kube.ListHPAsEachPage(ctx, client.Interface, namespace, metav1.ListOptions{LabelSelector: opts.Selector}, opts.ChunkSize, func(page *autoscalingv2.HorizontalPodAutoscalerList) error {
		for i := range page.Items {
			local := copyOptions(opts)
			local.Namespace = page.Items[i].Namespace
			pageClient := &kube.Client{Interface: client.Interface, Namespace: page.Items[i].Namespace}
			report, err := buildStatusReport(ctx, &local, pageClient, page.Items[i].Name, true, ec)
			if err != nil {
				return err
			}
			records = append(records, traceFromReport(report, interval))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list HPAs: %w", err)
	}
	return records, nil
}

func traceFromReport(report hpaanalysis.StatusReport, interval time.Duration) hpaanalysis.TimelineTrace {
	snapshot := hpaanalysis.SnapshotFromReport(report)
	return hpaanalysis.TimelineTrace{
		HPAName:   report.Analysis.Meta.Name,
		Namespace: report.Analysis.Meta.Namespace,
		Start:     snapshot.Timestamp,
		End:       snapshot.Timestamp,
		Interval:  interval,
		Snapshots: []hpaanalysis.TimelineSnapshot{snapshot},
	}
}

func writeRecordLine(w io.Writer, trace hpaanalysis.TimelineTrace) error {
	data, err := json.Marshal(trace)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write record line: %w", err)
	}
	return nil
}

func writeRecordSummary(out io.Writer, path string, counts map[string]int, changes map[string][]string, elapsed time.Duration) error {
	total := 0
	for _, count := range counts {
		total += count
	}
	if _, err := fmt.Fprintf(out, "Recorded %d snapshots for %d HPAs to %s in %s\n", total, len(counts), path, elapsed.Round(time.Second)); err != nil {
		return err
	}
	if len(changes) == 0 {
		_, err := fmt.Fprintln(out, "\nInteresting changes: none")
		return err
	}
	if _, err := fmt.Fprintln(out, "\nInteresting changes:"); err != nil {
		return err
	}
	for key, entries := range changes {
		if len(entries) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(out, "- %s\n", key); err != nil {
			return err
		}
		for _, entry := range entries {
			if _, err := fmt.Fprintf(out, "  %s\n", entry); err != nil {
				return err
			}
		}
	}
	return nil
}
