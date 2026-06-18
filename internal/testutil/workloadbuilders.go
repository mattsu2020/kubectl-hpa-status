package testutil

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// NewFakeClientWithObjects creates a fake Kubernetes clientset pre-loaded with
// arbitrary runtime objects (Deployments, StatefulSets, ReplicaSets, Pods,
// HPAs, Events, ...). It is the generalised form of NewFakeClient /
// NewFakeClientWithEvents for tests that need workload objects alongside HPAs.
func NewFakeClientWithObjects(objects ...runtime.Object) *fake.Clientset {
	return fake.NewSimpleClientset(objects...) //nolint:staticcheck // SA1019 deprecated, no replacement without applyconfig
}

// ContainerSpec describes a container for the workload builders. Requests and
// Limits are keyed by corev1 resource name (e.g. string(corev1.ResourceCPU)).
// Empty maps are allowed; a missing resource map means "no requirements".
type ContainerSpec struct {
	Name     string
	Requests map[string]string
	Limits   map[string]string
}

// WorkloadOption customises a workload builder. The concrete option types
// (DeploymentOption, StatefulSetOption, ReplicaSetOption, PodOption) all share
// this signature via the shared builderState below.
type WorkloadOption func(*builderState)

// builderState carries the fields common to every workload kind so the option
// functions can stay kind-agnostic. Each Build* helper seeds the relevant
// status/template from this state. Pod-only fields (phase, container statuses,
// conditions) live here too so a single option signature covers all builders.
type builderState struct {
	containers        []ContainerSpec
	selector          map[string]string
	labels            map[string]string
	replicas          int32
	ready             int32
	desired           int32
	podTemplate       *corev1.PodTemplateSpec
	statusSet         bool
	templateSet       bool
	podPhase          corev1.PodPhase
	podPhaseSet       bool
	containerStatuses []corev1.ContainerStatus
	podConditions     []corev1.PodCondition
}

// WithContainer adds a container to the workload's pod template.
func WithContainer(c ContainerSpec) WorkloadOption {
	return func(s *builderState) {
		s.containers = append(s.containers, c)
	}
}

// WithContainers replaces the workload's containers with the given slice.
func WithContainers(cs ...ContainerSpec) WorkloadOption {
	return func(s *builderState) {
		s.containers = append(s.containers[:0], cs...)
	}
}

// WithSelector sets the workload's label selector (MatchLabels). For Pods this
// is stored as the pod's labels so selector-based list calls match them.
func WithSelector(matchLabels map[string]string) WorkloadOption {
	return func(s *builderState) {
		s.selector = matchLabels
	}
}

// WithWorkloadLabels sets labels on the workload object itself.
func WithWorkloadLabels(labels map[string]string) WorkloadOption {
	return func(s *builderState) {
		s.labels = labels
	}
}

// WithReplicaStatus sets the workload's status Replicas and ReadyReplicas.
func WithReplicaStatus(replicas, readyReplicas int32) WorkloadOption {
	return func(s *builderState) {
		s.replicas = replicas
		s.ready = readyReplicas
		s.statusSet = true
	}
}

// WithDesiredReplicas sets the workload's status desired replica count
// (ReplicaSetStatus / StatefulSetStatus fill this from the controller).
func WithDesiredReplicas(desired int32) WorkloadOption {
	return func(s *builderState) {
		s.desired = desired
	}
}

// WithPodTemplate overrides the entire pod template. When set, container and
// selector options that target the template are ignored.
func WithPodTemplate(tmpl corev1.PodTemplateSpec) WorkloadOption {
	return func(s *builderState) {
		clone := tmpl
		s.podTemplate = &clone
		s.templateSet = true
	}
}

// DeploymentOption customises BuildDeployment. It is an alias of WorkloadOption
// so callers get kind-specific documentation in their build call site.
type DeploymentOption = WorkloadOption

// BuildDeployment creates an apps/v1 Deployment for testing.
func BuildDeployment(namespace, name string, opts ...DeploymentOption) *appsv1.Deployment {
	s := applyOptions(opts)
	tmpl := resolvePodTemplate(s)
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Labels: s.labels},
		Spec: appsv1.DeploymentSpec{
			Replicas:        ptrInt32(1),
			Template:        tmpl,
			Selector:        labelSelector(s),
			MinReadySeconds: 0,
		},
	}
	if s.statusSet {
		d.Status = appsv1.DeploymentStatus{
			Replicas:      s.replicas,
			ReadyReplicas: s.ready,
		}
	}
	return d
}

// StatefulSetOption customises BuildStatefulSet.
type StatefulSetOption = WorkloadOption

// BuildStatefulSet creates an apps/v1 StatefulSet for testing.
func BuildStatefulSet(namespace, name string, opts ...StatefulSetOption) *appsv1.StatefulSet {
	s := applyOptions(opts)
	tmpl := resolvePodTemplate(s)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Labels: s.labels},
		Spec: appsv1.StatefulSetSpec{
			Replicas:       ptrInt32(1),
			Template:       tmpl,
			Selector:       labelSelector(s),
			ServiceName:    name,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{Type: appsv1.RollingUpdateStatefulSetStrategyType},
		},
	}
	if s.statusSet {
		sts.Status = appsv1.StatefulSetStatus{
			Replicas:        s.replicas,
			ReadyReplicas:   s.ready,
		}
	}
	return sts
}

