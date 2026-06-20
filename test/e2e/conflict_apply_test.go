//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/mattsu2020/kubectl-hpa-status/cmd"
)

// This file covers the remaining E2E scenarios that need optional cluster
// resources or an explicit dry-run workflow:
//   - #6 VPA conflict present / absent
//   - #7 single-HPA --apply --dry-run (validate-only, nothing persisted)
//   - #5 KEDA CRD present (the existing TestE2E_KEDAManagedHPA covers the
//     CRD-absent branch by detecting labels only).

// newDynamicAndTypedClients builds a dynamic client and a typed clientset from
// the same kubeconfig. Used by VPA/KEDA CRD tests that must create resources
// outside the typed kubernetes.Clientset.
func newDynamicAndTypedClients(t *testing.T, kubeconfig string) (dynamic.Interface, *kubernetes.Clientset) {
	t.Helper()
	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatalf("failed to build rest config: %v", err)
	}
	dyn, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("failed to create dynamic client: %v", err)
	}
	typed, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("failed to create typed clientset: %v", err)
	}
	return dyn, typed
}

// crdInstalled reports whether a given group/version is registered via discovery.
func crdInstalled(t *testing.T, kubeconfig, groupVersion string) bool {
	t.Helper()
	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return false
	}
	disco, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return false
	}
	_, err = disco.ServerResourcesForGroupVersion(groupVersion)
	return err == nil
}

// vpaGVR is the GroupVersionResource for VerticalPodAutoscalers.
var vpaGVR = schema.GroupVersionResource{
	Group: "autoscaling.k8s.io", Version: "v1", Resource: "verticalpodautoscalers",
}

// createVPA creates a VPA targeting the same ReplicationController as the HPA,
// in "Auto" mode so it conflicts with HPA CPU/memory metrics. Returns a cleanup
// function that deletes the VPA.
func createVPA(t *testing.T, dyn dynamic.Interface, nsName, vpaName, targetName string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	vpa := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "autoscaling.k8s.io/v1",
		"kind":       "VerticalPodAutoscaler",
		"metadata": map[string]any{
			"name":      vpaName,
			"namespace": nsName,
		},
		"spec": map[string]any{
			"targetRef": map[string]any{
				"apiVersion": "v1",
				"kind":       "ReplicationController",
				"name":       targetName,
			},
			"updatePolicy": map[string]any{
				"updateMode": "Auto",
			},
		},
	}}
	if _, err := dyn.Resource(vpaGVR).Namespace(nsName).Create(ctx, vpa, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create VPA %s: %v", vpaName, err)
	}
	t.Cleanup(func() {
		delCtx, delCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer delCancel()
		_ = dyn.Resource(vpaGVR).Namespace(nsName).Delete(delCtx, vpaName, metav1.DeleteOptions{})
	})
}

