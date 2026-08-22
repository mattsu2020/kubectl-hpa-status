package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

func newOutputSchemaV2CLIRoot(client kubernetes.Interface) *cobra.Command {
	return NewRootCommandWithDeps(AppDeps{Kubernetes: client})
}

func executeOutputSchemaV2CLI(t *testing.T, client kubernetes.Interface, args ...string) string {
	t.Helper()
	root := newOutputSchemaV2CLIRoot(client)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("execute %v: %v\nstderr:\n%s\nstdout:\n%s", args, err, stderr.String(), stdout.String())
	}
	return stdout.String()
}

func executeOutputSchemaV2CLIWithError(t *testing.T, client kubernetes.Interface, args ...string) (string, error) {
	t.Helper()
	root := newOutputSchemaV2CLIRoot(client)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stderr.String() + stdout.String(), err
}

func assertV2SingleOutputShape(t *testing.T, data []byte) {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode v2 JSON: %v\n%s", err, data)
	}
	if got := decoded["apiVersion"]; got != hpaanalysis.SchemaVersionV2 {
		t.Fatalf("apiVersion = %#v, want %q", got, hpaanalysis.SchemaVersionV2)
	}
	analysis, ok := decoded["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis = %#v, want object", decoded["analysis"])
	}
	if _, flat := analysis["currentReplicas"]; flat {
		t.Fatalf("v2 analysis contains flat v1 field currentReplicas: %s", data)
	}
	meta, ok := analysis["meta"].(map[string]any)
	if !ok || meta["name"] != "web" {
		t.Fatalf("analysis.meta = %#v, want web identity", analysis["meta"])
	}
	replicas, ok := analysis["replicas"].(map[string]any)
	if !ok || replicas["currentReplicas"] != float64(3) || replicas["desiredReplicas"] != float64(3) {
		t.Fatalf("analysis.replicas = %#v, want current=desired=3", analysis["replicas"])
	}
}

func TestStatusOutputSchemaV2StructuredFormats(t *testing.T) {
	tests := []struct {
		name   string
		format string
		decode func(*testing.T, string) []byte
	}{
		{
			name:   "json",
			format: "json",
			decode: func(_ *testing.T, output string) []byte {
				return []byte(output)
			},
		},
		{
			name:   "yaml",
			format: "yaml",
			decode: func(t *testing.T, output string) []byte {
				t.Helper()
				data, err := yaml.YAMLToJSON([]byte(output))
				if err != nil {
					t.Fatalf("convert YAML output to JSON: %v\n%s", err, output)
				}
				return data
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hpa := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 3))
			output := executeOutputSchemaV2CLI(t, testutil.NewFakeClient(hpa),
				"status", "web", "--no-enrich", "--output-schema=v2", "--output="+tc.format)
			assertV2SingleOutputShape(t, tc.decode(t, output))
		})
	}
}

