package bundle

import "strings"

func writeBundleCapacityContextSection(b *Writer, data *Data) {
	b.Print("## Capacity Context\n\n")
	cc := data.StatusReport.Analysis.CapacityContext
	if cc == nil {
		b.Print("_No capacity context available._\n\n---\n\n")
		return
	}

	if len(cc.PendingPods) > 0 {
		b.Print("### Pending Pods\n\n")
		b.Print("| Name | Unschedulable | Reasons |\n")
		b.Print("|------|--------------|---------|\n")
		for _, pod := range cc.PendingPods {
			unsched := "No"
			if pod.Unschedulable {
				unsched = "Yes"
			}
			reasons := "-"
			if len(pod.Reasons) > 0 {
				reasons = mdEscape(strings.Join(pod.Reasons, ", "))
			}
			b.Printf("| %s | %s | %s |\n", mdEscape(pod.Name), unsched, reasons)
		}
		b.Println()
	}

	if len(cc.QuotaConstraints) > 0 {
		b.Print("### Quota Constraints\n\n")
		b.Print("| Name | Resource | Used | Hard | Message |\n")
		b.Print("|------|----------|------|------|---------|\n")
		for _, q := range cc.QuotaConstraints {
			b.Printf("| %s | %s | %s | %s | %s |\n",
				mdEscape(q.Name), mdEscape(q.Resource),
				mdEscape(q.Used), mdEscape(q.Hard), mdEscape(q.Message))
		}
		b.Println()
	}

	if len(cc.PDBInterference) > 0 {
		b.Print("### PDB Interference\n\n")
		b.Print("| Name | Min Available | Max Unavailable | Disruption |\n")
		b.Print("|------|--------------|----------------|------------|\n")
		for _, pdb := range cc.PDBInterference {
			b.Printf("| %s | %s | %s | %s |\n",
				mdEscape(pdb.Name), mdEscape(pdb.MinAvailable),
				mdEscape(pdb.MaxUnavailable), mdEscape(pdb.Disruption))
		}
		b.Println()
	}

	if len(cc.NodeHints) > 0 {
		b.Print("### Node Hints\n\n")
		for _, hint := range cc.NodeHints {
			b.Printf("- %s\n", mdEscape(hint))
		}
		b.Println()
	}

	b.Print("\n---\n\n")
}

func writeBundleScalePathSection(b *Writer, data *Data) {
	b.Print("## Scale Path\n\n")
	sp := data.StatusReport.Analysis.ScalePath
	if sp == nil {
		b.Print("_No scale path analysis available._\n\n---\n\n")
		return
	}

	if len(sp.Steps) > 0 {
		b.Print("| Step | Name | Summary |\n")
		b.Print("|------|------|--------|\n")
		for i, step := range sp.Steps {
			b.Printf("| %d | %s | %s |\n",
				i+1, mdEscape(step.Name), mdEscape(step.Summary))
		}
		b.Println()
	}

	if sp.BlockingPoint != "" {
		b.Printf("**Blocking Point:** %s\n\n", mdEscape(sp.BlockingPoint))
	}

	if len(sp.Evidence) > 0 {
		b.Print("**Evidence:**\n")
		for _, e := range sp.Evidence {
			b.Printf("- %s\n", mdEscape(e))
		}
		b.Println()
	}

	if len(sp.NextActions) > 0 {
		b.Print("**Next Actions:**\n")
		for _, a := range sp.NextActions {
			b.Printf("- %s\n", mdEscape(a))
		}
		b.Println()
	}

	b.Print("\n---\n\n")
}

func writeBundleBlockerSection(b *Writer, data *Data) {
	b.Print("## Blocker Analysis\n\n")
	br := data.StatusReport.Analysis.BlockerReport
	if br == nil {
		b.Print("_No blocker analysis available._\n\n---\n\n")
		return
	}

	b.Printf("**Summary:** %s\n\n", mdEscape(br.Summary))
	b.Printf("**HPA Wants Scale:** %v | **Desired:** %d | **Ready:** %d\n\n",
		br.HPAWantsScale, br.DesiredReplicas, br.ReadyReplicas)

	if len(br.Blockers) > 0 {
		b.Print("| Severity | Category | Message | Detail |\n")
		b.Print("|----------|----------|---------|--------|\n")
		for _, block := range br.Blockers {
			b.Printf("| %s | %s | %s | %s |\n",
				mdEscape(string(block.Severity)), mdEscape(block.Category),
				mdEscape(block.Message), mdEscape(block.Detail))
		}
		b.Println()
	}

	if br.Interpretation != "" {
		b.Printf("**Interpretation:** %s\n\n", mdEscape(br.Interpretation))
	}

	if len(br.NextCommands) > 0 {
		b.Print("**Investigation Commands:**\n")
		for _, cmd := range br.NextCommands {
			b.Printf("```bash\n%s\n```\n", cmd)
		}
		b.Println()
	}

	b.Print("\n---\n\n")
}
