package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

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