// TestE2E_VPAConflict verifies that `status <hpa> --vpa=on` reports a VPA
// conflict when a VPA targets the same workload (scenario #6 positive case).
// Requires the autoscaling.k8s.io/v1 CRD; skips otherwise.
func TestE2E_VPAConflict(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	if !crdInstalled(t, kubeconfig, "autoscaling.k8s.io/v1") {
		t.Skip("Skipping VPA conflict test: autoscaling.k8s.io/v1 CRD (VPA) not installed")
	}
	_, typed, nsName := setupTestNamespace(t, kubeconfig)
	dyn, _ := newDynamicAndTypedClients(t, kubeconfig)

	createTestRC(t, typed, nsName, "vpa-conflict-rc")
	// Healthy HPA uses a CPU resource metric, which is the overlapping signal
	// the VPA conflict detector keys on.
	createHealthyHPA(t, typed, nsName, "vpa-conflict-hpa", "vpa-conflict-rc")
	createVPA(t, dyn, nsName, "vpa-conflict-vpa", "vpa-conflict-rc")

	raw := runStatusJSON(t, kubeconfig, nsName, "vpa-conflict-hpa", "--vpa=on", "--explain")
	t.Logf("VPA conflict JSON:\n%s", raw)
	assertStatusReportShape(t, raw, "vpa-conflict-hpa")

	a := decodeStatusReport(t, raw).Analysis
	if a.VPAConflict == nil {
		t.Fatal("expected Analysis.VPAConflict to be populated with --vpa=on and a conflicting VPA present")
	}
	if a.VPAConflict.VPAName != "vpa-conflict-vpa" {
		t.Errorf("VPAConflict.VPAName = %q, want %q", a.VPAConflict.VPAName, "vpa-conflict-vpa")
	}
	if a.VPAConflict.Warning == "" {
		t.Error("VPAConflict.Warning is empty; expected a human-readable warning string")
	}
	if !strings.Contains(a.VPAConflict.Warning, "vpa-conflict-vpa") {
		t.Errorf("VPAConflict.Warning %q does not mention the VPA name", a.VPAConflict.Warning)
	}

	// VPA conflict carries a -20 health penalty; verify it was applied.
	if a.Health != "LIMITED" {
		t.Errorf("expected Health=LIMITED for VPA conflict, got %q (score=%d)", a.Health, a.HealthScore)
	}

	// The text path should also surface the conflict via --explain output.
	textBuf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(textBuf)
	rootCmd.SetErr(textBuf)
	rootCmd.SetArgs([]string{"status", "vpa-conflict-hpa", "-n", nsName, "--vpa=on", "--explain", "--kubeconfig", kubeconfig})
	if err := rootCmd.Execute(); err != nil {
		var ec *cmd.ExitCodeError
		if !asExitCodeError(err, &ec) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if !strings.Contains(textBuf.String(), "vpa-conflict-vpa") {
		t.Errorf("expected text output to reference the VPA name, got:\n%s", textBuf.String())
	}
}

// TestE2E_VPANoConflict is the negative case for scenario #6: with --vpa=on
// and no VPA in the namespace, the analysis must report no conflict and no
// VPAConflict field. Skips when the VPA CRD is absent because --vpa=on then
// has nothing to enumerate.
func TestE2E_VPANoConflict(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	if !crdInstalled(t, kubeconfig, "autoscaling.k8s.io/v1") {
		t.Skip("Skipping VPA no-conflict test: autoscaling.k8s.io/v1 CRD (VPA) not installed")
	}
	_, typed, nsName := setupTestNamespace(t, kubeconfig)
	createTestRC(t, typed, nsName, "vpa-clean-rc")
	createHealthyHPA(t, typed, nsName, "vpa-clean-hpa", "vpa-clean-rc")

	raw := runStatusJSON(t, kubeconfig, nsName, "vpa-clean-hpa", "--vpa=on", "--explain")
	t.Logf("VPA no-conflict JSON:\n%s", raw)
	assertStatusReportShape(t, raw, "vpa-clean-hpa")

	a := decodeStatusReport(t, raw).Analysis
	if a.VPAConflict != nil {
		t.Errorf("expected nil VPAConflict with no VPA present, got %+v", a.VPAConflict)
	}
}

// TestE2E_SingleHPAApplyDryRun exercises the single-HPA apply workflow
// (scenario #7) end-to-end: a maxReplicas-capped HPA yields a suggestion,
// the suggestion is validated via server-side dry-run, and nothing is
// persisted. The test re-reads the HPA after the run to confirm maxReplicas
// is unchanged.
func TestE2E_SingleHPAApplyDryRun(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "apply-single-rc")
	// A ScalingLimited HPA at maxReplicas is the canonical case that emits
	// a RaiseMaxReplicas suggestion, which the apply path can patch.
	createScalingLimitedHPA(t, client, nsName, "apply-single-hpa", "apply-single-rc")

	// Capture the original maxReplicas so we can prove the dry-run did not
	// persist anything.
	ctxGet, cancelGet := context.WithTimeout(context.Background(), 10*time.Second)
	before, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).Get(ctxGet, "apply-single-hpa", metav1.GetOptions{})
	cancelGet()
	if err != nil {
		t.Fatalf("failed to read HPA before apply: %v", err)
	}
	origMax := before.Spec.MaxReplicas

	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"status", "apply-single-hpa", "-n", nsName,
		"--suggest", "--apply", "--yes", "--dry-run",
		"--kubeconfig", kubeconfig,
	})
	if err := rootCmd.Execute(); err != nil {
		t.Logf("apply --dry-run returned error (may be ExitCodeError for LIMITED health): %v", err)
	}

	output := buf.String()
	t.Logf("Single-HPA apply dry-run output:\n%s", output)

	// The dry-run workflow prints these sentinel lines when a patch is
	// proposed and validated. If no patch was suggested, the command still
	// succeeds and prints "No applicable HPA patch was suggested." — accept
	// either branch so the test is not brittle to suggestion-rule changes.
	patchValidated := strings.Contains(output, "passed server-side dry-run validation")
	dryRunHeld := strings.Contains(output, "Dry-run mode is enabled")
	noPatch := strings.Contains(output, "No applicable HPA patch was suggested.")

	switch {
	case patchValidated && dryRunHeld:
		// Expected happy path: a patch was proposed, validated, and held.
	case noPatch:
		t.Logf("No patch was suggested for this HPA; dry-run path still exercised without error")
	default:
		t.Errorf("expected dry-run validation or no-patch sentinel in output, got:\n%s", output)
	}

	// The critical assertion: maxReplicas must be unchanged after dry-run.
	ctxAfter, cancelAfter := context.WithTimeout(context.Background(), 10*time.Second)
	after, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).Get(ctxAfter, "apply-single-hpa", metav1.GetOptions{})
	cancelAfter()
	if err != nil {
		t.Fatalf("failed to read HPA after apply: %v", err)
	}
	if after.Spec.MaxReplicas != origMax {
		t.Errorf("dry-run persisted a change: maxReplicas %d -> %d (nothing should be persisted with --dry-run)", origMax, after.Spec.MaxReplicas)
	}
}

