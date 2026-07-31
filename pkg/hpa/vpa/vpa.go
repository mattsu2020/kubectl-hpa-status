// Package vpa analyzes coexistence conflicts between an HPA and a
// VerticalPodAutoscaler targeting the same workload. It is a self-contained
// leaf domain: it depends only on the autoscaling/v2 API types. The cmd/
// layer reaches it through the pkg/hpa re-export facade
// (hpaanalysis.VPAConflictInfo, hpaanalysis.AnalyzeVPA, etc.) so existing
// import paths keep working.
package vpa

import (
	"fmt"
	"sort"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// RecommendationInfo captures the visible recommendation values for one
// container/resource pair, as extracted from a VPA object.
type RecommendationInfo struct {
	Container string `json:"container" yaml:"container"`
	Resource  string `json:"resource" yaml:"resource"`
	Target    string `json:"target,omitempty" yaml:"target,omitempty"`
	Lower     string `json:"lower,omitempty" yaml:"lower,omitempty"`
	Upper     string `json:"upper,omitempty" yaml:"upper,omitempty"`
}

// ContainerPolicy preserves the VPA policy that applies to one named
// container (or "*" for the wildcard/default policy).
type ContainerPolicy struct {
	ContainerName       string   `json:"containerName" yaml:"containerName"`
	Mode                string   `json:"mode,omitempty" yaml:"mode,omitempty"`
	ControlledResources []string `json:"controlledResources,omitempty" yaml:"controlledResources,omitempty"`
	// ControlledResourcesSpecified distinguishes an omitted field (which uses
	// the VPA cpu+memory defaults) from an explicitly empty resource list. It is
	// part of the public wire model so JSON/YAML persistence retains that choice.
	ControlledResourcesSpecified bool `json:"controlledResourcesSpecified,omitempty" yaml:"controlledResourcesSpecified,omitempty"`
}

// Info holds the parsed fields of a VerticalPodAutoscaler relevant to HPA
// conflict analysis. This type lives in pkg/hpa/vpa (canonical) and is
// re-exported as hpaanalysis.VPAInfo via a type alias; internal/kube has its
// own VPAInfo for extraction. External consumers build conflict inputs without
// depending on internal/kube by using the pkg/hpa alias.
type Info struct {
	Name                string               `json:"name" yaml:"name"`
	TargetRef           string               `json:"targetRef" yaml:"targetRef"`
	TargetAPIVersion    string               `json:"targetApiVersion,omitempty" yaml:"targetApiVersion,omitempty"`
	TargetKind          string               `json:"targetKind" yaml:"targetKind"`
	TargetName          string               `json:"targetName" yaml:"targetName"`
	UpdateMode          string               `json:"updateMode" yaml:"updateMode"`
	ControlledResources []string             `json:"controlledResources,omitempty" yaml:"controlledResources,omitempty"`
	ContainerPolicies   []ContainerPolicy    `json:"containerPolicies,omitempty" yaml:"containerPolicies,omitempty"`
	Recommendations     []RecommendationInfo `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
}

// ConflictInfo holds VPA conflict detection result.
type ConflictInfo struct {
	VPAName             string           `json:"vpaName" yaml:"vpaName"`
	TargetKind          string           `json:"targetKind" yaml:"targetKind"`
	TargetName          string           `json:"targetName" yaml:"targetName"`
	UpdateMode          string           `json:"updateMode,omitempty" yaml:"updateMode,omitempty"`
	ControlledResources []string         `json:"controlledResources,omitempty" yaml:"controlledResources,omitempty"`
	Recommendations     []Recommendation `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
	Warning             string           `json:"warning" yaml:"warning"`
	// ControlledResourcesResolved distinguishes an intentionally empty
	// HPA-specific overlap from an omitted VPA controlledResources field,
	// whose API default is cpu+memory. It is serialized so a no-overlap result
	// remains no-overlap after JSON/YAML persistence.
	ControlledResourcesResolved bool `json:"controlledResourcesResolved,omitempty" yaml:"controlledResourcesResolved,omitempty"`
	// sourceInfo retains policy and target identity for callers that use the
	// compatibility NewConflictInfo -> AnalyzeAdvisory path. It stays out of
	// the wire model; emitted ConflictResources remains the public result.
	sourceInfo *Info
}

// Recommendation is the display/API model for one visible VPA recommendation.
type Recommendation struct {
	Container string `json:"container" yaml:"container"`
	Resource  string `json:"resource" yaml:"resource"`
	Target    string `json:"target,omitempty" yaml:"target,omitempty"`
	Lower     string `json:"lower,omitempty" yaml:"lower,omitempty"`
	Upper     string `json:"upper,omitempty" yaml:"upper,omitempty"`
}

// Analyze generates warning lines when VPA conflicts with HPA.
// Returns nil if there is no conflict to report.
func Analyze(hpa *autoscalingv2.HorizontalPodAutoscaler, v *Info) []string {
	if hpa == nil || v == nil {
		return nil
	}

	// Skip if VPA is in "Off" mode — it only recommends, never applies changes.
	if strings.EqualFold(v.UpdateMode, "Off") {
		return nil
	}
	if !infoTargetsHPA(hpa, v) {
		return nil
	}

	conflictResources := conflictResourcesForInfo(hpa, v)
	if len(conflictResources) == 0 {
		return nil
	}

	var lines []string

	lines = append(lines, fmt.Sprintf("[observed] VPA %q targets the same resource %s/%s as this HPA.", v.Name, v.TargetKind, v.TargetName))
	lines = append(lines, fmt.Sprintf("[observed] Both VPA and HPA manage the overlapping resource(s) %s; this can cause conflicting scaling decisions and instability.", strings.Join(conflictResources, ", ")))
	lines = append(lines, "[observed] Consider setting the VPA updateMode to \"Off\" so it only provides recommendations without applying pod overrides, or remove the overlapping resource metric from one of the autoscalers.")

	switch {
	case strings.EqualFold(v.UpdateMode, "Auto"):
		lines = append(lines, fmt.Sprintf("[observed] VPA %q is in \"Auto\" mode, which can evict and resize pods — this directly conflicts with HPA replica-based scaling.", v.Name))
	case !strings.EqualFold(v.UpdateMode, "Initial"):
		mode := valueOrUnknown(v.UpdateMode)
		lines = append(lines, fmt.Sprintf("[observed] VPA %q is in active update mode %q, which can resize pod requests — this directly conflicts with HPA replica-based scaling.", v.Name, mode))
	}
	for _, rec := range v.Recommendations {
		if !containsResource(conflictResources, rec.Resource) ||
			!recommendationOverlapsHPA(hpa, v, rec) {
			continue
		}
		lines = append(lines, fmt.Sprintf("[estimated] VPA %q recommends %s target=%s for container %q while HPA also scales on %s; compare requests, limits, and HPA target utilization before applying both controllers.", v.Name, rec.Resource, valueOrUnknown(rec.Target), rec.Container, rec.Resource))
	}

	return lines
}

// NewConflictInfo converts extracted VPA data into the public analysis model.
func NewConflictInfo(v *Info) *ConflictInfo {
	if v == nil {
		return nil
	}
	info := &ConflictInfo{
		VPAName:             v.Name,
		TargetKind:          v.TargetKind,
		TargetName:          v.TargetName,
		UpdateMode:          v.UpdateMode,
		ControlledResources: append([]string(nil), v.ControlledResources...),
		Warning:             fmt.Sprintf("VPA %s targets %s/%s; compare it with a specific HPA before classifying resource conflicts", v.Name, v.TargetKind, v.TargetName),
		sourceInfo:          cloneInfo(v),
	}
	for _, rec := range v.Recommendations {
		info.Recommendations = append(info.Recommendations, Recommendation(rec))
	}
	return info
}

func cloneInfo(source *Info) *Info {
	if source == nil {
		return nil
	}
	clone := *source
	clone.ControlledResources = append([]string(nil), source.ControlledResources...)
	clone.ContainerPolicies = make([]ContainerPolicy, len(source.ContainerPolicies))
	for i := range source.ContainerPolicies {
		clone.ContainerPolicies[i] = source.ContainerPolicies[i]
		clone.ContainerPolicies[i].ControlledResources =
			append([]string(nil), source.ContainerPolicies[i].ControlledResources...)
	}
	clone.Recommendations = append([]RecommendationInfo(nil), source.Recommendations...)
	return &clone
}

// NewConflictInfoForHPA converts extracted VPA data while retaining only the
// resources that actually overlap this HPA after container policy resolution.
func NewConflictInfoForHPA(hpa *autoscalingv2.HorizontalPodAutoscaler, v *Info) *ConflictInfo {
	info := NewConflictInfo(v)
	if info == nil {
		return nil
	}
	info.ControlledResources = conflictResourcesForInfo(hpa, v)
	info.ControlledResourcesResolved = true
	if len(info.ControlledResources) == 0 {
		info.Warning = ""
		return info
	}
	targetKind := v.TargetKind
	targetName := v.TargetName
	if hpa != nil {
		targetKind = hpa.Spec.ScaleTargetRef.Kind
		targetName = hpa.Spec.ScaleTargetRef.Name
	}
	info.Warning = fmt.Sprintf(
		"VPA %s and HPA both target %s/%s with overlapping resource metrics: %s",
		v.Name, targetKind, targetName, strings.Join(info.ControlledResources, ", "),
	)
	return info
}

func hpaUsesResourceMetric(hpa *autoscalingv2.HorizontalPodAutoscaler, resource string) bool {
	for _, m := range hpa.Spec.Metrics {
		metricResource, _, _, ok := resourceMetricIdentity(m)
		if ok && metricResource == resource {
			return true
		}
	}
	return false
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "<unknown>"
	}
	return value
}

