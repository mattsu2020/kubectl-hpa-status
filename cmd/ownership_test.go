package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
)

func TestNewOwnershipCommand(t *testing.T) {
	opts := &options{}
	cmd := newOwnershipCommand(opts)

	if cmd.Use != "ownership NAME [NAME...]" {
		t.Fatalf("unexpected Use: %q", cmd.Use)
	}
	if !strings.Contains(cmd.Short, "ownership") {
		t.Fatalf("unexpected Short: %q", cmd.Short)
	}
}

func TestRunOwnershipJSONOutput(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 90),
		testutil.WithScaleTargetRef("Deployment", "web"),
	)
	deployment := testutil.BuildDeployment("default", "web")
	fakeClient := testutil.NewFakeClientWithObjects(hpa, deployment)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				Namespace:      "default",
				ClientOverride: fakeClient,
			},
			OutputOptions: OutputOptions{
				Output: "json",
			},
		},
	}

	if err := runOwnership(context.Background(), &buf, opts, []string{"web"}); err != nil {
		t.Fatalf("runOwnership returned error: %v", err)
	}

	var report ownershipReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse JSON output: %v\n%s", err, buf.String())
	}
	if report.Namespace != "default" || report.Name != "web" {
		t.Fatalf("unexpected identity: %s/%s", report.Namespace, report.Name)
	}
	if report.Target != "Deployment/web" {
		t.Fatalf("unexpected target: %q", report.Target)
	}
	if report.TargetSpecReplicas == nil || *report.TargetSpecReplicas != 1 {
		t.Fatalf("unexpected targetSpecReplicas: %v", report.TargetSpecReplicas)
	}
	// spec.replicas=1 differs from HPA desired 5, so the drift risk must fire.
	foundDrift := false
	for _, risk := range report.Risks {
		if strings.Contains(risk, "spec.replicas=1 differs from HPA desiredReplicas=5") {
			foundDrift = true
		}
	}
	if !foundDrift {
		t.Fatalf("expected spec.replicas drift risk, got risks: %v", report.Risks)
	}
}

func TestRunOwnershipMultipleNamesRendersListEnvelope(t *testing.T) {
	web := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 90),
		testutil.WithScaleTargetRef("Deployment", "web"),
	)
	api := testutil.BuildHPA("default", "api",
		testutil.WithReplicas(2, 2),
		testutil.WithResourceMetric("cpu", 80, 90),
		testutil.WithScaleTargetRef("Deployment", "api"),
	)
	webDeploy := testutil.BuildDeployment("default", "web")
	apiDeploy := testutil.BuildDeployment("default", "api")
	fakeClient := testutil.NewFakeClientWithObjects(web, api, webDeploy, apiDeploy)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				Namespace:      "default",
				ClientOverride: fakeClient,
			},
			OutputOptions: OutputOptions{
				Output: "json",
			},
		},
	}

	if err := runOwnership(context.Background(), &buf, opts, []string{"web", "api"}); err != nil {
		t.Fatalf("runOwnership returned error: %v", err)
	}

	var list ownershipListReport
	if err := json.Unmarshal(buf.Bytes(), &list); err != nil {
		t.Fatalf("failed to parse list JSON output: %v\n%s", err, buf.String())
	}
	if len(list.Items) != 2 {
		t.Fatalf("expected 2 items, got %d\n%s", len(list.Items), buf.String())
	}
	if list.Items[0].Name != "web" || list.Items[1].Name != "api" {
		t.Fatalf("expected input-order items web,api; got %s,%s", list.Items[0].Name, list.Items[1].Name)
	}
}

func TestRunOwnershipNotFoundWrapsSentinel(t *testing.T) {
	// No HPAs in the fake client: the lookup must fail with an error that
	// matches the ErrHPANotFound sentinel so classifyError and scripts that
	// rely on errors.Is keep working on this path.
	fakeClient := testutil.NewFakeClient()

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				Namespace:      "default",
				ClientOverride: fakeClient,
			},
			OutputOptions: OutputOptions{
				Output: "json",
			},
		},
	}

	err := runOwnership(context.Background(), &buf, opts, []string{"missing"})
	if err == nil {
		t.Fatal("runOwnership with missing HPA: want error, got nil")
	}
	if !errors.Is(err, ErrHPANotFound) {
		t.Fatalf("runOwnership error should match ErrHPANotFound, got: %v", err)
	}
}

