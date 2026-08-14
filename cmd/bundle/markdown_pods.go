package bundle

import "strings"

func writeBundlePodStatusTable(b *Writer, data *Data) {
	b.Print("## Pod Status\n\n")
	if len(data.PodInfos) == 0 {
		b.Print("_No pod information available._\n\n---\n\n")
		return
	}

	b.Print("| Name | Phase | Ready | Unschedulable | Reasons | Node |\n")
	b.Print("|------|-------|-------|---------------|---------|------|\n")
	for _, pod := range data.PodInfos {
		ready := "✓"
		if !pod.Ready {
			ready = "✗"
		}
		unschedulable := "-"
		if pod.Unschedulable {
			unschedulable = "Yes"
		}
		reasons := "-"
		if len(pod.Reasons) > 0 {
			reasons = mdEscape(strings.Join(pod.Reasons, ", "))
		}
		node := pod.NodeName
		if node == "" {
			node = "-"
		}
		b.Printf("| %s | %s | %s | %s | %s | %s |\n",
			mdEscape(pod.Name), mdEscape(pod.Phase), ready, unschedulable, reasons, mdEscape(node))
	}
	b.Print("\n---\n\n")
}

func writeBundleContainerStatusTable(b *Writer, data *Data) {
	b.Print("## Container Status\n\n")
	if len(data.ContainerStatuses) == 0 {
		b.Print("_No container status data available._\n\n---\n\n")
		return
	}

	b.Print("| Pod | Container | Waiting | Waiting Reason | Restarts |\n")
	b.Print("|-----|-----------|---------|----------------|----------|\n")
	for _, cs := range data.ContainerStatuses {
		waiting := "No"
		if cs.Waiting {
			waiting = "Yes"
		}
		waitReason := "-"
		if cs.WaitingReason != "" {
			waitReason = mdEscape(cs.WaitingReason)
		}
		b.Printf("| %s | %s | %s | %s | %d |\n",
			mdEscape(cs.Pod), mdEscape(cs.Container), waiting, waitReason, cs.RestartCount)
	}
	b.Print("\n---\n\n")
}

func writeBundleResourceRequests(b *Writer, data *Data) {
	b.Print("## Resource Requests/Limits\n\n")
	rc := data.StatusReport.Analysis.ResourceCheck
	if rc == nil || len(rc.Warnings) == 0 {
		b.Print("_No resource consistency issues detected._\n\n---\n\n")
		return
	}

	b.Print("| Container | Resource | Category | Severity | Details |\n")
	b.Print("|-----------|----------|----------|----------|--------|\n")
	for _, warn := range rc.Warnings {
		b.Printf("| %s | %s | %s | %s | %s |\n",
			mdEscape(warn.Container), mdEscape(warn.Resource),
			mdEscape(warn.Category), mdEscape(warn.Severity), mdEscape(warn.Details))
	}
	b.Print("\n---\n\n")
}
