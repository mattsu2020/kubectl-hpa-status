// Package kube provides Kubernetes client construction and resource helpers.
package kube

import (
	"context"
	"path/filepath"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // for cloud provider auth
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Options holds kubeconfig and cluster connection settings.
type Options struct {
	Namespace  string
	Context    string
	Kubeconfig string
	Cluster    string
	QPS        float32 // Client-side rate limiting queries per second. 0 means client-go default (5).
	Burst      int     // Client-side rate limiting burst size. 0 means client-go default (10).
}

// Client wraps a Kubernetes typed client with namespace information.
type Client struct {
	Interface kubernetes.Interface
	Namespace string
}

// ClientOption is a functional option for configuring a Client.
type ClientOption func(*Client)

// WithInterface injects a pre-built kubernetes.Interface, skipping kubeconfig resolution.
// Use this in tests to inject a fake client.
func WithInterface(iface kubernetes.Interface) ClientOption {
	return func(c *Client) { c.Interface = iface }
}

// WithNamespace sets an explicit namespace, skipping kubeconfig namespace resolution.
func WithNamespace(ns string) ClientOption {
	return func(c *Client) { c.Namespace = ns }
}

// NewClient creates a Client from the given Options.
func NewClient(opts Options, extra ...ClientOption) (*Client, error) {
	c := &Client{}
	for _, opt := range extra {
		opt(c)
	}

	if c.Interface != nil {
		if c.Namespace == "" {
			if opts.Namespace != "" {
				c.Namespace = opts.Namespace
			} else {
				c.Namespace = "default"
			}
		}
		return c, nil
	}

	loadingRules := newLoadingRules(opts)
	overrides := newOverrides(opts)

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)

	namespace := opts.Namespace
	if namespace == "" {
		var err error
		namespace, _, err = clientConfig.Namespace()
		if err != nil {
			return nil, err
		}
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	if opts.QPS > 0 {
		restConfig.QPS = opts.QPS
	}
	if opts.Burst > 0 {
		restConfig.Burst = opts.Burst
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	return &Client{Interface: client, Namespace: namespace}, nil
}

// ListHPAs reads HPAs with Kubernetes API pagination enabled when chunkSize is
// positive. This keeps list/scan memory growth predictable on large clusters
// while preserving the same returned object shape for existing analyzers.
func (c *Client) ListHPAs(ctx context.Context, namespace string, opts metav1.ListOptions, chunkSize int64) (*autoscalingv2.HorizontalPodAutoscalerList, error) {
	return ListHPAsFromInterface(ctx, c.Interface, namespace, opts, chunkSize)
}

// ListHPAsEachPage lists HPAs page by page, invoking fn for each page's HPA
// slice. fn receives the raw page (not the accumulated list) so streaming
// callers can convert items to a lighter shape and release raw HPAs before the
// next page arrives, keeping memory flat on large clusters. Pagination follows
// the Kubernetes Continue token. chunkSize <= 0 lists in a single page.
func ListHPAsEachPage(ctx context.Context, iface kubernetes.Interface, namespace string, opts metav1.ListOptions, chunkSize int64, fn func(*autoscalingv2.HorizontalPodAutoscalerList) error) error {
	if chunkSize <= 0 {
		list, err := iface.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, opts)
		if err != nil {
			return err
		}
		return fn(list)
	}

	opts.Limit = chunkSize
	opts.Continue = ""
	for {
		page, err := iface.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, opts)
		if err != nil {
			return err
		}
		if err := fn(page); err != nil {
			return err
		}
		if page.Continue == "" {
			return nil
		}
		opts.Continue = page.Continue
	}
}

// ListHPAsFromInterface reads HPAs from a raw kubernetes.Interface with the
// same pagination semantics as Client.ListHPAs. It is a thin wrapper over
// ListHPAsEachPage that accumulates all pages into a single list for callers
// that need the complete raw slice at once (tests, fleet, conflicts, compare).
func ListHPAsFromInterface(ctx context.Context, iface kubernetes.Interface, namespace string, opts metav1.ListOptions, chunkSize int64) (*autoscalingv2.HorizontalPodAutoscalerList, error) {
	all := &autoscalingv2.HorizontalPodAutoscalerList{}
	err := ListHPAsEachPage(ctx, iface, namespace, opts, chunkSize, func(page *autoscalingv2.HorizontalPodAutoscalerList) error {
		if all.ResourceVersion == "" {
			all.TypeMeta = page.TypeMeta
			all.ResourceVersion = page.ResourceVersion
		}
		all.Items = append(all.Items, page.Items...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

func newLoadingRules(opts Options) *clientcmd.ClientConfigLoadingRules {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if opts.Kubeconfig != "" {
		loadingRules.ExplicitPath = opts.Kubeconfig
	} else if loadingRules.ExplicitPath == "" {
		if home := homeDir(); home != "" {
			loadingRules.ExplicitPath = filepath.Join(home, ".kube", "config")
		}
	}
	return loadingRules
}

func newOverrides(opts Options) *clientcmd.ConfigOverrides {
	overrides := &clientcmd.ConfigOverrides{}
	if opts.Context != "" {
		overrides.CurrentContext = opts.Context
	}
	if opts.Cluster != "" {
		overrides.Context = clientcmdapi.Context{Cluster: opts.Cluster}
	}
	return overrides
}

// CRDAvailability holds the results of a one-time CRD availability check.
type CRDAvailability struct {
	KEDA bool
	VPA  bool
}

// NewDiscoveryClient creates a discovery client from the same Options used for
// the typed and dynamic clients.
func NewDiscoveryClient(opts Options) (discovery.DiscoveryInterface, error) {
	loadingRules := newLoadingRules(opts)
	overrides := newOverrides(opts)

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	return discovery.NewDiscoveryClientForConfig(restConfig)
}

// DetectCRDs checks whether KEDA and VPA CRDs are installed on the cluster.
// Any discovery error (including CRD not found) is treated as absent.
// The function never returns an error.
func DetectCRDs(disco discovery.DiscoveryInterface) CRDAvailability {
	var avail CRDAvailability

	_, err := disco.ServerResourcesForGroupVersion("keda.sh/v1alpha1")
	if err == nil {
		avail.KEDA = true
	}

	_, err = disco.ServerResourcesForGroupVersion("autoscaling.k8s.io/v1")
	if err == nil {
		avail.VPA = true
	}

	return avail
}
