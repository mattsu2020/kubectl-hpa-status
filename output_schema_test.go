package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

type outputSchemaNode struct {
	Ref                  string                      `json:"$ref"`
	Type                 string                      `json:"type"`
	Properties           map[string]outputSchemaNode `json:"properties"`
	Required             []string                    `json:"required"`
	Items                *outputSchemaNode           `json:"items"`
	Enum                 []any                       `json:"enum"`
	OneOf                []outputSchemaNode          `json:"oneOf"`
	Const                string                      `json:"const"`
	Minimum              *float64                    `json:"minimum"`
	Maximum              *float64                    `json:"maximum"`
	MinItems             *int                        `json:"minItems"`
	MinLength            *int                        `json:"minLength"`
	AdditionalProperties *bool                       `json:"additionalProperties"`
	Not                  *outputSchemaNode           `json:"not"`
}

// outputSchema contains both the documented definitions and the top-level
// union used by real JSON output.
type outputSchema struct {
	OneOf []outputSchemaNode          `json:"oneOf"`
	Defs  map[string]outputSchemaNode `json:"$defs"`
}

func loadOutputSchema(t *testing.T) outputSchema {
	return loadOutputSchemaFile(t, "docs/output-schema.json")
}

func loadOutputSchemaFile(t *testing.T, schemaPath string) outputSchema {
	t.Helper()
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read %s: %v", schemaPath, err)
	}
	var schema outputSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("parse %s: %v", schemaPath, err)
	}
	return schema
}

// jsonTagSet collects the top-level JSON field names emitted for a struct
// type, following the encoding/json tag rules used by the output writers.
func jsonTagSet(t *testing.T, typ reflect.Type) map[string]struct{} {
	t.Helper()
	tags := make(map[string]struct{}, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			name = field.Name
		}
		tags[name] = struct{}{}
	}
	return tags
}

// TestOutputSchemaFieldsExistInStructs verifies that every documented property
// still exists on its Go struct. Public envelope types are checked in both
// directions so additive fields cannot silently ship without documentation.
// Analysis remains a documented stable-core subset because its optional feature
// domains are intentionally broader than this interoperability schema.
func TestOutputSchemaFieldsExistInStructs(t *testing.T) {
	schema := loadOutputSchema(t)

	cases := []struct {
		def   string
		typ   reflect.Type
		exact bool
	}{
		{"statusReport", reflect.TypeOf(hpaanalysis.StatusReport{}), true},
		{"statusBatch", reflect.TypeOf(hpaanalysis.StatusBatch{}), true},
		{"statusBatchItem", reflect.TypeOf(hpaanalysis.StatusBatchItem{}), true},
		{"listReport", reflect.TypeOf(hpaanalysis.ListReport{}), true},
		// The analysis contract tracks the FlatAnalysis projection (the v1
		// wire shape emitted via Analysis.MarshalJSON), not the Analysis
		// storage type whose field tags no longer drive serialization.
		{"analysis", reflect.TypeOf(hpaanalysis.FlatAnalysis{}), false},
		{"listItem", reflect.TypeOf(hpaanalysis.ListItem{}), true},
		{"structuredEntry", reflect.TypeOf(hpaanalysis.StructuredMessage{}), false},
		{"decisionSignal", reflect.TypeOf(hpaanalysis.DecisionSignal{}), false},
		{"impactMetric", reflect.TypeOf(hpaanalysis.MetricImpactGuess{}), false},
	}
	for _, tc := range cases {
		def, ok := schema.Defs[tc.def]
		if !ok {
			t.Errorf("docs/output-schema.json is missing $defs.%s", tc.def)
			continue
		}
		tags := jsonTagSet(t, tc.typ)
		for prop := range def.Properties {
			if _, ok := tags[prop]; !ok {
				t.Errorf("$defs.%s documents property %q but %s has no matching json tag", tc.def, prop, tc.typ)
			}
		}
		if tc.exact {
			for tag := range tags {
				if _, ok := def.Properties[tag]; !ok {
					t.Errorf("%s emits property %q but $defs.%s does not document it", tc.typ, tag, tc.def)
				}
			}
		}
		for _, req := range def.Required {
			if _, ok := def.Properties[req]; !ok {
				t.Errorf("$defs.%s requires %q but does not define it in properties", tc.def, req)
			}
		}
	}
}

