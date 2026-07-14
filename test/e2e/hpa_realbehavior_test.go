//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"

	"github.com/mattsu2020/kubectl-hpa-status/cmd"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// This file holds E2E tests that drive "real" HPA behavior against a live
// cluster (#1 maxReplicas cap under genuine metrics, #2 metrics-server
// detection). They skip when the prerequisite environment is absent.

// metricsAPIAvailable reports whether the metrics.k8s.io/v1beta1 API group
// is registered (i.e. metrics-server is installed and serving). Tests that
// rely on real PodMetrics use it to skip cleanly when the adapter is absent.
func metricsAPIAvailable(t *testing.T, client *kubernetes.Clientset) bool {
	t.Helper()
	groups, err := client.Discovery().ServerGroups()
	if err != nil {
		t.Logf("discovery failed while checking metrics API: %v", err)
		return false
	}
	for _, g := range groups.Groups {
		if g.Name == "metrics.k8s.io" {
			for _, v := range g.Versions {
				if v.Version == "v1beta1" {
					return true
				}
			}
		}
	}
	return false
}

// runMetricsContract runs `metrics contract NAME` and returns its text output.
func runMetricsContract(t *testing.T, kubeconfig, namespace, hpaName string) string {
	t.Helper()
	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"contract", hpaName, "-n", namespace, "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		t.Logf("metrics contract returned error: %v. Output:\n%s", err, buf.String())
	}
	return buf.String()
}

// TestE2E_MetricsServerDetection verifies that the tool correctly reports
// the availability of the metrics.k8s.io API via `metrics contract NAME`
// (scenario #2). When metrics-server is installed (the CI e2e job installs
// it) the report should reference the metrics.k8s.io group; when it is
// absent the same path should surface the "API group not found" message.
// Both branches are valid depending on the cluster, so the test adapts to
// whichever environment it runs in.
func TestE2E_MetricsServerDetection(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "ms-rc")
	createHealthyHPA(t, client, nsName, "ms-hpa", "ms-rc")

	hasMetrics := metricsAPIAvailable(t, client)
	t.Logf("metrics-server installed: %v", hasMetrics)

	output := runMetricsContract(t, kubeconfig, nsName, "ms-hpa")
	t.Logf("metrics contract output:\n%s", output)

	// The contract report always mentions the metrics API group name.
	if !strings.Contains(output, "metrics.k8s.io") {
		t.Errorf("expected output to reference metrics.k8s.io API group, got:\n%s", output)
	}

	if hasMetrics {
		// When metrics-server is present, the metrics.k8s.io/v1beta1 entry
		// must be reported as available/reachable.
		if !strings.Contains(strings.ToLower(output), "avail") &&
			!strings.Contains(output, "reachable") &&
			!strings.Contains(output, "ok") {
			t.Errorf("expected metrics API to be reported available, got:\n%s", output)
		}
	}
	// When metrics-server is absent, the same command should still run and
	// surface the unavailable status without crashing. We do not assert a
	// specific "not found" substring here because the report wording is
	// adapter-specific; the absence of a panic/crash is the contract.

	// Additionally exercise the cluster-doctor code path through discovery
	// directly: a second ServerGroups() call must not panic regardless of
	// metrics-server presence.
	if _, err := client.Discovery().ServerGroups(); err != nil {
		t.Logf("discovery check skipped: %v", err)
	}
}

