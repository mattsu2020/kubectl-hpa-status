package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/lint"
	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
			fix, _ := cmd.Flags().GetBool("fix")
			failOn, _ := cmd.Flags().GetString("fail-on")
			return runLint(cmd.Context(), cmd.OutOrStdout(), opts, filePath, outputFmt, sarif, fix, failOn)
		},
	}
	cmd.Flags().StringP("file", "f", "", "path to HPA manifest file or directory")
	cmd.Flags().Bool("sarif", false, "output in SARIF format for CI integration")
	cmd.Flags().Bool("fix", false, "show auto-fix proposals (dry-run only)")
	cmd.Flags().String("fail-on", "error", "exit non-zero on findings at this severity or above: error, warning, info")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func runLint(ctx context.Context, out io.Writer, _ *options, filePath, outputFmt string, sarif, fix bool, failOn string) error {
	failOn = strings.ToLower(strings.TrimSpace(failOn))
	if err := validateLintFailOn(failOn); err != nil {
		return err
	}
	outputFmt = strings.ToLower(strings.TrimSpace(outputFmt))
	if err := validateLintOutputFormat(outputFmt, sarif); err != nil {
		return err
	}

	files, err := collectLintFiles(filePath)
	if err != nil {
		return err
	}

	if len(files) == 0 {
		if sarif || outputFmt == "sarif" || outputFmt == "json" || outputFmt == "yaml" || outputFmt == "github" {
			return emitLintOutput(out, nil, filePath, outputFmt, sarif, fix)
		}
		_, _ = fmt.Fprintln(out, "No YAML/JSON files found.")
		return nil
	}

	decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDeserializer()
	var allResults []lintFileResult
	workloads := collectLintWorkloads(files, decoder)

	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Files without an HPA document are skipped silently; lintOneFile
		// returns no results for them, so allResults is unchanged.
		results, _ := lintOneFile(f, decoder, workloads)
		allResults = append(allResults, results...)
	}

	if err := emitLintOutput(out, allResults, filePath, outputFmt, sarif, fix); err != nil {
		return err
	}
	if shouldFailOn(failOn, allResults) {
		return &exitCodeError{code: 1}
	}
	return nil
}

func validateLintFailOn(failOn string) error {
	switch failOn {
	case "error", "warning", "info":
		return nil
	default:
		return fmt.Errorf("--fail-on must be one of error, warning, info; got %q", failOn)
	}
}

func validateLintOutputFormat(outputFmt string, sarif bool) error {
	if sarif {
		return nil
	}
	switch outputFmt {
	case "", "text", "table", "json", "yaml", "sarif", "github":
		return nil
	default:
		return fmt.Errorf("lint output must be one of text, json, yaml, sarif, github; got %q", outputFmt)
	}
}

// shouldFailOn checks whether findings at the specified severity level
// or above warrant a non-zero exit code.
func shouldFailOn(failOn string, results []lintFileResult) bool {
	for _, r := range results {
		if r.Result == nil {
			continue
		}
		switch failOn {
		case "error":
			if r.Result.Errors > 0 {
				return true
			}
		case "warning":
			if r.Result.Errors > 0 || r.Result.Warnings > 0 {
				return true
			}
		case "info":
			if len(r.Result.Findings) > 0 {
				return true
			}
		}
	}
	return false
}

type lintWorkloadKey struct {
	Namespace string
	Kind      string
	Name      string
}

type lintWorkloadInfo struct {
	Replicas *int32
}

