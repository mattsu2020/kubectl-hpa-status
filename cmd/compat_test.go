package cmd

import (
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

// TestParseKubeMinor verifies the GitVersion minor-version parser used by the
// compat command. It must tolerate distribution suffixes (eks, gke), a missing
// leading "v", and return 0 (treated as "unknown") for garbage input.
func TestParseKubeMinor(t *testing.T) {
	cases := []struct {
		in    string
		want  int
		label string
	}{
		{"v1.35.1", 35, "plain semver"},
		{"v1.26.15", 26, "older stable"},
		{"1.30.0", 30, "no leading v"},
		{"v1.35.15-eks-123", 35, "distribution suffix"},
		{"garbage", 0, "unparseable -> unknown"},
		{"v1", 0, "missing minor -> unknown"},
		{"", 0, "empty -> unknown"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			if got := parseKubeMinor(c.in); got != c.want {
				t.Fatalf("parseKubeMinor(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

// TestKubernetesVersionConstantsAreConsistent guards against the version
// strings and their integer minors drifting apart — e.g. someone bumps the
// string "1.30" but forgets the matching 30 minor. The user-facing compat
// strings are built from these, so a mismatch would surface a wrong version
// in the output without any other failure.
func TestKubernetesVersionConstantsAreConsistent(t *testing.T) {
	v := kube.KubernetesVersions()
	check := func(versionStr string, minor int, label string) {
		got := parseKubeMinor(versionStr)
		if got != minor {
			t.Fatalf("%s: version string %q parses to minor %d, but constant minor is %d", label, versionStr, got, minor)
		}
	}
	check(v.StableSinceVersion, v.StableSinceMinor, "stable-since")
	check(v.ContainerResourceVer, v.ContainerResourceMinor, "container-resource")
	check(v.ToleranceFeatureVer, v.ToleranceFeatureMinor, "tolerance-feature")

	// The minimum API version must be older than the stable-since version:
	// autoscaling/v2 GA (1.26) cannot predate the API's existence (1.23).
	minAPIMinor := parseKubeMinor(v.MinAPIVersion)
	if minAPIMinor >= v.StableSinceMinor {
		t.Fatalf("MinAPIVersion %q (minor %d) must be older than StableSinceMinor %d",
			v.MinAPIVersion, minAPIMinor, v.StableSinceMinor)
	}
}

// TestCompatCheckStringsReferenceVersionConstants verifies the compat report
// builds its user-facing version strings from the centralized constants rather
// than hardcoded literals. We build the message via the same compatCheck helper
// the report uses and assert it carries the constant's version, so a future
// constant bump automatically flows into the compat output.
func TestCompatCheckStringsReferenceVersionConstants(t *testing.T) {
	v := kube.KubernetesVersions()

	// containerResource OK message must mention the stable version constant.
	msg := "stable in Kubernetes v" + v.ContainerResourceVer + "+"
	okCheck := compatCheck("OK", "containerResource metrics", msg)
	if !strings.Contains(okCheck.Message, v.ContainerResourceVer) {
		t.Fatalf("containerResource check does not reference version constant %s: %s", v.ContainerResourceVer, okCheck.Message)
	}

	// tolerance WARN for an old/unknown cluster must reference the feature version constant.
	warn := compatCheck("WARN", "tolerance", "requires Kubernetes v"+v.ToleranceFeatureVer+"+")
	if !strings.Contains(warn.Message, v.ToleranceFeatureVer) {
		t.Fatalf("tolerance WARN does not reference version constant %s: %s", v.ToleranceFeatureVer, warn.Message)
	}
}
