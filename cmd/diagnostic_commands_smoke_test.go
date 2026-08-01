package cmd

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// This file holds smoke tests for the diagnostic and archive commands that
// ship to users but had almost no direct coverage: readiness-doctor, snapshot,
// history, incident-bundle, alpha gitops review, and the controller-profile
// observation helper behind --explain.

// readinessDoctorFixture builds an HPA with a Deployment whose pod template
// carries readiness and startup probes, plus pods in mixed readiness states.
func readinessDoctorFixture() []runtime.Object {
	labels := map[string]string{"app": "web"}
	hpa := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 5))
	deploy := testutil.BuildDeployment("default", "web",
		testutil.WithSelector(labels),
		testutil.WithReplicaStatus(3, 2),
		testutil.WithPodTemplate(corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Name: "app",
				ReadinessProbe: &corev1.Probe{
					InitialDelaySeconds: 20,
					PeriodSeconds:       10,
				},
				StartupProbe: &corev1.Probe{
					InitialDelaySeconds: 10,
					PeriodSeconds:       5,
					FailureThreshold:    30,
				},
			}}},
		}),
	)

	started := metav1.NewTime(time.Now().Add(-2 * time.Minute))
	ready := testutil.BuildPod("default", "web-ready",
		testutil.WithPodLabels(labels),
		testutil.WithPodCondition(corev1.PodCondition{Type: corev1.PodReady, Status: corev1.ConditionTrue}),
	)
	ready.Status.StartTime = &started
	notReady := testutil.BuildPod("default", "web-starting",
		testutil.WithPodLabels(labels),
		testutil.WithPodCondition(corev1.PodCondition{Type: corev1.PodReady, Status: corev1.ConditionFalse}),
	)
	notReady.Status.StartTime = &started

	return []runtime.Object{hpa, deploy, ready, notReady}
}

func TestRunReadinessDoctorReportsProbeConfiguration(t *testing.T) {
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: testutil.NewFakeClientWithObjects(readinessDoctorFixture()...),
				Namespace:      "default",
			},
		},
	}

	var out bytes.Buffer
	if err := runReadinessDoctor(context.Background(), &out, opts, "web"); err != nil {
		t.Fatalf("runReadinessDoctor returned error: %v", err)
	}
	if got := out.String(); strings.TrimSpace(got) == "" {
		t.Fatal("expected readiness-doctor output, got nothing")
	}
}

func TestRunReadinessDoctorJSONIsStructured(t *testing.T) {
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: testutil.NewFakeClientWithObjects(readinessDoctorFixture()...),
				Namespace:      "default",
			},
			OutputOptions: OutputOptions{Output: "json"},
		},
	}

	var out bytes.Buffer
	if err := runReadinessDoctor(context.Background(), &out, opts, "web"); err != nil {
		t.Fatalf("runReadinessDoctor returned error: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(out.String()), "{") {
		t.Errorf("expected a JSON object, got:\n%s", out.String())
	}
}

func TestRunReadinessDoctorMissingHPA(t *testing.T) {
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: testutil.NewFakeClient(),
				Namespace:      "default",
			},
		},
	}

	var out bytes.Buffer
	if err := runReadinessDoctor(context.Background(), &out, opts, "absent"); err == nil {
		t.Fatal("expected an error for a missing HPA")
	}
}

func TestRunReadinessDoctorUnresolvableScaleTarget(t *testing.T) {
	// The HPA exists but its Deployment does not, so no selector can be
	// resolved. This must fail with a clear message rather than analyzing an
	// empty pod set as if it were healthy.
	hpa := testutil.BuildHPA("default", "web")
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: testutil.NewFakeClient(hpa),
				Namespace:      "default",
			},
		},
	}

	var out bytes.Buffer
	err := runReadinessDoctor(context.Background(), &out, opts, "web")
	if err == nil {
		t.Fatal("expected an error when the scale target cannot be resolved")
	}
	if !strings.Contains(err.Error(), "scale target") && !strings.Contains(err.Error(), "selector") {
		t.Errorf("expected a scale-target/selector error, got: %v", err)
	}
}

func TestRunSnapshotWritesZipArchive(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web")
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: testutil.NewFakeClient(hpa),
				Namespace:      "default",
			},
		},
	}

	path := filepath.Join(t.TempDir(), "snapshot.zip")
	var out bytes.Buffer
	if err := runSnapshot(context.Background(), &out, opts, "web", path, true); err != nil {
		t.Fatalf("runSnapshot returned error: %v", err)
	}
	if !strings.Contains(out.String(), path) {
		t.Errorf("expected the output to name the archive path, got: %q", out.String())
	}

	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("snapshot is not a readable zip: %v", err)
	}
	defer func() { _ = reader.Close() }()

	if len(reader.File) == 0 {
		t.Fatal("expected the snapshot archive to contain files")
	}
	var names []string
	for _, f := range reader.File {
		names = append(names, f.Name)
	}
	if !strings.Contains(strings.Join(names, ","), "hpa") {
		t.Errorf("expected an HPA document in the archive, got: %v", names)
	}
}

