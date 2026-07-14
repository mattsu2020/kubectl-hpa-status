package hpa

import (
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAnalyzeMetricFreshness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		hpa           *autoscalingv2.HorizontalPodAutoscaler
		events        []Event
		wantLen       int
		wantStatus    []string // expected status for each entry, in order
		wantSource    []string // expected source for each entry, in order
		wantWindow    []string // expected window for each entry, in order
		wantRisk      []bool   // whether risk should be non-empty
		wantEvidence  []bool   // whether evidence should be non-empty
		wantNextSteps []bool   // whether next steps should be non-empty
	}{
		{
			name:    "nil HPA returns nil",
			hpa:     nil,
			wantLen: 0,
		},
		{
			name:    "no spec metrics returns nil",
			hpa:     testutil.BuildHPA("default", "web"),
			wantLen: 0,
		},
		{
			name:          "resource metric OK",
			hpa:           testutil.BuildHPA("default", "web", testutil.WithResourceMetric("cpu", 80, 75)),
			wantLen:       1,
			wantStatus:    []string{"OK"},
			wantSource:    []string{"metrics.k8s.io"},
			wantWindow:    []string{"30s"},
			wantRisk:      []bool{false},
			wantEvidence:  []bool{false},
			wantNextSteps: []bool{false},
		},
		{
			name:          "resource metric missing",
			hpa:           buildHPAWithResourceSpecOnly("default", "web", "cpu"),
			wantLen:       1,
			wantStatus:    []string{"Missing"},
			wantSource:    []string{"metrics.k8s.io"},
			wantWindow:    []string{"30s"},
			wantRisk:      []bool{true},
			wantEvidence:  []bool{false},
			wantNextSteps: []bool{true},
		},
		{
			name: "resource metric missing with ScalingActive False",
			hpa: buildHPAWithResourceSpecOnly("default", "web", "cpu",
				testutil.WithScalingActiveFalse("FailedGetResourceMetric")),
			wantLen:       1,
			wantStatus:    []string{"Missing"},
			wantSource:    []string{"metrics.k8s.io"},
			wantRisk:      []bool{true},
			wantEvidence:  []bool{true},
			wantNextSteps: []bool{true},
		},
		{
			name: "external metric OK",
			hpa: testutil.BuildHPA("default", "web",
				testutil.WithExternalMetricWithStatus("queue_depth", "10", "12")),
			wantLen:       1,
			wantStatus:    []string{"OK"},
			wantSource:    []string{"external.metrics.k8s.io"},
			wantWindow:    []string{""},
			wantRisk:      []bool{false},
			wantEvidence:  []bool{false},
			wantNextSteps: []bool{false},
		},
		{
			name: "external metric missing",
			hpa: testutil.BuildHPA("default", "web",
				testutil.WithExternalMetric("queue_depth", "10")),
			wantLen:       1,
			wantStatus:    []string{"Missing"},
			wantSource:    []string{"external.metrics.k8s.io"},
			wantRisk:      []bool{true},
			wantEvidence:  []bool{false},
			wantNextSteps: []bool{true},
		},
		{
			name: "mixed metrics CPU OK and external missing",
			hpa: testutil.BuildHPA("default", "web",
				testutil.WithResourceMetric("cpu", 80, 75),
				testutil.WithExternalMetric("queue_depth", "10")),
			wantLen:       2,
			wantStatus:    []string{"OK", "Missing"},
			wantSource:    []string{"metrics.k8s.io", "external.metrics.k8s.io"},
			wantRisk:      []bool{false, true},
			wantEvidence:  []bool{false, false},
			wantNextSteps: []bool{false, true},
		},
		{
			name: "event evidence for missing external metric",
			hpa: testutil.BuildHPA("default", "web",
				testutil.WithExternalMetric("queue_depth", "10")),
			events: []Event{
				{Reason: "FailedGetExternalMetric", Message: "unable to get metric queue_depth: external.metrics.k8s.io unavailable"},
			},
			wantLen:       1,
			wantStatus:    []string{"Missing"},
			wantEvidence:  []bool{true},
			wantNextSteps: []bool{true},
		},
		{
			name:          "pods metric missing",
			hpa:           buildHPAWithPodsMetricSpecOnly("default", "web", "http_requests"),
			wantLen:       1,
			wantStatus:    []string{"Missing"},
			wantSource:    []string{"custom.metrics.k8s.io"},
			wantRisk:      []bool{true},
			wantNextSteps: []bool{true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := AnalyzeMetricFreshness(tt.hpa, tt.events)
			if len(got) != tt.wantLen {
				t.Fatalf("got %d entries, want %d", len(got), tt.wantLen)
			}
			for i, entry := range got {
				if i < len(tt.wantStatus) && entry.Status != tt.wantStatus[i] {
					t.Errorf("entry[%d].Status = %q, want %q", i, entry.Status, tt.wantStatus[i])
				}
				if i < len(tt.wantSource) && entry.Source != tt.wantSource[i] {
					t.Errorf("entry[%d].Source = %q, want %q", i, entry.Source, tt.wantSource[i])
				}
				if i < len(tt.wantWindow) && entry.Window != tt.wantWindow[i] {
					t.Errorf("entry[%d].Window = %q, want %q", i, entry.Window, tt.wantWindow[i])
				}
				if i < len(tt.wantRisk) {
					hasRisk := entry.Risk != ""
					if hasRisk != tt.wantRisk[i] {
						t.Errorf("entry[%d].Risk non-empty = %v, want %v (value: %q)", i, hasRisk, tt.wantRisk[i], entry.Risk)
					}
				}
				if i < len(tt.wantEvidence) {
					hasEvidence := len(entry.Evidence) > 0
					if hasEvidence != tt.wantEvidence[i] {
						t.Errorf("entry[%d].Evidence non-empty = %v, want %v (values: %v)", i, hasEvidence, tt.wantEvidence[i], entry.Evidence)
					}
				}
				if i < len(tt.wantNextSteps) {
					hasNextSteps := len(entry.NextSteps) > 0
					if hasNextSteps != tt.wantNextSteps[i] {
						t.Errorf("entry[%d].NextSteps non-empty = %v, want %v (values: %v)", i, hasNextSteps, tt.wantNextSteps[i], entry.NextSteps)
					}
				}
			}
		})
	}
}

