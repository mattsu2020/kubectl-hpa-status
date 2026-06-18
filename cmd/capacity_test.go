package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func TestScaleTargetSelectorWrapsGetErrorsWithTarget(t *testing.T) {
	client := &kube.Client{Interface: testutil.NewFakeClientWithObjects()}

	tests := []struct {
		kind string
		want string
	}{
		{kind: "Deployment", want: "failed to get Deployment default/web"},
		{kind: "StatefulSet", want: "failed to get StatefulSet default/web"},
		{kind: "ReplicaSet", want: "failed to get ReplicaSet default/web"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			selector, err := scaleTargetSelector(context.Background(), client, "default", autoscalingv2.CrossVersionObjectReference{
				Kind: tt.kind,
				Name: "web",
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if selector != nil {
				t.Fatalf("expected nil selector, got %#v", selector)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error to contain %q, got %q", tt.want, err.Error())
			}
			if !apierrors.IsNotFound(err) {
				t.Fatalf("expected wrapped not found error, got %v", err)
			}
		})
	}
}

func TestScaleTargetSelectorIgnoresUnsupportedKind(t *testing.T) {
	client := &kube.Client{Interface: testutil.NewFakeClientWithObjects()}

	selector, err := scaleTargetSelector(context.Background(), client, "default", autoscalingv2.CrossVersionObjectReference{
		Kind: "Job",
		Name: "web",
	})
	if err != nil {
		t.Fatalf("expected unsupported kind to be ignored, got error: %v", err)
	}
	if selector != nil {
		t.Fatalf("expected nil selector, got %#v", selector)
	}
}
