package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/lint"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

// collectLintFiles walks the given path and returns the list of YAML/JSON
// files to lint. If filePath is a single file it is returned as-is.
func collectLintFiles(filePath string) ([]string, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("cannot access %s: %w", filePath, err)
	}

	if !info.IsDir() {
		return []string{filePath}, nil
	}

	var files []string
	err = filepath.Walk(filePath, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yaml" || ext == ".yml" || ext == ".json" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}
	return files, nil
}

// lintFileResultForReadError builds a lintFileResult representing a file that
// could not be read.
func lintFileResultForReadError(file string, readErr error) lintFileResult {
	return lintFileResult{
		File: file,
		Result: &lint.Result{
			Findings: []lint.Finding{{
				Severity: lint.Error,
				Rule:     "file-read",
				Message:  fmt.Sprintf("Cannot read file: %v", readErr),
			}},
			Errors: 1,
			Pass:   false,
		},
	}
}

// decodeHPAFromDoc attempts to decode a single YAML/JSON document. isHPA stays
// true when a HorizontalPodAutoscaler document is malformed or uses an
// unsupported API version, allowing lint to surface the decode error instead
// of silently treating it as an unrelated manifest.
func decodeHPAFromDoc(doc []byte, decoder runtimeDecoder) (hpa *autoscalingv2.HorizontalPodAutoscaler, isHPA bool, decodeErr error) {
	doc = []byte(strings.TrimSpace(string(doc)))
	if len(doc) == 0 {
		return nil, false, nil
	}

	obj, gvk, decodeErr := decoder.Decode(doc, nil, nil)
	if decodeErr != nil {
		isHPA := looksLikeHPADocument(doc)
		if gvk != nil && gvk.Kind == "HorizontalPodAutoscaler" {
			isHPA = true
		}
		return nil, isHPA, decodeErr
	}

	hpa, ok := obj.(*autoscalingv2.HorizontalPodAutoscaler)
	if !ok {
		if (gvk != nil && gvk.Kind == "HorizontalPodAutoscaler") || looksLikeHPADocument(doc) {
			apiVersion := ""
			if gvk != nil {
				apiVersion = gvk.GroupVersion().String()
			}
			return nil, true, fmt.Errorf("unsupported HorizontalPodAutoscaler apiVersion %q; use autoscaling/v2", apiVersion)
		}
		return nil, false, nil
	}
	return hpa, true, nil
}

func looksLikeHPADocument(doc []byte) bool {
	var typeMeta metav1.TypeMeta
	if err := yaml.Unmarshal(doc, &typeMeta); err == nil && typeMeta.Kind == "HorizontalPodAutoscaler" {
		return true
	}
	// Keep a narrow fallback for syntactically broken YAML where TypeMeta
	// itself cannot be decoded but the intended resource kind is still clear.
	normalized := strings.ReplaceAll(string(doc), " ", "")
	normalized = strings.ReplaceAll(normalized, "\t", "")
	return strings.Contains(normalized, "kind:HorizontalPodAutoscaler") ||
		strings.Contains(normalized, "\"kind\":\"HorizontalPodAutoscaler\"")
}

func lintFileResultForDecodeError(file string, document int, decodeErr error) lintFileResult {
	return lintFileResult{
		File:     file,
		Document: document,
		Result: &lint.Result{
			Findings: []lint.Finding{{
				Severity: lint.Error,
				Rule:     "manifest-decode",
				Message:  fmt.Sprintf("Cannot decode HPA manifest document %d: %v", document, decodeErr),
			}},
			Errors: 1,
			Pass:   false,
		},
	}
}