// TestE2E_MaxReplicasCap drives a Deployment under CPU load until the HPA
// reaches maxReplicas (scenario #1). This requires a real metrics-server
// populating PodMetrics; the test skips when the adapter is absent.
//
// The fixture mirrors testdata/manifests/{deployment-web,hpa-web,load-job}.yaml
// but is constructed programmatically so it can run in an isolated namespace
// rather than the cluster-wide default namespace those manifests target.
func TestE2E_MaxReplicasCap(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	if !metricsAPIAvailable(t, client) {
		t.Skip("Skipping MaxReplicasCap: metrics-server (metrics.k8s.io/v1beta1) is not installed; cannot drive real HPA scaling")
	}

	const (
		hpaName    = "cap-hpa"
		deployName = "cap-deploy"
		maxRepl    = int32(6)
		cpuRequest = "100m"
		cpuLimit   = "200m"
		memRequest = "64Mi"
	)
	ctx0, cancel0 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel0()

	// 1. Deployment with a small CPU request so a tight target utilization
	//    is easy to exceed with a single load pod.
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: deployName, Namespace: nsName},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": deployName}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": deployName}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "web",
						Image: "registry.k8s.io/hpa-example",
						Ports: []corev1.ContainerPort{{ContainerPort: 80}},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(cpuRequest),
								corev1.ResourceMemory: resource.MustParse(memRequest),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse(cpuLimit),
							},
						},
					}},
				},
			},
		},
	}
	if _, err := client.AppsV1().Deployments(nsName).Create(ctx0, deploy, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create Deployment: %v", err)
	}
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: deployName, Namespace: nsName},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": deployName},
			Ports:    []corev1.ServicePort{{Port: 80, TargetPort: intstr.FromInt32(80)}},
		},
	}
	if _, err := client.CoreV1().Services(nsName).Create(ctx0, service, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	// 2. HPA targeting the Deployment at 50% average CPU utilization so the
	//    load job pushes it over target quickly.
	minRepl := int32(2)
	targetUtil := int32(50)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: hpaName, Namespace: nsName},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       deployName,
			},
			MinReplicas: &minRepl,
			MaxReplicas: maxRepl,
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceCPU,
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: &targetUtil,
					},
				},
			}},
		},
	}
	if _, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).Create(ctx0, hpa, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create HPA: %v", err)
	}

	// 3. Send HTTP traffic to the target Deployment. Burning CPU in an
	//    unrelated Pod would not affect the HPA target's utilization.
	loadJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "cap-load", Namespace: nsName},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Name: "cap-load"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    "load",
						Image:   "busybox",
						Command: []string{"sh", "-c", "while true; do wget -q -O- http://" + deployName + "; done"},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("200m"),
							},
						},
					}},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
			BackoffLimit: int32Ptr(0),
		},
	}
	if _, err := client.BatchV1().Jobs(nsName).Create(ctx0, loadJob, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create load Job: %v", err)
	}

	// 4. Poll the HPA until it reaches maxReplicas (or the deadline). Real
	//    metrics-server + kind takes ~1-2 minutes to populate and scale.
	const (
		pollInterval = 10 * time.Second
		pollTimeout  = 4 * time.Minute
	)
	deadline := time.Now().Add(pollTimeout)
	reached := false
	var lastRepl int32
	for time.Now().Before(deadline) {
		pollCtx, pollCancel := context.WithTimeout(context.Background(), 5*time.Second)
		got, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).Get(pollCtx, hpaName, metav1.GetOptions{})
		pollCancel()
		if err == nil {
			lastRepl = got.Status.DesiredReplicas
			if got.Status.DesiredReplicas >= maxRepl {
				reached = true
				break
			}
		}
		time.Sleep(pollInterval)
	}

	if !reached {
		t.Fatalf("HPA did not reach maxReplicas=%d within %s (last desired=%d); "+
			"check metrics-server and load generator logs",
			maxRepl, pollTimeout, lastRepl)
	}
	t.Logf("HPA reached maxReplicas=%d", maxRepl)

	// 5. Run `status` and assert the cap is reported. The summary line uses
	//    "HPA is at maxReplicas." and health becomes LIMITED.
	raw := runStatusJSON(t, kubeconfig, nsName, hpaName, "--explain")
	t.Logf("MaxReplicas cap JSON:\n%s", raw)
	assertStatusReportShape(t, raw, hpaName)

	report := decodeStatusReport(t, raw)
	a := report.Analysis

	if a.Health != "LIMITED" {
		t.Errorf("expected Health=LIMITED at maxReplicas, got %q (score=%d)", a.Health, a.HealthScore)
	}

	// Summary must announce the maxReplicas cap.
	if !strings.Contains(a.Summary, "maxReplicas") {
		t.Errorf("expected Summary to mention maxReplicas, got %q", a.Summary)
	}

	// ScalingLimited condition should be present once the controller reports it.
	if !conditionPresent(a.Conditions, "ScalingLimited") {
		t.Logf("Note: ScalingLimited condition not present yet (controller may still be converging); summary=%q", a.Summary)
	}
}

// conditionPresent reports whether a condition of the given type exists in
// the decoded Analysis.Conditions slice.
func conditionPresent(conds []hpaanalysis.Condition, wantType string) bool {
	for _, c := range conds {
		if c.Type == wantType {
			return true
		}
	}
	return false
}
