package kube

import (
	"fmt"
	"math"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ExtractKEDAInfo parses an unstructured ScaledObject into a structured KEDAInfo.
func ExtractKEDAInfo(u *unstructured.Unstructured) KEDAInfo {
	if u == nil {
		return KEDAInfo{}
	}
	info := KEDAInfo{
		ScaledObjectName: u.GetName(),
	}

	spec, ok := nestedMap(u.Object, "spec")
	if ok {
		info.Triggers = extractTriggers(spec)
		info.PollingInterval = extractInt32Ptr(spec, "pollingInterval")
		info.CooldownPeriod = extractInt32Ptr(spec, "cooldownPeriod")
		info.MinReplicaCount = extractInt32Ptr(spec, "minReplicaCount")
		info.MaxReplicaCount = extractInt32Ptr(spec, "maxReplicaCount")
		info.IdleReplicaCount = extractInt32Ptr(spec, "idleReplicaCount")
		if advanced, ok := nestedMap(spec, "advanced"); ok {
			info.Advanced = extractAdvanced(advanced)
		}
		info.Fallback = extractFallback(spec)
		info.ScalingPolicies = extractScalingPolicies(spec)
	}

	if status, ok := nestedMap(u.Object, "status"); ok {
		info.Conditions = extractKEDAConditions(status)
		// Merge trigger health status into triggers extracted from spec.
		extractTriggerStatus(u, info.Triggers)
	}

	return info
}

func extractTriggers(spec map[string]any) []KEDATrigger {
	triggersRaw, ok := nestedSlice(spec, "triggers")
	if !ok {
		return nil
	}
	triggers := make([]KEDATrigger, 0, len(triggersRaw))
	for _, t := range triggersRaw {
		tm, ok := mapAt(t)
		if !ok {
			continue
		}
		trigger := KEDATrigger{
			Type: stringValue(tm, "type"),
			Name: stringValue(tm, "name"),
		}
		if metadata, ok := nestedMap(tm, "metadata"); ok {
			trigger.Metadata = make(map[string]string, len(metadata))
			for k, v := range metadata {
				trigger.Metadata[k] = fmt.Sprintf("%v", v)
			}
			// Extract threshold from common metadata keys used by KEDA scalers.
			if v, ok := metadata["threshold"]; ok {
				trigger.Threshold = fmt.Sprintf("%v", v)
			} else if v, ok := metadata["value"]; ok {
				trigger.Threshold = fmt.Sprintf("%v", v)
			}
		}
		// Extract metricType to determine the produced metric name.
		if ms, ok := nestedString(tm, "metricType"); ok && ms != "" {
			trigger.MetricName = ms
		}
		// Extract authenticationRef.name from the trigger spec.
		if authRef, ok := nestedMap(tm, "authenticationRef"); ok {
			trigger.AuthenticationRef = stringValue(authRef, "name")
		}
		triggers = append(triggers, trigger)
	}
	return triggers
}

// extractTriggerStatus reads status.health from the ScaledObject and merges
// per-trigger health status (Active/Inactive/Unknown) into the triggers slice.
func extractTriggerStatus(u *unstructured.Unstructured, triggers []KEDATrigger) {
	status, ok := nestedMap(u.Object, "status")
	if !ok {
		return
	}
	health, ok := nestedMap(status, "health")
	if !ok {
		// No per-trigger health; try conditions for overall status.
		return
	}

	// KEDA v2: status.health is a map keyed by trigger name or index.
	for i := range triggers {
		t := &triggers[i]
		var entry map[string]any
		if t.Name != "" {
			entry, _ = health[t.Name].(map[string]any)
		}
		if entry == nil {
			entry, _ = health[t.Type].(map[string]any)
		}
		if entry == nil {
			continue
		}
		t.Status = mapHealthStatus(stringValue(entry, "status"))
		t.Message = stringValue(entry, "message")
		// Extract current metric value from health entry.
		if cv, ok := entry["currentValue"]; ok {
			t.CurrentValue = fmt.Sprintf("%v", cv)
		}
		// Override threshold from health entry if available (more accurate than spec metadata).
		if th, ok := entry["threshold"]; ok {
			t.Threshold = fmt.Sprintf("%v", th)
		}
	}
}

// mapHealthStatus converts KEDA health status strings to a normalized form.
func mapHealthStatus(s string) string {
	switch strings.ToLower(s) {
	case "active", "happy", "true":
		return "Active"
	case "inactive", "false":
		return "Inactive"
	case "unknown", "":
		return "Unknown"
	default:
		return s
	}
}

// extractFallback reads spec.fallback from the ScaledObject.
func extractFallback(spec map[string]any) *KEDAFallback {
	fm, ok := nestedMap(spec, "fallback")
	if !ok {
		return nil
	}
	threshold := extractInt32Ptr(fm, "failureThreshold")
	replicas := extractInt32Ptr(fm, "replicas")
	if threshold == nil && replicas == nil {
		return nil
	}
	fallback := &KEDAFallback{}
	if threshold != nil {
		fallback.FailureThreshold = *threshold
	}
	if replicas != nil {
		fallback.Replicas = *replicas
	}
	return fallback
}

// extractScalingPolicies reads scaling policies from
// spec.advanced.horizontalPodAutoscalerConfig.behavior.
func extractScalingPolicies(spec map[string]any) []KEDAScalingPolicy {
	advanced, ok := nestedMap(spec, "advanced")
	if !ok {
		return nil
	}
	hpaConfig, ok := nestedMap(advanced, "horizontalPodAutoscalerConfig")
	if !ok {
		return nil
	}
	behavior, ok := nestedMap(hpaConfig, "behavior")
	if !ok {
		return nil
	}

	var policies []KEDAScalingPolicy
	for _, direction := range []string{"scaleUp", "scaleDown"} {
		rules, ok := nestedMap(behavior, direction)
		if !ok {
			continue
		}
		rawPolicies, ok := nestedSlice(rules, "policies")
		if !ok {
			continue
		}
		for _, p := range rawPolicies {
			pm, ok := mapAt(p)
			if !ok {
				continue
			}
			value := extractInt32Ptr(pm, "value")
			period := extractInt32Ptr(pm, "periodSeconds")
			if value == nil {
				continue
			}
			sp := KEDAScalingPolicy{
				Type:  direction,
				Value: *value,
			}
			if period != nil {
				sp.PeriodSeconds = *period
			}
			policies = append(policies, sp)
		}
	}
	return policies
}

func extractKEDAConditions(status map[string]any) []KEDACondition {
	conditionsRaw, ok := nestedSlice(status, "conditions")
	if !ok {
		return nil
	}
	conditions := make([]KEDACondition, 0, len(conditionsRaw))
	for _, c := range conditionsRaw {
		cm, ok := mapAt(c)
		if !ok {
			continue
		}
		conditions = append(conditions, KEDACondition{
			Type:    stringValue(cm, "type"),
			Status:  stringValue(cm, "status"),
			Reason:  stringValue(cm, "reason"),
			Message: stringValue(cm, "message"),
		})
	}
	return conditions
}

func extractScaleTargetRef(u *unstructured.Unstructured) *autoscalingv2.CrossVersionObjectReference {
	spec, ok := nestedMap(u.Object, "spec")
	if !ok {
		return nil
	}
	ref, ok := nestedMap(spec, "scaleTargetRef")
	if !ok {
		return nil
	}
	return &autoscalingv2.CrossVersionObjectReference{
		APIVersion: stringValue(ref, "apiVersion"),
		Kind:       stringValue(ref, "kind"),
		Name:       stringValue(ref, "name"),
	}
}

func extractInt32Ptr(m map[string]any, key string) *int32 {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case int64:
		if v < math.MinInt32 || v > math.MaxInt32 {
			return nil
		}
		val := int32(v) // #nosec G115 -- bounds checked immediately above
		return &val
	case int:
		if v < math.MinInt32 || v > math.MaxInt32 {
			return nil
		}
		val := int32(v) // #nosec G115 -- bounds checked immediately above
		return &val
	case float64:
		if v < math.MinInt32 || v > math.MaxInt32 {
			return nil
		}
		val := int32(v)
		return &val
	default:
		return nil
	}
}

func extractAdvanced(advanced map[string]any) map[string]string {
	result := make(map[string]string, len(advanced))
	for k, v := range advanced {
		result[k] = fmt.Sprintf("%v", v)
	}
	return result
}

func stringValue(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}
