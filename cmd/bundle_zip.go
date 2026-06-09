package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// writeBundleZip writes all collected data into a zip archive.
func writeBundleZip(data *bundleData, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	zw := zip.NewWriter(file)
	defer func() { _ = zw.Close() }()

	// Build the markdown report for inclusion in the zip.
	var mdBuf bytes.Buffer
	writeBundleMarkdown(&mdBuf, data)

	entries := []struct {
		Name    string
		Content []byte
	}{
		{"report.md", mdBuf.Bytes()},
		{"hpa.yaml", data.HPA},
		{"scale-target.yaml", data.ScaleTarget},
		{"replicasets.yaml", data.ReplicaSets},
		{"pods.yaml", data.Pods},
		{"events.txt", data.Events},
		{"metrics-api.txt", data.MetricsAPI},
	}

	// Full analysis JSON.
	analysisJSON, err := json.MarshalIndent(data.StatusReport.Analysis, "", "  ")
	if err == nil {
		entries = append(entries, struct {
			Name    string
			Content []byte
		}{"analysis.json", analysisJSON})
	}

	// Container statuses JSON.
	if len(data.ContainerStatuses) > 0 {
		csJSON, err := json.MarshalIndent(data.ContainerStatuses, "", "  ")
		if err == nil {
			entries = append(entries, struct {
				Name    string
				Content []byte
			}{"container-statuses.json", csJSON})
		}
	}

	// ResourceQuotas JSON.
	if len(data.ResourceQuotas) > 0 {
		qJSON, err := json.MarshalIndent(data.ResourceQuotas, "", "  ")
		if err == nil {
			entries = append(entries, struct {
				Name    string
				Content []byte
			}{"resourcequotas.json", qJSON})
		}
	}

	// LimitRanges JSON.
	if len(data.LimitRanges) > 0 {
		lrJSON, err := json.MarshalIndent(data.LimitRanges, "", "  ")
		if err == nil {
			entries = append(entries, struct {
				Name    string
				Content []byte
			}{"limitranges.json", lrJSON})
		}
	}

	// PDBs JSON.
	if len(data.PDBs) > 0 {
		pdbJSON, err := json.MarshalIndent(data.PDBs, "", "  ")
		if err == nil {
			entries = append(entries, struct {
				Name    string
				Content []byte
			}{"pdbs.json", pdbJSON})
		}
	}

	// Node capacity JSON.
	if data.NodeCapacity != nil {
		ncJSON, err := json.MarshalIndent(data.NodeCapacity, "", "  ")
		if err == nil {
			entries = append(entries, struct {
				Name    string
				Content []byte
			}{"node-capacity.json", ncJSON})
		}
	}

	// Metadata.
	entries = append(entries, struct {
		Name    string
		Content []byte
	}{"metadata.txt", []byte(fmt.Sprintf(
		"HPA: %s/%s\nNamespace: %s\nTimestamp: %s\nFormat: bundle\n",
		data.Namespace, data.HPAName, data.Namespace, data.Timestamp.Format(time.RFC3339),
	))})

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
