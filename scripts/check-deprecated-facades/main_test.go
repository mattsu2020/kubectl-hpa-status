package main

import "testing"

func TestScanSourceFindsDeprecatedFacadeSelectors(t *testing.T) {
	source := []byte(`package sample

import (
	legacy "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	oldtrend "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
)

func use() {
	_ = legacy.KEDAAnalysis{}
	_ = legacy.WriteHTMLListReport
	_ = oldtrend.HealthTrendResult{}
}
`)

	violations, err := scanSource("sample.go", source)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 3 {
		t.Fatalf("got %d violations, want 3: %+v", len(violations), violations)
	}
	want := []string{
		"legacy.KEDAAnalysis",
		"legacy.WriteHTMLListReport",
		"oldtrend.HealthTrendResult",
	}
	for i := range want {
		if violations[i].symbol != want[i] {
			t.Fatalf("violation[%d] = %q, want %q", i, violations[i].symbol, want[i])
		}
	}
}

func TestScanSourceAllowsCanonicalPackagesAndUnrelatedRootSymbols(t *testing.T) {
	source := []byte(`package sample

import (
	hpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/render"
)

func use() {
	_ = hpa.Analysis{}
	_ = keda.Analysis{}
	_ = render.WriteHTMLListReport
}
`)

	violations, err := scanSource("sample.go", source)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("canonical APIs must be allowed: %+v", violations)
	}
}

func TestScanSourceRejectsDotImportOfFacadePackage(t *testing.T) {
	source := []byte(`package sample
import . "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
`)

	violations, err := scanSource("sample.go", source)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || !violations[0].dotImport {
		t.Fatalf("dot import should be rejected: %+v", violations)
	}
}