// lintOneFile decodes and lints every HPA document in file, appending a
// lintFileResult per HPA found. Returns true if at least one HPA document was
// found in the file.
func lintOneFile(file string, decoder runtimeDecoder, workloads map[lintWorkloadKey]lintWorkloadInfo) ([]lintFileResult, bool) {
	data, readErr := os.ReadFile(file)
	if readErr != nil {
		return []lintFileResult{lintFileResultForReadError(file, readErr)}, true
	}

	docs, splitErr := readYAMLDocuments(data)
	if splitErr != nil {
		return []lintFileResult{lintFileResultForDecodeError(file, 0, splitErr)}, true
	}
	var results []lintFileResult
	foundHPA := false
	for i, doc := range docs {
		hpa, isHPA, decodeErr := decodeHPAFromDoc(doc, decoder)
		if !isHPA {
			continue
		}
		foundHPA = true
		if decodeErr != nil {
			results = append(results, lintFileResultForDecodeError(file, i+1, decodeErr))
			continue
		}

		result := lint.Run(hpa)
		addGitOpsLintFindings(result, hpa, workloads)
		results = append(results, lintFileResult{
			File:     file,
			Document: i + 1,
			HPA:      hpa.Name,
			Result:   result,
		})
	}
	return results, foundHPA
}

// splitYAMLDocuments uses Kubernetes' stream reader, which accepts standard
// separator variants (including comments and CRLF) instead of matching only
// one literal newline sequence.
func splitYAMLDocuments(data []byte) [][]byte {
	docs, _ := readYAMLDocuments(data)
	return docs
}

func readYAMLDocuments(data []byte) ([][]byte, error) {
	reader := k8syaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	var docs [][]byte
	for {
		doc, err := reader.Read()
		if err == io.EOF {
			return docs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read YAML document stream: %w", err)
		}
		if len(bytes.TrimSpace(doc)) > 0 {
			docs = append(docs, doc)
		}
	}
}

type lintReport struct {
	APIVersion string           `json:"apiVersion" yaml:"apiVersion"`
	Source     string           `json:"source" yaml:"source"`
	Results    []lintFileResult `json:"results" yaml:"results"`
	Summary    lintSummary      `json:"summary" yaml:"summary"`
}

type lintSummary struct {
	Documents int  `json:"documents" yaml:"documents"`
	Errors    int  `json:"errors" yaml:"errors"`
	Warnings  int  `json:"warnings" yaml:"warnings"`
	Infos     int  `json:"infos" yaml:"infos"`
	Pass      bool `json:"pass" yaml:"pass"`
}

func buildLintReport(filePath string, results []lintFileResult) lintReport {
	report := lintReport{
		APIVersion: hpaanalysis.SchemaVersion,
		Source:     filePath,
		Results:    results,
		Summary:    lintSummary{Documents: len(results), Pass: true},
	}
	for _, result := range results {
		if result.Result == nil {
			continue
		}
		report.Summary.Errors += result.Result.Errors
		report.Summary.Warnings += result.Result.Warnings
		report.Summary.Infos += result.Result.Infos
	}
	report.Summary.Pass = report.Summary.Errors == 0
	return report
}

// emitLintOutput writes exactly one output document for structured formats.
// Exit policy is evaluated by runLint after this function for every format.
func emitLintOutput(out io.Writer, allResults []lintFileResult, filePath, outputFmt string, sarif, fix bool) error {
	if sarif || outputFmt == "sarif" {
		combined := combineLintResults(allResults)
		sarifJSON := lint.FormatLintSARIF(combined, filePath)
		_, err := fmt.Fprintln(out, sarifJSON)
		return err
	}
	if outputFmt == "github" {
		return writeGitHubLintAnnotations(out, allResults)
	}
	if outputFmt == "json" || outputFmt == "yaml" {
		if err := writeOutput(out, outputFmt, "", buildLintReport(filePath, allResults), nil); err != nil {
			return err
		}
		return nil
	}

	for _, r := range allResults {
		if err := writeLintTextResult(out, r, fix); err != nil {
			return err
		}
	}
	return nil
}

// writeLintTextResult writes a single lint result in human-readable text form,
// optionally followed by an auto-fix diff.
func writeLintTextResult(out io.Writer, r lintFileResult, fix bool) error {
	if r.HPA != "" {
		_, _ = fmt.Fprintf(out, "%s (%s):\n", r.File, r.HPA)
	} else {
		_, _ = fmt.Fprintf(out, "%s:\n", r.File)
	}
	if err := hpaanalysis.WriteLintText(out, r.Result); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out)

	if fix {
		if err := hpaanalysis.WriteLintDiff(out, r.Result); err != nil {
			return err
		}
	}
	return nil
}
