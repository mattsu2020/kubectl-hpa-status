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
}

// TestAnalysisGroupViews_SharedPointersAreReadOnly documents that pointer
// fields in the views share storage with the underlying Analysis. This is the
// intended additive-migration contract: views are read-only snapshots, callers
// must not mutate the shared pointers.
func TestAnalysisGroupViews_SharedPointersAreReadOnly(t *testing.T) {
	tr := &TargetReplicaInfo{ReadyReplicas: 5}
	a := &Analysis{TargetReplicas: tr}

	r := a.Replicas()
	if r.TargetReplicas != tr {
		t.Fatal("Replicas view should share the TargetReplicas pointer")
	}
}
