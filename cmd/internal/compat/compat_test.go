package compat

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

// stubDiscovery implements just enough of discovery.DiscoveryInterface to drive
// BuildReport. Embedding the interface leaves the unused methods nil, which is
// safe because BuildReport only calls the two overridden below.
type stubDiscovery struct {
	discovery.DiscoveryInterface

	gitVersion    string
	versionErr    error
	resources     *metav1.APIResourceList
	resourcesErr  error
	requestedGVer string
}

func (s *stubDiscovery) ServerVersion() (*version.Info, error) {
	if s.versionErr != nil {
		return nil, s.versionErr
	}
	return &version.Info{GitVersion: s.gitVersion}, nil
}

func (s *stubDiscovery) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	s.requestedGVer = groupVersion
	if s.resourcesErr != nil {
		return nil, s.resourcesErr
	}
	return s.resources, nil
}

func hpaV2Resources() *metav1.APIResourceList {
	return &metav1.APIResourceList{
		GroupVersion: "autoscaling/v2",
		APIResources: []metav1.APIResource{
			{Kind: "Scale"},
			{Kind: "HorizontalPodAutoscaler"},
		},
	}
}

func findCheck(report Report, feature string) (CheckResult, bool) {
	for _, check := range report.Checks {
		if check.Feature == feature {
			return check, true
		}
	}
	return CheckResult{}, false
}

func TestBuildReportOnSupportedCluster(t *testing.T) {
	vers := kube.KubernetesVersions()
	disco := &stubDiscovery{gitVersion: "v1.35.2", resources: hpaV2Resources()}

	report := BuildReport(context.Background(), disco)

	if report.ClusterVersion != "v1.35.2" {
		t.Errorf("expected the discovered version, got %q", report.ClusterVersion)
	}
	if report.HPAAPI != "autoscaling/v2" {
		t.Errorf("expected autoscaling/v2, got %q", report.HPAAPI)
	}
	if disco.requestedGVer != "autoscaling/v2" {
		t.Errorf("expected a lookup of autoscaling/v2, got %q", disco.requestedGVer)
	}
	for _, check := range report.Checks {
		if check.Status == "ERROR" {
			t.Errorf("did not expect an ERROR on a supported cluster: %+v", check)
		}
	}
	// v1.35 is at the tolerance feature minor, so that check must be OK.
	tolerance, ok := findCheck(report, "behavior scaleUp/scaleDown tolerance")
	if !ok {
		t.Fatal("expected a tolerance check")
	}
	if tolerance.Status != "OK" {
		t.Errorf("expected OK tolerance at minor %d, got %+v", vers.ToleranceFeatureMinor, tolerance)
	}
}

func TestBuildReportWarnsBelowToleranceMinor(t *testing.T) {
	disco := &stubDiscovery{gitVersion: "v1.30.0", resources: hpaV2Resources()}

	report := BuildReport(context.Background(), disco)

	tolerance, ok := findCheck(report, "behavior scaleUp/scaleDown tolerance")
	if !ok {
		t.Fatal("expected a tolerance check")
	}
	if tolerance.Status != "WARN" {
		t.Errorf("expected WARN below the tolerance minor, got %+v", tolerance)
	}
	if !strings.Contains(tolerance.Message, "HPAConfigurableTolerance") {
		t.Errorf("expected the feature-gate hint, got %q", tolerance.Message)
	}
}

func TestBuildReportSurfacesVersionDiscoveryFailure(t *testing.T) {
	// An RBAC denial must not be silently reported as an old cluster.
	disco := &stubDiscovery{versionErr: errors.New("forbidden"), resources: hpaV2Resources()}

	report := BuildReport(context.Background(), disco)

	if report.ClusterVersion != "unknown" {
		t.Errorf("expected an unknown cluster version, got %q", report.ClusterVersion)
	}
	check, ok := findCheck(report, "cluster version discovery")
	if !ok {
		t.Fatal("expected a version-discovery check")
	}
	if check.Status != "WARN" || !strings.Contains(check.Message, "forbidden") {
		t.Errorf("expected a WARN naming the failure, got %+v", check)
	}
	// Version unknown means the tolerance check cannot claim support.
	tolerance, _ := findCheck(report, "behavior scaleUp/scaleDown tolerance")
	if tolerance.Status != "WARN" || !strings.Contains(tolerance.Message, "unknown") {
		t.Errorf("expected the unknown-version tolerance warning, got %+v", tolerance)
	}
}

