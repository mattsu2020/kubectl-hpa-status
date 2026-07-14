package cmd

import (
	"errors"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

type alwaysFailWriter struct{}

func (alwaysFailWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestTextRenderersPropagateWriterErrors(t *testing.T) {
	tests := map[string]func() error{
		"simulate":         func() error { return writeSimulateText(alwaysFailWriter{}, simulateReport{}, style.Theme{}) },
		"behavior":         func() error { return writeBehaviorText(alwaysFailWriter{}, behaviorOutput{}) },
		"summary-markdown": func() error { return writeClusterSummaryMarkdown(alwaysFailWriter{}, hpaanalysis.ListReport{}) },
		"summary-html":     func() error { return writeClusterSummaryHTML(alwaysFailWriter{}, hpaanalysis.ListReport{}) },
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			if err := run(); err == nil {
				t.Fatal("expected writer error")
			}
		})
	}
}
