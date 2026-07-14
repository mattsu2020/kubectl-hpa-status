package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func buildControllerProfile(ctx context.Context, client *kube.Client, assumeProfile, profileFile string) *hpaanalysis.ControllerProfile {
	profile := hpaanalysis.DefaultControllerProfile()
	if assumeProfile != "" {
		profile.Source = "assumed:" + assumeProfile
		return &profile
	}
	if profileFile != "" {
		loaded, err := loadControllerProfileFile(profileFile)
		if err == nil {
			return loaded
		}
		profile.Warnings = append(profile.Warnings, fmt.Sprintf("failed to load controller profile file: %v", err))
	}
	if client == nil {
		return &profile
	}
	observed, ok := observeControllerManagerProfile(ctx, client)
	if !ok {
		profile.Warnings = append(profile.Warnings, "kube-controller-manager args were not visible; using Kubernetes defaults")
		return &profile
	}
	return observed
}

func loadControllerProfileFile(path string) (*hpaanalysis.ControllerProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	profile := hpaanalysis.DefaultControllerProfile()
	if err := yaml.Unmarshal(data, &profile); err != nil {
		return nil, err
	}
	if profile.Source == "" {
		profile.Source = "file:" + path
	}
	return &profile, nil
}

func observeControllerManagerProfile(ctx context.Context, client *kube.Client) (*hpaanalysis.ControllerProfile, bool) {
	pods, err := client.Interface.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, false
	}
	for _, pod := range pods.Items {
		if !strings.Contains(pod.Name, "kube-controller-manager") {
			continue
		}
		profile := hpaanalysis.DefaultControllerProfile()
		profile.Source = "kube-system/" + pod.Name
		for _, container := range pod.Spec.Containers {
			for _, arg := range container.Command {
				applyControllerArg(&profile, arg)
			}
			for _, arg := range container.Args {
				applyControllerArg(&profile, arg)
			}
		}
		return &profile, true
	}
	return nil, false
}

func applyControllerArg(profile *hpaanalysis.ControllerProfile, arg string) {
	if profile == nil || !strings.HasPrefix(arg, "--") {
		return
	}
	key, value, ok := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
	if !ok {
		return
	}
	switch key {
	case "horizontal-pod-autoscaler-sync-period":
		profile.SyncPeriod = value
	case "horizontal-pod-autoscaler-downscale-stabilization":
		profile.DownscaleStabilization = value
	case "horizontal-pod-autoscaler-initial-readiness-delay":
		profile.InitialReadinessDelay = value
	case "horizontal-pod-autoscaler-cpu-initialization-period":
		profile.CPUInitializationPeriod = value
	case "horizontal-pod-autoscaler-tolerance":
		profile.Tolerance = value
	}
}