func TestBuildReportSurfacesAPIDiscoveryFailure(t *testing.T) {
	disco := &stubDiscovery{gitVersion: "v1.35.0", resourcesErr: errors.New("forbidden")}

	report := BuildReport(context.Background(), disco)

	discoCheck, ok := findCheck(report, "HPA API discovery")
	if !ok {
		t.Fatal("expected an HPA API discovery check")
	}
	if discoCheck.Status != "WARN" {
		t.Errorf("expected WARN for a failed lookup, got %+v", discoCheck)
	}
	// The absent API is still reported as an ERROR so the exit path is clear.
	apiCheck, ok := findCheck(report, "HPA API")
	if !ok || apiCheck.Status != "ERROR" {
		t.Errorf("expected an ERROR for a missing autoscaling/v2, got %+v (found=%v)", apiCheck, ok)
	}
}

func TestBuildReportErrorsWhenHPAKindAbsent(t *testing.T) {
	// The group version resolves but carries no HorizontalPodAutoscaler kind.
	disco := &stubDiscovery{
		gitVersion: "v1.35.0",
		resources:  &metav1.APIResourceList{GroupVersion: "autoscaling/v2", APIResources: []metav1.APIResource{{Kind: "Scale"}}},
	}

	report := BuildReport(context.Background(), disco)

	if report.HPAAPI != "unknown" {
		t.Errorf("expected an unknown HPA API, got %q", report.HPAAPI)
	}
	if check, ok := findCheck(report, "HPA API"); !ok || check.Status != "ERROR" {
		t.Errorf("expected an ERROR check, got %+v (found=%v)", check, ok)
	}
}

func TestWriteTextRendersEveryCheck(t *testing.T) {
	report := Report{
		ClusterVersion: "v1.35.0",
		HPAAPI:         "autoscaling/v2",
		Checks: []CheckResult{
			Check("OK", "multiple metrics", "supported by autoscaling/v2"),
			Check("ERROR", "HPA API", ""),
		},
	}

	var out bytes.Buffer
	if err := WriteText(&out, report); err != nil {
		t.Fatalf("WriteText returned error: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"Cluster: v1.35.0",
		"HPA API: autoscaling/v2",
		"OK: multiple metrics - supported by autoscaling/v2",
		"ERROR: HPA API",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in output, got:\n%s", want, got)
		}
	}
	// A check with no message must not render a trailing " - ".
	if strings.Contains(got, "HPA API - ") {
		t.Errorf("expected no dangling separator for an empty message, got:\n%s", got)
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
		if got := ParseKubeMinor(versionStr); got != minor {
			t.Errorf("%s: version string %q parses to minor %d, but constant minor is %d", label, versionStr, got, minor)
		}
	}
	check(v.StableSinceVersion, v.StableSinceMinor, "stable-since")
	check(v.ContainerResourceVer, v.ContainerResourceMinor, "container-resource")
	check(v.ToleranceFeatureVer, v.ToleranceFeatureMinor, "tolerance-feature")

	// The minimum API version must be older than the stable-since version:
	// autoscaling/v2 GA (1.26) cannot predate the API's existence (1.23).
	minAPIMinor := ParseKubeMinor(v.MinAPIVersion)
	if minAPIMinor >= v.StableSinceMinor {
		t.Errorf("MinAPIVersion %q (minor %d) must be older than StableSinceMinor %d",
			v.MinAPIVersion, minAPIMinor, v.StableSinceMinor)
	}
}

// TestReportMessagesReferenceVersionConstants asserts the messages BuildReport
// actually emits are derived from the centralized version constants rather
// than hardcoded literals, so a constant bump flows into the compat output.
func TestReportMessagesReferenceVersionConstants(t *testing.T) {
	v := kube.KubernetesVersions()
	disco := &stubDiscovery{gitVersion: "v1.35.0", resources: hpaV2Resources()}

	report := BuildReport(context.Background(), disco)

	containerResource, ok := findCheck(report, "containerResource metrics")
	if !ok {
		t.Fatal("expected a containerResource check")
	}
	if !strings.Contains(containerResource.Message, v.ContainerResourceVer) {
		t.Errorf("containerResource message %q does not reference %s", containerResource.Message, v.ContainerResourceVer)
	}

	tolerance, ok := findCheck(report, "behavior scaleUp/scaleDown tolerance")
	if !ok {
		t.Fatal("expected a tolerance check")
	}
	if !strings.Contains(tolerance.Message, v.ToleranceFeatureVer) {
		t.Errorf("tolerance message %q does not reference %s", tolerance.Message, v.ToleranceFeatureVer)
	}
}

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
		{"v1.28.9+gke.100", 28, "gke build suffix"},
		{"garbage", 0, "unparseable -> unknown"},
		{"v1", 0, "missing minor -> unknown"},
		{"", 0, "empty -> unknown"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			if got := ParseKubeMinor(c.in); got != c.want {
				t.Errorf("ParseKubeMinor(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}
