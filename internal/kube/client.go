package kube

import (
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/clientcmd"
)

type Options struct {
	Namespace  string
	Context    string
	Kubeconfig string
	Cluster    string
}

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

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if opts.Kubeconfig != "" {
		loadingRules.ExplicitPath = opts.Kubeconfig
	} else if loadingRules.ExplicitPath == "" {
		if home := homeDir(); home != "" {
			loadingRules.ExplicitPath = filepath.Join(home, ".kube", "config")
		}
	}

	overrides := &clientcmd.ConfigOverrides{}
	if opts.Context != "" {
		overrides.CurrentContext = opts.Context
	}
	if opts.Cluster != "" {
		overrides.Context.Cluster = opts.Cluster
	}

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

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	return &Client{Interface: client, Namespace: namespace}, nil
}
