package kube

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// NewFakeClient creates a fake Kubernetes clientset pre-loaded with HPA objects.
func NewFakeClient(hpas ...*autoscalingv2.HorizontalPodAutoscaler) *fake.Clientset {
	objects := make([]runtime.Object, 0, len(hpas))
	for _, hpa := range hpas {
		objects = append(objects, hpa)
	}
	return fake.NewSimpleClientset(objects...) //nolint:staticcheck // SA1019 deprecated, no replacement without applyconfig
}

// NewFakeClientWithEvents creates a fake Kubernetes clientset pre-loaded with
// HPA objects and associated Events.
func NewFakeClientWithEvents(hpas []*autoscalingv2.HorizontalPodAutoscaler, events []*corev1.Event) *fake.Clientset {
	objects := make([]runtime.Object, 0, len(hpas)+len(events))
	for _, hpa := range hpas {
		objects = append(objects, hpa)
	}
	for _, event := range events {
		objects = append(objects, event)
	}
	return fake.NewSimpleClientset(objects...) //nolint:staticcheck // SA1019 deprecated, no replacement without applyconfig
}

// BuildHPA creates a HorizontalPodAutoscaler with sensible defaults for testing.
func BuildHPA(namespace, name string, opts ...HPAOption) *autoscalingv2.HorizontalPodAutoscaler {
	minReplicas := int32(1)
	maxReplicas := int32(10)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: name,
			},
			MinReplicas: &minReplicas,
			MaxReplicas: maxReplicas,
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 2,
			DesiredReplicas: 2,
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{
					Type:   autoscalingv2.ScalingActive,
					Status: corev1.ConditionTrue,
					Reason: "ValidMetricFound",
				},
			},
		},
	}
	for _, opt := range opts {
		opt(hpa)
	}
	return hpa
}

// HPAOption is a functional option for customizing a test HPA.
type HPAOption func(*autoscalingv2.HorizontalPodAutoscaler)

// WithReplicas sets current and desired replicas.
func WithReplicas(current, desired int32) HPAOption {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		hpa.Status.CurrentReplicas = current
		hpa.Status.DesiredReplicas = desired
	}
}

// WithMinMax sets min and max replicas.
func WithMinMax(minReplicas, maxR int32) HPAOption {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		hpa.Spec.MinReplicas = &minReplicas
		hpa.Spec.MaxReplicas = maxR
	}
}

// WithResourceMetric adds a resource metric spec and status.
func WithResourceMetric(name string, targetUtil, currentUtil int32) HPAOption {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		hpa.Spec.Metrics = append(hpa.Spec.Metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceName(name),
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: &targetUtil,
				},
			},
		})
		hpa.Status.CurrentMetrics = append(hpa.Status.CurrentMetrics, autoscalingv2.MetricStatus{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricStatus{
				Name: corev1.ResourceName(name),
				Current: autoscalingv2.MetricValueStatus{
					AverageUtilization: &currentUtil,
				},
			},
		})
	}
}

// WithConditions replaces the HPA status conditions.
func WithConditions(conditions ...autoscalingv2.HorizontalPodAutoscalerCondition) HPAOption {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		hpa.Status.Conditions = conditions
	}
}

// WithScalingActiveFalse sets ScalingActive=False with the given reason.
func WithScalingActiveFalse(reason string) HPAOption {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		var filtered []autoscalingv2.HorizontalPodAutoscalerCondition
		for _, c := range hpa.Status.Conditions {
			if c.Type != autoscalingv2.ScalingActive {
				filtered = append(filtered, c)
			}
		}
		filtered = append(filtered, autoscalingv2.HorizontalPodAutoscalerCondition{
			Type:   autoscalingv2.ScalingActive,
			Status: corev1.ConditionFalse,
			Reason: reason,
		})
		hpa.Status.Conditions = filtered
	}
}

// WithScalingLimitedTrue sets ScalingLimited=True with the given reason.
func WithScalingLimitedTrue(reason string) HPAOption {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		hpa.Status.Conditions = append(hpa.Status.Conditions, autoscalingv2.HorizontalPodAutoscalerCondition{
			Type:   autoscalingv2.ScalingLimited,
			Status: corev1.ConditionTrue,
			Reason: reason,
		})
	}
}

// BuildEvent creates a corev1.Event for the given HPA.
func BuildEvent(namespace, hpaName, reason, message string) *corev1.Event {
	return &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      hpaName + "." + reason,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "HorizontalPodAutoscaler",
			Namespace: namespace,
			Name:      hpaName,
		},
		Reason:  reason,
		Message: message,
	}
}

// WithExternalMetric adds an external metric spec (without current status) to simulate
// a custom/external metrics adapter that is not configured.
func WithExternalMetric(name string, targetValue string) HPAOption {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		target := resource.MustParse(targetValue)
		hpa.Spec.Metrics = append(hpa.Spec.Metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{Name: name},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		})
	}
}

// WithExternalMetricWithStatus adds an external metric spec with current status.
func WithExternalMetricWithStatus(name string, targetValue, currentValue string) HPAOption {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		target := resource.MustParse(targetValue)
		current := resource.MustParse(currentValue)
		hpa.Spec.Metrics = append(hpa.Spec.Metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{Name: name},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		})
		hpa.Status.CurrentMetrics = append(hpa.Status.CurrentMetrics, autoscalingv2.MetricStatus{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricStatus{
				Metric:  autoscalingv2.MetricIdentifier{Name: name},
				Current: autoscalingv2.MetricValueStatus{Value: &current},
			},
		})
	}
}

// WithScaleDownStabilized adds AbleToScale condition with ScaleDownStabilized reason.
func WithScaleDownStabilized() HPAOption {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		hpa.Status.Conditions = append(hpa.Status.Conditions, autoscalingv2.HorizontalPodAutoscalerCondition{
			Type:   autoscalingv2.AbleToScale,
			Status: corev1.ConditionTrue,
			Reason: "ScaleDownStabilized",
		})
	}
}

// WithScaleDownStabilizationWindow sets the scaleDown stabilization window.
func WithScaleDownStabilizationWindow(seconds int32) HPAOption {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		if hpa.Spec.Behavior == nil {
			hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{}
		}
		hpa.Spec.Behavior.ScaleDown = &autoscalingv2.HPAScalingRules{
			StabilizationWindowSeconds: &seconds,
		}
	}
}

// WithKEDALabels adds KEDA-specific labels to the HPA.
func WithKEDALabels(scaledObjectName string) HPAOption {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		if hpa.Labels == nil {
			hpa.Labels = map[string]string{}
		}
		hpa.Labels["app.kubernetes.io/managed-by"] = "keda-operator"
		hpa.Labels["scaledobject.keda.sh/name"] = scaledObjectName
	}
}

// WithDesiredAtMax sets desiredReplicas equal to maxReplicas to simulate implicit max cap.
func WithDesiredAtMax() HPAOption {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		hpa.Status.CurrentReplicas = hpa.Spec.MaxReplicas
		hpa.Status.DesiredReplicas = hpa.Spec.MaxReplicas
	}
}
