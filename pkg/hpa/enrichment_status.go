package hpa

// Clone returns an independent enrichment status. The status attached to an
// enrichment context can be shared by concurrent report builders, so report
// assembly must clone it before changing per-HPA state.
func (s EnrichmentStatus) Clone() EnrichmentStatus {
	return EnrichmentStatus{
		KEDA: cloneEnrichmentStatusEntry(s.KEDA),
		VPA:  cloneEnrichmentStatusEntry(s.VPA),
	}
}

func cloneEnrichmentStatusEntry(entry *EnrichmentStatusEntry) *EnrichmentStatusEntry {
	if entry == nil {
		return nil
	}
	cloned := *entry
	return &cloned
}

// KEDAEntry returns the KEDA status, or a disabled default.
func (s *EnrichmentStatus) KEDAEntry() EnrichmentStatusEntry {
	if s != nil && s.KEDA != nil {
		return *s.KEDA
	}
	return EnrichmentStatusEntry{Source: EnrichmentSourceKEDA, State: EnrichmentStateDisabled}
}

// VPAEntry returns the VPA status, or a disabled default.
func (s *EnrichmentStatus) VPAEntry() EnrichmentStatusEntry {
	if s != nil && s.VPA != nil {
		return *s.VPA
	}
	return EnrichmentStatusEntry{Source: EnrichmentSourceVPA, State: EnrichmentStateDisabled}
}