func TestRunSnapshotMissingHPADoesNotCreateArchive(t *testing.T) {
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: testutil.NewFakeClient(),
				Namespace:      "default",
			},
		},
	}

	path := filepath.Join(t.TempDir(), "snapshot.zip")
	var out bytes.Buffer
	if err := runSnapshot(context.Background(), &out, opts, "absent", path, true); err == nil {
		t.Fatal("expected an error for a missing HPA")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no archive to be written on failure, stat err = %v", err)
	}
}

func TestRunHistoryRendersTrendReport(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 3))
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: testutil.NewFakeClient(hpa),
				Namespace:      "default",
			},
		},
	}

	var out bytes.Buffer
	if err := runHistory(context.Background(), &out, opts, "web", 6*time.Hour, ""); err != nil {
		t.Fatalf("runHistory returned error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "HPA history: default/web") {
		t.Errorf("expected the history header, got:\n%s", got)
	}
	if !strings.Contains(got, "Health:") {
		t.Errorf("expected a health line, got:\n%s", got)
	}
	if strings.Contains(got, "Prometheus query_range links:") {
		t.Errorf("expected no Prometheus links without --prometheus, got:\n%s", got)
	}
}

func TestRunHistoryEmitsPrometheusLinks(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web", testutil.WithResourceMetric("cpu", 70, 95))
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: testutil.NewFakeClient(hpa),
				Namespace:      "default",
			},
		},
	}

	var out bytes.Buffer
	if err := runHistory(context.Background(), &out, opts, "web", time.Hour, "http://prometheus.example"); err != nil {
		t.Fatalf("runHistory returned error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Prometheus query_range links:") {
		t.Errorf("expected Prometheus links, got:\n%s", got)
	}
	if !strings.Contains(got, "http://prometheus.example") {
		t.Errorf("expected the configured Prometheus base URL, got:\n%s", got)
	}
}

func TestRunIncidentBundleWritesMarkdown(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web")
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: testutil.NewFakeClient(hpa),
				Namespace:      "default",
			},
		},
	}

	path := filepath.Join(t.TempDir(), "incident.md")
	var out bytes.Buffer
	if err := runIncidentBundle(context.Background(), &out, opts, "web", "markdown", path, true); err != nil {
		t.Fatalf("runIncidentBundle returned error: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("incident bundle was not written: %v", err)
	}
	if !strings.Contains(string(content), "web") {
		t.Errorf("expected the HPA name in the bundle, got:\n%s", content)
	}
}

func TestRunIncidentBundleRejectsUnknownFormat(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web")
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: testutil.NewFakeClient(hpa),
				Namespace:      "default",
			},
		},
	}

	path := filepath.Join(t.TempDir(), "incident.out")
	var out bytes.Buffer
	err := runIncidentBundle(context.Background(), &out, opts, "web", "pdf", path, true)
	if err == nil {
		t.Fatal("expected an error for an unsupported format")
	}
	if !strings.Contains(err.Error(), "markdown or zip") {
		t.Errorf("expected the error to list the supported formats, got: %v", err)
	}
}

func TestRunGitOpsReviewFlagsRiskyManifest(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "hpa.yaml")
	writeFile(t, manifest, `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: web
  namespace: prod
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: web
  minReplicas: 1
  maxReplicas: 2
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 95
`)

	opts := &options{}
	var out bytes.Buffer
	if err := runGitOpsReview(context.Background(), &out, opts, manifest); err != nil {
		t.Fatalf("runGitOpsReview returned error: %v", err)
	}
	if strings.TrimSpace(out.String()) == "" {
		t.Fatal("expected review output for an HPA manifest")
	}
}

func TestRunGitOpsReviewDirectoryWithoutHPAManifests(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "configmap.yaml"), `apiVersion: v1
kind: ConfigMap
metadata:
  name: settings
data:
  key: value
`)

	opts := &options{}
	var out bytes.Buffer
	if err := runGitOpsReview(context.Background(), &out, opts, dir); err != nil {
		t.Fatalf("runGitOpsReview returned error: %v", err)
	}
	if !strings.Contains(out.String(), "No HPA manifests found.") {
		t.Errorf("expected the no-manifests message, got: %q", out.String())
	}
}