func collectLintWorkloads(files []string, decoder runtimeDecoder) map[lintWorkloadKey]lintWorkloadInfo {
	workloads := make(map[lintWorkloadKey]lintWorkloadInfo)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, doc := range splitYAMLDocuments(data) {
			doc = []byte(strings.TrimSpace(string(doc)))
			if len(doc) == 0 {
				continue
			}
			obj, _, decodeErr := decoder.Decode(doc, nil, nil)
			if decodeErr != nil {
				continue
			}
			switch workload := obj.(type) {
			case *appsv1.Deployment:
				workloads[lintObjectKey(workload.Namespace, "Deployment", workload.Name)] = lintWorkloadInfo{Replicas: workload.Spec.Replicas}
			case *appsv1.StatefulSet:
				workloads[lintObjectKey(workload.Namespace, "StatefulSet", workload.Name)] = lintWorkloadInfo{Replicas: workload.Spec.Replicas}
			}
		}
	}
	return workloads
}

type runtimeDecoder interface {
	Decode(data []byte, defaults *schema.GroupVersionKind, into runtime.Object) (runtime.Object, *schema.GroupVersionKind, error)
}

func lintObjectKey(namespace, kind, name string) lintWorkloadKey {
	if namespace == "" {
		namespace = "default"
	}
	return lintWorkloadKey{Namespace: namespace, Kind: kind, Name: name}
}

func addGitOpsLintFindings(result *lint.Result, hpa *autoscalingv2.HorizontalPodAutoscaler, workloads map[lintWorkloadKey]lintWorkloadInfo) {
	if result == nil || hpa == nil {
		return
	}
	info, ok := workloads[lintObjectKey(hpa.Namespace, hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name)]
	if !ok || info.Replicas == nil {
		return
	}
	result.Findings = append(result.Findings, lint.Finding{
		Severity: lint.Warning,
		Rule:     "gitops-replicas",
		Message: fmt.Sprintf("%s/%s sets spec.replicas=%d while HPA %s exists. GitOps apply may reset HPA-managed replicas.",
			hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name, *info.Replicas, hpa.Name),
		Recommendation: "Remove spec.replicas from the workload manifest after the HPA is created, or manage replica ownership explicitly in GitOps.",
	})
	result.Warnings++
}

type lintFileResult struct {
	File     string       `json:"file" yaml:"file"`
	Document int          `json:"document,omitempty" yaml:"document,omitempty"`
	HPA      string       `json:"hpa,omitempty" yaml:"hpa,omitempty"`
	Result   *lint.Result `json:"result" yaml:"result"`
}

// exitCodeError is returned when lint finds errors.
type exitCodeError struct {
	code int
}

func (e *exitCodeError) Error() string {
	return fmt.Sprintf("lint found issues (exit code %d)", e.code)
}

// combineLintResults combines multiple lint results into one for SARIF output.
func combineLintResults(results []lintFileResult) *lint.Result {
	combined := &lint.Result{Pass: true}
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

func writeGitHubLintAnnotations(out io.Writer, results []lintFileResult) error {
	for _, r := range results {
		if r.Result == nil {
			continue
		}
		for _, finding := range r.Result.Findings {
			level := "notice"
			switch finding.Severity {
			case lint.Error:
				level = "error"
			case lint.Warning:
				level = "warning"
			}
			message := strings.TrimSpace(finding.Message)
			if finding.Recommendation != "" {
				message += " Recommendation: " + finding.Recommendation
			}
			if _, err := fmt.Fprintf(out, "::%s file=%s::%s\n", level, escapeGitHubAnnotationProperty(r.File), escapeGitHubAnnotationMessage(message)); err != nil {
				return err
			}
		}
	}
	return nil
}

func escapeGitHubAnnotationProperty(value string) string {
	value = strings.ReplaceAll(value, "%", "%25")
	value = strings.ReplaceAll(value, "\r", "%0D")
	value = strings.ReplaceAll(value, "\n", "%0A")
	value = strings.ReplaceAll(value, ":", "%3A")
	value = strings.ReplaceAll(value, ",", "%2C")
	return value
}

func escapeGitHubAnnotationMessage(value string) string {
	value = strings.ReplaceAll(value, "%", "%25")
	value = strings.ReplaceAll(value, "\r", "%0D")
	value = strings.ReplaceAll(value, "\n", "%0A")
	return value
}
