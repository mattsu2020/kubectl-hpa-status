package hpa

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAnalyzeWarmup(t *testing.T) {
	tests := []struct {
		name            string
		input           WarmupInput
		wantNil         bool
		wantSummary     string
		wantBottlenecks int
		wantActions     int
		wantAvgTime     int64
		wantP95Time     int64
		wantCapacityPct int
	}{
		{
			name: "all pods ready returns nil",
			input: WarmupInput{
				DesiredReplicas: 5,
				MinReplicas:     1,
				ReadyPods:       5,
				TotalPods:       5,
			},
			wantNil: true,
		},
		{
			name: "at minReplicas returns nil",
			input: WarmupInput{
				DesiredReplicas: 2,
				MinReplicas:     2,
				ReadyPods:       1,
				TotalPods:       2,
			},
			wantNil: true,
		},
		{
			name: "zero total pods returns nil",
			input: WarmupInput{
				DesiredReplicas: 5,
				MinReplicas:     1,
				ReadyPods:       0,
				TotalPods:       0,
			},
			wantNil: true,
		},
		{
			name: "readiness probe bottleneck",
			input: WarmupInput{
				DesiredReplicas:         10,
				CurrentReplicas:         10,
				MinReplicas:             1,
				ReadyPods:               4,
				TotalPods:               10,
				ScalingActive:           true,
				TargetReadyReplicas:     4,
				TargetAvailableReplicas: 3,
				ReadinessProbePresent:   true,
				PodDetails: []WarmupPodDetail{
					{Name: "pod-1", AgeSeconds: 120, Ready: true, ContainerState: "running", TimeToReadySeconds: 142},
					{Name: "pod-2", AgeSeconds: 120, Ready: true, ContainerState: "running", TimeToReadySeconds: 130},
					{Name: "pod-3", AgeSeconds: 120, Ready: true, ContainerState: "running", TimeToReadySeconds: 150},
					{Name: "pod-4", AgeSeconds: 120, Ready: true, ContainerState: "running", TimeToReadySeconds: 145},
					{Name: "pod-5", AgeSeconds: 60, Ready: false, ContainerState: "running"},
					{Name: "pod-6", AgeSeconds: 60, Ready: false, ContainerState: "running"},
					{Name: "pod-7", AgeSeconds: 60, Ready: false, ContainerState: "running"},
					{Name: "pod-8", AgeSeconds: 60, Ready: false, ContainerState: "running"},
					{Name: "pod-9", AgeSeconds: 60, Ready: false, ContainerState: "running"},
					{Name: "pod-10", AgeSeconds: 60, Ready: false, ContainerState: "running"},
				},
				Now: metav1.Now(),
			},
			wantSummary:     "capacity_warming_up",
			wantBottlenecks: 1,
			wantActions:     2,
			wantAvgTime:     141,
			wantP95Time:     150,
			wantCapacityPct: 40,
		},
		{
			name: "image pull bottleneck",
			input: WarmupInput{
				DesiredReplicas: 5,
				CurrentReplicas: 5,
				MinReplicas:     1,
				ReadyPods:       3,
				TotalPods:       5,
				ScalingActive:   true,
				PodDetails: []WarmupPodDetail{
					{Name: "pod-1", Ready: true, ContainerState: "running"},
					{Name: "pod-2", Ready: true, ContainerState: "running"},
					{Name: "pod-3", Ready: true, ContainerState: "running"},
					{Name: "pod-4", Ready: false, ContainerState: "waiting", WaitingReason: "ImagePullBackOff"},
					{Name: "pod-5", Ready: false, ContainerState: "waiting", WaitingReason: "ErrImagePull"},
				},
				Now: metav1.Now(),
			},
			wantSummary:     "capacity_warming_up",
			wantBottlenecks: 1,
			wantActions:     1,
			wantCapacityPct: 60,
		},
		{
			name: "scheduling bottleneck",
			input: WarmupInput{
				DesiredReplicas: 5,
				CurrentReplicas: 5,
				MinReplicas:     1,
				ReadyPods:       3,
				TotalPods:       5,
				ScalingActive:   true,
				PodDetails: []WarmupPodDetail{
					{Name: "pod-1", Ready: true, ContainerState: "running"},
					{Name: "pod-2", Ready: true, ContainerState: "running"},
					{Name: "pod-3", Ready: true, ContainerState: "running"},
					{Name: "pod-4", Ready: false, ContainerState: ""},
					{Name: "pod-5", Ready: false, ContainerState: ""},
				},
				Now: metav1.Now(),
			},
			wantSummary:     "capacity_warming_up",
			wantBottlenecks: 1,
			wantActions:     1,
			wantCapacityPct: 60,
		},
		{
			name: "container crash bottleneck",
			input: WarmupInput{
				DesiredReplicas: 5,
				CurrentReplicas: 5,
				MinReplicas:     1,
				ReadyPods:       3,
				TotalPods:       5,
				ScalingActive:   true,
				PodDetails: []WarmupPodDetail{
					{Name: "pod-1", Ready: true, ContainerState: "running"},
					{Name: "pod-2", Ready: true, ContainerState: "running"},
					{Name: "pod-3", Ready: true, ContainerState: "running"},
					{Name: "pod-4", Ready: false, ContainerState: "waiting", WaitingReason: "CrashLoopBackOff"},
					{Name: "pod-5", Ready: false, ContainerState: "terminated", RestartCount: 5},
				},
				Now: metav1.Now(),
			},
			wantSummary:     "capacity_warming_up",
			wantBottlenecks: 1,
			wantActions:     1,
			wantCapacityPct: 60,
		},
		{
			name: "multiple bottlenecks",
			input: WarmupInput{
				DesiredReplicas:         10,
				CurrentReplicas:         10,
				MinReplicas:             1,
				ReadyPods:               4,
				TotalPods:               10,
				ScalingActive:           true,
				ReadinessProbePresent:   true,
				TargetAvailableReplicas: 3,
				PodDetails: []WarmupPodDetail{
					{Name: "pod-1", Ready: true, ContainerState: "running"},
					{Name: "pod-2", Ready: true, ContainerState: "running"},
					{Name: "pod-3", Ready: true, ContainerState: "running"},
					{Name: "pod-4", Ready: true, ContainerState: "running"},
					{Name: "pod-5", Ready: false, ContainerState: "running"},
					{Name: "pod-6", Ready: false, ContainerState: "running"},
					{Name: "pod-7", Ready: false, ContainerState: "waiting", WaitingReason: "ImagePullBackOff"},
					{Name: "pod-8", Ready: false, ContainerState: ""},
					{Name: "pod-9", Ready: false, ContainerState: "waiting", WaitingReason: "CrashLoopBackOff"},
					{Name: "pod-10", Ready: false, ContainerState: "running"},
				},
				Now: metav1.Now(),
			},
			wantSummary:     "capacity_warming_up",
			wantBottlenecks: 4, // readiness_probe + image_pull + scheduling + container_crash
			wantActions:     5, // + pre-warming since ReadinessProbePresent
			wantCapacityPct: 40,
		},
		{
			name: "no pods ready yet time to ready is zero",
			input: WarmupInput{
				DesiredReplicas:       5,
				CurrentReplicas:       5,
				MinReplicas:           1,
				ReadyPods:             0,
				TotalPods:             5,
				ScalingActive:         true,
				ReadinessProbePresent: true,
				PodDetails: []WarmupPodDetail{
					{Name: "pod-1", AgeSeconds: 30, Ready: false, ContainerState: "running"},
					{Name: "pod-2", AgeSeconds: 30, Ready: false, ContainerState: "running"},
					{Name: "pod-3", AgeSeconds: 30, Ready: false, ContainerState: "running"},
					{Name: "pod-4", AgeSeconds: 30, Ready: false, ContainerState: "running"},
					{Name: "pod-5", AgeSeconds: 30, Ready: false, ContainerState: "running"},
				},
				Now: metav1.Now(),
			},
			wantSummary:     "capacity_warming_up",
			wantBottlenecks: 1,
			wantActions:     2, // readiness probe + pre-warming
			wantAvgTime:     0,
			wantP95Time:     0,
			wantCapacityPct: 0,
		},
		{
			name: "metrics inactive adds critical bottleneck",
			input: WarmupInput{
				DesiredReplicas: 5,
				CurrentReplicas: 5,
				MinReplicas:     1,
				ReadyPods:       3,
				TotalPods:       5,
				ScalingActive:   false,
				PodDetails: []WarmupPodDetail{
					{Name: "pod-1", Ready: true, ContainerState: "running"},
					{Name: "pod-2", Ready: true, ContainerState: "running"},
					{Name: "pod-3", Ready: true, ContainerState: "running"},
					{Name: "pod-4", Ready: false, ContainerState: "running"},
					{Name: "pod-5", Ready: false, ContainerState: "running"},
				},
				Now: metav1.Now(),
			},
			wantSummary:     "capacity_warming_up",
			wantBottlenecks: 2, // metrics_inactive + unknown (running not ready, no probe)
			wantActions:     1,
			wantCapacityPct: 60,
		},
		{
			name: "startup probe bottleneck",
			input: WarmupInput{
				DesiredReplicas:             5,
				CurrentReplicas:             5,
				MinReplicas:                 1,
				ReadyPods:                   3,
				TotalPods:                   5,
				ScalingActive:               true,
				ReadinessProbePresent:       true,
				StartupProbePresent:         true,
				StartupProbeMaxDelaySeconds: 180,
				PodDetails: []WarmupPodDetail{
					{Name: "pod-1", AgeSeconds: 300, Ready: true, ContainerState: "running"},
					{Name: "pod-2", AgeSeconds: 300, Ready: true, ContainerState: "running"},
					{Name: "pod-3", AgeSeconds: 300, Ready: true, ContainerState: "running"},
					{Name: "pod-4", AgeSeconds: 60, Ready: false, ContainerState: "running"},
					{Name: "pod-5", AgeSeconds: 60, Ready: false, ContainerState: "running"},
				},
				Now: metav1.Now(),
			},
			wantSummary:     "capacity_warming_up",
			wantBottlenecks: 2, // readiness_probe + startup_probe
			wantActions:     3,
			wantCapacityPct: 60,
		},
		{
			name: "scaling limited adds action",
			input: WarmupInput{
				DesiredReplicas: 5,
				CurrentReplicas: 5,
				MinReplicas:     1,
				MaxReplicas:     5,
				ReadyPods:       3,
				TotalPods:       5,
				ScalingActive:   true,
				ScalingLimited:  true,
				PodDetails: []WarmupPodDetail{
					{Name: "pod-1", Ready: true, ContainerState: "running"},
					{Name: "pod-2", Ready: true, ContainerState: "running"},
					{Name: "pod-3", Ready: true, ContainerState: "running"},
					{Name: "pod-4", Ready: false, ContainerState: "running"},
					{Name: "pod-5", Ready: false, ContainerState: "running"},
				},
				Now: metav1.Now(),
			},
			wantSummary:     "capacity_warming_up",
			wantBottlenecks: 1, // unknown (running not ready, no probe)
			wantActions:     1, // raise maxReplicas (unknown bottleneck has no specific action)
			wantCapacityPct: 60,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AnalyzeWarmup(tt.input)

			if tt.wantNil {
				if got != nil {
					t.Errorf("AnalyzeWarmup() = %+v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Fatal("AnalyzeWarmup() = nil, want non-nil")
			}

			if got.Summary != tt.wantSummary {
				t.Errorf("Summary = %q, want %q", got.Summary, tt.wantSummary)
			}
			if len(got.Bottlenecks) != tt.wantBottlenecks {
				t.Errorf("len(Bottlenecks) = %d, want %d (got: %+v)", len(got.Bottlenecks), tt.wantBottlenecks, got.Bottlenecks)
			}
			if len(got.RecommendedActions) != tt.wantActions {
				t.Errorf("len(RecommendedActions) = %d, want %d (got: %v)", len(got.RecommendedActions), tt.wantActions, got.RecommendedActions)
			}
			if got.AvgTimeToReadySeconds != tt.wantAvgTime {
				t.Errorf("AvgTimeToReadySeconds = %d, want %d", got.AvgTimeToReadySeconds, tt.wantAvgTime)
			}
			if got.P95TimeToReadySeconds != tt.wantP95Time {
				t.Errorf("P95TimeToReadySeconds = %d, want %d", got.P95TimeToReadySeconds, tt.wantP95Time)
			}

			expectedPct := tt.wantCapacityPct
			actualPct := int(got.EffectiveCapacityRatio * 100)
			if actualPct != expectedPct {
				t.Errorf("EffectiveCapacityRatio = %.2f (~%d%%), want ~%d%%", got.EffectiveCapacityRatio, actualPct, expectedPct)
			}
		})
	}
}

func TestComputeTimeToReady(t *testing.T) {
	tests := []struct {
		name    string
		details []WarmupPodDetail
		wantAvg int64
		wantP95 int64
		wantMax int64
	}{
		{
			name:    "empty details",
			details: nil,
			wantAvg: 0,
			wantP95: 0,
			wantMax: 0,
		},
		{
			name: "no ready pods",
			details: []WarmupPodDetail{
				{Name: "pod-1", Ready: false},
				{Name: "pod-2", Ready: false},
			},
			wantAvg: 0,
			wantP95: 0,
			wantMax: 0,
		},
		{
			name: "single ready pod",
			details: []WarmupPodDetail{
				{Name: "pod-1", Ready: true, TimeToReadySeconds: 100},
			},
			wantAvg: 100,
			wantP95: 100,
			wantMax: 100,
		},
		{
			name: "multiple ready pods",
			details: []WarmupPodDetail{
				{Name: "pod-1", Ready: true, TimeToReadySeconds: 100},
				{Name: "pod-2", Ready: true, TimeToReadySeconds: 200},
				{Name: "pod-3", Ready: true, TimeToReadySeconds: 300},
				{Name: "pod-4", Ready: false},
			},
			wantAvg: 200,
			wantP95: 300,
			wantMax: 300,
		},
		{
			name: "many pods for p95 calculation",
			details: []WarmupPodDetail{
				{Name: "pod-1", Ready: true, TimeToReadySeconds: 10},
				{Name: "pod-2", Ready: true, TimeToReadySeconds: 20},
				{Name: "pod-3", Ready: true, TimeToReadySeconds: 30},
				{Name: "pod-4", Ready: true, TimeToReadySeconds: 40},
				{Name: "pod-5", Ready: true, TimeToReadySeconds: 50},
				{Name: "pod-6", Ready: true, TimeToReadySeconds: 60},
				{Name: "pod-7", Ready: true, TimeToReadySeconds: 70},
				{Name: "pod-8", Ready: true, TimeToReadySeconds: 80},
				{Name: "pod-9", Ready: true, TimeToReadySeconds: 90},
				{Name: "pod-10", Ready: true, TimeToReadySeconds: 100},
			},
			wantAvg: 55,
			wantP95: 100,
			wantMax: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			avg, p95, max := computeTimeToReady(tt.details)
			if avg != tt.wantAvg {
				t.Errorf("avg = %d, want %d", avg, tt.wantAvg)
			}
			if p95 != tt.wantP95 {
				t.Errorf("p95 = %d, want %d", p95, tt.wantP95)
			}
			if max != tt.wantMax {
				t.Errorf("max = %d, want %d", max, tt.wantMax)
			}
		})
	}
}

func TestEffectiveCapacityRatio(t *testing.T) {
	tests := []struct {
		name    string
		ready   int32
		desired int32
		want    float64
	}{
		{name: "zero desired", ready: 0, desired: 0, want: 1.0},
		{name: "half ready", ready: 5, desired: 10, want: 0.5},
		{name: "all ready", ready: 10, desired: 10, want: 1.0},
		{name: "none ready", ready: 0, desired: 10, want: 0.0},
		{name: "over ready clamped", ready: 15, desired: 10, want: 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectiveCapacityRatio(tt.ready, tt.desired)
			if got != tt.want {
				t.Errorf("effectiveCapacityRatio(%d, %d) = %f, want %f", tt.ready, tt.desired, got, tt.want)
			}
		})
	}
}