// TestOwnsSpecReplicas pins the field-hierarchy parse behind the ownership
// check. A substring search for "f:replicas" also matched status.replicas,
// which controllers update constantly, so the status updater was misreported
// as a spec.replicas owner.
func TestOwnsSpecReplicas(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{
			name: "spec.replicas is ownership",
			raw:  `{"f:metadata":{},"f:spec":{"f:replicas":{}}}`,
			want: true,
		},
		{
			name: "status.replicas is not spec ownership",
			raw:  `{"f:status":{"f:replicas":{}}}`,
			want: false,
		},
		{
			name: "spec without replicas is not ownership",
			raw:  `{"f:spec":{"f:template":{}}}`,
			want: false,
		},
		{
			name: "invalid JSON is not ownership",
			raw:  `{not json`,
			want: false,
		},
		{
			name: "empty field set is not ownership",
			raw:  `{}`,
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ownsSpecReplicas([]byte(tc.raw)); got != tc.want {
				t.Fatalf("ownsSpecReplicas(%s) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// TestReplicaOwnershipManagersIgnoreStatusOnlyUpdater walks a realistic
// managed-fields pair — a GitOps controller owning spec.replicas and the
// workload controller owning only status fields — and requires exactly one
// spec.replicas manager.
func TestReplicaOwnershipManagersIgnoreStatusOnlyUpdater(t *testing.T) {
	entries := []metav1.ManagedFieldsEntry{
		{
			Manager:   "kubectl-client-side-apply",
			Operation: metav1.ManagedFieldsOperationUpdate,
			FieldsV1:  &metav1.FieldsV1{Raw: []byte(`{"f:spec":{"f:replicas":{}}}`)},
		},
		{
			Manager:   "kube-controller-manager",
			Operation: metav1.ManagedFieldsOperationUpdate,
			FieldsV1:  &metav1.FieldsV1{Raw: []byte(`{"f:status":{"f:replicas":{},"f:readyReplicas":{}}}`)},
		},
	}
	managers := replicaOwnershipManagers(entries)
	if len(managers) != 1 {
		t.Fatalf("expected exactly one spec.replicas manager, got %+v", managers)
	}
	if managers[0].Manager != "kubectl-client-side-apply" || managers[0].Field != "spec.replicas" {
		t.Fatalf("unexpected manager entry: %+v", managers[0])
	}
}

// TestRunOwnershipStatusReplicaUpdaterNotFlagged is the end-to-end false
// positive check: a Deployment whose managed fields only carry status.replicas
// (the workload controller's status updates) must not produce an ownership
// risk claiming that manager owns spec.replicas.
func TestRunOwnershipStatusReplicaUpdaterNotFlagged(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 90),
		testutil.WithScaleTargetRef("Deployment", "web"),
	)
	deployment := testutil.BuildDeployment("default", "web")
	deployment.ManagedFields = []metav1.ManagedFieldsEntry{
		{
			Manager:   "kube-controller-manager",
			Operation: metav1.ManagedFieldsOperationUpdate,
			FieldsV1:  &metav1.FieldsV1{Raw: []byte(`{"f:status":{"f:replicas":{}}}`)},
		},
	}
	fakeClient := testutil.NewFakeClientWithObjects(hpa, deployment)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				Namespace:      "default",
				ClientOverride: fakeClient,
			},
			OutputOptions: OutputOptions{Output: "json"},
		},
	}
	if err := runOwnership(context.Background(), &buf, opts, []string{"web"}); err != nil {
		t.Fatalf("runOwnership returned error: %v", err)
	}

	var report ownershipReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse JSON output: %v\n%s", err, buf.String())
	}
	if len(report.Managers) != 0 {
		t.Fatalf("status-only updater must not be listed as a spec.replicas manager, got %+v", report.Managers)
	}
	for _, risk := range report.Risks {
		if strings.Contains(risk, "manager") && strings.Contains(risk, "own spec.replicas") {
			t.Fatalf("false ownership risk emitted: %v", report.Risks)
		}
	}
}
