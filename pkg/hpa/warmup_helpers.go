package hpa

import "fmt"

// podStateCounts tallies not-ready pods by their failure category.
// Ready pods are skipped entirely.
type podStateCounts struct {
	runningNotReady int32
	imagePull       int32
	scheduling      int32
	crashLoop       int32
	unknown         int32
}

// countPodStates inspects each pod detail and increments the matching
// failure category for pods that are not yet Ready.
func countPodStates(details []WarmupPodDetail) podStateCounts {
	var c podStateCounts
	for _, pod := range details {
		if pod.Ready {
			continue
		}
		switch {
		case pod.WaitingReason == "ImagePullBackOff" || pod.WaitingReason == "ErrImagePull":
			c.imagePull++
		case pod.WaitingReason == "CrashLoopBackOff":
			c.crashLoop++
		case pod.ContainerState == "" && !pod.Ready:
			// No container state means pod is Pending (not scheduled yet).
			c.scheduling++
		case pod.ContainerState == "running" && !pod.Ready:
			c.runningNotReady++
		case pod.ContainerState == "terminated" || pod.RestartCount > 3:
			c.crashLoop++
		default:
			c.unknown++
		}
	}
	return c
}

// readinessProbeBottlenecks emits the readiness/unknown bottleneck for pods
// that are Running but not Ready. When a readinessProbe is present the failure
// is attributed to the probe; otherwise it is reported as unknown.
func readinessProbeBottlenecks(runningNotReady int32, probePresent bool) []WarmupBottleneck {
	if runningNotReady <= 0 {
		return nil
	}
	if probePresent {
		return []WarmupBottleneck{{
			Type:       "readiness_probe",
			Severity:   SeverityWarning,
			Confidence: ConfidenceHigh,
			Count:      runningNotReady,
			Message:    fmt.Sprintf("%d pods are Running but not Ready (readinessProbe failing)", runningNotReady),
		}}
	}
	return []WarmupBottleneck{{
		Type:       "unknown",
		Severity:   SeverityWarning,
		Confidence: ConfidenceMedium,
		Count:      runningNotReady,
		Message:    fmt.Sprintf("%d pods are Running but not Ready (no readinessProbe configured)", runningNotReady),
	}}
}

// startupProbeBottleneck emits a startupProbe bottleneck when young running
// pods are likely still inside the startupProbe phase. Returns nil otherwise.
func startupProbeBottleneck(input WarmupInput, runningNotReady int32) []WarmupBottleneck {
	if !input.StartupProbePresent || runningNotReady <= 0 {
		return nil
	}
	var startupBlocked int32
	for _, pod := range input.PodDetails {
		if !pod.Ready && pod.ContainerState == "running" && pod.AgeSeconds < int64(input.StartupProbeMaxDelaySeconds) {
			startupBlocked++
		}
	}
	if startupBlocked <= 0 {
		return nil
	}
	return []WarmupBottleneck{{
		Type:       "startup_probe",
		Severity:   SeverityInfo,
		Confidence: ConfidenceMedium,
		Count:      startupBlocked,
		Message:    fmt.Sprintf("%d pods may still be in startupProbe phase", startupBlocked),
	}}
}

// imagePullBottleneck emits an image_pull bottleneck when any pod is in
// ImagePullBackOff or ErrImagePull.
func imagePullBottleneck(count int32) []WarmupBottleneck {
	if count <= 0 {
		return nil
	}
	return []WarmupBottleneck{{
		Type:       "image_pull",
		Severity:   SeverityError,
		Confidence: ConfidenceHigh,
		Count:      count,
		Message:    fmt.Sprintf("%d pods have image pull issues (ImagePullBackOff/ErrImagePull)", count),
	}}
}

// schedulingBottleneck emits a scheduling bottleneck for Pending pods that
// have not been scheduled yet.
func schedulingBottleneck(count int32) []WarmupBottleneck {
	if count <= 0 {
		return nil
	}
	return []WarmupBottleneck{{
		Type:       "scheduling",
		Severity:   SeverityWarning,
		Confidence: ConfidenceHigh,
		Count:      count,
		Message:    fmt.Sprintf("%d pods are Pending (not yet scheduled)", count),
	}}
}

// containerCrashBottleneck emits a container_crash bottleneck for crash-looping
// pods (CrashLoopBackOff or high restart count).
func containerCrashBottleneck(count int32) []WarmupBottleneck {
	if count <= 0 {
		return nil
	}
	return []WarmupBottleneck{{
		Type:       "container_crash",
		Severity:   SeverityError,
		Confidence: ConfidenceHigh,
		Count:      count,
		Message:    fmt.Sprintf("%d pods are crash-looping (CrashLoopBackOff or high restart count)", count),
	}}
}

// unknownBottleneck emits an unknown bottleneck for not-ready pods that do not
// match any known failure category.
func unknownBottleneck(count int32) []WarmupBottleneck {
	if count <= 0 {
		return nil
	}
	return []WarmupBottleneck{{
		Type:       "unknown",
		Severity:   SeverityInfo,
		Confidence: ConfidenceLow,
		Count:      count,
		Message:    fmt.Sprintf("%d pods are not Ready for unknown reasons", count),
	}}
}

// metricsInactiveBottleneck emits the metrics_inactive bottleneck when the HPA
// ScalingActive condition is false.
func metricsInactiveBottleneck(scalingActive bool) []WarmupBottleneck {
	if scalingActive {
		return nil
	}
	return []WarmupBottleneck{{
		Type:       "metrics_inactive",
		Severity:   SeverityError,
		Confidence: ConfidenceHigh,
		Message:    "HPA ScalingActive is false; metrics pipeline may be down",
	}}
}
