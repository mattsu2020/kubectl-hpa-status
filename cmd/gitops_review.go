package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
)

func newGitOpsReviewCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Review HPA manifest changes for risky modifications in PR diffs",
		Long:  "Compares HPA manifests against best practices and detects risky changes like maxReplicas decreases, removed stabilization, aggressive targets, and metric removals.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("path")
			if path == "" {
				return fmt.Errorf("--path is required")
			}
			return runGitOpsReview(cmd.Context(), cmd.OutOrStdout(), opts, path)
		},
	}
	cmd.Flags().String("path", "", "path to manifest file or directory containing HPA manifests")
	_ = cmd.MarkFlagRequired("path")
	return cmd
}

func runGitOpsReview(_ context.Context, out io.Writer, opts *options, filePath string) error {
	files, err := collectGitOpsReviewFiles(filePath)
	if err != nil {
		return err
	}

	if len(files) == 0 {
		_, _ = fmt.Fprintln(out, "No YAML/JSON files found.")
		return nil
	}

	inputs := decodeGitOpsReviewInputs(files)
	if len(inputs) == 0 {
		_, _ = fmt.Fprintln(out, "No HPA manifests found.")
		return nil
	}

	review := hpaanalysis.AnalyzeGitOpsReview(inputs)

	format, templateStr := outputSelection(outputConfig{
		output: opts.Output, template: opts.Template, outputTemplates: opts.OutputTemplates,
	})

	return writeOutput(out, format, templateStr, review, func() error {
		theme := style.NewTheme(shouldColorize(opts.Color, out))
		return hpaanalysis.WriteGitOpsReviewText(out, review, theme)
	})
}

func collectGitOpsReviewFiles(filePath string) ([]string, error) {
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

func decodeGitOpsReviewInputs(files []string) []hpaanalysis.GitOpsReviewInput {
	decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDeserializer()
	var inputs []hpaanalysis.GitOpsReviewInput

	for _, f := range files {
		data, readErr := os.ReadFile(f)
		if readErr != nil {
			continue
		}

		docs := splitYAMLDocuments(data)
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

			inputs = append(inputs, hpaanalysis.GitOpsReviewInput{
				After:    hpa,
				FilePath: f,
			})
		}
	}

	return inputs
}
