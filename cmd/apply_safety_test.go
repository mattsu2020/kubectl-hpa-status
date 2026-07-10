package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
)

func TestNewTUIApplyCallbacksRequiresExplicitPersistentMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		apply    bool
		dryRun   bool
		wantLive bool
	}{
		{name: "default read only", apply: false, dryRun: true, wantLive: false},
		{name: "apply still defaults to dry run", apply: true, dryRun: true, wantLive: false},
		{name: "explicit persistent mode", apply: true, dryRun: false, wantLive: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &options{Common: commonOptions{Apply: tt.apply, DryRun: tt.dryRun}}
			live, dryRun := newTUIApplyCallbacks(opts)
			if (live != nil) != tt.wantLive {
				t.Fatalf("live callback presence=%v, want %v", live != nil, tt.wantLive)
			}
			if dryRun == nil {
				t.Fatal("server-side dry-run callback must always be available")
			}
		})
	}
}

func TestTUIDryRunCallbackNeverSendsLivePatch(t *testing.T) {
	t.Parallel()
	hpa := testutil.BuildHPA("default", "web", testutil.WithMinMax(1, 10))
	fakeClient := testutil.NewFakeClient(hpa)
	opts := &options{Common: commonOptions{
		ClientOverride: fakeClient,
		Apply:          true,
		DryRun:         false, // callback must override even a persistent caller
	}}
	_, dryRun := newTUIApplyCallbacks(opts)
	err := dryRun(context.Background(), "default", "web", []hpaanalysis.Suggestion{{
		Title: "raise max",
		Patch: `{"spec":{"maxReplicas":20}}`,
		Apply: true,
	}})
	if err != nil {
		t.Fatal(err)
	}

	patches := 0
	for _, action := range fakeClient.Actions() {
		patchAction, ok := action.(interface{ GetPatchOptions() metav1.PatchOptions })
		if !ok || action.GetVerb() != "patch" {
			continue
		}
		patches++
		options := patchAction.GetPatchOptions()
		if len(options.DryRun) != 1 || options.DryRun[0] != metav1.DryRunAll {
			t.Fatalf("TUI dry-run sent non-dry-run patch options: %+v", options)
		}
	}
	if patches == 0 {
		t.Fatal("expected at least one server-side dry-run patch")
	}
}

func TestApplyDryRunValidatesCombinedFinalPatch(t *testing.T) {
	t.Parallel()
	hpa := testutil.BuildHPA("default", "web", testutil.WithMinMax(1, 10))
	fakeClient := testutil.NewFakeClient(hpa)
	opts := &options{Common: commonOptions{ClientOverride: fakeClient, DryRun: true}}

	var out bytes.Buffer
	_, err := applySuggestionsInNamespace(context.Background(), &out, opts, "default", "web", []hpaanalysis.Suggestion{
		{Title: "raise min", Patch: `{"spec":{"minReplicas":2}}`, Apply: true},
		{Title: "raise max", Patch: `{"spec":{"maxReplicas":20}}`, Apply: true},
	}, false)
	if err != nil {
		t.Fatal(err)
	}

	patches := 0
	combinedFound := false
	for _, action := range fakeClient.Actions() {
		if action.GetVerb() != "patch" {
			continue
		}
		patches++
		patchAction := action.(ktesting.PatchAction)
		patchOptions := action.(interface{ GetPatchOptions() metav1.PatchOptions }).GetPatchOptions()
		if len(patchOptions.DryRun) != 1 || patchOptions.DryRun[0] != metav1.DryRunAll {
			t.Fatalf("dry-run workflow sent a live patch: %+v", patchOptions)
		}
		patchJSON := string(patchAction.GetPatch())
		if strings.Contains(patchJSON, `"minReplicas":2`) && strings.Contains(patchJSON, `"maxReplicas":20`) {
			combinedFound = true
		}
	}
	if patches != 3 || !combinedFound {
		t.Fatalf("expected two individual validations and one combined validation, patches=%d combined=%v", patches, combinedFound)
	}
}

func TestApplyRejectsObjectChangedAfterReview(t *testing.T) {
	t.Parallel()
	hpa := testutil.BuildHPA("default", "web", testutil.WithMinMax(1, 10))
	hpa.ResourceVersion = "1"
	fakeClient := testutil.NewFakeClient(hpa)
	getCount := 0
	fakeClient.PrependReactor("get", "horizontalpodautoscalers", func(_ ktesting.Action) (bool, runtime.Object, error) {
		getCount++
		if getCount < 2 {
			return false, nil, nil
		}
		changed := hpa.DeepCopy()
		changed.ResourceVersion = "2"
		changed.Spec.MaxReplicas = 12
		return true, changed, nil
	})

	opts := &options{Common: commonOptions{
		ClientOverride: fakeClient,
		DryRun:         false,
		Yes:            true,
	}}
	var out bytes.Buffer
	_, err := applySuggestionsInNamespace(context.Background(), &out, opts, "default", "web", []hpaanalysis.Suggestion{{
		Title: "raise max",
		Patch: `{"spec":{"maxReplicas":20}}`,
		Apply: true,
	}}, false)
	if err == nil || !strings.Contains(err.Error(), "changed after it was reviewed") {
		t.Fatalf("expected resourceVersion conflict, got %v", err)
	}

	// Only the pre-confirmation server-side validation may have been sent.
	for _, action := range fakeClient.Actions() {
		if action.GetVerb() != "patch" {
			continue
		}
		patchAction, ok := action.(interface{ GetPatchOptions() metav1.PatchOptions })
		if !ok || len(patchAction.GetPatchOptions().DryRun) == 0 {
			t.Fatalf("unexpected live patch after resourceVersion conflict: %#v", action)
		}
	}
}

func TestApplyPolicyChecksCombinedFinalState(t *testing.T) {
	t.Parallel()
	hpa := testutil.BuildHPA("default", "web", testutil.WithMinMax(2, 10))
	fakeClient := testutil.NewFakeClient(hpa)
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "policy.yaml")
	policyYAML := `apiVersion: hpa-status/v1
rules:
  - id: replica-range
    name: Replica Range
    severity: critical
    parameters:
      maxRatio: 10
`
	if err := os.WriteFile(policyPath, []byte(policyYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	opts := &options{
		Common: commonOptions{ClientOverride: fakeClient, DryRun: true},
		Status: statusOptions{PolicyGuard: policyPath, PolicyGuardMode: "block"},
	}
	var out bytes.Buffer
	_, err := applySuggestionsInNamespace(context.Background(), &out, opts, "default", "web", []hpaanalysis.Suggestion{
		{Title: "lower min", Patch: `{"spec":{"minReplicas":1}}`, Apply: true},
		{Title: "raise max", Patch: `{"spec":{"maxReplicas":15}}`, Apply: true},
	}, false)
	if !errors.Is(err, ErrPolicyGuardBlocked) {
		t.Fatalf("expected combined policy block, got %v\n%s", err, out.String())
	}
	for _, action := range fakeClient.Actions() {
		if action.GetVerb() == "patch" {
			t.Fatalf("policy-blocked final state must not reach Kubernetes patch: %#v", action)
		}
	}
}

func TestMergePatchWithResourceVersionPreservesMetadata(t *testing.T) {
	t.Parallel()
	got, err := mergePatchWithResourceVersion(`{"metadata":{"annotations":{"owner":"sre"}},"spec":{"maxReplicas":20}}`, "42")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"resourceVersion":"42"`, `"owner":"sre"`, `"maxReplicas":20`} {
		if !strings.Contains(got, want) {
			t.Fatalf("merged patch %s does not contain %s", got, want)
		}
	}
}
