package cmdoptions

import (
	"io"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"k8s.io/client-go/kubernetes"
)

// Common holds CLI flags shared across commands: Kubernetes connection, output
// formatting, language, debug settings, and cross-command workflow flags. It
// groups fields into embedded sub-structs by concern; anonymous embedding
// promotes every field, so callers keep using the flat opts.Namespace-style
// access regardless of which group a field lives in.
type Common struct {
	ConnectionOptions
	OutputOptions
	ApplyOptions
	TrendOptions
}

// ConnectionOptions holds the Kubernetes connection and request-shaping
// flags, plus the I/O ports commands read from and write diagnostics to.
type ConnectionOptions struct {
	Namespace      string
	AllNamespaces  bool
	ContextName    string
	Kubeconfig     string
	Cluster        string
	Selector       string
	Config         string
	ChunkSize      int64
	Concurrency    int
	QPS            float32
	Burst          int
	RequestTimeout time.Duration
	ClientOverride kubernetes.Interface
	In             io.Reader
	// Err receives diagnostic output such as warnings and per-item failures.
	// It is intentionally not a CLI flag: cobra wires it to ErrOrStderr so
	// machine-readable stdout remains safe to redirect.
	Err io.Writer
}

// OutputOptions holds rendering and formatting flags: output format,
// templates, color, language, and export destination.
type OutputOptions struct {
	Output          string
	Template        string
	Wide            bool
	Color           string
	Lang            string
	Debug           bool
	OutputTemplates map[string]OutputTemplateConfig
	Export          string
}

// ApplyOptions holds the mutating-workflow flags shared by commands that can
// write changes back to the cluster (apply, diff, dry-run, confirmation).
type ApplyOptions struct {
	Apply        bool
	Diff         bool
	DryRun       bool
	Yes          bool
	AllowPartial bool
}

// TrendOptions holds the health-trend and health-scoring flags shared across
// commands that analyze HPA history.
type TrendOptions struct {
	Trend                 bool
	TrendSince            time.Duration
	TrendRetain           time.Duration
	HealthWeights         hpaanalysis.HealthWeights
	HealthWeightOverrides []string
}

// OutputTemplateConfig defines a named output template entry in the config file.
type OutputTemplateConfig struct {
	Type     string `json:"type" yaml:"type"`
	Template string `json:"template" yaml:"template"`
}

// KubeOptions returns the kube client options derived from Common.
func (c *Common) KubeOptions() kube.Options {
	return kube.Options{
		Namespace:  c.Namespace,
		Context:    c.ContextName,
		Kubeconfig: c.Kubeconfig,
		Cluster:    c.Cluster,
		QPS:        c.QPS,
		Burst:      c.Burst,
		Timeout:    c.RequestTimeout,
	}
}

// NewClient constructs a Kubernetes client from Common settings.
func (c *Common) NewClient() (*kube.Client, error) {
	if c.ClientOverride != nil {
		return kube.NewClient(c.KubeOptions(), kube.WithInterface(c.ClientOverride))
	}
	return kube.NewClient(c.KubeOptions())
}
