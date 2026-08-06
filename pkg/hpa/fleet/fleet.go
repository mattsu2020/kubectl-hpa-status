// Package fleet aggregates fleet-wide HPA capacity risk from a slice of
// HorizontalPodAutoscaler objects. It is a self-contained leaf domain that
// depends only on the autoscaling/v2 API types and produces a typed Report
// suitable for direct JSON/YAML serialization or text rendering by the cmd
// layer. The risk model classifies each HPA by the headroom between current
// replicas and maxReplicas, then ranks the top risks by additional pods that
// could be requested if every HPA scaled to its configured ceiling.
package fleet

import (
	"fmt"
	"sort"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// topRisksLimit caps the number of entries returned in Report.TopRisks so
// downstream renderers stay bounded on large fleets.
const topRisksLimit = 10

// Report is the fleet-wide HPA capacity risk summary. Field names and JSON
// tags are stable output schema; preserve them when extending the model.
type Report struct {
	Risk                    string     `json:"risk" yaml:"risk"`
	HPAs                    int        `json:"hpas" yaml:"hpas"`
	CurrentPods             int32      `json:"currentPods" yaml:"currentPods"`
	WorstCasePods           int32      `json:"worstCasePods" yaml:"worstCasePods"`
	AdditionalPods          int32      `json:"additionalPods" yaml:"additionalPods"`
	AtMaxReplicas           int        `json:"atMaxReplicas" yaml:"atMaxReplicas"`
	WithoutConfiguredMetric int        `json:"withoutConfiguredMetric" yaml:"withoutConfiguredMetric"`
	TopRisks                []RiskItem `json:"topRisks,omitempty" yaml:"topRisks,omitempty"`
}

// RiskItem is one HPA's contribution to the fleet risk ranking. It captures
// the current/max replica counts plus the additional pods that could be added
// if the HPA reaches maxReplicas.
type RiskItem struct {
	Namespace      string `json:"namespace" yaml:"namespace"`
	Name           string `json:"name" yaml:"name"`
	Target         string `json:"target" yaml:"target"`
	Current        int32  `json:"currentReplicas" yaml:"currentReplicas"`
	Max            int32  `json:"maxReplicas" yaml:"maxReplicas"`
	AdditionalPods int32  `json:"additionalPods" yaml:"additionalPods"`
	Risk           string `json:"risk" yaml:"risk"`
}

// BuildReport aggregates a slice of HPAs into a fleet risk Report using the
// supplied risk model label. HPAs whose current replica count is zero fall
// back to DesiredReplicas; additional-pod counts that would go negative are
// clamped to zero. The TopRisks list is sorted by AdditionalPods descending,
// then namespace/name ascending, and truncated to the top ten entries.
//
// BuildReport is pure: the input slice is read but neither reordered nor
// modified.
func BuildReport(hpas []autoscalingv2.HorizontalPodAutoscaler, risk string) Report {
	report := Report{Risk: risk, HPAs: len(hpas)}
	for i := range hpas {
		hpa := &hpas[i]
		current := hpa.Status.CurrentReplicas
		if current == 0 {
			current = hpa.Status.DesiredReplicas
		}
		additional := hpa.Spec.MaxReplicas - current
		if additional < 0 {
			additional = 0
		}
		report.CurrentPods += current
		report.WorstCasePods += current + additional
		report.AdditionalPods += additional
		if current >= hpa.Spec.MaxReplicas {
			report.AtMaxReplicas++
		}
		if len(hpa.Spec.Metrics) == 0 {
			report.WithoutConfiguredMetric++
		}
		if additional > 0 {
			report.TopRisks = append(report.TopRisks, RiskItem{
				Namespace:      hpa.Namespace,
				Name:           hpa.Name,
				Target:         hpa.Spec.ScaleTargetRef.Kind + "/" + hpa.Spec.ScaleTargetRef.Name,
				Current:        current,
				Max:            hpa.Spec.MaxReplicas,
				AdditionalPods: additional,
				Risk:           fmt.Sprintf("could add +%d pod(s) if this HPA reaches maxReplicas", additional),
			})
		}
	}
	sort.Slice(report.TopRisks, func(i, j int) bool {
		if report.TopRisks[i].AdditionalPods != report.TopRisks[j].AdditionalPods {
			return report.TopRisks[i].AdditionalPods > report.TopRisks[j].AdditionalPods
		}
		if report.TopRisks[i].Namespace != report.TopRisks[j].Namespace {
			return report.TopRisks[i].Namespace < report.TopRisks[j].Namespace
		}
		return report.TopRisks[i].Name < report.TopRisks[j].Name
	})
	if len(report.TopRisks) > topRisksLimit {
		report.TopRisks = report.TopRisks[:topRisksLimit]
	}
	return report
}
