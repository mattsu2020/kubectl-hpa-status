package cmdoptions

import (
	"io"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"k8s.io/client-go/kubernetes"
)

// Common holds CLI flags shared across commands: Kubernetes connection, output
// formatting, language, debug settings, and cross-command workflow flags.
type Common struct {
	Namespace             string
	AllNamespaces         bool
	ContextName           string
	Kubeconfig            string
	Cluster               string
	Output                string
	Template              string
	Wide                  bool
	Selector              string
	Color                 string
	Lang                  string
	Debug                 bool
	Config                string
	ChunkSize             int64
	Concurrency           int
	QPS                   float32
	Burst                 int
	OutputTemplates       map[string]OutputTemplateConfig
	ClientOverride        kubernetes.Interface
	In                    io.Reader
	Apply                 bool
	Diff                  bool
	DryRun                bool
	Yes                   bool
	AllowPartial          bool
	Export                string
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
	}
}

// NewClient constructs a Kubernetes client from Common settings.
func (c *Common) NewClient() (*kube.Client, error) {
	if c.ClientOverride != nil {
		return kube.NewClient(c.KubeOptions(), kube.WithInterface(c.ClientOverride))
	}
	return kube.NewClient(c.KubeOptions())
}
