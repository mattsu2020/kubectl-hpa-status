// Package enrichment provides KEDA and VPA enrichment logic for HPA analysis.
package enrichment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// Config holds the parameters needed to create an enrichment context.
// This decouples enrichment from the CLI options struct.
type Config struct {
	// Kube carries the full client connection settings (namespace, context,
	// kubeconfig, cluster, rate limits, request timeout) so enrichment
	// clients honor the same tuning flags as the primary typed client.
	Kube kube.Options
	KEDA string // "auto" (default), "on" (force), "off" (disable)
	VPA  string // "auto" (default), "on" (force), "off" (disable)
}

// Context holds reusable clients and CRD availability for enrichment
// operations. Created once per command invocation and shared across all
// HPA processing. Safe for concurrent use after construction because
// dynamic.Interface is goroutine-safe and CRDAvailability is read-only.
type Context struct {
	dynClient   dynamic.Interface
	namespace   string
	crdAvail    kube.CRDAvailability
	kedaEnabled bool
	vpaEnabled  bool
	status      Status

	cacheMu  sync.Mutex
	vpaCache map[string]cachedVPAResult
	soCache  map[string]cachedSOResult
}

type cachedVPAResult struct {
	items []kube.VPAInfo
	err   error
}

type cachedSOResult struct {
	items []unstructured.Unstructured
	err   error
}

func (ec *Context) fetchNamespaceVPAs(ctx context.Context, namespace string) ([]kube.VPAInfo, error) {
	if ec == nil || ec.dynClient == nil {
		return nil, fmt.Errorf("dynamic client is unavailable")
	}
	ec.cacheMu.Lock()
	if entry, ok := ec.vpaCache[namespace]; ok {
		ec.cacheMu.Unlock()
		return entry.items, entry.err
	}
	ec.cacheMu.Unlock()

	rawList, err := kube.FetchVPAs(ctx, ec.dynClient, namespace)
	var infos []kube.VPAInfo
	if err == nil {
		infos = make([]kube.VPAInfo, 0, len(rawList))
		for i := range rawList {
			infos = append(infos, kube.ExtractVPAInfo(&rawList[i]))
		}
	}

	ec.cacheMu.Lock()
	if ec.vpaCache == nil {
		ec.vpaCache = make(map[string]cachedVPAResult)
	}
	ec.vpaCache[namespace] = cachedVPAResult{items: infos, err: err}
	ec.cacheMu.Unlock()
	return infos, err
}

func (ec *Context) fetchNamespaceScaledObjects(ctx context.Context, namespace string) ([]unstructured.Unstructured, error) {
	if ec == nil || ec.dynClient == nil {
		return nil, fmt.Errorf("dynamic client is unavailable")
	}
	ec.cacheMu.Lock()
	if entry, ok := ec.soCache[namespace]; ok {
		ec.cacheMu.Unlock()
		return entry.items, entry.err
	}
	ec.cacheMu.Unlock()

	items, err := kube.FetchScaledObjects(ctx, ec.dynClient, namespace)

	ec.cacheMu.Lock()
	if ec.soCache == nil {
		ec.soCache = make(map[string]cachedSOResult)
	}
	ec.soCache[namespace] = cachedSOResult{items: items, err: err}
	ec.cacheMu.Unlock()
	return items, err
}

// NewObservation shares the clients and discovery results with a fresh
// namespace cache for a single observation cycle.
func (ec *Context) NewObservation() *Context {
	if ec == nil {
		return nil
	}
	return &Context{
		dynClient: ec.dynClient, namespace: ec.namespace, crdAvail: ec.crdAvail,
		kedaEnabled: ec.kedaEnabled, vpaEnabled: ec.vpaEnabled, status: ec.status,
	}
}

// Status returns the enrichment status for diagnostic output.
func (ec *Context) Status() Status {
	if ec == nil {
		return Status{}
	}
	return ec.status
}

// KEDAEnabled reports whether KEDA enrichment is active.
func (ec *Context) KEDAEnabled() bool { return ec != nil && ec.kedaEnabled }

// VPAEnabled reports whether VPA enrichment is active.
func (ec *Context) VPAEnabled() bool { return ec != nil && ec.vpaEnabled }