func TestMetricSourceAPI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		metricType autoscalingv2.MetricSourceType
		want       string
	}{
		{autoscalingv2.ResourceMetricSourceType, "metrics.k8s.io"},
		{autoscalingv2.ContainerResourceMetricSourceType, "metrics.k8s.io"},
		{autoscalingv2.PodsMetricSourceType, "custom.metrics.k8s.io"},
		{autoscalingv2.ObjectMetricSourceType, "custom.metrics.k8s.io"},
		{autoscalingv2.ExternalMetricSourceType, "external.metrics.k8s.io"},
	}
	for _, tt := range tests {
		t.Run(string(tt.metricType), func(t *testing.T) {
			t.Parallel()
			got := metricSourceAPI(tt.metricType)
			if got != tt.want {
				t.Errorf("metricSourceAPI(%s) = %q, want %q", tt.metricType, got, tt.want)
			}
		})
	}
}

func TestMetricWindow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		metricType autoscalingv2.MetricSourceType
		want       string
	}{
		{autoscalingv2.ResourceMetricSourceType, "30s"},
		{autoscalingv2.ContainerResourceMetricSourceType, "30s"},
		{autoscalingv2.ExternalMetricSourceType, ""},
		{autoscalingv2.PodsMetricSourceType, ""},
		{autoscalingv2.ObjectMetricSourceType, ""},
	}
	for _, tt := range tests {
		t.Run(string(tt.metricType), func(t *testing.T) {
			t.Parallel()
			got := metricWindow(tt.metricType)
			if got != tt.want {
				t.Errorf("metricWindow(%s) = %q, want %q", tt.metricType, got, tt.want)
			}
		})
	}
}

func TestScanEventsForEvidence(t *testing.T) {
	t.Parallel()
	events := []Event{
		{Reason: "FailedGetResourceMetric", Message: "unable to get metric cpu: metrics-server error"},
		{Reason: "FailedGetExternalMetric", Message: "unable to get metric queue_depth: adapter unavailable"},
		{Reason: "SuccessfulRescale", Message: "HPA scaled up"},
	}

	tests := []struct {
		name       string
		metricName string
		metricType string
		wantCount  int
	}{
		{
			name:       "matching resource event",
			metricName: "cpu",
			metricType: "Resource",
			wantCount:  1,
		},
		{
			name:       "matching external event",
			metricName: "queue_depth",
			metricType: "External",
			wantCount:  1,
		},
		{
			name:       "no matching event for type",
			metricName: "cpu",
			metricType: "External",
			wantCount:  0,
		},
		{
			name:       "empty events",
			metricName: "cpu",
			metricType: "Resource",
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ev := events
			if tt.wantCount == 0 && tt.name == "empty events" {
				ev = nil
			}
			got := scanEventsForEvidence(ev, tt.metricName, tt.metricType)
			if len(got) != tt.wantCount {
				t.Errorf("got %d evidence entries, want %d (values: %v)", len(got), tt.wantCount, got)
			}
		})
	}
}

func TestBuildFreshnessNextSteps(t *testing.T) {
	t.Parallel()

	t.Run("missing resource metric has next steps", func(t *testing.T) {
		t.Parallel()
		spec := autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: "cpu",
			},
		}
		steps := buildFreshnessNextSteps(FreshnessMissing, spec)
		if len(steps) == 0 {
			t.Error("expected non-empty next steps for missing resource metric")
		}
		for _, step := range steps {
			if !strings.Contains(step, "metrics") {
				t.Errorf("expected next step to mention metrics, got: %q", step)
			}
		}
	})

	t.Run("missing external metric has next steps", func(t *testing.T) {
		t.Parallel()
		spec := autoscalingv2.MetricSpec{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth"},
			},
		}
		steps := buildFreshnessNextSteps(FreshnessMissing, spec)
		if len(steps) == 0 {
			t.Error("expected non-empty next steps for missing external metric")
		}
	})

	t.Run("OK status returns nil", func(t *testing.T) {
		t.Parallel()
		spec := autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
		}
		steps := buildFreshnessNextSteps(FreshnessOK, spec)
		if steps != nil {
			t.Errorf("expected nil next steps for OK status, got: %v", steps)
		}
	})
}