func TestWatchOutputSchemaV2UsesProjectedValueForEverySupportedFormat(t *testing.T) {
	report := hpaanalysis.StatusReport{
		APIVersion: hpaanalysis.SchemaVersion,
		Analysis: hpaanalysis.Analysis{
			Meta:     hpaanalysis.MetaView{Namespace: "default", Name: "web", Target: "Deployment/web"},
			Replicas: hpaanalysis.ReplicasView{Current: 3, Desired: 3, Min: 1, Max: 10},
			Decision: hpaanalysis.DecisionView{Health: string(hpaanalysis.HealthOK), HealthScore: 100, Summary: "HPA is healthy"},
		},
	}

	t.Run("json", func(t *testing.T) {
		opts := defaultRootOptions()
		opts.OutputSchema = "v2"
		opts.Output = "json"
		var output bytes.Buffer
		if err := writeWatchReport(&output, &opts, report, nil); err != nil {
			t.Fatal(err)
		}
		assertV2SingleOutputShape(t, output.Bytes())
	})

	t.Run("yaml", func(t *testing.T) {
		opts := defaultRootOptions()
		opts.OutputSchema = "v2"
		opts.Output = "yaml"
		var output bytes.Buffer
		if err := writeWatchReport(&output, &opts, report, nil); err != nil {
			t.Fatal(err)
		}
		data, err := yaml.YAMLToJSON(output.Bytes())
		if err != nil {
			t.Fatalf("convert YAML output to JSON: %v", err)
		}
		assertV2SingleOutputShape(t, data)
	})

	t.Run("jsonl", func(t *testing.T) {
		opts := defaultRootOptions()
		opts.OutputSchema = "v2"
		opts.Output = "jsonl"
		var output bytes.Buffer
		if err := writeWatchReport(&output, &opts, report, nil); err != nil {
			t.Fatal(err)
		}
		records := decodeStatusRecordLinesV2(t, output.String())
		if len(records) != 1 || records[0].Report == nil || records[0].Status != hpaanalysis.StatusRecordSuccessV2 {
			t.Fatalf("watch JSONL record = %#v", records)
		}
	})

	t.Run("jsonpath", func(t *testing.T) {
		opts := defaultRootOptions()
		opts.OutputSchema = "v2"
		opts.Output = "jsonpath={.apiVersion}"
		var output bytes.Buffer
		if err := writeWatchReport(&output, &opts, report, nil); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(output.String()); got != hpaanalysis.SchemaVersionV2 {
			t.Fatalf("watch JSONPath apiVersion = %q", got)
		}
	})

	t.Run("go-template", func(t *testing.T) {
		opts := defaultRootOptions()
		opts.OutputSchema = "v2"
		opts.Output = "go-template"
		opts.Template = "{{.APIVersion}}"
		var output bytes.Buffer
		if err := writeWatchReport(&output, &opts, report, nil); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(output.String()); got != hpaanalysis.SchemaVersionV2 {
			t.Fatalf("watch Go template apiVersion = %q", got)
		}
	})
}

func decodeStatusRecordLinesV2(t *testing.T, output string) []hpaanalysis.StatusRecordV2 {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	records := make([]hpaanalysis.StatusRecordV2, len(lines))
	for i := range lines {
		if err := json.Unmarshal([]byte(lines[i]), &records[i]); err != nil {
			t.Fatalf("decode v2 JSONL line %d: %v\n%s", i, err, output)
		}
		if records[i].APIVersion != hpaanalysis.SchemaVersionV2 {
			t.Fatalf("record %d apiVersion = %q", i, records[i].APIVersion)
		}
	}
	return records
}

func TestStatusOutputSchemaV2JSONLSingleSuccessUsesRecordEnvelope(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 3))
	output := executeOutputSchemaV2CLI(t, testutil.NewFakeClient(hpa),
		"status", "web", "--no-enrich", "--output-schema=v2", "--output=jsonl")

	records := decodeStatusRecordLinesV2(t, output)
	if len(records) != 1 {
		t.Fatalf("single status emitted %d records, want 1", len(records))
	}
	record := records[0]
	if record.Namespace != "default" || record.Name != "web" ||
		record.Status != hpaanalysis.StatusRecordSuccessV2 ||
		record.Report == nil || record.Error != "" {
		t.Fatalf("single success record = %#v", record)
	}
}

func TestStatusOutputSchemaV2JSONLMultipleSuccessesUseRecordEnvelopes(t *testing.T) {
	web := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 3))
	api := testutil.BuildHPA("default", "api", testutil.WithReplicas(2, 2))
	output := executeOutputSchemaV2CLI(t, testutil.NewFakeClient(web, api),
		"status", "web", "api", "--no-enrich", "--output-schema=v2", "--output=jsonl")

	records := decodeStatusRecordLinesV2(t, output)
	if len(records) != 2 {
		t.Fatalf("multi status emitted %d records, want 2", len(records))
	}
	for i, record := range records {
		if record.Status != hpaanalysis.StatusRecordSuccessV2 || record.Report == nil || record.Error != "" {
			t.Fatalf("success record %d = %#v", i, record)
		}
	}
}

