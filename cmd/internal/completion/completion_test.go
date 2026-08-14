package completion

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

func TestOutputCompletions(t *testing.T) {
	completions, directive := Output(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected NoFileComp, got %v", directive)
	}
	expected := []string{"table", "wide", "json", "yaml", "jsonpath=", "template=", "prometheus"}
	for i, exp := range expected {
		if i >= len(completions) {
			t.Errorf("missing completion for %q", exp)
			continue
		}
		if completions[i] == "" {
			t.Errorf("empty completion at index %d", i)
		}
	}
}

func TestFilterCompletions(t *testing.T) {
	completions, directive := Filter(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected NoFileComp, got %v", directive)
	}
	expected := []string{"all", "ok", "error", "limited", "issue"}
	if len(completions) != len(expected) {
		t.Errorf("expected %d completions, got %d", len(expected), len(completions))
	}
	for i, exp := range expected {
		if i < len(completions) && completions[i] == "" {
			t.Errorf("empty completion at index %d", i)
		}
		if i < len(completions) {
			found := false
			for _, c := range completions {
				if len(c) >= len(exp) && c[:len(exp)] == exp {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("missing completion for %q", exp)
			}
		}
	}
}

func TestSortByCompletions(t *testing.T) {
	completions, directive := SortBy(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected NoFileComp, got %v", directive)
	}
	expected := []string{"name", "namespace", "health", "healthscore", "current", "desired", "diff", "age", "issue", "min", "max", "target"}
	if len(completions) != len(expected) {
		t.Errorf("expected %d completions, got %d", len(expected), len(completions))
	}
}

func TestColorCompletions(t *testing.T) {
	completions, directive := Color(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected NoFileComp, got %v", directive)
	}
	if len(completions) != 3 {
		t.Errorf("expected 3 completions, got %d", len(completions))
	}
}

func TestLangCompletions(t *testing.T) {
	completions, directive := Lang(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected NoFileComp, got %v", directive)
	}
	if len(completions) != 2 {
		t.Errorf("expected 2 completions, got %d", len(completions))
	}
}

func TestEventsCompletions(t *testing.T) {
	completions, directive := Events(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected NoFileComp, got %v", directive)
	}
	if len(completions) != 2 {
		t.Errorf("expected 2 completions, got %d", len(completions))
	}
}

func TestUntilConditionCompletions(t *testing.T) {
	completions, directive := UntilCondition(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected NoFileComp, got %v", directive)
	}
	if len(completions) != 5 {
		t.Errorf("expected 5 completions, got %d", len(completions))
	}
}

func TestContextNames(t *testing.T) {
	config := &api.Config{
		Contexts: map[string]*api.Context{
			"dev":     {},
			"staging": {},
			"prod":    {},
		},
	}
	names := contextNames(config)
	if len(names) != 3 {
		t.Errorf("expected 3 context names, got %d", len(names))
	}
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	for _, exp := range []string{"dev", "staging", "prod"} {
		if !found[exp] {
			t.Errorf("missing context name %q", exp)
		}
	}
}

func TestContextNamesEmpty(t *testing.T) {
	config := &api.Config{
		Contexts: map[string]*api.Context{},
	}
	names := contextNames(config)
	if len(names) != 0 {
		t.Errorf("expected 0 context names, got %d", len(names))
	}
}

func TestStaticCompletionsNonEmpty(t *testing.T) {
	for name, values := range map[string][]completionValue{
		"output":          OutputValues,
		"filter":          FilterValues,
		"sort-by":         SortByValues,
		"color":           ColorValues,
		"lang":            LangValues,
		"events":          EventsValues,
		"until-condition": UntilConditionValues,
	} {
		if len(values) == 0 {
			t.Errorf("%s value list is empty", name)
		}
		for _, v := range values {
			if v.value == "" || v.desc == "" {
				t.Errorf("%s entry has empty value/desc: %+v", name, v)
			}
		}
	}
}

func TestShellCommand(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			root := &cobra.Command{Use: "example"}
			cmd := ShellCommand(root)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs([]string{shell})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if out.Len() == 0 {
				t.Fatal("generated completion script is empty")
			}
		})
	}

	cmd := ShellCommand(&cobra.Command{Use: "example"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"tcsh"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "unsupported shell") {
		t.Fatalf("error = %v, want unsupported shell", err)
	}
}

