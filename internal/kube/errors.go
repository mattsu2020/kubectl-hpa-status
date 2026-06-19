package kube

import "errors"

// Sentinel errors for Kubernetes client / scale-target lookups. Wrap these with
// fmt.Errorf("...: %w", ErrXxx) at the call site so callers can match on the
// concrete condition with errors.Is instead of substring matching on the
// English message text.
var (
	// ErrScaledObjectNotFound is returned when no KEDA ScaledObject can be
	// resolved for an HPA.
	ErrScaledObjectNotFound = errors.New("scaledobject not found for HPA")

	// ErrUnsupportedScaleTargetKind is returned when the HPA scaleTargetRef
	// references a kind this package does not know how to resolve
	// (Deployment / StatefulSet / ReplicaSet).
	ErrUnsupportedScaleTargetKind = errors.New("unsupported scale target kind")

	// ErrKEDACRDNotDetected is returned by DetectCRDs when the
	// keda.sh/v1alpha1 API group cannot be resolved via discovery. A wrapped
	// error of nil means the API group is simply absent; a non-nil wrapped
	// error means discovery itself failed (network, RBAC, etc.).
	ErrKEDACRDNotDetected = errors.New("keda.sh/v1alpha1 not found in API discovery")

	// ErrVPACRDNotDetected is returned by DetectCRDs when the
	// autoscaling.k8s.io/v1 API group cannot be resolved via discovery. See
	// ErrKEDACRDNotDetected for the wrapping semantics.
	ErrVPACRDNotDetected = errors.New("autoscaling.k8s.io/v1 not found in API discovery")
)
