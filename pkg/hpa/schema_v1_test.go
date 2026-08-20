package hpa

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// exportedFields returns the exported fields of a struct type in declaration
// order, carrying name, type, and the json/yaml tags.
type fieldSpec struct {
	name string
	typ  reflect.Type
	json string
	yaml string
}

func exportedFields(t *testing.T, typ reflect.Type) []fieldSpec {
	t.Helper()
	if typ.Kind() != reflect.Struct {
		t.Fatalf("%s is not a struct", typ)
	}
	var out []fieldSpec
	for i := range typ.NumField() {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		out = append(out, fieldSpec{
			name: f.Name,
			typ:  f.Type,
			json: f.Tag.Get("json"),
			yaml: f.Tag.Get("yaml"),
		})
	}
	return out
}

// TestFlatAnalysisMirrorsAnalysis guards the storage-flip precondition: the
// flat v1 DTO must declare exactly the exported fields of Analysis, in the
// same order, with the same tags and types, so the v1 wire bytes emitted via
// FlatAnalysis are identical to the historical Analysis-based output. New
// Analysis fields must be added to FlatAnalysis (and to a group view) in the
// same commit.
func TestFlatAnalysisMirrorsAnalysis(t *testing.T) {
	want := exportedFields(t, reflect.TypeOf(Analysis{}))
	got := exportedFields(t, reflect.TypeOf(FlatAnalysis{}))

	if len(want) != len(got) {
		t.Fatalf("FlatAnalysis has %d exported fields, Analysis has %d; they must mirror exactly", len(got), len(want))
	}
	for i := range want {
		if want[i].name != got[i].name {
			t.Errorf("field %d: name %q, want %q (declaration order must match for stable JSON key order)", i, got[i].name, want[i].name)
		}
		if want[i].typ != got[i].typ {
			t.Errorf("field %s: type %v, want %v", want[i].name, got[i].typ, want[i].typ)
		}
		if want[i].json != got[i].json || want[i].yaml != got[i].yaml {
			t.Errorf("field %s: tags json=%q yaml=%q, want json=%q yaml=%q", want[i].name, got[i].json, got[i].yaml, want[i].json, want[i].yaml)
		}
	}
}

// fillDeep populates every settable exported field with a distinctive
// non-zero value so round-trip comparisons cannot pass on accident. External
// scalar structs (metav1.Time) get dedicated values; structs whose fields are
// all unexported are left zero.
func fillDeep(v reflect.Value, path string) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		fillDeep(v.Elem(), path)
	case reflect.Slice:
		elem := reflect.MakeSlice(v.Type(), 1, 1)
		fillDeep(elem.Index(0), path+"[0]")
		v.Set(elem)
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(metav1.Time{}) {
			// Second-precision UTC: the v1/v2 wire formats both serialize
			// RFC3339 without sub-second precision.
			v.Set(reflect.ValueOf(metav1.NewTime(time.Date(2026, 8, 20, 9, 30, 0, 0, time.UTC))))
			return
		}
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() || !v.Field(i).CanSet() {
				continue
			}
			fillDeep(v.Field(i), path+"."+f.Name)
		}
	case reflect.String:
		v.SetString("v-" + path)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(7)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5)
	}
}

// filledAnalysis builds an Analysis with every exported field populated.
func filledAnalysis() Analysis {
	var a Analysis
	fillDeep(reflect.ValueOf(&a).Elem(), "a")
	return a
}

// TestFlatRoundTripsGroupedViews verifies Analysis.Flat() reproduces the flat
// fields field-for-field after the detour through Grouped(). This is the
// inverse mapping the storage flip depends on: once the grouped views become
// the primary storage, Flat() must keep producing these exact values.
func TestFlatRoundTripsGroupedViews(t *testing.T) {
	src := filledAnalysis()

	flat := src.Flat()

	srcFields := exportedFields(t, reflect.TypeOf(Analysis{}))
	flatValue := reflect.ValueOf(flat)
	srcValue := reflect.ValueOf(src)
	for _, spec := range srcFields {
		want := srcValue.FieldByName(spec.name)
		got := flatValue.FieldByName(spec.name)
		if !want.IsValid() || !got.IsValid() {
			t.Fatalf("field %s missing on one side", spec.name)
		}
		if !reflect.DeepEqual(want.Interface(), got.Interface()) {
			t.Errorf("Flat().%s = %+v, want %+v", spec.name, got.Interface(), want.Interface())
		}
	}
}

// TestProjectStatusReportV1IsByteIdentical pins the wire guarantee the flip
// must preserve: the v1 projection serializes to exactly the bytes the
// Analysis-based StatusReport produces, for both populated and empty shapes.
func TestProjectStatusReportV1IsByteIdentical(t *testing.T) {
	reports := []StatusReport{
		{APIVersion: SchemaVersion, Analysis: filledAnalysis()},
		{APIVersion: SchemaVersion},
		{APIVersion: SchemaVersion, Analysis: filledAnalysis(), Events: []Event{{Reason: "SuccessfulRescale", Message: "New size: 5"}}},
	}

	for i, report := range reports {
		want, err := json.Marshal(report)
		if err != nil {
			t.Fatalf("report %d: marshal Analysis-based report: %v", i, err)
		}
		got, err := json.Marshal(ProjectStatusReportV1(report))
		if err != nil {
			t.Fatalf("report %d: marshal v1 projection: %v", i, err)
		}
		if string(want) != string(got) {
			t.Errorf("report %d: v1 projection bytes diverge:\nwant: %s\ngot:  %s", i, want, got)
		}
	}
}

// TestFlatOfNilAnalysisKeepsZeroShape documents that nil reports project to
// the zero flat value rather than panicking, matching Analysis's other
// nil-tolerant accessors.
func TestFlatOfNilAnalysisKeepsZeroShape(t *testing.T) {
	var a *Analysis
	if got := a.Flat(); got.Namespace != "" || got.Current != 0 {
		t.Errorf("nil Analysis Flat() = %+v, want zero value", got)
	}
}
