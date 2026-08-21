package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

// liveApplyOptions builds an options value that skips confirmation and
// applies for real against the fake client.
func liveApplyOptions(fake *fake.Clientset, allowPartial bool) *options {
	return &options{Common: commonOptions{
		ConnectionOptions: ConnectionOptions{ClientOverride: fake},
		ApplyOptions:      ApplyOptions{DryRun: false, Yes: true, AllowPartial: allowPartial},
	}}
}

func TestApplyLiveMergedPatchAppliesAtomically(t *testing.T) {
	t.Parallel()
	hpa := testutil.BuildHPA("default", "web", testutil.WithMinMax(1, 10))
	hpa.ResourceVersion = "1"
	fakeClient := testutil.NewFakeClient(hpa)
	opts := liveApplyOptions(fakeClient, false)

	var out bytes.Buffer
	messages, err := applySuggestionsInNamespace(context.Background(), &out, opts, "default", "web", []hpaanalysis.Suggestion{
		{Title: "raise min", Patch: `{"spec":{"minReplicas":2}}`, Apply: true},
		{Title: "raise max", Patch: `{"spec":{"maxReplicas":20}}`, Apply: true},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected one message per applied patch, got %v", messages)
	}

	// Exactly one live patch carrying both changes must reach the server.
	livePatches := 0
	for _, action := range fakeClient.Actions() {
		if action.GetVerb() != "patch" {
			continue
		}
		patchOptions := action.(interface{ GetPatchOptions() metav1.PatchOptions }).GetPatchOptions()
		if len(patchOptions.DryRun) == 1 && patchOptions.DryRun[0] == metav1.DryRunAll {
			continue
		}
		livePatches++
		patchJSON := string(action.(ktesting.PatchAction).GetPatch())
		if !strings.Contains(patchJSON, `"minReplicas":2`) || !strings.Contains(patchJSON, `"maxReplicas":20`) {
			t.Fatalf("live patch is not the combined final state: %s", patchJSON)
		}
	}
	if livePatches != 1 {
		t.Fatalf("expected exactly one live (merged) patch, got %d", livePatches)
	}
}

func TestExecutePatchesWithoutAllowPartialRefusesUnmergeable(t *testing.T) {
	t.Parallel()
	hpa := testutil.BuildHPA("default", "web", testutil.WithMinMax(1, 10))
	hpa.ResourceVersion = "1"
	fakeClient := testutil.NewFakeClient(hpa)
	client, err := (&options{Common: commonOptions{ConnectionOptions: ConnectionOptions{ClientOverride: fakeClient}}}).NewClient()
	if err != nil {
		t.Fatal(err)
	}

	patches := []hpaanalysis.Suggestion{
		{Title: "raise min", Patch: `{"spec":{"minReplicas":2}}`, Apply: true},
		// A non-object JSON patch cannot participate in a merge patch, so the
		// atomic combined apply is impossible.
		{Title: "non-mergeable", Patch: `[1,2]`, Apply: true},
	}
	var out bytes.Buffer
	_, err = executePatches(context.Background(), &out, client, "default", "web", patches, false, "1")
	if err == nil {
		t.Fatal("expected unmergeable patches to be refused without --allow-partial")
	}
	if !strings.Contains(err.Error(), "--allow-partial") {
		t.Errorf("error should name the opt-in flag, got: %v", err)
	}
	for _, action := range fakeClient.Actions() {
		if action.GetVerb() == "patch" {
			patchOptions := action.(interface{ GetPatchOptions() metav1.PatchOptions }).GetPatchOptions()
			if len(patchOptions.DryRun) == 0 {
				t.Fatal("a live patch was sent despite the merge failure")
			}
		}
	}
}

func TestExecutePatchesSequentialFallbackWarnsAndStopsAtFailure(t *testing.T) {
	t.Parallel()
	hpa := testutil.BuildHPA("default", "web", testutil.WithMinMax(1, 10))
	hpa.ResourceVersion = "1"
	fakeClient := testutil.NewFakeClient(hpa)
	client, err := (&options{Common: commonOptions{ConnectionOptions: ConnectionOptions{ClientOverride: fakeClient}}}).NewClient()
	if err != nil {
		t.Fatal(err)
	}

	patches := []hpaanalysis.Suggestion{
		{Title: "raise min", Patch: `{"spec":{"minReplicas":2}}`, Apply: true},
		{Title: "non-mergeable", Patch: `[1,2]`, Apply: true},
	}
	var out bytes.Buffer
	applied, err := executePatches(context.Background(), &out, client, "default", "web", patches, true, "1")
	if err == nil {
		t.Fatal("expected the sequential apply to fail on the non-object patch")
	}
	if !strings.Contains(err.Error(), `precondition to "non-mergeable"`) {
		t.Errorf("error should name the failing patch, got: %v", err)
	}
	if len(applied) != 1 || !strings.Contains(applied[0], "raise min") {
		t.Fatalf("expected the first patch to be applied before the failure, got %v", applied)
	}
	output := out.String()
	if !strings.Contains(output, "non-atomic") || !strings.Contains(output, "partially modified") {
		t.Errorf("expected fallback warnings, got:\n%s", output)
	}
}

func TestConfirmApplyPromptAnswers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		answer  string
		wantErr bool
	}{
		{name: "y confirms", answer: "y\n"},
		{name: "yes confirms", answer: "YES\n"},
		{name: "n declines", answer: "n\n", wantErr: true},
		{name: "empty declines", answer: "\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &options{Common: commonOptions{ConnectionOptions: ConnectionOptions{In: strings.NewReader(tt.answer)}}}
			var out bytes.Buffer
			err := confirmApply(&out, opts, 1, "default", "web")
			if tt.wantErr && err == nil {
				t.Fatal("expected the apply to be declined")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected confirmation, got: %v", err)
			}
			if !strings.Contains(out.String(), "Apply 1 patches to HPA default/web?") {
				t.Errorf("expected the confirmation prompt, got:\n%s", out.String())
			}
		})
	}
}

func TestApplySliceConfigRespectsFlagPrecedence(t *testing.T) {
	t.Parallel()
	t.Run("config value applied when flag unchanged", func(t *testing.T) {
		dst := []string{"flag"}
		applySliceConfig([]string{"config"}, "example", func(string) bool { return false }, &dst)
		if len(dst) != 1 || dst[0] != "config" {
			t.Fatalf("expected config value to win, got %v", dst)
		}
	})
	t.Run("flag wins over config", func(t *testing.T) {
		dst := []string{"flag"}
		applySliceConfig([]string{"config"}, "example", func(string) bool { return true }, &dst)
		if len(dst) != 1 || dst[0] != "flag" {
			t.Fatalf("expected flag value to win, got %v", dst)
		}
	})
	t.Run("empty config leaves destination", func(t *testing.T) {
		dst := []string{"flag"}
		applySliceConfig(nil, "example", func(string) bool { return false }, &dst)
		if len(dst) != 1 || dst[0] != "flag" {
			t.Fatalf("expected destination untouched, got %v", dst)
		}
	})
}