// TestOutputSchemaAPIVersionMatchesCode derives the output variants from the
// schema's top-level oneOf and ensures each pins the same version emitted by the
// code. This catches a SchemaVersion bump that forgets the published schema.
func TestOutputSchemaAPIVersionMatchesCode(t *testing.T) {
	assertOutputSchemaVersion(t, loadOutputSchema(t), hpaanalysis.SchemaVersion)
}

func assertOutputSchemaVersion(t *testing.T, schema outputSchema, want string) {
	t.Helper()
	const refPrefix = "#/$defs/"
	for _, variant := range schema.OneOf {
		if !strings.HasPrefix(variant.Ref, refPrefix) {
			t.Errorf("top-level output variant has unsupported reference %q", variant.Ref)
			continue
		}
		name := strings.TrimPrefix(variant.Ref, refPrefix)
		def, ok := schema.Defs[name]
		if !ok {
			t.Errorf("top-level output variant references missing definition %q", name)
			continue
		}
		apiVersion, ok := def.Properties["apiVersion"]
		if !ok {
			t.Errorf("$defs.%s does not document apiVersion", name)
			continue
		}
		if apiVersion.Const != want {
			t.Errorf("$defs.%s apiVersion const=%q, want %q", name, apiVersion.Const, want)
		}
	}
}

func TestOutputSchemaV2MatchesProjectionTypes(t *testing.T) {
	schema := loadOutputSchemaFile(t, "docs/output-schema-v2.json")
	assertOutputSchemaVersion(t, schema, hpaanalysis.SchemaVersionV2)

	cases := []struct {
		def string
		typ reflect.Type
	}{
		{"statusReportV2", reflect.TypeOf(hpaanalysis.StatusReportV2{})},
		{"statusBatchV2", reflect.TypeOf(hpaanalysis.StatusBatchV2{})},
		{"statusBatchItemV2", reflect.TypeOf(hpaanalysis.StatusBatchItemV2{})},
		{"groupedAnalysis", reflect.TypeOf(hpaanalysis.GroupedAnalysis{})},
		{"meta", reflect.TypeOf(hpaanalysis.MetaView{})},
		{"replicas", reflect.TypeOf(hpaanalysis.ReplicasView{})},
		{"decision", reflect.TypeOf(hpaanalysis.DecisionView{})},
		{"metricsGroup", reflect.TypeOf(hpaanalysis.MetricsView{})},
		{"conditionsGroup", reflect.TypeOf(hpaanalysis.ConditionsView{})},
		{"capacity", reflect.TypeOf(hpaanalysis.CapacityView{})},
		{"scaleToZeroGroup", reflect.TypeOf(hpaanalysis.ScaleToZeroView{})},
		{"stability", reflect.TypeOf(hpaanalysis.StabilityView{})},
		{"advisory", reflect.TypeOf(hpaanalysis.AdvisoryView{})},
		{"controllers", reflect.TypeOf(hpaanalysis.ControllersView{})},
		{"blockers", reflect.TypeOf(hpaanalysis.BlockersView{})},
		{"actions", reflect.TypeOf(hpaanalysis.ActionsView{})},
		{"lifecycle", reflect.TypeOf(hpaanalysis.LifecycleView{})},
	}
	for _, tc := range cases {
		t.Run(tc.def, func(t *testing.T) {
			def, ok := schema.Defs[tc.def]
			if !ok {
				t.Fatalf("docs/output-schema-v2.json is missing $defs.%s", tc.def)
			}
			tags := jsonTagSet(t, tc.typ)
			for property := range def.Properties {
				if _, ok := tags[property]; !ok {
					t.Errorf("$defs.%s documents %q but %s has no matching json tag", tc.def, property, tc.typ)
				}
			}
			for tag := range tags {
				if _, ok := def.Properties[tag]; !ok {
					t.Errorf("%s emits %q but $defs.%s does not document it", tc.typ, tag, tc.def)
				}
			}
			for _, required := range def.Required {
				if _, ok := def.Properties[required]; !ok {
					t.Errorf("$defs.%s requires undefined property %q", tc.def, required)
				}
			}
		})
	}
}

