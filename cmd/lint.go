package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
)

func newLintCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Lint HPA manifests offline for CI validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			filePath, _ := cmd.Flags().GetString("file")
			outputFmt, _ := cmd.Flags().GetString("output")
			sarif, _ := cmd.Flags().GetBool("sarif")
			return runLint(cmd.Context(), cmd.OutOrStdout(), opts, filePath, outputFmt, sarif)
		},
	}
	cmd.Flags().StringP("file", "f", "", "path to HPA manifest file or directory")
	cmd.Flags().Bool("sarif", false, "output in SARIF format for CI integration")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func runLint(ctx context.Context, out io.Writer, opts *options, filePath, outputFmt string, sarif bool) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("cannot access %s: %w", filePath, err)
	}

	var files []string
	if info.IsDir() {
		err := filepath.Walk(filePath, func(path string, fi os.FileInfo, walkErr error) error {
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
			return fmt.Errorf("walking directory: %w", err)
		}
	} else {
		files = []string{filePath}
	}

	if len(files) == 0 {
		_, _ = fmt.Fprintln(out, "No YAML/JSON files found.")
		return nil
	}

	decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDeserializer()
	var allResults []lintFileResult
	exitCode := 0

	for _, f := range files {
		data, readErr := os.ReadFile(f)
		if readErr != nil {
			allResults = append(allResults, lintFileResult{
				File: f,
				Result: &hpaanalysis.LintResult{
					Findings: []hpaanalysis.LintFinding{{
						Severity: hpaanalysis.LintError,
						Rule:     "file-read",
						Message:  fmt.Sprintf("Cannot read file: %v", readErr),
					}},
					Errors: 1,
					Pass:   false,
				},
			})
			exitCode = 1
			continue
		}

		// Handle multi-document YAML.
		docs := splitYAMLDocuments(data)
		foundHPA := false
		for _, doc := range docs {
			doc = []byte(strings.TrimSpace(string(doc)))
			if len(doc) == 0 {
				continue
			}

			obj, _, decodeErr := decoder.Decode(doc, nil, nil)
			if decodeErr != nil {
				continue
			}

			hpa, ok := obj.(*autoscalingv2.HorizontalPodAutoscaler)
			if !ok {
				continue
			}
			foundHPA = true

			result := hpaanalysis.LintHPA(hpa)
			allResults = append(allResults, lintFileResult{
				File:   f,
				HPA:    hpa.Name,
				Result: result,
			})
			if !result.Pass {
				exitCode = 1
			}
		}

		if !foundHPA {
			// Not an HPA manifest — skip silently.
			continue
		}
	}

	// Output.
	if sarif || outputFmt == "sarif" {
		combined := combineLintResults(allResults)
		sarifJSON := hpaanalysis.FormatLintSARIF(combined, filePath)
		_, err := fmt.Fprintln(out, sarifJSON)
		return err
	}

	for _, r := range allResults {
		if outputFmt == "json" {
			if err := writeOutput(out, "json", "", r.Result, nil); err != nil {
				return err
			}
			continue
		}
		if r.HPA != "" {
			_, _ = fmt.Fprintf(out, "%s (%s):\n", r.File, r.HPA)
		} else {
			_, _ = fmt.Fprintf(out, "%s:\n", r.File)
		}
		if err := hpaanalysis.WriteLintText(out, r.Result); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(out)
	}

	_ = ctx
	if exitCode != 0 {
		return &exitCodeError{code: exitCode}
	}
	return nil
}

type lintFileResult struct {
	File   string
	HPA    string
	Result *hpaanalysis.LintResult
}

// exitCodeError is returned when lint finds errors.
type exitCodeError struct {
	code int
}

func (e *exitCodeError) Error() string {
	return fmt.Sprintf("lint found issues (exit code %d)", e.code)
}

// splitYAMLDocuments splits a multi-document YAML byte slice.
func splitYAMLDocuments(data []byte) [][]byte {
	s := string(data)
	docs := strings.Split(s, "\n---\n")
	var result [][]byte
	for _, doc := range docs {
		trimmed := strings.TrimSpace(doc)
		if trimmed != "" {
			result = append(result, []byte(doc))
		}
	}
	return result
}

// combineLintResults combines multiple lint results into one for SARIF output.
func combineLintResults(results []lintFileResult) *hpaanalysis.LintResult {
	combined := &hpaanalysis.LintResult{Pass: true}
	for _, r := range results {
		if r.Result == nil {
			continue
		}
		combined.Findings = append(combined.Findings, r.Result.Findings...)
		combined.Errors += r.Result.Errors
		combined.Warnings += r.Result.Warnings
		combined.Infos += r.Result.Infos
		if !r.Result.Pass {
			combined.Pass = false
		}
	}
	return combined
}
