// Package kube provides Kubernetes client construction and resource helpers.
package kube

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // for cloud provider auth
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Options holds kubeconfig and cluster connection settings.
type Options struct {
	Namespace  string
	Context    string
	Kubeconfig string
	Cluster    string
	QPS        float32       // Client-side rate limiting queries per second. 0 means client-go default (5).
	Burst      int           // Client-side rate limiting burst size. 0 means client-go default (10).
	Timeout    time.Duration // Per-request timeout applied to the REST config. 0 means no timeout.
}

// Client wraps a Kubernetes typed client with namespace information and shares
// a single REST config and HTTP transport across lazy discovery/dynamic clients.
type Client struct {
	Interface kubernetes.Interface
	Namespace string

	restConfig *restclient.Config
	httpClient *http.Client
	mu         sync.Mutex
	disco      discovery.DiscoveryInterface
	dyn        dynamic.Interface
}

// ClientBundle shares one parsed rest.Config and HTTP transport across typed,
// discovery, and dynamic Kubernetes clients.
type ClientBundle struct {
	Client    *Client
	Discovery discovery.DiscoveryInterface
	Dynamic   dynamic.Interface
	Config    *restclient.Config
	HTTP      *http.Client
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

	namespace, restConfig, err := resolveNamespaceAndRestConfig(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client from kubeconfig/context flags: %w", err)
	}

	httpClient, err := restclient.HTTPClientFor(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create shared HTTP transport: %w", err)
	}

	client, err := kubernetes.NewForConfigAndClient(restConfig, httpClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client from kubeconfig/context flags: %w", err)
	}

	return &Client{
		Interface:  client,
		Namespace:  namespace,
		restConfig: restConfig,
		httpClient: httpClient,
	}, nil
}

// NewClientBundle creates a typed Client along with discovery and dynamic
// clients that all share one parsed rest.Config and underlying HTTP transport.
func NewClientBundle(opts Options, extra ...ClientOption) (*ClientBundle, error) {
	c, err := NewClient(opts, extra...)
	if err != nil {
		return nil, err
	}
	disco, err := c.DiscoveryClient()
	if err != nil {
		return nil, err
	}
	dyn, err := c.DynamicClient()
	if err != nil && c.restConfig != nil {
		return nil, err
	}
	return &ClientBundle{
		Client:    c,
		Discovery: disco,
		Dynamic:   dyn,
		Config:    c.restConfig,
		HTTP:      c.httpClient,
	}, nil
}

// DiscoveryClient returns a discovery client sharing this Client's HTTP
// transport, or falls back to Interface.Discovery() when injected in tests.
func (c *Client) DiscoveryClient() (discovery.DiscoveryInterface, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disco != nil {
		return c.disco, nil
	}
	if c.restConfig != nil && c.httpClient != nil {
		disco, err := discovery.NewDiscoveryClientForConfigAndClient(c.restConfig, c.httpClient)
		if err != nil {
			return nil, err
		}
		c.disco = disco
		return disco, nil
	}
	if c.Interface != nil {
		disco := c.Interface.Discovery()
		c.disco = disco
		return disco, nil
	}
	return nil, fmt.Errorf("client is not initialized")
}

// DynamicClient returns a dynamic client sharing this Client's HTTP transport.
func (c *Client) DynamicClient() (dynamic.Interface, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dyn != nil {
		return c.dyn, nil
	}
	if c.restConfig != nil && c.httpClient != nil {
		dyn, err := dynamic.NewForConfigAndClient(c.restConfig, c.httpClient)
		if err != nil {
			return nil, err
		}
		c.dyn = dyn
		return dyn, nil
	}
	return nil, fmt.Errorf("dynamic client unavailable without REST config")
}

