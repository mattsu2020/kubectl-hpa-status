package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// bundleZipEntry is a single file in the bundle zip archive.
type bundleZipEntry struct {
	Name    string
	Content []byte
}

// WriteZip writes all collected data into a zip archive.
func WriteZip(data *Data, outputPath string) error {
	entries, err := buildBundleZipEntries(data)
	if err != nil {
		return err
	}
	return WritePrivateFileAtomic(outputPath, func(w io.Writer) error {
		zw := zip.NewWriter(w)
		for _, entry := range entries {
			if len(entry.Content) == 0 {
				continue
			}
			content := entry.Content
			if data.Redacted {
				if strings.HasSuffix(entry.Name, ".json") || strings.HasSuffix(entry.Name, ".yaml") {
					content = RedactStructuredBytes(content)
				} else {
					content = RedactBytes(content)
				}
			}
			entryWriter, createErr := zw.Create(entry.Name)
			if createErr != nil {
				_ = zw.Close()
				return fmt.Errorf("creating zip entry %s: %w", entry.Name, createErr)
			}
			if _, writeErr := entryWriter.Write(content); writeErr != nil {
				_ = zw.Close()
				return fmt.Errorf("writing zip entry %s: %w", entry.Name, writeErr)
			}
		}
		if closeErr := zw.Close(); closeErr != nil {
			return fmt.Errorf("finalizing zip archive: %w", closeErr)
		}
		return nil
	})
}

func buildBundleZipEntries(data *Data) ([]bundleZipEntry, error) {
	// Build the markdown report for inclusion in the zip.
	var mdBuf bytes.Buffer
	if err := RenderMarkdown(&mdBuf, data); err != nil {
		return nil, fmt.Errorf("rendering bundle markdown: %w", err)
	}

	entries := []bundleZipEntry{
		{"report.md", mdBuf.Bytes()},
		{"hpa.yaml", data.HPA},
		{"scale-target.yaml", data.ScaleTarget},
		{"replicasets.yaml", data.ReplicaSets},
		{"pods.yaml", data.Pods},
		{"events.txt", data.Events},
		{"metrics-api.txt", data.MetricsAPI},
	}

	if err := appendJSONEntry(&entries, "analysis.json", data.StatusReport.Analysis); err != nil {
		return nil, err
	}
	if len(data.ContainerStatuses) > 0 {
		if err := appendJSONEntry(&entries, "container-statuses.json", data.ContainerStatuses); err != nil {
			return nil, err
		}
	}
	if len(data.ResourceQuotas) > 0 {
		if err := appendJSONEntry(&entries, "resourcequotas.json", data.ResourceQuotas); err != nil {
			return nil, err
		}
	}
	if len(data.LimitRanges) > 0 {
		if err := appendJSONEntry(&entries, "limitranges.json", data.LimitRanges); err != nil {
			return nil, err
		}
	}
	if len(data.PDBs) > 0 {
		if err := appendJSONEntry(&entries, "pdbs.json", data.PDBs); err != nil {
			return nil, err
		}
	}
	if data.NodeCapacity != nil {
		if err := appendJSONEntry(&entries, "node-capacity.json", data.NodeCapacity); err != nil {
			return nil, err
		}
	}

	// Metadata.
	entries = append(entries, bundleZipEntry{
		Name: "metadata.txt",
		Content: []byte(fmt.Sprintf(
			"HPA: %s/%s\nNamespace: %s\nTimestamp: %s\nFormat: bundle\n",
			data.Namespace, data.HPAName, data.Namespace, data.Timestamp.Format(time.RFC3339),
		)),
	})

	return entries, nil
}

// appendJSONEntry marshals value as pretty JSON and appends it under name when
// marshalling succeeds.
func appendJSONEntry(entries *[]bundleZipEntry, name string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal zip entry %s: %w", name, err)
	}
	*entries = append(*entries, bundleZipEntry{Name: name, Content: payload})
	return nil
}
