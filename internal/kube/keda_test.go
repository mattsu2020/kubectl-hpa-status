package kube

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDetectKEDA_ByLabel(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "my-hpa",
			Labels: map[string]string{"scaledobject.keda.sh/name": "worker"},
		},
	}
	det := DetectKEDA(hpa)
	if !det.Managed {
		t.Fatal("expected KEDA detection from label")
	}
	if det.Name != "worker" {
		t.Fatalf("expected scaledObject name 'worker', got %q", det.Name)
	}
	if det.Source != KEDADetectionLabel {
		t.Fatalf("expected source 'label', got %q", det.Source)
	}
	if det.Confidence != KEDAConfidenceMedium {
		t.Fatalf("expected confidence 'medium', got %q", det.Confidence)
	}
}

func TestDetectKEDA_ByAnnotation(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-hpa",
			Annotations: map[string]string{"app.kubernetes.io/managed-by": "keda-operator"},
		},
	}
	det := DetectKEDA(hpa)
	if !det.Managed {
		t.Fatal("expected KEDA detection from annotation")
	}
	if det.Source != KEDADetectionAnnotation {
		t.Fatalf("expected source 'annotation', got %q", det.Source)
	}
}

func TestDetectKEDA_ByNamePrefix(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name: "keda-hpa-worker",
		},
	}
	det := DetectKEDA(hpa)
	if !det.Managed {
		t.Fatal("expected KEDA detection from name prefix")
	}
	if det.Name != "worker" {
		t.Fatalf("expected scaledObject name 'worker', got %q", det.Name)
	}
	if det.Source != KEDADetectionNamePrefix {
		t.Fatalf("expected source 'name-prefix', got %q", det.Source)
	}
	if det.Confidence != KEDAConfidenceLow {
		t.Fatalf("expected confidence 'low', got %q", det.Confidence)
	}
}

func TestDetectKEDA_NotKEDA(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-app-hpa",
		},
	}
	det := DetectKEDA(hpa)
	if det.Managed {
		t.Fatal("expected no KEDA detection for plain HPA")
	}
}

func TestDetectKEDA_Nil(t *testing.T) {
	det := DetectKEDA(nil)
	if det.Managed {
		t.Fatal("expected false for nil HPA")
	}
}

func TestExtractKEDAInfo(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name":      "worker-so",
				"namespace": "default",
			},
			"spec": map[string]any{
				"pollingInterval": int64(30),
				"cooldownPeriod":  int64(300),
				"minReplicaCount": int64(1),
				"maxReplicaCount": int64(50),
				"triggers": []any{
					map[string]any{
						"type": "azure-queue",
						"metadata": map[string]any{
							"queueName": "orders",
						},
					},
				},
			},
			"status": map[string]any{
				"conditions": []any{
					map[string]any{
						"type":    "Ready",
						"status":  "True",
						"reason":  "ScaledObjectReady",
						"message": "ScaledObject is ready",
					},
				},
			},
		},
	}

	info := ExtractKEDAInfo(u)

	if info.ScaledObjectName != "worker-so" {
		t.Fatalf("expected name 'worker-so', got %q", info.ScaledObjectName)
	}
	if info.PollingInterval == nil || *info.PollingInterval != 30 {
		t.Fatalf("expected pollingInterval 30, got %v", info.PollingInterval)
	}
	if info.CooldownPeriod == nil || *info.CooldownPeriod != 300 {
		t.Fatalf("expected cooldownPeriod 300, got %v", info.CooldownPeriod)
	}
	if info.MinReplicaCount == nil || *info.MinReplicaCount != 1 {
		t.Fatalf("expected minReplicaCount 1, got %v", info.MinReplicaCount)
	}
	if info.MaxReplicaCount == nil || *info.MaxReplicaCount != 50 {
		t.Fatalf("expected maxReplicaCount 50, got %v", info.MaxReplicaCount)
	}
	if len(info.Triggers) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(info.Triggers))
	}
	if info.Triggers[0].Type != "azure-queue" {
		t.Fatalf("expected trigger type 'azure-queue', got %q", info.Triggers[0].Type)
	}
	if len(info.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(info.Conditions))
	}
}

func TestExtractKEDAInfo_Nil(t *testing.T) {
	info := ExtractKEDAInfo(nil)
	if info.ScaledObjectName != "" {
		t.Fatalf("expected empty name for nil input, got %q", info.ScaledObjectName)
	}
}