// SharedHTTPClient returns the underlying HTTP client when built from a REST config.
func (c *Client) SharedHTTPClient() *http.Client {
	return c.httpClient
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
// the Kubernetes Continue token, including the repeated-token guard against
// servers that hand back the same token forever. chunkSize <= 0 lists in a
// single page. Each page is retried briefly on transient API-server failures
// (429/5xx/timeouts).
func ListHPAsEachPage(ctx context.Context, iface kubernetes.Interface, namespace string, opts metav1.ListOptions, chunkSize int64, fn func(*autoscalingv2.HorizontalPodAutoscalerList) error) error {
	if chunkSize > 0 {
		opts.Limit = chunkSize
	}
	opts.Continue = ""
	seenContinue := make(map[string]bool)
	for {
		page, err := retryTransient(ctx, func() (*autoscalingv2.HorizontalPodAutoscalerList, error) {
			return iface.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, opts)
		})
		if err != nil {
			return err
		}
		if err := fn(page); err != nil {
			return err
		}
		if page.Continue == "" {
			return nil
		}
		if err := guardRepeatedContinueToken(seenContinue, page.Continue, fmt.Sprintf("listing HPAs in namespace %s", namespace)); err != nil {
			return err
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

// deferredClientConfig builds the standard clientcmd.ClientConfig from the same
// Options used by NewClient, NewDiscoveryClient, and NewDynamicClient. It is the
// shared step that previously was inlined in each constructor.
func deferredClientConfig(opts Options) clientcmd.ClientConfig {
	loadingRules := newLoadingRules(opts)
	overrides := newOverrides(opts)
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
}

// restConfigFromOptions resolves a *restclient.Config from Options without
// resolving the namespace. Used by constructors that do not need a namespace
// (e.g. NewDiscoveryClient).
func restConfigFromOptions(opts Options) (*restclient.Config, error) {
	restConfig, err := deferredClientConfig(opts).ClientConfig()
	if err != nil {
		return nil, err
	}
	applyRestOptions(restConfig, opts)
	return restConfig, nil
}

// applyRestOptions copies the client tuning knobs (rate limiting, per-request
// timeout) from Options onto the resolved REST config. It is the single
// application point shared by every client constructor (typed, dynamic,
// discovery), so the flags behave identically regardless of which client a
// command builds.
func applyRestOptions(restConfig *restclient.Config, opts Options) {
	if opts.QPS > 0 {
		restConfig.QPS = opts.QPS
	}
	if opts.Burst > 0 {
		restConfig.Burst = opts.Burst
	}
	if opts.Timeout > 0 {
		restConfig.Timeout = opts.Timeout
	}
}

// resolveNamespaceAndRestConfig resolves both the effective namespace and the
// *restclient.Config from Options. When opts.Namespace is empty the namespace
// is resolved from the kubeconfig current context. Used by NewClient and
// NewDynamicClient, which both need a namespace.
func resolveNamespaceAndRestConfig(opts Options) (string, *restclient.Config, error) {
	clientConfig := deferredClientConfig(opts)
	namespace := opts.Namespace
	if namespace == "" {
		var err error
		namespace, _, err = clientConfig.Namespace()
		if err != nil {
			return "", nil, err
		}
	}
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return "", nil, err
	}
	applyRestOptions(restConfig, opts)
	return namespace, restConfig, nil
}

// ClusterIdentity returns a stable local identifier for the cluster the
// Options point at: an explicit --cluster / --context flag when set, otherwise
// the kubeconfig current-context name, otherwise "default". Local persistence
// keyed by this identifier (e.g. health history) must include it so switching
// kubeconfig contexts never merges two clusters' data; it identifies the
// *local view* of the cluster, not the cluster itself.
func ClusterIdentity(opts Options) string {
	if opts.Cluster != "" {
		return opts.Cluster
	}
	if opts.Context != "" {
		return opts.Context
	}
	if raw, err := deferredClientConfig(opts).RawConfig(); err == nil && raw.CurrentContext != "" {
		return raw.CurrentContext
	}
	return "default"
}

// CRDAvailability holds the results of a one-time CRD availability check.
// KEDError / VPAError carry the discovery error (if any) for each source so
// callers can distinguish "CRD is simply absent" (nil error) from "discovery
// failed" (non-nil error, e.g. RBAC denial or network timeout). They wrap
// ErrKEDACRDNotDetected / ErrVPACRDNotDetected respectively, so callers can
// match with errors.Is.
type CRDAvailability struct {
	KEDA     bool
	VPA      bool
	KEDError error
	VPAError error
}

// NewDiscoveryClient creates a discovery client from the same Options used for
// the typed and dynamic clients.
func NewDiscoveryClient(opts Options) (discovery.DiscoveryInterface, error) {
	restConfig, err := restConfigFromOptions(opts)
	if err != nil {
		return nil, err
	}

	return discovery.NewDiscoveryClientForConfig(restConfig)
}

// DetectCRDs checks whether KEDA and VPA CRDs are installed on the cluster.
// Each source's discovery error is preserved in CRDAvailability.KEDError /
// VPAError (wrapping ErrKEDACRDNotDetected / ErrVPACRDNotDetected) so callers
// can distinguish "CRD is absent" (the wrapped discovery error, typically a
// not-found) from "discovery itself failed" (e.g. RBAC denial, network
// timeout). The function never returns a top-level error: callers consume the
// per-source results and surface them via Status entries.
func DetectCRDs(disco discovery.DiscoveryInterface) CRDAvailability {
	var avail CRDAvailability

	avail.KEDA, avail.KEDError = detectCRDResource(disco, "keda.sh/v1alpha1", "scaledobjects", ErrKEDACRDNotDetected)
	avail.VPA, avail.VPAError = detectCRDResource(disco, "autoscaling.k8s.io/v1", "verticalpodautoscalers", ErrVPACRDNotDetected)

	return avail
}

func detectCRDResource(disco discovery.DiscoveryInterface, groupVersion, resourceName string, notDetectedErr error) (bool, error) {
	list, err := disco.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		if isGroupVersionNotFound(err) {
			return false, fmt.Errorf("%s: %w: %w", groupVersion, notDetectedErr, err)
		}
		return false, fmt.Errorf("%s: %w", groupVersion, err)
	}
	if list == nil || !containsAPIResource(list.APIResources, resourceName) {
		return false, fmt.Errorf("%s: %w: resource %q not served", groupVersion, notDetectedErr, resourceName)
	}
	return true, nil
}

func containsAPIResource(resources []metav1.APIResource, name string) bool {
	for i := range resources {
		if resources[i].Name == name {
			return true
		}
	}
	return false
}

func isGroupVersionNotFound(err error) bool {
	if err == nil {
		return false
	}
	if apierrors.IsNotFound(err) {
		return true
	}
	var statusErr *apierrors.StatusError
	if errors.As(err, &statusErr) {
		return false
	}
	// client-go FakeDiscovery returns plain errors when a GroupVersion is absent.
	msg := err.Error()
	return strings.Contains(msg, "not found") || strings.Contains(msg, "could not find the requested resource")
}