// ReplicaSetOption customises BuildReplicaSet.
type ReplicaSetOption = WorkloadOption

// BuildReplicaSet creates an apps/v1 ReplicaSet for testing.
func BuildReplicaSet(namespace, name string, opts ...ReplicaSetOption) *appsv1.ReplicaSet {
	s := applyOptions(opts)
	tmpl := resolvePodTemplate(s)
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Labels: s.labels},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: ptrInt32(1),
			Template: tmpl,
			Selector: labelSelector(s),
		},
	}
	if s.statusSet {
		rs.Status = appsv1.ReplicaSetStatus{
			Replicas:      s.replicas,
			ReadyReplicas: s.ready,
		}
	}
	return rs
}

// PodOption customises BuildPod. It shares the WorkloadOption signature so the
// generic options (WithContainer, WithSelector, ...) apply to Pods too, and the
// pod-specific options below extend the same builderState.
type PodOption = WorkloadOption

// WithPodPhase sets the pod's status phase (Pending, Running, ...).
func WithPodPhase(phase corev1.PodPhase) PodOption {
	return func(s *builderState) {
		s.podPhase = phase
		s.podPhaseSet = true
	}
}

// WithPodLabels sets labels on the pod. For selector-matching tests prefer
// WithSelector so the same map drives both the label set and assertions.
func WithPodLabels(labels map[string]string) PodOption {
	return func(s *builderState) {
		s.labels = labels
	}
}

// WithContainerStatus appends a container status entry to the pod.
func WithContainerStatus(status corev1.ContainerStatus) PodOption {
	return func(s *builderState) {
		s.containerStatuses = append(s.containerStatuses, status)
	}
}

// WithPodCondition appends a pod condition (e.g. PodScheduled=False,
// Reason=Unschedulable) used by pending-pod diagnostics.
func WithPodCondition(condition corev1.PodCondition) PodOption {
	return func(s *builderState) {
		s.podConditions = append(s.podConditions, condition)
	}
}

// BuildPod creates a corev1.Pod for testing. When WithSelector is provided via
// the pod options, the selector map is applied as the pod's labels.
func BuildPod(namespace, name string, opts ...PodOption) *corev1.Pod {
	s := applyOptions(opts)
	tmpl := resolvePodTemplate(s)
	labels := s.labels
	if labels == nil {
		labels = map[string]string{}
	}
	for k, v := range s.selector {
		labels[k] = v
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Labels: labels},
		Spec:       tmpl.Spec,
	}
	if s.podPhaseSet {
		pod.Status.Phase = s.podPhase
	}
	pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, s.containerStatuses...)
	pod.Status.Conditions = append(pod.Status.Conditions, s.podConditions...)
	return pod
}

// applyOptions runs the given workload options against a fresh builderState.
func applyOptions(opts []WorkloadOption) *builderState {
	s := &builderState{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// resolvePodTemplate returns the pod template to embed in a workload. A caller
// supplied template wins; otherwise a template is built from containers/labels.
func resolvePodTemplate(s *builderState) corev1.PodTemplateSpec {
	if s.templateSet && s.podTemplate != nil {
		return *s.podTemplate
	}
	tmpl := corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{Containers: toContainers(s.containers)},
	}
	if len(s.labels) > 0 {
		tmpl.ObjectMeta = metav1.ObjectMeta{Labels: s.labels}
	}
	return tmpl
}

// labelSelector builds a *metav1.LabelSelector from the selector map, returning
// nil when no selector was configured so callers keep their default behavior.
func labelSelector(s *builderState) *metav1.LabelSelector {
	if len(s.selector) == 0 {
		return nil
	}
	return &metav1.LabelSelector{MatchLabels: s.selector}
}

// toContainers converts the lightweight ContainerSpec list into corev1.Container
// entries with parsed resource quantities.
func toContainers(specs []ContainerSpec) []corev1.Container {
	if len(specs) == 0 {
		return nil
	}
	containers := make([]corev1.Container, 0, len(specs))
	for _, spec := range specs {
		c := corev1.Container{Name: spec.Name}
		if len(spec.Requests) > 0 || len(spec.Limits) > 0 {
			c.Resources = corev1.ResourceRequirements{
				Requests: parseResourceList(spec.Requests),
				Limits:   parseResourceList(spec.Limits),
			}
		}
		containers = append(containers, c)
	}
	return containers
}

// parseResourceList parses a {name: quantity-string} map into a ResourceList.
// Unknown/empty entries are skipped; invalid quantities panic (test-only).
func parseResourceList(m map[string]string) corev1.ResourceList {
	if len(m) == 0 {
		return nil
	}
	out := make(corev1.ResourceList, len(m))
	for k, v := range m {
		if v == "" {
			continue
		}
		out[corev1.ResourceName(k)] = resource.MustParse(v)
	}
	return out
}

// ptrInt32 returns a pointer to the given int32. Used for default replica counts.
func ptrInt32(v int32) *int32 { return &v }
