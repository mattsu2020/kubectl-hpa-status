package bundle

import (
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// Data holds all collected diagnostic data for a single HPA bundle. Fields are
// exported because the data-collection orchestrator (collectBundleData) lives
// in the cmd package and writes into this struct directly.
type Data struct {
	HPA         []byte
	ScaleTarget []byte
	ReplicaSets []byte
	Pods        []byte
	Events      []byte
	MetricsAPI  []byte

	// Container statuses (restart/waiting).
	ContainerStatuses []kube.ContainerStatusDetail

	// Infrastructure context.
	ResourceQuotas []kube.QuotaInfo
	LimitRanges    []kube.LimitRangeInfo
	PDBs           []kube.PDBInfo
	NodeCapacity   *kube.NodeCapacityInfo

	// Full doctor-level analysis.
	StatusReport hpaanalysis.StatusReport

	// Raw pod info for table rendering.
	PodInfos []kube.PodInfo

	// Warnings records non-fatal collection errors (RBAC denials, API server
	// timeouts) so the bundle consumer can see why a section is empty rather
	// than guessing. Best-effort collection never fails the whole bundle.
	Warnings []string

	Namespace string
	HPAName   string
	Timestamp time.Time

	// Redacted records that the bundle is intended for external sharing. ZIP
	// assembly uses it to run a final redaction pass over every generated entry,
	// including entries derived from typed fields such as StatusReport.
	Redacted bool
}
