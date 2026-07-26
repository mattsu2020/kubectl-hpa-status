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

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

type outputSchemaNode struct {
	Ref        string                      `json:"$ref"`
	Type       string                      `json:"type"`
	Properties map[string]outputSchemaNode `json:"properties"`
	Required   []string                    `json:"required"`
	Items      *outputSchemaNode           `json:"items"`
	Enum       []any                       `json:"enum"`
	OneOf      []outputSchemaNode          `json:"oneOf"`
	Minimum    *float64                    `json:"minimum"`
	Maximum    *float64                    `json:"maximum"`
	MinItems   *int                        `json:"minItems"`
}

// outputSchema contains both the documented definitions and the top-level
// union used by real JSON output.
type outputSchema struct {
	OneOf []outputSchemaNode          `json:"oneOf"`
	Defs  map[string]outputSchemaNode `json:"$defs"`
}

func loadOutputSchema(t *testing.T) outputSchema {
	t.Helper()
	data, err := os.ReadFile("docs/output-schema.json")
	if err != nil {
		t.Fatalf("read docs/output-schema.json: %v", err)
	}
	var schema outputSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("parse docs/output-schema.json: %v", err)
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

// TestOutputSchemaFieldsExistInStructs verifies that every property documented
// in docs/output-schema.json still exists as a JSON field on the Go structs
// that produce the output. The schema documents the stable core subset (the
// structs may emit additional fields), so the check is one-directional:
// schema ⊆ struct. A failure means a documented field was renamed or removed,
// which is a breaking change for JSON consumers and requires either restoring
// the field or updating the schema plus bumping hpaanalysis.SchemaVersion.
func TestOutputSchemaFieldsExistInStructs(t *testing.T) {
	schema := loadOutputSchema(t)

	cases := []struct {
		def string
		typ reflect.Type
	}{
		{"statusReport", reflect.TypeOf(hpaanalysis.StatusReport{})},
		{"statusBatch", reflect.TypeOf(hpaanalysis.StatusBatch{})},
		{"statusBatchItem", reflect.TypeOf(hpaanalysis.StatusBatchItem{})},
		{"listReport", reflect.TypeOf(hpaanalysis.ListReport{})},
		{"analysis", reflect.TypeOf(hpaanalysis.Analysis{})},
		{"listItem", reflect.TypeOf(hpaanalysis.ListItem{})},
		{"structuredEntry", reflect.TypeOf(hpaanalysis.StructuredMessage{})},
		{"decisionSignal", reflect.TypeOf(hpaanalysis.DecisionSignal{})},
		{"impactMetric", reflect.TypeOf(hpaanalysis.MetricImpactGuess{})},
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
		for _, req := range def.Required {
			if _, ok := def.Properties[req]; !ok {
				t.Errorf("$defs.%s requires %q but does not define it in properties", tc.def, req)
			}
		}
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
		return nil
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

	switch node.Type {
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
		if _, ok := value.(string); !ok {
			return []string{fmt.Sprintf("%s: got %T, want string", path, value)}
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
	analysis := hpaanalysis.Analysis{
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
	}
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
