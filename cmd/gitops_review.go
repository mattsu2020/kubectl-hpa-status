package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
)

func newGitOpsReviewCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Review HPA manifest changes for risky modifications in PR diffs",
		Long:  "Compares HPA manifests against best practices and detects risky changes like maxReplicas decreases, removed stabilization, aggressive targets, and metric removals.",
		RunE: func(cmd *cobra.Command, args []string) error {
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

func runGitOpsReview(ctx context.Context, out io.Writer, opts *options, filePath string) error {
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

	if len(inputs) == 0 {
		_, _ = fmt.Fprintln(out, "No HPA manifests found.")
		return nil
	}

	review := hpaanalysis.AnalyzeGitOpsReview(inputs)

	format, templateStr := outputSelection(outputConfig{
		output: opts.output, template: opts.template, outputTemplates: opts.outputTemplates,
	})

	return writeOutput(out, format, templateStr, review, func() error {
		theme := style.NewTheme(shouldColorize(opts.color, out))
		return hpaanalysis.WriteGitOpsReviewText(out, review, theme)
	})
}

// decoder interface for testing.
type hpaDecoder interface {
	Decode(data []byte, defaults *schema.GroupVersionKind, into runtime.Object) (runtime.Object, *schema.GroupVersionKind, error)
}
