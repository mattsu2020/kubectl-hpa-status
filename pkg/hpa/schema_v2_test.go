package hpa

import (
	"encoding/json"
	"testing"
)

func TestProjectStatusReportV2UsesNestedSchemaWithoutMutatingV1(t *testing.T) {
	report := StatusReport{
		APIVersion: SchemaVersion,
		Analysis: *NewAnalysis(FlatAnalysis{
			Namespace:   "default",
			Name:        "web",
			Current:     2,
			Desired:     3,
			Health:      "OK",
			HealthScore: 100,
		}),
		Events: []Event{{Reason: "SuccessfulRescale"}},
	}

	projected := ProjectStatusReportV2(report)
	if projected.APIVersion != SchemaVersionV2 {
		t.Fatalf("apiVersion = %q", projected.APIVersion)
	}
	if projected.Analysis.Meta.Namespace != "default" || projected.Analysis.Replicas.Desired != 3 {
		t.Fatalf("nested projection = %#v", projected.Analysis)
	}
	projected.Events[0].Reason = "changed"
	if report.Events[0].Reason != "SuccessfulRescale" {
		t.Fatal("projection mutated the v1 event slice")
	}

	data, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	analysis, ok := decoded["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis JSON = %#v", decoded["analysis"])
	}
	if _, flat := analysis["currentReplicas"]; flat {
		t.Fatalf("v2 unexpectedly emitted flat v1 field: %s", data)
	}
	if _, nested := analysis["replicas"]; !nested {
		t.Fatalf("v2 missing replicas group: %s", data)
	}
}

func TestProjectStatusReportV2UsesFrozenCanonicalSnapshot(t *testing.T) {
	report := StatusReport{APIVersion: SchemaVersion, Analysis: *NewAnalysis(FlatAnalysis{Name: "before", Health: "OK"})}
	report.FreezeCanonical()
	report.Analysis.SetName("legacy-mutated")
	projected := ProjectStatusReportV2(report)
	if projected.Analysis.Meta.Name != "before" {
		t.Fatalf("canonical name = %q, want frozen value", projected.Analysis.Meta.Name)
	}
}

func TestProjectStatusBatchV2PreservesErrors(t *testing.T) {
	report := StatusReport{APIVersion: SchemaVersion, Analysis: *NewAnalysis(FlatAnalysis{Name: "ok"})}
	batch := StatusBatch{APIVersion: SchemaVersion, Items: []StatusBatchItem{
		{Name: "ok", Status: BatchStatusOK, Report: &report},
		{Name: "missing", Status: BatchStatusError, Error: "not found"},
	}}

	projected := ProjectStatusBatchV2(batch)
	if projected.APIVersion != SchemaVersionV2 {
		t.Fatalf("apiVersion = %q", projected.APIVersion)
	}
	if projected.Items[0].Report == nil || projected.Items[0].Report.APIVersion != SchemaVersionV2 {
		t.Fatalf("successful item = %#v", projected.Items[0])
	}
	if projected.Items[1].Error != "not found" || projected.Items[1].Report != nil {
		t.Fatalf("failed item = %#v", projected.Items[1])
	}
}

func TestProjectStatusRecordV2UsesStableSuccessEnvelope(t *testing.T) {
	report := StatusReport{
		APIVersion: SchemaVersion,
		Analysis: *NewAnalysis(FlatAnalysis{
			Namespace: "default",
			Name:      "web",
		}),
	}

	record := ProjectStatusRecordV2(report)
	if record.APIVersion != SchemaVersionV2 ||
		record.Namespace != "default" ||
		record.Name != "web" ||
		record.Status != StatusRecordSuccessV2 ||
		record.Report == nil ||
		record.Error != "" {
		t.Fatalf("ProjectStatusRecordV2() = %#v", record)
	}
}

func TestProjectStatusRecordsV2PreservesPartialFailures(t *testing.T) {
	report := StatusReport{
		APIVersion: SchemaVersion,
		Analysis: *NewAnalysis(FlatAnalysis{
			Namespace: "default",
			Name:      "web",
		}),
	}
	batch := StatusBatch{APIVersion: SchemaVersion, Items: []StatusBatchItem{
		{Namespace: "default", Name: "web", Status: BatchStatusOK, Report: &report},
		{Namespace: "default", Name: "missing", Status: BatchStatusError, Error: "not found"},
	}}

	records := ProjectStatusRecordsV2(batch)
	if len(records) != 2 {
		t.Fatalf("ProjectStatusRecordsV2() returned %d records", len(records))
	}
	if records[0].Status != StatusRecordSuccessV2 || records[0].Report == nil || records[0].Error != "" {
		t.Fatalf("successful record = %#v", records[0])
	}
	if records[1].Status != StatusRecordErrorV2 || records[1].Report != nil || records[1].Error != "not found" {
		t.Fatalf("failed record = %#v", records[1])
	}
}