// NewContext creates an enrichment context from the given config. It checks
// CRD availability via API discovery and creates a dynamic client only when
// at least one enrichment source is available. Always returns a non-nil Context
// with status populated to explain why enrichment may be unavailable.
func NewContext(ctx context.Context, cfg Config) *Context {
	kedaMode, vpaMode := ParseMode(cfg.KEDA), ParseMode(cfg.VPA)
	kedaEntry := &Entry{Source: SourceKEDA, State: StateDisabled}
	vpaEntry := &Entry{Source: SourceVPA, State: StateDisabled}
	if kedaMode.Requested() {
		kedaEntry.State = StateUnavailable
		kedaEntry.Reason = "not yet checked"
	}
	if vpaMode.Requested() {
		vpaEntry.State = StateUnavailable
		vpaEntry.Reason = "not yet checked"
	}

	status := Status{KEDA: kedaEntry, VPA: vpaEntry}

	if !kedaMode.Requested() && !vpaMode.Requested() {
		kedaEntry.State = StateDisabled
		vpaEntry.State = StateDisabled
		return &Context{status: status}
	}
	if err := ctx.Err(); err != nil {
		kedaEntry.Reason = err.Error()
		vpaEntry.Reason = err.Error()
		return &Context{status: status}
	}
	// Discovery does not accept a context directly. Propagate the operation
	// deadline through rest.Config.Timeout so cancellation still bounds the
	// blocking discovery request.
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && (cfg.Kube.Timeout <= 0 || remaining < cfg.Kube.Timeout) {
			cfg.Kube.Timeout = remaining
		}
	}

	bundle, err := kube.NewClientBundle(cfg.Kube)
	if err != nil {
		setEnrichmentError(kedaEntry, Requested(cfg.KEDA), fmt.Sprintf("discovery client creation failed: %v", err))
		setEnrichmentError(vpaEntry, Requested(cfg.VPA), fmt.Sprintf("discovery client creation failed: %v", err))
		return &Context{status: status}
	}

	crdAvail := kube.DetectCRDs(bundle.Discovery)

	kedaEnabled := kedaMode.Enabled(crdAvail.KEDA)
	vpaEnabled := vpaMode.Enabled(crdAvail.VPA)

	// Surface the real discovery outcome in each Entry.Reason. When discovery
	// itself failed (RBAC denial, network timeout), the wrapped error replaces
	// the misleading hard-coded "CRD ... not found" string so operators see the
	// actual cause. A nil error means the CRD is simply absent.
	applyCRDAvailability(kedaEntry, Requested(cfg.KEDA), crdAvail.KEDA, crdReason(crdAvail.KEDError))
	applyCRDAvailability(vpaEntry, Requested(cfg.VPA), crdAvail.VPA, crdReason(crdAvail.VPAError))

	if !kedaEnabled && !vpaEnabled {
		return &Context{status: status}
	}

	dynClient := bundle.Dynamic
	ns := bundle.Client.Namespace
	if dynClient == nil {
		setEnrichmentError(kedaEntry, kedaEnabled, "dynamic client creation failed: unavailable")
		setEnrichmentError(vpaEntry, vpaEnabled, "dynamic client creation failed: unavailable")
		return &Context{status: status}
	}

	// Mark enabled sources as available (per-HPA state will be set during enrichment)
	clearEnrichmentReason(kedaEntry, kedaEnabled)
	clearEnrichmentReason(vpaEntry, vpaEnabled)

	return &Context{
		dynClient:   dynClient,
		namespace:   ns,
		crdAvail:    crdAvail,
		kedaEnabled: kedaEnabled,
		vpaEnabled:  vpaEnabled,
		status:      status,
	}
}

// crdReason formats a DetectCRDs per-source error for display in an enrichment
// Status entry. A nil error means the CRD is simply absent, so we keep the
// historical short string; a non-nil error carries the real discovery failure
// (RBAC denial, network timeout, etc.) and is surfaced verbatim so operators
// see the actual cause instead of a misleading "not found".
func crdReason(err error) string {
	if err == nil || errors.Is(err, kube.ErrKEDACRDNotDetected) || errors.Is(err, kube.ErrVPACRDNotDetected) {
		return "CRD not found in API discovery"
	}
	return err.Error()
}

// setEnrichmentError marks the entry as errored with the given reason when enabled is true.
func setEnrichmentError(entry *Entry, enabled bool, reason string) {
	if !enabled {
		return
	}
	entry.State = StateError
	entry.Reason = reason
}

// applyCRDAvailability records the per-source CRD availability, setting a reason string when the CRD is missing.
func applyCRDAvailability(entry *Entry, requested, available bool, missingReason string) {
	if !requested {
		return
	}
	entry.State = StateUnavailable // will be updated per-HPA
	if !available {
		entry.Reason = missingReason
	}
}

// clearEnrichmentReason resets the entry's reason when enabled (marking it ready for per-HPA updates).
func clearEnrichmentReason(entry *Entry, enabled bool) {
	if !enabled {
		return
	}
	entry.State = StateUnavailable
	entry.Reason = ""
}
