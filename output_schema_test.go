package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// outputSchema mirrors the parts of docs/output-schema.json the consistency
// test needs: per-definition property names and required lists.
type outputSchema struct {
	Defs map[string]struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	} `json:"$defs"`
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
		{"analysis", reflect.TypeOf(hpaanalysis.Analysis{})},
		{"listItem", reflect.TypeOf(hpaanalysis.ListItem{})},
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
