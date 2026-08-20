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

// TestFlatAccessorSurface guards the storage-flip compatibility contract:
// every FlatAnalysis wire field must have a same-named getter and a Set*
// setter on Analysis with matching types, so the retired flat fields stay
// reachable (with parentheses) for callers and the v1 surface cannot drift
// from the projection. Adding a FlatAnalysis field without accessors (or
// vice versa) fails here.
func TestFlatAccessorSurface(t *testing.T) {
	analysisType := reflect.TypeOf(&Analysis{})
	for _, spec := range exportedFields(t, reflect.TypeOf(FlatAnalysis{})) {
		getter, ok := analysisType.MethodByName(spec.name)
		if !ok {
			t.Errorf("Analysis has no getter %s() for FlatAnalysis field %s", spec.name, spec.name)
			continue
		}
		getterType := getter.Type
		if getterType.NumIn() != 1 || getterType.NumOut() != 1 || getterType.Out(0) != spec.typ {
			t.Errorf("getter %s has signature %s, want (receiver) () %s", spec.name, getterType, spec.typ)
		}
		setter, ok := analysisType.MethodByName("Set" + spec.name)
		if !ok {
			t.Errorf("Analysis has no setter Set%s() for FlatAnalysis field %s", spec.name, spec.name)
			continue
		}
		setterType := setter.Type
		if setterType.NumIn() != 2 || setterType.In(1) != spec.typ || setterType.NumOut() != 0 {
			t.Errorf("setter Set%s has signature %s, want (receiver) (%s)", spec.name, setterType, spec.typ)
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

// filledFlat builds a FlatAnalysis with every field populated.
func filledFlat() FlatAnalysis {
	var f FlatAnalysis
	fillDeep(reflect.ValueOf(&f).Elem(), "f")
	return f
}

// TestNewAnalysisFlatRoundTrip is the core storage-fidelity guarantee of the
// flip: constructing Analysis from a flat fixture and projecting back must
// reproduce the fixture field-for-field. Flat() inverts the grouped storage,
// so any field routed to (or read from) the wrong group fails here.
func TestNewAnalysisFlatRoundTrip(t *testing.T) {
	fixture := filledFlat()

	got := NewAnalysis(fixture).Flat()

	fields := exportedFields(t, reflect.TypeOf(FlatAnalysis{}))
	gotValue := reflect.ValueOf(got)
	fixtureValue := reflect.ValueOf(fixture)
	if len(fields) == 0 {
		t.Fatal("FlatAnalysis unexpectedly has no exported fields")
	}
	for _, spec := range fields {
		want := fixtureValue.FieldByName(spec.name)
		have := gotValue.FieldByName(spec.name)
		if !reflect.DeepEqual(want.Interface(), have.Interface()) {
			t.Errorf("round trip %s = %+v, want %+v", spec.name, have.Interface(), want.Interface())
		}
	}
}

// TestAccessorsReturnFixtureValues verifies each getter reads the storage the
// constructor populated — the in-tree read path after the flip.
func TestAccessorsReturnFixtureValues(t *testing.T) {
	fixture := filledFlat()
	a := NewAnalysis(fixture)
	analysisType := reflect.TypeOf(&Analysis{})
	fixtureValue := reflect.ValueOf(fixture)

	for _, spec := range exportedFields(t, reflect.TypeOf(FlatAnalysis{})) {
		method, ok := analysisType.MethodByName(spec.name)
		if !ok {
			t.Fatalf("missing getter %s", spec.name)
		}
		got := method.Func.Call([]reflect.Value{reflect.ValueOf(a)})[0]
		want := fixtureValue.FieldByName(spec.name)
		if !reflect.DeepEqual(got.Interface(), want.Interface()) {
			t.Errorf("getter %s() = %+v, want %+v", spec.name, got.Interface(), want.Interface())
		}
	}
}

// TestProjectStatusReportV1IsByteIdentical pins the wire guarantee the flip
// must preserve: the v1 projection serializes to exactly the bytes a report
// carrying the same storage serializes to directly, for both populated and
// empty shapes.
func TestProjectStatusReportV1IsByteIdentical(t *testing.T) {
	reports := []StatusReport{
		{APIVersion: SchemaVersion, Analysis: *NewAnalysis(filledFlat())},
		{APIVersion: SchemaVersion},
		{APIVersion: SchemaVersion, Analysis: *NewAnalysis(filledFlat()), Events: []Event{{Reason: "SuccessfulRescale", Message: "New size: 5"}}},
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

// TestAnalysisJSONRoundTripDecodes verifies the decode side of the wire
// contract: v1 JSON unmarshaled into Analysis (as external consumers and
// in-tree tests do) re-serializes to identical bytes. Fields the wire never
// carried (json:"-" runtime fields inside domain types) are not expected to
// survive; byte stability is the contract.
func TestAnalysisJSONRoundTripDecodes(t *testing.T) {
	fixture := filledFlat()
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	var decoded Analysis
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal into Analysis: %v", err)
	}

	remarshaled, err := json.Marshal(decoded.Flat())
	if err != nil {
		t.Fatalf("remarshal decoded analysis: %v", err)
	}
	if string(remarshaled) != string(data) {
		t.Errorf("decode/encode is not byte-stable:\nwant: %s\ngot:  %s", data, remarshaled)
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