// TestE2E_KEDACRDPresent covers scenario #5 with a real ScaledObject CRD
// installed. It creates a ScaledObject and an HPA referencing it, then runs
// `status <hpa> --keda=on` and asserts the KEDA enrichment is populated.
// Skips when the keda.sh/v1alpha1 CRD is absent (the common case; the
// CRD-absent branch is already covered by TestE2E_KEDAManagedHPA).
func TestE2E_KEDACRDPresent(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	if !crdInstalled(t, kubeconfig, "keda.sh/v1alpha1") {
		t.Skip("Skipping KEDA CRD-present test: keda.sh/v1alpha1 CRD not installed")
	}
	_, typed, nsName := setupTestNamespace(t, kubeconfig)
	dyn, _ := newDynamicAndTypedClients(t, kubeconfig)

	const (
		soName  = "keda-crd-so"
		hpaName = "keda-crd-hpa"
		rcName  = "keda-crd-rc"
	)
	createTestRC(t, typed, nsName, rcName)

	// Minimal ScaledObject referencing the RC with a cron trigger.
	soGVR := schema.GroupVersionResource{Group: "keda.sh", Version: "v1alpha1", Resource: "scaledobjects"}
	so := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "keda.sh/v1alpha1",
		"kind":       "ScaledObject",
		"metadata": map[string]any{
			"name":      soName,
			"namespace": nsName,
		},
		"spec": map[string]any{
			"scaleTargetRef": map[string]any{
				"apiVersion": "v1",
				"kind":       "ReplicationController",
				"name":       rcName,
			},
			"minReplicaCount": int64(2),
			"maxReplicaCount": int64(10),
			"triggers": []any{
				map[string]any{
					"type": "cron",
					"metadata": map[string]any{
						"timezone": "UTC",
						"start":    "0 * * * *",
						"end":      "1 * * * *",
						"desired":  "5",
					},
				},
			},
		},
	}}
	ctxCreate, cancelCreate := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelCreate()
	if _, err := dyn.Resource(soGVR).Namespace(nsName).Create(ctxCreate, so, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create ScaledObject: %v", err)
	}
	t.Cleanup(func() {
		delCtx, delCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer delCancel()
		_ = dyn.Resource(soGVR).Namespace(nsName).Delete(delCtx, soName, metav1.DeleteOptions{})
	})

	// HPA with KEDA label pointing back at the ScaledObject.
	minReplicas := int32(2)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hpaName,
			Namespace: nsName,
			Labels:    map[string]string{"scaledobject.keda.sh/name": soName},
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "v1",
				Kind:       "ReplicationController",
				Name:       rcName,
			},
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ExternalMetricSourceType,
				External: &autoscalingv2.ExternalMetricSource{
					Metric: autoscalingv2.MetricIdentifier{Name: "s1-cron"},
					Target: autoscalingv2.MetricTarget{
						Type:  autoscalingv2.AverageValueMetricType,
						Value: resourcePtr("5"),
					},
				},
			}},
		},
	}
	ctxHPA, cancelHPA := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelHPA()
	if _, err := typed.AutoscalingV2().HorizontalPodAutoscalers(nsName).Create(ctxHPA, hpa, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create HPA: %v", err)
	}
	// Mark ScalingActive so the status is healthy enough to render.
	hpa.Status = autoscalingv2.HorizontalPodAutoscalerStatus{
		CurrentReplicas: 2,
		DesiredReplicas: 2,
		Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{{
			Type:               autoscalingv2.ScalingActive,
			Status:             corev1.ConditionTrue,
			Reason:             "ValidMetricFound",
			Message:            "the HPA was able to successfully calculate a recommendation",
			LastTransitionTime: metav1.Now(),
		}},
	}
	if _, err := typed.AutoscalingV2().HorizontalPodAutoscalers(nsName).UpdateStatus(ctxHPA, hpa, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update HPA status: %v", err)
	}

	raw := runStatusJSON(t, kubeconfig, nsName, hpaName, "--keda=on", "--explain")
	t.Logf("KEDA CRD-present JSON:\n%s", raw)
	assertStatusReportShape(t, raw, hpaName)

	// With the CRD present and --keda=on, KEDA enrichment should populate the
	// analysis.keda object. The exact trigger status depends on controller
	// reconciliation, so we only assert the ScaledObject name is attached.
	result := decodeStatusReportJSON(t, raw)
	a := analysisMap(t, result)
	keda, ok := a["keda"].(map[string]any)
	if !ok {
		t.Fatalf("analysis.keda missing or wrong type with --keda=on and CRD present; got %T", a["keda"])
	}
	scaledObjectName, _ := keda["scaledObjectName"].(string)
	if scaledObjectName != soName {
		t.Errorf("analysis.keda.scaledObjectName = %q, want %q", scaledObjectName, soName)
	}
}
