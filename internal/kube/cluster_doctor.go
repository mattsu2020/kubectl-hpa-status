package kube

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CheckAPIServices checks the availability of metrics API services.
func CheckAPIServices(ctx context.Context, client Interface) []APIServiceStatus {
	services := []struct {
		name       string
		apiGroup   string
		apiVersion string
	}{
		{"metrics.k8s.io/v1beta1", "metrics.k8s.io", "v1beta1"},
		{"custom.metrics.k8s.io/v1beta1", "custom.metrics.k8s.io", "v1beta1"},
		{"external.metrics.k8s.io/v1beta1", "external.metrics.k8s.io", "v1beta1"},
	}

	var results []APIServiceStatus
	for _, svc := range services {
		status := checkAPIGroup(ctx, client, svc.name, svc.apiGroup, svc.apiVersion)
		results = append(results, status)
	}
	return results
}

// APIServiceStatus holds the availability status of a metrics API service.
type APIServiceStatus struct {
	Name    string
	Status  string // "available", "unavailable", "unknown"
	Message string
}

func checkAPIGroup(ctx context.Context, client kubernetes.Interface, name, apiGroup, apiVersion string) APIServiceStatus {
	groups, err := client.Discovery().ServerGroups()
	if err != nil {
		return APIServiceStatus{
			Name:    name,
			Status:  "unknown",
			Message: fmt.Sprintf("failed to discover API groups: %v", err),
		}
	}

	for _, group := range groups.Groups {
		if group.Name == apiGroup {
			for _, v := range group.Versions {
				if v.Version == apiVersion {
					return APIServiceStatus{
						Name:   name,
						Status: "available",
					}
				}
			}
		}
	}

	return APIServiceStatus{
		Name:    name,
		Status:  "unavailable",
		Message: fmt.Sprintf("API group %s version %s not found; install the corresponding metrics adapter", apiGroup, apiVersion),
	}
}

// CheckMetricsServer checks whether the metrics-server Deployment is running
// and healthy in common namespaces (kube-system, openshift-monitoring).
func CheckMetricsServer(ctx context.Context, client kubernetes.Interface) *MetricsServerStatus {
	namespaces := []string{"kube-system", "openshift-monitoring"}
	names := []string{"metrics-server", "openshift-state-metrics"}

	for _, ns := range namespaces {
		for _, name := range names {
			deploy, err := client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				continue
			}
			return buildMetricsServerStatus(deploy, ns)
		}
	}

	// Try listing deployments in kube-system with metrics-server label.
	deploys, err := client.AppsV1().Deployments("kube-system").List(ctx, metav1.ListOptions{
		LabelSelector: "k8s-app=metrics-server",
	})
	if err == nil && len(deploys.Items) > 0 {
		return buildMetricsServerStatus(&deploys.Items[0], "kube-system")
	}

	return &MetricsServerStatus{
		Available: false,
		Message:   "metrics-server deployment not found in kube-system or openshift-monitoring; HPA resource metrics will not work without it",
	}
}

// MetricsServerStatus holds the health status of the metrics-server.
type MetricsServerStatus struct {
	Available     bool
	Ready         bool
	Replicas      int32
	ReadyReplicas int32
	Namespace     string
	Version       string
	Message       string
}

func buildMetricsServerStatus(deploy *appsv1.Deployment, ns string) *MetricsServerStatus {
	status := &MetricsServerStatus{
		Available:     true,
		Replicas:      deploy.Status.Replicas,
		ReadyReplicas: deploy.Status.ReadyReplicas,
		Namespace:     ns,
	}

	if deploy.Status.ReadyReplicas > 0 {
		status.Ready = true
	}

	// Extract version from image.
	for _, container := range deploy.Spec.Template.Spec.Containers {
		if container.Image != "" {
			status.Version = container.Image
			break
		}
	}

	if !status.Ready {
		status.Message = fmt.Sprintf("metrics-server has %d replicas but 0 ready; pods may be crashing or image pulling", deploy.Status.Replicas)
	}

	return status
}

// CheckRBAC uses SelfSubjectAccessReview to verify the current user/service
// account has the required permissions for HPA diagnostics.
func CheckRBAC(ctx context.Context, client kubernetes.Interface, namespace string) *RBACStatus {
	if namespace == "" {
		namespace = "default"
	}

	return &RBACStatus{
		CanGetHPA:    checkAccess(ctx, client, namespace, "get", "horizontalpodautoscalers", "autoscaling"),
		CanListHPA:   checkAccess(ctx, client, namespace, "list", "horizontalpodautoscalers", "autoscaling"),
		CanGetPods:   checkAccess(ctx, client, namespace, "get", "pods", ""),
		CanGetEvents: checkAccess(ctx, client, namespace, "get", "events", ""),
	}
}

// RBACStatus holds the result of RBAC permission checks.
type RBACStatus struct {
	CanGetHPA    bool
	CanListHPA   bool
	CanGetPods   bool
	CanGetEvents bool
}

func checkAccess(ctx context.Context, client kubernetes.Interface, namespace, verb, resource, group string) bool {
	sar := &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace: namespace,
				Verb:      verb,
				Resource:  resource,
				Group:     group,
			},
		},
	}

	result, err := client.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
	if err != nil {
		return false
	}
	return result.Status.Allowed
}
