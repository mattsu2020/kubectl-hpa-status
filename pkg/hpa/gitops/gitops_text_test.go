package gitops

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteGitOpsConflictHTMLEscapesAllDynamicFields(t *testing.T) {
	report := &Conflict{
		Namespace: `<script>namespace</script>`,
		Name:      `<script>name</script>`,
		Target:    `<script>target</script>`,
		Conflicts: []ConflictEntry{{
			Severity:      `<script>severity</script>`,
			Kind:          `<script>kind</script>`,
			Name:          `<script>entry-name</script>`,
			Field:         `<script>field</script>`,
			ManifestValue: `<script>manifest</script>`,
			LiveValue:     `<script>live</script>`,
			HPADesired:    `<script>desired</script>`,
			Detail:        `<script>detail</script>`,
			Remediation:   `<script>remediation</script>`,
		}},
	}
	var buf bytes.Buffer
	if err := WriteConflictHTML(&buf, report); err != nil {
		t.Fatalf("WriteConflictHTML: %v", err)
	}
	if strings.Contains(buf.String(), "<script>") {
		t.Fatalf("HTML contains unescaped dynamic input:\n%s", buf.String())
	}
	for _, want := range []string{"&lt;script&gt;namespace&lt;/script&gt;", "&lt;script&gt;field&lt;/script&gt;", "&lt;script&gt;remediation&lt;/script&gt;"} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("escaped value %q missing from output:\n%s", want, buf.String())
		}
	}
}