func TestStatusOutputSchemaV2JSONLPartialFailureUsesSameRecordEnvelope(t *testing.T) {
	web := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 3))
	root := newOutputSchemaV2CLIRoot(testutil.NewFakeClient(web))
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"status", "web", "missing", "--no-enrich", "--output-schema=v2", "--output=jsonl",
	})
	if err := root.Execute(); err == nil {
		t.Fatal("partial failure returned nil, want aggregate error")
	}

	records := decodeStatusRecordLinesV2(t, stdout.String())
	if len(records) != 2 {
		t.Fatalf("partial status emitted %d records, want 2: %s", len(records), stdout.String())
	}
	if records[0].Status != hpaanalysis.StatusRecordSuccessV2 || records[0].Report == nil || records[0].Error != "" {
		t.Fatalf("partial success record = %#v", records[0])
	}
	if records[1].Status != hpaanalysis.StatusRecordErrorV2 || records[1].Report != nil || records[1].Error == "" {
		t.Fatalf("partial error record = %#v", records[1])
	}
	if stderr.Len() != 0 {
		t.Fatalf("v2 JSONL duplicated carried error on stderr: %s", stderr.String())
	}
}

func TestStatusOutputSchemaV2SingleFailureUsesErrorRecord(t *testing.T) {
	for _, format := range []string{"json", "yaml", "jsonl"} {
		t.Run(format, func(t *testing.T) {
			root := newOutputSchemaV2CLIRoot(testutil.NewFakeClient())
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs([]string{"status", "missing", "--no-enrich", "--output-schema=v2", "--output=" + format})
			if err := root.Execute(); err == nil {
				t.Fatal("missing HPA returned nil")
			}
			data := stdout.Bytes()
			if format == "yaml" {
				var err error
				data, err = yaml.YAMLToJSON(data)
				if err != nil {
					t.Fatal(err)
				}
			}
			var record hpaanalysis.StatusRecordV2
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatalf("decode %s error record: %v\n%s", format, err, stdout.String())
			}
			if record.Status != hpaanalysis.StatusRecordErrorV2 || record.Name != "missing" || record.Error == "" || record.Report != nil {
				t.Fatalf("%s error record = %#v", format, record)
			}
		})
	}
}

func TestWatchCLIOutputSchemaV2KeepsStdoutMachineReadable(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(10, 10),
		testutil.WithMinMax(2, 10),
		testutil.WithScalingLimitedTrue("TooManyReplicas"),
	)
	for _, args := range [][]string{
		{"status", "web", "--watch", "--no-enrich", "--until-condition=ScalingLimited", "--output-schema=v2", "--output=jsonl"},
		{"watch", "web", "--until-condition=ScalingLimited", "--output-schema=v2", "--output=jsonl"},
	} {
		root := newOutputSchemaV2CLIRoot(testutil.NewFakeClient(hpa))
		var stdout, stderr bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("execute %v: %v\nstderr: %s", args, err, stderr.String())
		}
		records := decodeStatusRecordLinesV2(t, stdout.String())
		if len(records) != 1 || records[0].Name != "web" || records[0].Report == nil {
			t.Fatalf("watch records for %v = %#v", args, records)
		}
		if strings.Contains(stdout.String(), "Updated:") || strings.Contains(stdout.String(), "Stopped:") {
			t.Fatalf("watch stdout was polluted for %v: %s", args, stdout.String())
		}
	}
}

func TestStatusOutputSchemaV1IsRejected(t *testing.T) {
	web := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 3))
	_, err := executeOutputSchemaV2CLIWithError(t, testutil.NewFakeClient(web),
		"status", "web", "--no-enrich", "--output-schema=v1", "--output=jsonl")
	if err == nil {
		t.Fatal("expected --output-schema=v1 to be rejected in v4")
	}
	if !strings.Contains(err.Error(), "v1") || !strings.Contains(err.Error(), "v2") {
		t.Fatalf("error should point from v1 to v2, got: %v", err)
	}
}

