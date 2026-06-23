// Package kube provides Kubernetes client construction and resource helpers.
package kube

// Kubernetes version thresholds that govern this plugin's compatibility and
// feature gating. Centralizing them here keeps the error messages (status.go),
// the compat command (compat.go), the README requirements, and the CI matrix
// referencing a single source of truth, so a version bump touches one place.
//
// The split mirrors the three layers a user may care about:
//   - API availability: autoscaling/v2 exists (1.23+). The plugin *may* run,
//     but is not officially supported or E2E-tested below the stable line.
//   - Official support: the version line this project tests in CI and
//     documents as the requirement in the README (1.26+).
//   - Feature gates: versions where specific autoscaling/v2 fields became
//     stable or beta, used by the compat command and behavior tuning advice.
const (
	// k8sMinAPIVersion is the oldest Kubernetes minor version that ships the
	// autoscaling/v2 API. The plugin can technically load an HPA on this
	// version, but it is below the officially supported / E2E-tested range.
	k8sMinAPIVersion = "1.23"

	// k8sStableSinceVersion is the oldest Kubernetes minor version this
	// project officially supports and exercises in the CI kind matrix. The
	// autoscaling/v2 API is GA from this version.
	k8sStableSinceVersion = "1.26"

	// k8sContainerResourceStableVersion is the minor version where the
	// containerResource metric type became stable in autoscaling/v2.
	k8sContainerResourceStableVersion = "1.30"

	// k8sToleranceFeatureVersion is the minor version where the behavior
	// scaleUp/scaleDown tolerance field (HPAConfigurableTolerance) became
	// available as a beta field.
	k8sToleranceFeatureVersion = "1.35"
)

// KubernetesVersionInfo exposes the pinned version constants for external
// packages (cmd) that build user-facing strings from them. The values are
// intentionally copied into a struct of string/int fields rather than
// exported as loose constants so callers cannot accidentally depend on the
// raw minor integer for an unrelated comparison.
type KubernetesVersionInfo struct {
	MinAPIVersion        string
	StableSinceVersion   string
	ContainerResourceVer string
	ToleranceFeatureVer  string
	// StableSinceMinor is the integer minor of StableSinceVersion, for
	// numeric comparisons (e.g. the compat command's version switch).
	StableSinceMinor       int
	ContainerResourceMinor int
	ToleranceFeatureMinor  int
}

// KubernetesVersions returns the pinned Kubernetes version thresholds used
// across the plugin. Callers should format user-facing strings from these
// values rather than re-hardcoding "1.23"/"1.26"/etc.
func KubernetesVersions() KubernetesVersionInfo {
	return KubernetesVersionInfo{
		MinAPIVersion:          k8sMinAPIVersion,
		StableSinceVersion:     k8sStableSinceVersion,
		ContainerResourceVer:   k8sContainerResourceStableVersion,
		ToleranceFeatureVer:    k8sToleranceFeatureVersion,
		StableSinceMinor:       26,
		ContainerResourceMinor: 30,
		ToleranceFeatureMinor:  35,
	}
}