func decodeSerializedJSON(t *testing.T, value any) any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %T: %v", value, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("decode serialized %T: %v", value, err)
	}
	return decoded
}

func validateOutputSchemaNode(schema outputSchema, node outputSchemaNode, value any, path string) []string {
	if node.Ref != "" {
		const prefix = "#/$defs/"
		if !strings.HasPrefix(node.Ref, prefix) {
			return []string{fmt.Sprintf("%s: unsupported schema reference %q", path, node.Ref)}
		}
		name := strings.TrimPrefix(node.Ref, prefix)
		definition, ok := schema.Defs[name]
		if !ok {
			return []string{fmt.Sprintf("%s: schema reference %q is undefined", path, node.Ref)}
		}
		return validateOutputSchemaNode(schema, definition, value, path)
	}

	if len(node.OneOf) > 0 {
		matches := 0
		var failures []string
		for i, candidate := range node.OneOf {
			errs := validateOutputSchemaNode(schema, candidate, value, path)
			if len(errs) == 0 {
				matches++
				continue
			}
			failures = append(failures, fmt.Sprintf("alternative %d: %s", i+1, errs[0]))
		}
		if matches != 1 {
			return []string{fmt.Sprintf("%s: oneOf matched %d alternatives (want exactly one); %s", path, matches, strings.Join(failures, "; "))}
		}
	}

	if node.Not != nil {
		if errs := validateOutputSchemaNode(schema, *node.Not, value, path); len(errs) == 0 {
			return []string{fmt.Sprintf("%s: value matches a forbidden schema", path)}
		}
	}

	if len(node.Enum) > 0 {
		matched := false
		for _, allowed := range node.Enum {
			if reflect.DeepEqual(value, allowed) {
				matched = true
				break
			}
		}
		if !matched {
			return []string{fmt.Sprintf("%s: value %#v is not in enum %v", path, value, node.Enum)}
		}
	}

	if node.Const != "" && value != node.Const {
		return []string{fmt.Sprintf("%s: value %#v does not equal const %q", path, value, node.Const)}
	}

	nodeType := node.Type
	// JSON Schema object keywords apply even when a subschema omits an
	// explicit type (for example inside oneOf/not conditions).
	if nodeType == "" && (len(node.Properties) > 0 || len(node.Required) > 0 || node.AdditionalProperties != nil) {
		nodeType = "object"
	}

	switch nodeType {
	case "":
		return nil
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: got %T, want object", path, value)}
		}
		var errs []string
		for _, required := range node.Required {
			if _, exists := object[required]; !exists {
				errs = append(errs, fmt.Sprintf("%s: missing required property %q", path, required))
			}
		}
		for name, property := range node.Properties {
			if propertyValue, exists := object[name]; exists {
				errs = append(errs, validateOutputSchemaNode(schema, property, propertyValue, path+"."+name)...)
			}
		}
		if node.AdditionalProperties != nil && !*node.AdditionalProperties {
			for name := range object {
				if _, documented := node.Properties[name]; !documented {
					errs = append(errs, fmt.Sprintf("%s: undocumented property %q", path, name))
				}
			}
		}
		return errs
	case "array":
		array, ok := value.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s: got %T, want array", path, value)}
		}
		if node.MinItems != nil && len(array) < *node.MinItems {
			return []string{fmt.Sprintf("%s: array has %d items, below minItems %d", path, len(array), *node.MinItems)}
		}
		if node.Items == nil {
			return nil
		}
		var errs []string
		for i, item := range array {
			errs = append(errs, validateOutputSchemaNode(schema, *node.Items, item, fmt.Sprintf("%s[%d]", path, i))...)
		}
		return errs
	case "string":
		stringValue, ok := value.(string)
		if !ok {
			return []string{fmt.Sprintf("%s: got %T, want string", path, value)}
		}
		if node.MinLength != nil && utf8.RuneCountInString(stringValue) < *node.MinLength {
			return []string{fmt.Sprintf("%s: string length is below minLength %d", path, *node.MinLength)}
		}
		return nil
	case "boolean":
		if _, ok := value.(bool); !ok {
			return []string{fmt.Sprintf("%s: got %T, want boolean", path, value)}
		}
		return nil
	case "null":
		if value != nil {
			return []string{fmt.Sprintf("%s: got %T, want null", path, value)}
		}
		return nil
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return []string{fmt.Sprintf("%s: got %T, want integer", path, value)}
		}
		if _, err := strconv.ParseInt(number.String(), 10, 64); err != nil {
			return []string{fmt.Sprintf("%s: value %q is not an integer", path, number)}
		}
		return validateOutputNumberBounds(node, number, path)
	case "number":
		number, ok := value.(json.Number)
		if !ok {
			return []string{fmt.Sprintf("%s: got %T, want number", path, value)}
		}
		return validateOutputNumberBounds(node, number, path)
	default:
		return []string{fmt.Sprintf("%s: unsupported schema type %q", path, node.Type)}
	}
}