func TestHpaNameCompletion(t *testing.T) {
	web := testutil.BuildHPA("default", "web", testutil.WithScaleTargetRef("Deployment", "web-deploy"))
	api := testutil.BuildHPA("team-a", "api", testutil.WithScaleTargetRef("StatefulSet", "api-sts"))
	clientset := testutil.NewFakeClient(web, api)

	deps := Deps{
		NewClient: func() (*kube.Client, error) {
			return &kube.Client{Interface: clientset, Namespace: "default"}, nil
		},
	}
	complete := HpaName(deps)
	names, directive := complete(commandWithContext(), nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp || len(names) != 1 || names[0] != "web\tweb-deploy" {
		t.Fatalf("names = %v, directive = %v", names, directive)
	}

	deps.AllNamespaces = func() bool { return true }
	names, _ = HpaName(deps)(commandWithContext(), nil, "")
	if len(names) != 2 || !containsCompletion(names, "default/web\tweb-deploy") || !containsCompletion(names, "team-a/api\tapi-sts") {
		t.Fatalf("all-namespace names = %v", names)
	}

	names, _ = complete(commandWithContext(), []string{"already-selected"}, "")
	if names != nil {
		t.Fatalf("completion after positional argument = %v, want nil", names)
	}

	failing := HpaName(Deps{NewClient: func() (*kube.Client, error) { return nil, errors.New("boom") }})
	if names, _ := failing(commandWithContext(), nil, ""); names != nil {
		t.Fatalf("completion on client error = %v, want nil", names)
	}
}

func TestNamespaceCompletion(t *testing.T) {
	clientset := testutil.NewFakeClientWithObjects(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a"}},
	)
	deps := Deps{NewClient: func() (*kube.Client, error) {
		return &kube.Client{Interface: clientset}, nil
	}}
	names, directive := Namespace(deps)(commandWithContext(), nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp || len(names) != 2 || !containsCompletion(names, "team-a") {
		t.Fatalf("names = %v, directive = %v", names, directive)
	}

	failing := Namespace(Deps{NewClient: func() (*kube.Client, error) { return nil, errors.New("boom") }})
	if names, _ := failing(commandWithContext(), nil, ""); names != nil {
		t.Fatalf("completion on client error = %v, want nil", names)
	}
}

func TestContextCompletionUsesExplicitKubeconfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	config := api.NewConfig()
	config.Contexts["dev"] = &api.Context{Cluster: "local"}
	if err := clientcmd.WriteToFile(*config, path); err != nil {
		t.Fatal(err)
	}

	names, directive := Context(Deps{Kubeconfig: func() string { return path }})(&cobra.Command{}, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp || len(names) != 1 || names[0] != "dev" {
		t.Fatalf("names = %v, directive = %v", names, directive)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	names, _ = Context(Deps{Kubeconfig: func() string { return missing }})(&cobra.Command{}, nil, "")
	if names != nil {
		t.Fatalf("completion for missing kubeconfig = %v, want nil", names)
	}
}

func TestRegisterFlagCompletions(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	root.PersistentFlags().String("namespace", "", "")
	child := &cobra.Command{Use: "child"}
	child.Flags().String("output", "", "")
	root.AddCommand(child)

	if err := RegisterFlagCompletions(root, Deps{}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterFlagCompletions(root, Deps{}); err == nil || !strings.Contains(err.Error(), "registering completion") {
		t.Fatalf("second registration error = %v", err)
	}
}

func containsCompletion(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func commandWithContext() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	return cmd
}