func TestRunGitOpsReviewMissingPath(t *testing.T) {
	opts := &options{}
	var out bytes.Buffer
	err := runGitOpsReview(context.Background(), &out, opts, filepath.Join(t.TempDir(), "absent"))
	if err == nil {
		t.Fatal("expected an error for a missing path")
	}
	if !strings.Contains(err.Error(), "cannot access") {
		t.Errorf("expected an access error, got: %v", err)
	}
}

func TestBuildControllerProfilePrefersAssumedProfile(t *testing.T) {
	profile := buildControllerProfile(context.Background(), nil, "gke", "")
	if profile == nil {
		t.Fatal("expected a profile")
	}
	if profile.Source != "assumed:gke" {
		t.Errorf("expected the assumed source, got %q", profile.Source)
	}
}

func TestBuildControllerProfileReadsProfileFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.yaml")
	writeFile(t, path, "syncPeriod: 30s\ntolerance: \"0.05\"\n")

	profile := buildControllerProfile(context.Background(), nil, "", path)
	if profile == nil {
		t.Fatal("expected a profile")
	}
	if profile.SyncPeriod != "30s" {
		t.Errorf("expected the file's syncPeriod, got %q", profile.SyncPeriod)
	}
	if !strings.HasPrefix(profile.Source, "file:") {
		t.Errorf("expected a file source, got %q", profile.Source)
	}
}

func TestBuildControllerProfileWarnsOnUnreadableProfileFile(t *testing.T) {
	profile := buildControllerProfile(context.Background(), nil, "", filepath.Join(t.TempDir(), "absent.yaml"))
	if profile == nil {
		t.Fatal("expected a profile")
	}
	if len(profile.Warnings) == 0 {
		t.Error("expected a warning when the profile file cannot be read")
	}
}

func TestObserveControllerManagerProfileParsesArgs(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "kube-controller-manager-node1"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "kube-controller-manager",
			Command: []string{
				"kube-controller-manager",
				"--horizontal-pod-autoscaler-sync-period=30s",
				"--horizontal-pod-autoscaler-tolerance=0.05",
			},
			Args: []string{"--horizontal-pod-autoscaler-downscale-stabilization=10m"},
		}}},
	}

	client := fakeKubeClient(t, pod)
	profile, ok := observeControllerManagerProfile(context.Background(), client)
	if !ok {
		t.Fatal("expected the controller-manager pod to be observed")
	}
	if profile.SyncPeriod != "30s" {
		t.Errorf("expected syncPeriod 30s, got %q", profile.SyncPeriod)
	}
	if profile.Tolerance != "0.05" {
		t.Errorf("expected tolerance 0.05, got %q", profile.Tolerance)
	}
	if profile.DownscaleStabilization != "10m" {
		t.Errorf("expected downscaleStabilization 10m, got %q", profile.DownscaleStabilization)
	}
	if !strings.HasPrefix(profile.Source, "kube-system/") {
		t.Errorf("expected a kube-system source, got %q", profile.Source)
	}
}

func TestObserveControllerManagerProfileAbsent(t *testing.T) {
	client := fakeKubeClient(t)
	if _, ok := observeControllerManagerProfile(context.Background(), client); ok {
		t.Error("expected no profile when no controller-manager pod is visible")
	}
}

func TestApplyControllerArgIgnoresUnrelatedArgs(t *testing.T) {
	profile := hpaanalysis.DefaultControllerProfile()
	before := profile

	applyControllerArg(&profile, "--leader-elect=true")
	applyControllerArg(&profile, "not-a-flag")
	applyControllerArg(&profile, "--no-equals-sign")
	applyControllerArg(nil, "--horizontal-pod-autoscaler-tolerance=0.9")

	// ControllerProfile carries a []string, so compare the fields the parser
	// is allowed to touch rather than the whole struct.
	if profile.SyncPeriod != before.SyncPeriod ||
		profile.DownscaleStabilization != before.DownscaleStabilization ||
		profile.InitialReadinessDelay != before.InitialReadinessDelay ||
		profile.CPUInitializationPeriod != before.CPUInitializationPeriod ||
		profile.Tolerance != before.Tolerance {
		t.Errorf("unrelated args must not change the profile: %+v -> %+v", before, profile)
	}
}

// fakeKubeClient wraps a fake clientset in the kube.Client shape the helpers
// under test expect.
func fakeKubeClient(t *testing.T, objects ...runtime.Object) *kube.Client {
	t.Helper()
	return &kube.Client{
		Interface: testutil.NewFakeClientWithObjects(objects...),
		Namespace: "default",
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