func TestExtractKEDAInfo_TriggerStatus(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name":      "worker-so",
				"namespace": "default",
			},
			"spec": map[string]any{
				"triggers": []any{
					map[string]any{
						"type": "kafka",
						"name": "my-topic",
					},
					map[string]any{
						"type": "prometheus",
						"name": "http-rate",
					},
				},
			},
			"status": map[string]any{
				"health": map[string]any{
					"my-topic": map[string]any{
						"status":  "Active",
						"message": " scaler is active",
					},
					"http-rate": map[string]any{
						"status":  "Inactive",
						"message": "no metrics available",
					},
				},
			},
		},
	}

	info := ExtractKEDAInfo(u)

	if len(info.Triggers) != 2 {
		t.Fatalf("expected 2 triggers, got %d", len(info.Triggers))
	}

	if info.Triggers[0].Status != "Active" {
		t.Fatalf("expected trigger 0 status 'Active', got %q", info.Triggers[0].Status)
	}
	if info.Triggers[0].Message != " scaler is active" {
		t.Fatalf("expected trigger 0 message, got %q", info.Triggers[0].Message)
	}
	if info.Triggers[1].Status != "Inactive" {
		t.Fatalf("expected trigger 1 status 'Inactive', got %q", info.Triggers[1].Status)
	}
	if info.Triggers[1].Message != "no metrics available" {
		t.Fatalf("expected trigger 1 message, got %q", info.Triggers[1].Message)
	}
}

func TestExtractKEDAInfo_Fallback(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name":      "worker-so",
				"namespace": "default",
			},
			"spec": map[string]any{
				"triggers": []any{
					map[string]any{"type": "cpu"},
				},
				"fallback": map[string]any{
					"failureThreshold": int64(3),
					"replicas":         int64(5),
				},
			},
		},
	}

	info := ExtractKEDAInfo(u)

	if info.Fallback == nil {
		t.Fatal("expected fallback to be non-nil")
	}
	if info.Fallback.FailureThreshold != 3 {
		t.Fatalf("expected failureThreshold 3, got %d", info.Fallback.FailureThreshold)
	}
	if info.Fallback.Replicas != 5 {
		t.Fatalf("expected fallback replicas 5, got %d", info.Fallback.Replicas)
	}
}

func TestExtractKEDAInfo_ScalingPolicies(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name":      "worker-so",
				"namespace": "default",
			},
			"spec": map[string]any{
				"triggers": []any{
					map[string]any{"type": "cpu"},
				},
				"advanced": map[string]any{
					"horizontalPodAutoscalerConfig": map[string]any{
						"behavior": map[string]any{
							"scaleUp": map[string]any{
								"policies": []any{
									map[string]any{
										"value":         int64(4),
										"periodSeconds": int64(60),
									},
								},
							},
							"scaleDown": map[string]any{
								"policies": []any{
									map[string]any{
										"value":         int64(1),
										"periodSeconds": int64(120),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	info := ExtractKEDAInfo(u)

	if len(info.ScalingPolicies) != 2 {
		t.Fatalf("expected 2 scaling policies, got %d", len(info.ScalingPolicies))
	}

	var foundUp, foundDown bool
	for _, p := range info.ScalingPolicies {
		switch p.Type {
		case "scaleUp":
			foundUp = true
			if p.Value != 4 {
				t.Fatalf("expected scaleUp value 4, got %d", p.Value)
			}
			if p.PeriodSeconds != 60 {
				t.Fatalf("expected scaleUp periodSeconds 60, got %d", p.PeriodSeconds)
			}
		case "scaleDown":
			foundDown = true
			if p.Value != 1 {
				t.Fatalf("expected scaleDown value 1, got %d", p.Value)
			}
			if p.PeriodSeconds != 120 {
				t.Fatalf("expected scaleDown periodSeconds 120, got %d", p.PeriodSeconds)
			}
		}
	}
	if !foundUp {
		t.Fatal("expected scaleUp policy to be found")
	}
	if !foundDown {
		t.Fatal("expected scaleDown policy to be found")
	}
}

func TestExtractKEDAInfo_AuthenticationRef(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name":      "worker-so",
				"namespace": "default",
			},
			"spec": map[string]any{
				"triggers": []any{
					map[string]any{
						"type": "kafka",
						"name": "my-topic",
						"authenticationRef": map[string]any{
							"name": "kafka-trigger-auth",
						},
					},
				},
			},
		},
	}

	info := ExtractKEDAInfo(u)

	if len(info.Triggers) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(info.Triggers))
	}
	if info.Triggers[0].AuthenticationRef != "kafka-trigger-auth" {
		t.Fatalf("expected authenticationRef 'kafka-trigger-auth', got %q", info.Triggers[0].AuthenticationRef)
	}
}

func TestMapHealthStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"active", "Active"},
		{"Active", "Active"},
		{"happy", "Active"},
		{"true", "Active"},
		{"inactive", "Inactive"},
		{"false", "Inactive"},
		{"unknown", "Unknown"},
		{"", "Unknown"},
		{"SomethingElse", "SomethingElse"},
	}
	for _, tt := range tests {
		result := mapHealthStatus(tt.input)
		if result != tt.expected {
			t.Errorf("mapHealthStatus(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
