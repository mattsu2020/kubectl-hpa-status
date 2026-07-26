package kube

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestNewLoadingRules_PreservesKUBECONFIGPrecedence(t *testing.T) {
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	t.Setenv(clientcmd.RecommendedConfigPathEnvVar, first+string(os.PathListSeparator)+second)

	rules := newLoadingRules(Options{})

	if rules.ExplicitPath != "" {
		t.Fatalf("ExplicitPath = %q, want empty so KUBECONFIG precedence is used", rules.ExplicitPath)
	}
	want := []string{first, second}
	if !reflect.DeepEqual(rules.Precedence, want) {
		t.Fatalf("Precedence = %#v, want %#v", rules.Precedence, want)
	}
}

func TestNewLoadingRules_ExplicitFlagWinsOverKUBECONFIG(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "from-env")
	explicitPath := filepath.Join(t.TempDir(), "explicit")
	t.Setenv(clientcmd.RecommendedConfigPathEnvVar, envPath)

	rules := newLoadingRules(Options{Kubeconfig: explicitPath})

	if rules.ExplicitPath != explicitPath {
		t.Fatalf("ExplicitPath = %q, want %q", rules.ExplicitPath, explicitPath)
	}
}

func TestDeferredClientConfig_MergesKUBECONFIGInStandardOrder(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	writeLoadingTestConfig(t, first, "first-context", "first-namespace")
	writeLoadingTestConfig(t, second, "second-context", "second-namespace")
	t.Setenv(clientcmd.RecommendedConfigPathEnvVar, first+string(os.PathListSeparator)+second)

	config := deferredClientConfig(Options{})
	namespace, _, err := config.Namespace()
	if err != nil {
		t.Fatalf("resolve namespace: %v", err)
	}
	if namespace != "first-namespace" {
		t.Fatalf("namespace = %q, want first KUBECONFIG entry to win", namespace)
	}
	raw, err := config.RawConfig()
	if err != nil {
		t.Fatalf("load merged config: %v", err)
	}
	if _, ok := raw.Contexts["second-context"]; !ok {
		t.Fatalf("second KUBECONFIG entry was not merged: %#v", raw.Contexts)
	}
}

func writeLoadingTestConfig(t *testing.T, path, contextName, namespace string) {
	t.Helper()
	clusterName := contextName + "-cluster"
	config := clientcmdapi.NewConfig()
	config.CurrentContext = contextName
	config.Clusters[clusterName] = &clientcmdapi.Cluster{Server: "https://127.0.0.1"}
	config.Contexts[contextName] = &clientcmdapi.Context{
		Cluster:   clusterName,
		Namespace: namespace,
	}
	if err := clientcmd.WriteToFile(*config, path); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
}
