package kube

import "k8s.io/apimachinery/pkg/runtime/schema"

const (
	kedaAPIGroup    = "keda.sh"
	kedaAPIVersion  = "v1alpha1"
	kedaResourcePlr = "scaledobjects"
)

var scaledObjectGVR = schema.GroupVersionResource{
	Group:    kedaAPIGroup,
	Version:  kedaAPIVersion,
	Resource: kedaResourcePlr,
}

// ScaledObjectGVR returns the GroupVersionResource for KEDA ScaledObjects.
func ScaledObjectGVR() schema.GroupVersionResource {
	return scaledObjectGVR
}

// KEDAInfo holds extracted information about a KEDA ScaledObject.
type KEDAInfo struct {
	ScaledObjectName string              `json:"scaledObjectName" yaml:"scaledObjectName"`
	Triggers         []KEDATrigger       `json:"triggers,omitempty" yaml:"triggers,omitempty"`
	PollingInterval  *int32              `json:"pollingInterval,omitempty" yaml:"pollingInterval,omitempty"`
	CooldownPeriod   *int32              `json:"cooldownPeriod,omitempty" yaml:"cooldownPeriod,omitempty"`
	MinReplicaCount  *int32              `json:"minReplicaCount,omitempty" yaml:"minReplicaCount,omitempty"`
	MaxReplicaCount  *int32              `json:"maxReplicaCount,omitempty" yaml:"maxReplicaCount,omitempty"`
	IdleReplicaCount *int32              `json:"idleReplicaCount,omitempty" yaml:"idleReplicaCount,omitempty"`
	Conditions       []KEDACondition     `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	Advanced         map[string]string   `json:"advanced,omitempty" yaml:"advanced,omitempty"`
	Fallback         *KEDAFallback       `json:"fallback,omitempty" yaml:"fallback,omitempty"`
	ScalingPolicies  []KEDAScalingPolicy `json:"scalingPolicies,omitempty" yaml:"scalingPolicies,omitempty"`
}

// KEDATrigger represents a single KEDA scaler trigger.
type KEDATrigger struct {
	Type              string            `json:"type" yaml:"type"`
	Name              string            `json:"name,omitempty" yaml:"name,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Status            string            `json:"status,omitempty" yaml:"status,omitempty"` // "Active", "Inactive", "Unknown"
	Message           string            `json:"message,omitempty" yaml:"message,omitempty"`
	AuthenticationRef string            `json:"authenticationRef,omitempty" yaml:"authenticationRef,omitempty"`
	MetricName        string            `json:"metricName,omitempty" yaml:"metricName,omitempty"`
	Threshold         string            `json:"threshold,omitempty" yaml:"threshold,omitempty"`
	CurrentValue      string            `json:"currentValue,omitempty" yaml:"currentValue,omitempty"`
}

// KEDACondition represents a condition from the ScaledObject status.
type KEDACondition struct {
	Type    string `json:"type" yaml:"type"`
	Status  string `json:"status" yaml:"status"`
	Reason  string `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

// KEDAFallback holds fallback configuration from a ScaledObject.
type KEDAFallback struct {
	FailureThreshold int32 `json:"failureThreshold" yaml:"failureThreshold"`
	Replicas         int32 `json:"replicas" yaml:"replicas"`
}

// KEDAScalingPolicy represents a scaling policy from a ScaledObject.
type KEDAScalingPolicy struct {
	Type          string `json:"type" yaml:"type"` // "scaleUp" or "scaleDown"
	Value         int32  `json:"value" yaml:"value"`
	PeriodSeconds int32  `json:"periodSeconds" yaml:"periodSeconds"`
}