// buildHPAWithResourceSpecOnly creates an HPA with a resource metric spec but
// no matching current metric status, simulating a missing metric.
func buildHPAWithResourceSpecOnly(namespace, name, resourceName string, extraOpts ...testutil.HPAOption) *autoscalingv2.HorizontalPodAutoscaler {
	opts := []testutil.HPAOption{
		func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
			hpa.Spec.Metrics = append(hpa.Spec.Metrics, autoscalingv2.MetricSpec{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceName(resourceName),
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: ptrInt32(80),
					},
				},
			})
		},
	}
	opts = append(opts, extraOpts...)
	return testutil.BuildHPA(namespace, name, opts...)
}

// buildHPAWithPodsMetricSpecOnly creates an HPA with a Pods metric spec but
// no matching current metric status.
func buildHPAWithPodsMetricSpecOnly(namespace, name, metricName string) *autoscalingv2.HorizontalPodAutoscaler {
	return testutil.BuildHPA(namespace, name, func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		hpa.Spec.Metrics = append(hpa.Spec.Metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.PodsMetricSourceType,
			Pods: &autoscalingv2.PodsMetricSource{
				Metric: autoscalingv2.MetricIdentifier{Name: metricName},
				Target: autoscalingv2.MetricTarget{
					Type:         autoscalingv2.AverageValueMetricType,
					AverageValue: ptrQuantity("500m"),
				},
			},
		})
	})
}

var _ = metav1.Time{}       // ensure import is used
var _ = resource.Quantity{} // ensure import is used

func TestBuildStaleNextSteps(t *testing.T) {
	t.Parallel()

	t.Run("stale resource metric", func(t *testing.T) {
		t.Parallel()
		spec := autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
		}
		steps := buildFreshnessNextSteps(FreshnessStale, spec)
		if len(steps) == 0 {
			t.Error("expected non-empty next steps for stale resource metric")
		}
	})

	t.Run("stale external metric", func(t *testing.T) {
		t.Parallel()
		spec := autoscalingv2.MetricSpec{
			Type: autoscalingv2.ExternalMetricSourceType,
		}
		steps := buildFreshnessNextSteps(FreshnessStale, spec)
		if len(steps) == 0 {
			t.Error("expected non-empty next steps for stale external metric")
		}
	})
}

func TestIsMetricValueZero(t *testing.T) {
	t.Parallel()

	t.Run("resource metric with zero utilization", func(t *testing.T) {
		t.Parallel()
		spec := autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: "cpu",
			},
		}
		zero := int32(0)
		currentMetrics := []autoscalingv2.MetricStatus{
			{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricStatus{
					Name: "cpu",
					Current: autoscalingv2.MetricValueStatus{
						AverageUtilization: &zero,
					},
				},
			},
		}
		if !isMetricValueZero(spec, currentMetrics) {
			t.Error("expected zero value to be detected as stale")
		}
	})

	t.Run("resource metric with non-zero utilization", func(t *testing.T) {
		t.Parallel()
		spec := autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: "cpu",
			},
		}
		util := int32(75)
		currentMetrics := []autoscalingv2.MetricStatus{
			{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricStatus{
					Name: "cpu",
					Current: autoscalingv2.MetricValueStatus{
						AverageUtilization: &util,
					},
				},
			},
		}
		if isMetricValueZero(spec, currentMetrics) {
			t.Error("expected non-zero value to not be detected as stale")
		}
	})

	t.Run("external metric with nil value fields", func(t *testing.T) {
		t.Parallel()
		spec := autoscalingv2.MetricSpec{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth"},
			},
		}
		currentMetrics := []autoscalingv2.MetricStatus{
			{
				Type: autoscalingv2.ExternalMetricSourceType,
				External: &autoscalingv2.ExternalMetricStatus{
					Metric:  autoscalingv2.MetricIdentifier{Name: "queue_depth"},
					Current: autoscalingv2.MetricValueStatus{},
				},
			},
		}
		if !isMetricValueZero(spec, currentMetrics) {
			t.Error("expected nil value fields to be detected as stale")
		}
	})
}

func TestTruncateMessage(t *testing.T) {
	t.Parallel()
	if got := truncateMessage("short", 10); got != "short" {
		t.Errorf("truncateMessage(%q, 10) = %q, want %q", "short", got, "short")
	}
	long := strings.Repeat("a", 200)
	got := truncateMessage(long, 50)
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected truncated message to end with '...', got: %q", got)
	}
	if len(got) != 50 { // max length includes the ellipsis
		t.Errorf("expected length 50, got %d", len(got))
	}
}