// ConflictLevel classifies the severity of a VPA-HPA coexistence conflict.
type ConflictLevel string

const (
	// ConflictNone indicates no active conflict between VPA and HPA.
	ConflictNone ConflictLevel = "NONE"
	// ConflictWarning indicates a potential conflict that warrants monitoring.
	ConflictWarning ConflictLevel = "WARNING"
	// ConflictError indicates both controllers are actively fighting over the same resources.
	ConflictError ConflictLevel = "ERROR"
)

// Advisory provides structured VPA-HPA coexistence analysis with actionable
// recommendations for resolving or mitigating conflicts.
type Advisory struct {
	Level             ConflictLevel `json:"level" yaml:"level"`
	ConflictResources []string      `json:"conflictResources,omitempty" yaml:"conflictResources,omitempty"`
	RecommendedMode   string        `json:"recommendedMode,omitempty" yaml:"recommendedMode,omitempty"`
	Recommendations   []string      `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
	SafeCoexistence   bool          `json:"safeCoexistence" yaml:"safeCoexistence"`
	Explanation       string        `json:"explanation,omitempty" yaml:"explanation,omitempty"`
	VPAPatch          string        `json:"vpaPatch,omitempty" yaml:"vpaPatch,omitempty"`
	HPAActions        []string      `json:"hpaActions,omitempty" yaml:"hpaActions,omitempty"`
	VPAActions        []string      `json:"vpaActions,omitempty" yaml:"vpaActions,omitempty"`
}

// AnalyzeAdvisory produces a structured VPA-HPA coexistence advisory.
// Returns nil if either hpa or vpa is nil.
func AnalyzeAdvisory(hpa *autoscalingv2.HorizontalPodAutoscaler, v *ConflictInfo) *Advisory {
	if hpa == nil || v == nil {
		return nil
	}

	level := determineConflictLevel(hpa, v)
	conflictResources := identifyConflictResources(hpa, v)

	advisory := &Advisory{
		Level:             level,
		ConflictResources: conflictResources,
		SafeCoexistence:   level != ConflictError,
	}

	advisory.Recommendations = generateRecommendations(level, v)
	advisory.RecommendedMode = recommendedModeForLevel(level)
	advisory.Explanation = buildExplanation(level, v, conflictResources)
	advisory.VPAPatch = generateVPAPatch(level)
	advisory.HPAActions = generateHPAActions(level, conflictResources)
	advisory.VPAActions = generateVPAActions(level, v)

	return advisory
}

// determineConflictLevel classifies the VPA-HPA conflict severity.
func determineConflictLevel(hpa *autoscalingv2.HorizontalPodAutoscaler, v *ConflictInfo) ConflictLevel {
	mode := v.UpdateMode

	if strings.EqualFold(mode, "Off") {
		return ConflictNone
	}

	if len(identifyConflictResources(hpa, v)) == 0 {
		return ConflictNone
	}

	if strings.EqualFold(mode, "Initial") {
		return ConflictWarning
	}

	// Auto and any other active mode that evicts/resizes pods.
	return ConflictError
}

// identifyConflictResources returns the subset of VPA-controlled resources
// that the HPA also scales on.
func identifyConflictResources(hpa *autoscalingv2.HorizontalPodAutoscaler, v *ConflictInfo) []string {
	if v.sourceInfo != nil {
		return conflictResourcesForInfo(hpa, v.sourceInfo)
	}
	if v.ControlledResourcesResolved && len(v.ControlledResources) == 0 {
		return nil
	}
	return overlappingResources(hpa, v.ControlledResources)
}

func overlappingResources(hpa *autoscalingv2.HorizontalPodAutoscaler, controlledResources []string) []string {
	if hpa == nil {
		return nil
	}
	controlled := controlledResources
	if len(controlled) == 0 {
		// VPA's API default is to control cpu and memory when the field is omitted.
		controlled = []string{"cpu", "memory"}
	}
	seen := make(map[string]struct{}, len(controlled))
	var conflicts []string
	for _, resource := range controlled {
		resource = strings.ToLower(resource)
		if _, ok := seen[resource]; ok || !hpaUsesResourceMetric(hpa, resource) {
			continue
		}
		seen[resource] = struct{}{}
		conflicts = append(conflicts, resource)
	}
	sort.Strings(conflicts)
	return conflicts
}

func infoTargetsHPA(hpa *autoscalingv2.HorizontalPodAutoscaler, info *Info) bool {
	if hpa == nil || info == nil {
		return false
	}
	ref := hpa.Spec.ScaleTargetRef
	if info.TargetKind != "" && info.TargetKind != ref.Kind {
		return false
	}
	if info.TargetName != "" && info.TargetName != ref.Name {
		return false
	}
	return info.TargetAPIVersion == "" ||
		ref.APIVersion == "" ||
		strings.EqualFold(info.TargetAPIVersion, ref.APIVersion)
}

func conflictResourcesForInfo(hpa *autoscalingv2.HorizontalPodAutoscaler, info *Info) []string {
	if hpa == nil || info == nil || !infoTargetsHPA(hpa, info) {
		return nil
	}
	if len(info.ContainerPolicies) == 0 {
		return overlappingResources(hpa, info.ControlledResources)
	}
	conflicts := map[string]struct{}{}
	for _, metric := range hpa.Spec.Metrics {
		resource, container, aggregate, ok := resourceMetricIdentity(metric)
		if !ok || (resource != "cpu" && resource != "memory") {
			continue
		}
		var controlled bool
		if aggregate {
			controlled = policiesControlAggregate(info.ContainerPolicies, resource)
		} else {
			controlled = policiesControlContainer(info.ContainerPolicies, container, resource)
		}
		if controlled {
			conflicts[resource] = struct{}{}
		}
	}
	resources := make([]string, 0, len(conflicts))
	for resource := range conflicts {
		resources = append(resources, resource)
	}
	sort.Strings(resources)
	return resources
}

// recommendationOverlapsHPA reports whether a VPA recommendation describes a
// resource/container pair that this HPA actually scales on and that the VPA's
// effective container policy controls. Aggregate Resource metrics may match
// recommendations for any controlled container; ContainerResource metrics
// only match their named container.
func recommendationOverlapsHPA(
	hpa *autoscalingv2.HorizontalPodAutoscaler,
	info *Info,
	recommendation RecommendationInfo,
) bool {
	if hpa == nil || info == nil {
		return false
	}
	resource := strings.ToLower(recommendation.Resource)
	for _, metric := range hpa.Spec.Metrics {
		metricResource, metricContainer, aggregate, ok := resourceMetricIdentity(metric)
		if !ok || metricResource != resource {
			continue
		}
		if !aggregate && recommendation.Container != metricContainer {
			continue
		}
		if len(info.ContainerPolicies) > 0 {
			if policiesControlContainer(info.ContainerPolicies, recommendation.Container, resource) {
				return true
			}
			continue
		}
		if containsResource(effectiveTopLevelResources(info.ControlledResources), resource) {
			return true
		}
	}
	return false
}

func effectiveTopLevelResources(controlledResources []string) []string {
	if len(controlledResources) == 0 {
		return []string{"cpu", "memory"}
	}
	return controlledResources
}

func resourceMetricIdentity(metric autoscalingv2.MetricSpec) (resource, container string, aggregate, ok bool) {
	switch metric.Type {
	case autoscalingv2.ResourceMetricSourceType:
		if metric.Resource != nil && isUtilizationTarget(metric.Resource.Target) {
			return strings.ToLower(string(metric.Resource.Name)), "", true, true
		}
	case autoscalingv2.ContainerResourceMetricSourceType:
		if metric.ContainerResource != nil && isUtilizationTarget(metric.ContainerResource.Target) {
			return strings.ToLower(string(metric.ContainerResource.Name)), metric.ContainerResource.Container, false, true
		}
	}
	return "", "", false, false
}

func isUtilizationTarget(target autoscalingv2.MetricTarget) bool {
	return target.Type == autoscalingv2.UtilizationMetricType || target.Type == ""
}

func effectiveResources(policy ContainerPolicy) []string {
	if !policy.ControlledResourcesSpecified {
		return []string{"cpu", "memory"}
	}
	return policy.ControlledResources
}

func containerPolicyControls(policy ContainerPolicy, resource string) bool {
	if strings.EqualFold(policy.Mode, "Off") {
		return false
	}
	for _, controlled := range effectiveResources(policy) {
		if strings.EqualFold(controlled, resource) {
			return true
		}
	}
	return false
}

func anyContainerPolicyControls(policies []ContainerPolicy, resource string) bool {
	for _, policy := range policies {
		if containerPolicyControls(policy, resource) {
			return true
		}
	}
	return false
}

func policiesControlContainer(policies []ContainerPolicy, container, resource string) bool {
	var exact, wildcard []ContainerPolicy
	for _, policy := range policies {
		switch policy.ContainerName {
		case container:
			exact = append(exact, policy)
		case "*":
			wildcard = append(wildcard, policy)
		}
	}
	if len(exact) > 0 {
		return anyContainerPolicyControls(exact, resource)
	}
	if len(wildcard) > 0 {
		return anyContainerPolicyControls(wildcard, resource)
	}
	return resource == "cpu" || resource == "memory"
}

func policiesControlAggregate(policies []ContainerPolicy, resource string) bool {
	var wildcard []ContainerPolicy
	for _, policy := range policies {
		if policy.ContainerName == "*" {
			wildcard = append(wildcard, policy)
			continue
		}
		if containerPolicyControls(policy, resource) {
			return true
		}
	}
	if len(wildcard) > 0 {
		return anyContainerPolicyControls(wildcard, resource)
	}
	return resource == "cpu" || resource == "memory"
}

func containsResource(resources []string, wanted string) bool {
	for _, resource := range resources {
		if strings.EqualFold(resource, wanted) {
			return true
		}
	}
	return false
}

// generateRecommendations produces actionable recommendation strings for the
// given conflict level.
func generateRecommendations(level ConflictLevel, v *ConflictInfo) []string {
	switch level {
	case ConflictError:
		return []string{
			"Switch VPA updateMode to 'Initial' so it only sets initial requests without evicting pods",
			"Or move HPA to external/custom metrics (RPS, queue depth) to eliminate resource overlap",
			"Review resource requests on the workload to ensure they are appropriate",
		}
	case ConflictWarning:
		return []string{
			"VPA is in 'Initial' mode: safe but monitor for pod restart timing interactions",
			"Consider adding resource requests explicitly to avoid VPA recalculating at each rollout",
		}
	case ConflictNone:
		if v.UpdateMode != "" {
			return []string{
				fmt.Sprintf("VPA is in '%s' mode: no active conflict with HPA", v.UpdateMode),
			}
		}
		return nil
	default:
		return nil
	}
}

// recommendedModeForLevel returns the safest VPA updateMode for the given
// conflict level.
func recommendedModeForLevel(level ConflictLevel) string {
	switch level {
	case ConflictError:
		return "Initial"
	case ConflictWarning:
		return "Off"
	default:
		return ""
	}
}

// generateVPAPatch returns a JSON patch to remediate the conflict, if
// applicable.
func generateVPAPatch(level ConflictLevel) string {
	if level == ConflictError {
		return `{"spec":{"updatePolicy":{"updateMode":"Initial"}}}`
	}
	return ""
}

// buildExplanation produces a human-readable explanation of the conflict.
func buildExplanation(level ConflictLevel, v *ConflictInfo, conflictResources []string) string {
	switch level {
	case ConflictError:
		return fmt.Sprintf(
			"VPA %q is in %q mode and both VPA and HPA are actively managing %v on %s/%s. "+
				"VPA evicts pods to resize resource requests while HPA adjusts replica counts based on utilization. "+
				"This creates a feedback loop where VPA resizing triggers HPA scaling and vice versa, "+
				"leading to unstable workload behavior.",
			v.VPAName, v.UpdateMode, conflictResources,
			v.TargetKind, v.TargetName,
		)
	case ConflictWarning:
		return fmt.Sprintf(
			"VPA %q is in %q mode targeting %s/%s. While VPA only sets initial resource requests "+
				"at pod creation time and does not evict running pods, there is still a potential "+
				"interaction during rollouts when new pods receive VPA-calculated requests that may "+
				"affect HPA utilization calculations.",
			v.VPAName, v.UpdateMode,
			v.TargetKind, v.TargetName,
		)
	case ConflictNone:
		if strings.EqualFold(v.UpdateMode, "Off") {
			return fmt.Sprintf(
				"VPA %q is in %q mode targeting %s/%s: it provides recommendations only "+
					"without applying any changes to pods, so there is no conflict with HPA.",
				v.VPAName, v.UpdateMode,
				v.TargetKind, v.TargetName,
			)
		}
		return fmt.Sprintf(
			"VPA %q is in %q mode targeting %s/%s. Its controlledResources do not overlap "+
				"the HPA resource metrics, so there is no active resource conflict between the two controllers.",
			v.VPAName, v.UpdateMode,
			v.TargetKind, v.TargetName,
		)
	default:
		return ""
	}
}

// generateHPAActions returns recommended actions for the HPA side.
func generateHPAActions(level ConflictLevel, conflictResources []string) []string {
	if level == ConflictError {
		actions := []string{
			"Consider replacing resource metrics with external or custom metrics to avoid overlap with VPA",
		}
		if len(conflictResources) > 0 {
			actions = append(actions,
				fmt.Sprintf("Remove %v metric(s) from HPA and let VPA manage those resource requests", conflictResources),
			)
		}
		return actions
	}
	return nil
}

// generateVPAActions returns recommended actions for the VPA side.
func generateVPAActions(level ConflictLevel, v *ConflictInfo) []string {
	switch level {
	case ConflictError:
		return []string{
			fmt.Sprintf("Change VPA %q updateMode from %q to 'Initial' to prevent pod evictions", v.VPAName, v.UpdateMode),
			"Alternatively, set updateMode to 'Off' to disable active resource management while retaining recommendations",
		}
	case ConflictWarning:
		return []string{
			fmt.Sprintf("Consider changing VPA %q updateMode to 'Off' for safest coexistence with HPA", v.VPAName),
		}
	default:
		return nil
	}
}