func validateOutputNumberBounds(node outputSchemaNode, number json.Number, path string) []string {
	value, err := strconv.ParseFloat(number.String(), 64)
	if err != nil {
		return []string{fmt.Sprintf("%s: invalid number %q", path, number)}
	}
	if node.Minimum != nil && value < *node.Minimum {
		return []string{fmt.Sprintf("%s: value %v is below minimum %v", path, value, *node.Minimum)}
	}
	if node.Maximum != nil && value > *node.Maximum {
		return []string{fmt.Sprintf("%s: value %v is above maximum %v", path, value, *node.Maximum)}
	}
	return nil
}

func TestOutputSchemaValidatesSerializedOutputVariants(t *testing.T) {
	schema := loadOutputSchema(t)
	analysis := *hpaanalysis.NewAnalysis(hpaanalysis.FlatAnalysis{
		Namespace:   "default",
		Name:        "web",
		Target:      "Deployment/web",
		Current:     3,
		Desired:     4,
		Min:         1,
		Max:         10,
		Health:      string(hpaanalysis.HealthOK),
		HealthScore: 90,
		Summary:     "HPA is healthy",
		Conditions: []hpaanalysis.Condition{{
			Type:    "AbleToScale",
			Status:  "True",
			Reason:  "ReadyForNewScale",
			Message: "recommended size matches current size",
		}},
		Metrics: []hpaanalysis.Metric{{
			Type: "Resource",
			Name: "cpu",
			Text: "50% / 70%",
		}},
		StructuredInterpretation: []hpaanalysis.StructuredMessage{{
			Reason:         "MetricWinnerUnknown",
			Message:        "the controller does not expose its internal winner",
			Severity:       hpaanalysis.SeverityInfo,
			Confidence:     hpaanalysis.ConfidenceLow,
			Classification: hpaanalysis.ClassificationUnknown,
		}},
		DecisionSignals: []hpaanalysis.DecisionSignal{{
			Reason:         "EstimatedWinner",
			Message:        "best effort only",
			Confidence:     string(hpaanalysis.ConfidenceLow),
			Classification: string(hpaanalysis.ClassificationUnknown),
			AdapterVersion: "estimation-v1",
		}},
		ImpactMetric: &hpaanalysis.MetricImpactGuess{
			Name:       "cpu",
			Ratio:      0.7,
			Note:       "inferred from visible metrics",
			Confidence: string(hpaanalysis.ConfidenceLow),
		},
	})
	status := hpaanalysis.StatusReport{
		APIVersion: hpaanalysis.SchemaVersion,
		Analysis:   analysis,
		Events: []hpaanalysis.Event{{
			Reason:  "SuccessfulRescale",
			Message: "New size: 4",
		}},
	}
	statusForBatch := status

	samples := []struct {
		name  string
		value any
	}{
		{
			name:  "single status",
			value: status,
		},
		{
			name: "status batch",
			value: hpaanalysis.StatusBatch{
				APIVersion: hpaanalysis.SchemaVersion,
				Items: []hpaanalysis.StatusBatchItem{
					{
						Namespace: "default",
						Name:      "web",
						Status:    hpaanalysis.BatchStatusOK,
						Report:    &statusForBatch,
					},
					{
						Namespace: "default",
						Name:      "missing",
						Status:    hpaanalysis.BatchStatusError,
						Error:     "horizontalpodautoscaler not found",
					},
				},
			},
		},
		{
			name: "list",
			value: hpaanalysis.ListReport{
				APIVersion: hpaanalysis.SchemaVersion,
				Items: []hpaanalysis.ListItem{{
					Namespace:   "default",
					Name:        "web",
					Target:      "Deployment/web",
					Current:     3,
					Desired:     4,
					Min:         1,
					Max:         10,
					Summary:     "HPA is healthy",
					Health:      string(hpaanalysis.HealthOK),
					HealthScore: 90,
				}},
			},
		},
		{
			name: "empty list",
			value: hpaanalysis.ListReport{
				APIVersion: hpaanalysis.SchemaVersion,
				Items:      []hpaanalysis.ListItem{},
			},
		},
	}

	topLevel := outputSchemaNode{OneOf: schema.OneOf}
	for _, sample := range samples {
		t.Run(sample.name, func(t *testing.T) {
			serialized := decodeSerializedJSON(t, sample.value)
			if errs := validateOutputSchemaNode(schema, topLevel, serialized, "$"); len(errs) > 0 {
				t.Fatalf("serialized %T does not satisfy docs/output-schema.json:\n%s", sample.value, strings.Join(errs, "\n"))
			}
		})
	}
}

