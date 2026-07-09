package hpa

import (
	"testing"
)

// TestAnalysisGroupViews asserts the additive group-view methods snapshot the
// correct flat fields. These views are the first step of the v2 grouping
// migration (ROADMAP.md): the flat fields stay the source of truth and keep
// their JSON tags, while new code reaches related fields via these views.
func TestAnalysisGroupViews(t *testing.T) {
	a := &Analysis{
		Namespace:   "production",
		Name:        "web",
		Target:      "Deployment/web",
		Current:     5,
		Desired:     7,
		Min:         2,
		Max:         10,
		Health:      "OK",
		HealthScore: 90,
		Summary:     "scaling up",
		SummaryKey:  "dir_scale_up",
		Conditions:  []Condition{{Type: "ScalingActive", Status: "True"}},
		Metrics:     []Metric{{Name: "cpu"}},
		Actions:     []string{"raise maxReplicas"},
		Warnings:    []string{"enrichment skipped"},
	}

	t.Run("Meta snapshots identity", func(t *testing.T) {
		m := a.Meta()
		if m.Namespace != "production" || m.Name != "web" || m.Target != "Deployment/web" {
			t.Fatalf("Meta identity wrong: %+v", m)
		}
	})

	t.Run("Replicas snapshots scaling envelope", func(t *testing.T) {
		r := a.Replicas()
		if r.Current != 5 || r.Desired != 7 || r.Min != 2 || r.Max != 10 {
			t.Fatalf("Replicas envelope wrong: %+v", r)
		}
	})

	t.Run("Decision snapshots health signals", func(t *testing.T) {
		d := a.Decision()
		if d.Health != "OK" || d.HealthScore != 90 || d.Summary != "scaling up" || d.SummaryKey != "dir_scale_up" {
			t.Fatalf("Decision signals wrong: %+v", d)
		}
	})

	t.Run("MetricsGroup snapshots pipeline signals", func(t *testing.T) {
		m := a.MetricsGroup()
		if len(m.Metrics) != 1 || m.Metrics[0].Name != "cpu" {
			t.Fatalf("MetricsGroup wrong: %+v", m)
		}
	})

	t.Run("ConditionsGroup snapshots conditions", func(t *testing.T) {
		c := a.ConditionsGroup()
		if len(c.Conditions) != 1 || c.Conditions[0].Type != "ScalingActive" {
			t.Fatalf("ConditionsGroup wrong: %+v", c)
		}
	})

	t.Run("ActionsGroup snapshots recommendations", func(t *testing.T) {
		act := a.ActionsGroup()
		if len(act.Actions) != 1 || act.Actions[0] != "raise maxReplicas" {
			t.Fatalf("ActionsGroup wrong: %+v", act)
		}
		if len(act.Warnings) != 1 || act.Warnings[0] != "enrichment skipped" {
			t.Fatalf("ActionsGroup warnings wrong: %+v", act)
		}
	})

	t.Run("Lifecycle snapshots telemetry", func(t *testing.T) {
		// No stale/trend set; view must be zero-valued, not panic.
		l := a.Lifecycle()
		if l.StaleStatus != nil || l.HealthTrend != nil {
			t.Fatalf("Lifecycle should be zero for unset telemetry: %+v", l)
		}
	})

	t.Run("Capacity snapshots capacity signals", func(t *testing.T) {
		// Unset capacity fields must yield a zero view without panic.
		c := a.Capacity()
		if c.CapacityContext != nil || c.CapacityPlan != nil || c.PodAnalysis != nil {
			t.Fatalf("Capacity should be zero for unset fields: %+v", c)
		}
	})

	t.Run("ScaleToZeroGroup snapshots cold-start signals", func(t *testing.T) {
		s := a.ScaleToZeroGroup()
		if s.ScaleToZero != nil || s.WarmupAnalysis != nil {
			t.Fatalf("ScaleToZeroGroup should be zero for unset fields: %+v", s)
		}
	})

	t.Run("Stability snapshots flapping/churn signals", func(t *testing.T) {
		s := a.Stability()
		if s.FlappingSimulation != nil || s.ChurnAnalysis != nil {
			t.Fatalf("Stability should be zero for unset fields: %+v", s)
		}
	})

	t.Run("Advisory snapshots VPA/tuning advice", func(t *testing.T) {
		adv := a.Advisory()
		if adv.VPAConflict != nil || adv.ContainerAdvisor != nil {
			t.Fatalf("Advisory should be zero for unset fields: %+v", adv)
		}
	})

	t.Run("Controllers snapshots external controller signals", func(t *testing.T) {
		c := a.Controllers()
		if c.KEDAInfo != nil || c.RolloutDiagnosis != nil || c.ControllerProfile != nil {
			t.Fatalf("Controllers should be zero for unset fields: %+v", c)
		}
	})

	t.Run("Blockers snapshots apply-time gates", func(t *testing.T) {
		b := a.Blockers()
		if b.BlockerReport != nil || b.GitOpsConflict != nil {
			t.Fatalf("Blockers should be zero for unset fields: %+v", b)
		}
	})

	t.Run("HealthState mirrors Health string", func(t *testing.T) {
		if a.HealthState() != HealthOK {
			t.Fatalf("HealthState() = %q, want %q", a.HealthState(), HealthOK)
		}
	})
}

// TestAnalysisGroupViews_SharedPointersAreReadOnly documents that pointer
// fields in the views share storage with the underlying Analysis. This is the
// intended additive-migration contract: views are read-only snapshots, callers
// must not mutate the shared pointers.
func TestAnalysisGroupViews_SharedPointersAreReadOnly(t *testing.T) {
	tr := &TargetReplicaInfo{ReadyReplicas: 5}
	plan := &CapacityPlan{Safe: true}
	a := &Analysis{TargetReplicas: tr, CapacityPlan: plan}

	r := a.Replicas()
	if r.TargetReplicas != tr {
		t.Fatal("Replicas view should share the TargetReplicas pointer")
	}
	c := a.Capacity()
	if c.CapacityPlan != plan {
		t.Fatal("Capacity view should share the CapacityPlan pointer")
	}
}
