package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/lint"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
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

// decodeHPAFromDoc attempts to decode a single YAML/JSON document and returns
// the HPA object if the document is an HPA manifest, ok=false otherwise.
func decodeHPAFromDoc(doc []byte, decoder runtimeDecoder) (*autoscalingv2.HorizontalPodAutoscaler, bool) {
	doc = []byte(strings.TrimSpace(string(doc)))
	if len(doc) == 0 {
		return nil, false
	}

	obj, _, decodeErr := decoder.Decode(doc, nil, nil)
	if decodeErr != nil {
		return nil, false
	}

	hpa, ok := obj.(*autoscalingv2.HorizontalPodAutoscaler)
	if !ok {
		return nil, false
	}
	return hpa, true
}

// lintOneFile decodes and lints every HPA document in file, appending a
// lintFileResult per HPA found. Returns true if at least one HPA document was
// found in the file.
func lintOneFile(file string, decoder runtimeDecoder, workloads map[lintWorkloadKey]lintWorkloadInfo) ([]lintFileResult, bool) {
	data, readErr := os.ReadFile(file)
	if readErr != nil {
		return []lintFileResult{lintFileResultForReadError(file, readErr)}, true
	}

	docs := splitYAMLDocuments(data)
	var results []lintFileResult
	foundHPA := false
	for _, doc := range docs {
		hpa, ok := decodeHPAFromDoc(doc, decoder)
		if !ok {
			continue
		}
		foundHPA = true

		result := lint.Run(hpa)
		addGitOpsLintFindings(result, hpa, workloads)
		results = append(results, lintFileResult{
			File:   file,
			HPA:    hpa.Name,
			Result: result,
		})
	}
	return results, foundHPA
}

// emitLintOutput writes the lint results to out in the requested format and
// returns the error, if any. If special handling (sarif/github) applies it
// returns (handled=true, err) so the caller can short-circuit the exit-code
// logic.
func emitLintOutput(out io.Writer, allResults []lintFileResult, filePath, outputFmt string, sarif, fix bool, exitCode int) (handled bool, err error) {
	if sarif || outputFmt == "sarif" {
		combined := combineLintResults(allResults)
		sarifJSON := lint.FormatLintSARIF(combined, filePath)
		_, err := fmt.Fprintln(out, sarifJSON)
		return true, err
	}
	if outputFmt == "github" {
		if err := writeGitHubLintAnnotations(out, allResults); err != nil {
			return true, err
		}
		if exitCode != 0 {
			return true, &exitCodeError{code: exitCode}
		}
		return true, nil
	}

	for _, r := range allResults {
		if outputFmt == "json" {
			if err := writeOutput(out, "json", "", r.Result, nil); err != nil {
				return false, err
			}
			continue
		}
		if err := writeLintTextResult(out, r, fix); err != nil {
			return false, err
		}
	}
	return false, nil
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
