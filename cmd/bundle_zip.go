package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// bundleZipEntry is a single file in the bundle zip archive.
type bundleZipEntry struct {
	Name    string
	Content []byte
}

// writeBundleZip writes all collected data into a zip archive.
func writeBundleZip(data *bundleData, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	zw := zip.NewWriter(file)
	defer func() { _ = zw.Close() }()

	entries := buildBundleZipEntries(data)

	for _, entry := range entries {
		if len(entry.Content) == 0 {
			continue
		}
		w, err := zw.Create(entry.Name)
		if err != nil {
			return fmt.Errorf("creating zip entry %s: %w", entry.Name, err)
		}
		if _, err := w.Write(entry.Content); err != nil {
			return fmt.Errorf("writing zip entry %s: %w", entry.Name, err)
		}
	}

	return nil
}

func buildBundleZipEntries(data *bundleData) []bundleZipEntry {
	// Build the markdown report for inclusion in the zip.
	var mdBuf bytes.Buffer
	writeBundleMarkdown(&mdBuf, data)

	entries := []bundleZipEntry{
		{"report.md", mdBuf.Bytes()},
		{"hpa.yaml", data.HPA},
		{"scale-target.yaml", data.ScaleTarget},
		{"replicasets.yaml", data.ReplicaSets},
		{"pods.yaml", data.Pods},
		{"events.txt", data.Events},
		{"metrics-api.txt", data.MetricsAPI},
	}

	appendJSONEntry(&entries, "analysis.json", data.StatusReport.Analysis)
	if len(data.ContainerStatuses) > 0 {
		appendJSONEntry(&entries, "container-statuses.json", data.ContainerStatuses)
	}
	if len(data.ResourceQuotas) > 0 {
		appendJSONEntry(&entries, "resourcequotas.json", data.ResourceQuotas)
	}
	if len(data.LimitRanges) > 0 {
		appendJSONEntry(&entries, "limitranges.json", data.LimitRanges)
	}
	if len(data.PDBs) > 0 {
		appendJSONEntry(&entries, "pdbs.json", data.PDBs)
	}
	if data.NodeCapacity != nil {
		appendJSONEntry(&entries, "node-capacity.json", data.NodeCapacity)
	}

	// Metadata.
	entries = append(entries, bundleZipEntry{
		Name: "metadata.txt",
		Content: []byte(fmt.Sprintf(
			"HPA: %s/%s\nNamespace: %s\nTimestamp: %s\nFormat: bundle\n",
			data.Namespace, data.HPAName, data.Namespace, data.Timestamp.Format(time.RFC3339),
		)),
	})

	return entries
}

// appendJSONEntry marshals value as pretty JSON and appends it under name when
// marshalling succeeds.
func appendJSONEntry(entries *[]bundleZipEntry, name string, value any) {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return
	}
	*entries = append(*entries, bundleZipEntry{Name: name, Content: payload})
}
