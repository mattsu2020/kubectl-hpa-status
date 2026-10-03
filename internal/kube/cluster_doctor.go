package kube

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CheckAPIServices checks the availability of metrics API services. Discovery
// runs once and the cached group list is shared by all three checks instead of
// one full ServerGroups round trip per service.
func CheckAPIServices(ctx context.Context, client kubernetes.Interface) []APIServiceStatus {
	if err := ctx.Err(); err != nil {
		return []APIServiceStatus{{
			Name:    "metrics API services",
			Status:  "unknown",
			Message: fmt.Sprintf("context cancelled before discovery: %v", err),
		}}
	}
	services := []struct {
		name       string
		apiGroup   string
		apiVersion string
	}{
		{"metrics.k8s.io/v1beta1", "metrics.k8s.io", "v1beta1"},
		{"custom.metrics.k8s.io/v1beta1", "custom.metrics.k8s.io", "v1beta1"},
		{"external.metrics.k8s.io/v1beta1", "external.metrics.k8s.io", "v1beta1"},
	}

	groups, groupsErr := client.Discovery().ServerGroups()

	var results []APIServiceStatus
	for _, svc := range services {
		var shared *metav1.APIGroupList
		if groupsErr == nil {
			shared = groups
		}
		results = append(results, checkAPIGroup(client, svc.name, svc.apiGroup, svc.apiVersion, shared))
	}
	return results
}

// APIServiceStatus holds the availability status of a metrics API service.
type APIServiceStatus struct {
	Name    string
	Status  string // "available", "unavailable", "unknown"
	Message string
}

// checkAPIGroup reports one API group's availability. groups may carry a
// previously fetched discovery result; nil triggers a fresh ServerGroups call.
func checkAPIGroup(client kubernetes.Interface, name, apiGroup, apiVersion string, groups *metav1.APIGroupList) APIServiceStatus {
	if groups == nil {
		fetched, err := client.Discovery().ServerGroups()
		if err != nil {
			return APIServiceStatus{
				Name:    name,
				Status:  "unknown",
				Message: fmt.Sprintf("failed to discover API groups: %v", err),
			}
		}
		groups = fetched
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
// A missing deployment and a failed lookup are reported differently: an
// RBAC denial or network error must not be diagnosed as "not installed",
// mirroring the DetectCRDs absent-vs-unavailable policy.
func CheckMetricsServer(ctx context.Context, client kubernetes.Interface) *MetricsServerStatus {
	namespaces := []string{"kube-system", "openshift-monitoring"}
	names := []string{"metrics-server", "openshift-state-metrics"}

	var observeErr error
	for _, ns := range namespaces {
		for _, name := range names {
			deploy, err := client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				if !apierrors.IsNotFound(err) && observeErr == nil {
					observeErr = err
				}
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
	if err != nil && !apierrors.IsNotFound(err) && observeErr == nil {
		observeErr = err
	}

	if observeErr != nil {
		return &MetricsServerStatus{
			Available: false,
			Message:   fmt.Sprintf("could not observe the metrics-server deployment: %v", observeErr),
		}
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

	result := &RBACStatus{Errors: map[string]string{}}
	check := func(verb, resource, group string) bool {
		allowed, err := checkAccess(ctx, client, namespace, verb, resource, group)
		if err != nil {
			result.Errors[verb+"/"+resource] = err.Error()
		}
		return allowed
	}
	result.CanGetHPA = check("get", "horizontalpodautoscalers", "autoscaling")
	result.CanListHPA = check("list", "horizontalpodautoscalers", "autoscaling")
	result.CanGetPods = check("get", "pods", "")
	result.CanGetEvents = check("get", "events", "")
	result.CanListPods = check("list", "pods", "")
	result.CanListEvents = check("list", "events", "")
	return result
}

// RBACStatus holds the result of RBAC permission checks.
type RBACStatus struct {
	Errors        map[string]string
	CanGetHPA     bool
	CanListHPA    bool
	CanGetPods    bool
	CanGetEvents  bool
	CanListPods   bool
	CanListEvents bool
}

func checkAccess(ctx context.Context, client kubernetes.Interface, namespace, verb, resource, group string) (bool, error) {
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
		return false, err
	}
	if result.Status.EvaluationError != "" {
		return false, fmt.Errorf("access review evaluation failed: %s", result.Status.EvaluationError)
	}
	return result.Status.Allowed, nil
}
