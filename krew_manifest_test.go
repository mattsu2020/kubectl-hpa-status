// Package main validates the krew plugin manifest.
package main

import (
	"os"
	"strings"
	"testing"
)

func TestKrewManifestUsesHPAStatusName(t *testing.T) {
	data, err := os.ReadFile(".krew.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"name: hpa-status",
		"bin: kubectl-hpa-status",
		"https://github.com/mattsu2020/kubectl-hpa-status/releases/download/",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected .krew.yaml to contain %q", want)
		}
	}
}
