package bundle

func writeBundleKEDASection(b *Writer, data *Data) {
	ki := data.StatusReport.Analysis.Controllers.KEDAInfo
	if ki == nil {
		return // Omit entire section when KEDA is not detected.
	}

	b.Print("## KEDA Status\n\n")
	b.Printf("**ScaledObject:** %s\n\n", mdEscape(ki.ScaledObjectName))

	if ki.PollingInterval != nil {
		b.Printf("**Polling Interval:** %ds\n", *ki.PollingInterval)
	}
	if ki.CooldownPeriod != nil {
		b.Printf("**Cooldown Period:** %ds\n", *ki.CooldownPeriod)
	}
	if ki.MinReplicaCount != nil {
		b.Printf("**Min Replica Count:** %d\n", *ki.MinReplicaCount)
	}
	if ki.MaxReplicaCount != nil {
		b.Printf("**Max Replica Count:** %d\n", *ki.MaxReplicaCount)
	}

	if len(ki.Triggers) > 0 {
		b.Print("\n### Triggers\n\n")
		b.Print("| Type | Name | Status | Threshold | Current | Auth Ref |\n")
		b.Print("|------|------|--------|-----------|---------|----------|\n")
		for _, t := range ki.Triggers {
			authRef := "-"
			if t.AuthRef != "" {
				authRef = mdEscape(t.AuthRef)
			}
			b.Printf("| %s | %s | %s | %s | %s | %s |\n",
				mdEscape(t.Type), mdEscape(t.Name), mdEscape(t.Status),
				mdEscape(t.Threshold), mdEscape(t.CurrentValue), authRef)
		}
		b.Println()
	}

	if ki.Fallback != nil {
		b.Printf("**Fallback:** failureThreshold=%d, replicas=%d\n\n",
			ki.Fallback.FailureThreshold, ki.Fallback.Replicas)
	}

	if len(ki.Lines) > 0 {
		b.Print("**Notes:**\n")
		for _, line := range ki.Lines {
			b.Printf("- %s\n", mdEscape(line))
		}
		b.Println()
	}

	b.Print("\n---\n\n")
}

func writeBundleQuotasSection(b *Writer, data *Data) {
	b.Print("## ResourceQuotas\n\n")
	if len(data.ResourceQuotas) == 0 {
		b.Print("_No ResourceQuotas found in namespace._\n\n---\n\n")
		return
	}

	b.Print("| Name | Resource | Used | Hard | Ratio |\n")
	b.Print("|------|----------|------|------|-------|\n")
	for _, q := range data.ResourceQuotas {
		b.Printf("| %s | %s | %s | %s | %.0f%% |\n",
			mdEscape(q.Name), mdEscape(q.Resource),
			mdEscape(q.Used), mdEscape(q.Hard), q.Ratio*100)
	}
	b.Print("\n---\n\n")
}

func writeBundleLimitRangesSection(b *Writer, data *Data) {
	b.Print("## LimitRanges\n\n")
	if len(data.LimitRanges) == 0 {
		b.Print("_No LimitRanges found in namespace._\n\n---\n\n")
		return
	}

	b.Print("| Name | Type | Resource | Min | Max |\n")
	b.Print("|------|------|----------|-----|-----|\n")
	for _, lr := range data.LimitRanges {
		minVal := "-"
		if lr.Min != "" {
			minVal = mdEscape(lr.Min)
		}
		maxVal := "-"
		if lr.Max != "" {
			maxVal = mdEscape(lr.Max)
		}
		b.Printf("| %s | %s | %s | %s | %s |\n",
			mdEscape(lr.Name), mdEscape(lr.Type),
			mdEscape(lr.Resource), minVal, maxVal)
	}
	b.Print("\n---\n\n")
}

func writeBundlePDBsSection(b *Writer, data *Data) {
	b.Print("## PodDisruptionBudgets\n\n")
	if len(data.PDBs) == 0 {
		b.Print("_No PodDisruptionBudgets found in namespace._\n\n---\n\n")
		return
	}

	b.Print("| Name | Min Available | Max Unavailable |\n")
	b.Print("|------|--------------|----------------|\n")
	for _, pdb := range data.PDBs {
		minAvail := "-"
		if pdb.MinAvailable != "" {
			minAvail = mdEscape(pdb.MinAvailable)
		}
		maxUnavail := "-"
		if pdb.MaxUnavailable != "" {
			maxUnavail = mdEscape(pdb.MaxUnavailable)
		}
		b.Printf("| %s | %s | %s |\n",
			mdEscape(pdb.Name), minAvail, maxUnavail)
	}
	b.Print("\n---\n\n")
}

func writeBundleNodeCapacitySection(b *Writer, data *Data) {
	b.Print("## Node Capacity\n\n")
	if data.NodeCapacity == nil {
		b.Print("_Node capacity information unavailable._\n\n---\n\n")
		return
	}

	nc := data.NodeCapacity
	b.Print("| Metric | Value |\n")
	b.Print("|--------|-------|\n")
	b.Printf("| Total Nodes | %d |\n", nc.TotalNodes)
	b.Printf("| Allocatable CPU | %s |\n", nc.AllocCPU.String())
	b.Printf("| Allocatable Memory | %s |\n", nc.AllocMemory.String())
	b.Printf("| Tainted Nodes | %d |\n", nc.TaintedNodes)
	b.Print("\n---\n\n")
}