func TestOutputSchemaV2DeclaresStrictStatusRecord(t *testing.T) {
	data, err := os.ReadFile("../docs/output-schema-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		OneOf []struct {
			Ref string `json:"$ref"`
		} `json:"oneOf"`
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("parse output-schema-v2.json: %v", err)
	}
	foundRecordRef := false
	for _, branch := range schema.OneOf {
		if branch.Ref == "#/$defs/statusRecordV2" {
			foundRecordRef = true
		}
	}
	if !foundRecordRef {
		t.Fatal("top-level oneOf does not reference statusRecordV2")
	}
	var record struct {
		AdditionalProperties *bool             `json:"additionalProperties"`
		Required             []string          `json:"required"`
		OneOf                []json.RawMessage `json:"oneOf"`
		Properties           map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema.Defs["statusRecordV2"], &record); err != nil {
		t.Fatalf("decode statusRecordV2 definition: %v", err)
	}
	if record.AdditionalProperties == nil || *record.AdditionalProperties {
		t.Fatal("statusRecordV2 must set additionalProperties=false")
	}
	required := make(map[string]struct{}, len(record.Required))
	for _, field := range record.Required {
		required[field] = struct{}{}
	}
	for _, field := range []string{"apiVersion", "namespace", "name", "status"} {
		if _, ok := required[field]; !ok {
			t.Fatalf("statusRecordV2 required = %v, missing %q", record.Required, field)
		}
	}
	if got := strings.Join(record.Properties["status"].Enum, ","); got != "success,warning,error" {
		t.Fatalf("statusRecordV2 status enum = %q", got)
	}
	if len(record.OneOf) != 2 {
		t.Fatalf("statusRecordV2 outcome branches = %d, want 2", len(record.OneOf))
	}
}

func TestStatusOutputSchemaV2BatchEnvelope(t *testing.T) {
	web := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 3))
	api := testutil.BuildHPA("default", "api", testutil.WithReplicas(2, 2))
	output := executeOutputSchemaV2CLI(t, testutil.NewFakeClient(web, api),
		"status", "web", "api", "--no-enrich", "--output-schema=v2", "--output=json")

	var batch hpaanalysis.StatusBatchV2
	if err := json.Unmarshal([]byte(output), &batch); err != nil {
		t.Fatalf("decode v2 batch: %v\n%s", err, output)
	}
	if batch.APIVersion != hpaanalysis.SchemaVersionV2 {
		t.Fatalf("batch apiVersion = %q", batch.APIVersion)
	}
	if len(batch.Items) != 2 {
		t.Fatalf("batch items = %d, want 2", len(batch.Items))
	}
	for i, item := range batch.Items {
		if item.Report == nil || item.Report.APIVersion != hpaanalysis.SchemaVersionV2 {
			t.Fatalf("batch item %d report = %#v", i, item.Report)
		}
	}
}

func TestOutputSchemaV2RejectsInvalidCLICombinations(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "unknown schema",
			args:    []string{"status", "web", "--output=json", "--output-schema=v3"},
			wantErr: "--output-schema must be one of",
		},
		{
			name:    "text output",
			args:    []string{"status", "web", "--output-schema=v2"},
			wantErr: "--output-schema=v2 requires",
		},
		{
			name:    "prometheus output",
			args:    []string{"status", "web", "--output=prometheus", "--output-schema=v2"},
			wantErr: "--output-schema=v2 requires",
		},
		{
			name:    "report output",
			args:    []string{"status", "web", "--report=markdown", "--output-schema=v2"},
			wantErr: "--output-schema=v2 requires",
		},
		{
			name:    "non-status command",
			args:    []string{"list", "--output=json", "--output-schema=v2"},
			wantErr: "--output-schema=v2 is supported by status and watch output only",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := NewRootCommand()
			var output bytes.Buffer
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs(tc.args)
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want substring %q\noutput:\n%s", err, tc.wantErr, output.String())
			}
		})
	}
}

func TestStatusOutputSchemaV2IsDefaultForStructuredOutput(t *testing.T) {
	web := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 3))

	// No --output-schema flag: v2 is the default wire schema, so JSON output
	// carries the grouped projection.
	output := executeOutputSchemaV2CLI(t, testutil.NewFakeClient(web),
		"status", "web", "--no-enrich", "--output=json")
	assertV2SingleOutputShape(t, []byte(output))

	// The text path is schema-independent: plain `status NAME` must keep
	// working without a schema error now that v2 is the default.
	text := executeOutputSchemaV2CLI(t, testutil.NewFakeClient(web),
		"status", "web", "--no-enrich")
	if !strings.Contains(text, "web") {
		t.Fatalf("plain text status output = %q", text)
	}
}
