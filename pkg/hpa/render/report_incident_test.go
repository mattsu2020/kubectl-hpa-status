package render

import (
	"bytes"
	"strings"
	"testing"

	hpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestWriteIncidentReport(t *testing.T) {
	var buf bytes.Buffer
	report := hpa.StatusReport{Analysis: hpa.Analysis{Namespace: "default", Name: "web", Health: "OK"}}
	if err := WriteIncidentReport(&buf, report); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "default/web") {
		t.Errorf("expected incident report to mention default/web, got:\n%s", buf.String())
	}
}