func TestOutputSchemaV2ValidatesProjectedOutputVariants(t *testing.T) {
	schema := loadOutputSchemaFile(t, "docs/output-schema-v2.json")
	status := hpaanalysis.StatusReport{
		APIVersion: hpaanalysis.SchemaVersion,
		Analysis: *hpaanalysis.NewAnalysis(hpaanalysis.FlatAnalysis{
			Namespace:   "default",
			Name:        "web",
			Target:      "Deployment/web",
			Current:     3,
			Desired:     4,
			Min:         1,
			Max:         10,
			Health:      string(hpaanalysis.HealthOK),
			HealthScore: 90,
			Summary:     "HPA is healthy",
			Conditions:  []hpaanalysis.Condition{{Type: "AbleToScale", Status: "True"}},
			Metrics:     []hpaanalysis.Metric{{Type: "Resource", Name: "cpu", Text: "50% / 70%"}},
		}),
		Events: []hpaanalysis.Event{{Reason: "SuccessfulRescale", Message: "New size: 4"}},
	}
	batchReport := status
	batch := hpaanalysis.StatusBatch{
		APIVersion: hpaanalysis.SchemaVersion,
		Items: []hpaanalysis.StatusBatchItem{
			{
				Namespace: "default",
				Name:      "web",
				Status:    hpaanalysis.BatchStatusOK,
				Report:    &batchReport,
			},
			{
				Namespace: "default",
				Name:      "missing",
				Status:    hpaanalysis.BatchStatusError,
				Error:     "horizontalpodautoscaler not found",
			},
		},
	}

	samples := []struct {
		name  string
		value any
	}{
		{name: "single status v2", value: hpaanalysis.ProjectStatusReportV2(status)},
		{name: "status batch v2", value: hpaanalysis.ProjectStatusBatchV2(batch)},
		{name: "status record v2", value: hpaanalysis.ProjectStatusRecordV2(status)},
		{name: "error record v2", value: hpaanalysis.StatusRecordV2{
			APIVersion: hpaanalysis.SchemaVersionV2,
			Namespace:  "default",
			Name:       "missing",
			Status:     hpaanalysis.StatusRecordErrorV2,
			Error:      "not found",
		}},
	}
	topLevel := outputSchemaNode{OneOf: schema.OneOf}
	for _, sample := range samples {
		t.Run(sample.name, func(t *testing.T) {
			serialized := decodeSerializedJSON(t, sample.value)
			if errs := validateOutputSchemaNode(schema, topLevel, serialized, "$"); len(errs) > 0 {
				t.Fatalf("serialized %T does not satisfy docs/output-schema-v2.json:\n%s", sample.value, strings.Join(errs, "\n"))
			}
		})
	}
}
