package cmd

import (
	"context"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
)

// TestTargetReplicaObservationsEnricherGating verifies the previously
// always-on enricher that reads the scale target (Deployment/StatefulSet/
// ReplicaSet and Pods) is now gated behind the depth-tier flags. A plain
// status must not touch the scale target; explain/depth flags must.
func TestTargetReplicaObservationsEnricherGating(t *testing.T) {
	// BuildHPA gives a Deployment scale target, so when the enricher runs it
	// issues a Deployment Get (+ Pods List when there are pending pods).
	hpa := testutil.BuildHPA("default", "web", testutil.WithReplicas(2, 2))
	fakeClient := testutil.NewFakeClient(hpa)

	cases := []struct {
		label string
		opts  *options
		// readsScaleTarget reports whether the run is expected to touch the
		// scale target (Deployment/StatefulSet/ReplicaSet) or Pods. Plain
		// status must not; depth-tier flags must.
		readsScaleTarget bool
	}{
		{
			label: "plain status reads no scale target",
			opts: &options{Common: commonOptions{
				ConnectionOptions: ConnectionOptions{
					ClientOverride: fakeClient,
				},
			}},
			readsScaleTarget: false,
		},
		{
			label: "--explain reads scale target",
			opts: &options{
				Common: commonOptions{
					ConnectionOptions: ConnectionOptions{
						ClientOverride: fakeClient,
					},
				},
				Status: statusOptions{Features: feats("explain"), Events: EventOption{Enabled: false}},
			},
			readsScaleTarget: true,
		},
		{
			label: "--explain-pods reads scale target",
			opts: &options{
				Common: commonOptions{
					ConnectionOptions: ConnectionOptions{
						ClientOverride: fakeClient,
					},
				},
				Status: statusOptions{Features: feats("explainPods"), Events: EventOption{Enabled: false}},
			},
			readsScaleTarget: true,
		},
		{
			label: "--deep reads scale target",
			opts: &options{
				Common: commonOptions{
					ConnectionOptions: ConnectionOptions{
						ClientOverride: fakeClient,
					},
				},
				Status: statusOptions{Features: feats("deep"), Events: EventOption{Enabled: false}},
			},
			readsScaleTarget: true,
		},
		{
			label: "--no-enrich reads no scale target and skips all enrichment",
			opts: &options{
				Common: commonOptions{
					ConnectionOptions: ConnectionOptions{
						ClientOverride: fakeClient,
					},
				},
				Status: statusOptions{Features: feats("noEnrich"), Events: EventOption{Enabled: false}},
			},
			readsScaleTarget: false,
		},
		{
			label: "--hpa-only alias reads no scale target",
			opts: &options{
				Common: commonOptions{
					ConnectionOptions: ConnectionOptions{
						ClientOverride: fakeClient,
					},
				},
				Status: statusOptions{Features: feats("hpaOnly"), Events: EventOption{Enabled: false}},
			},
			readsScaleTarget: false,
		},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			// Fresh fake per case so Actions() reflects only this run.
			fresh := testutil.NewFakeClient(hpa)
			c.opts.ClientOverride = fresh
			c.opts.Normalize()

			var out nulWriter
			if err := runStatus(context.Background(), out, c.opts, "web", c.opts.Explain); err != nil {
				// Some paths return a benign exit-code warning on limited
				// analysis; treat non-exitcode errors as fatal.
				if !isExitCodeWarning(err) {
					t.Fatalf("runStatus: %v", err)
				}
			}

			// The scale-target enricher reads Deployment/StatefulSet/ReplicaSet
			// and (when there are pending pods) Pods. Detecting any of these
			// resources proves the enricher ran; detecting none proves plain
			// status stayed HPA-only — the RBAC-light guarantee.
			scaleTargetResources := map[string]bool{
				"pods": true, "deployments": true, "statefulsets": true, "replicasets": true,
			}
			touched := false
			for _, a := range fresh.Actions() {
				if scaleTargetResources[a.GetResource().Resource] {
					touched = true
					break
				}
			}
			if touched != c.readsScaleTarget {
				t.Fatalf("scale-target read = %v, want %v (actions: %+v)", touched, c.readsScaleTarget, fresh.Actions())
			}
		})
	}
}

// TestDeepProfileEnablesDeepFeatures verifies --analysis-profile deep turns on
// the same feature set as --deep, so the two entry points cannot drift.
func TestDeepProfileEnablesDeepFeatures(t *testing.T) {
	// Normalize applies both the profile expansion and the --deep expansion;
	// compare the resulting feature booleans for equivalence.
	profileOpts := &options{}
	profileOpts.AnalysisProfile = "deep"
	profileOpts.Normalize()

	flagOpts := &options{}
	flagOpts.Deep = true
	flagOpts.Normalize()

	deepFields := []struct {
		name string
		get  func(o *options) bool
	}{
		{"CapacityContext", func(o *options) bool { return o.CapacityContext }},
		{"CapacityHeadroom", func(o *options) bool { return o.CapacityHeadroom }},
		{"CapacityDeep", func(o *options) bool { return o.CapacityDeep }},
		{"ScalePath", func(o *options) bool { return o.ScalePath }},
		{"RolloutImpact", func(o *options) bool { return o.RolloutImpact }},
		{"ReadinessImpact", func(o *options) bool { return o.ReadinessImpact }},
		{"AdapterDiagnostics", func(o *options) bool { return o.AdapterDiagnostics }},
		{"ExplainPods", func(o *options) bool { return o.ExplainPods }},
	}
	for _, f := range deepFields {
		if f.get(profileOpts) != f.get(flagOpts) {
			t.Fatalf("profile deep vs --deep mismatch on %s: profile=%v flag=%v",
				f.name, f.get(profileOpts), f.get(flagOpts))
		}
		if !f.get(flagOpts) {
			t.Fatalf("--deep did not enable %s", f.name)
		}
	}
}

// nulWriter discards all output; the gating tests only inspect client Actions.
type nulWriter struct{}

func (nulWriter) Write(p []byte) (int, error) { return len(p), nil }

// feats returns a Features value with the named flags enabled, for terser
// test setup. Names match the featureSetters keys in cmdoptions/features.go.
func feats(names ...string) featuresOptions {
	var f featuresOptions
	for _, n := range names {
		f.Enable(n)
	}
	return f
}
