package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mattsu2020/kubectl-hpa-status/cmd/internal/mcp"
)

// newMCPCommand builds the read-only Model Context Protocol (MCP) server
// subcommand communicating via JSON-RPC 2.0 over stdio.
func newMCPCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start a read-only MCP (Model Context Protocol) server over stdio",
		Long: "Start a read-only Model Context Protocol (MCP) JSON-RPC 2.0 server on stdin/stdout.\n" +
			"Exposes status, doctor, list, timeline, and simulate as read-only tools for AI agents.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			srv := buildMCPServer(opts)
			return srv.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}

func buildMCPServer(opts *options) *mcp.Server {
	return mcp.NewServer("kubectl-hpa-status", version,
		mcpStatusTool(opts),
		mcpDoctorTool(opts),
		mcpListTool(opts),
		mcpTimelineTool(opts),
		mcpSimulateTool(opts),
	)
}

func mcpStatusTool(opts *options) mcp.Tool {
	return mcp.Tool{
		Name:        "hpa_status",
		Description: "Inspect an HPA's health score, conditions, metrics, and diagnostic explanation (read-only).",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"HPA name (or namespace/name)"},"namespace":{"type":"string","description":"Kubernetes namespace"},"explain":{"type":"boolean","description":"Include detailed interpretation and recommended actions (default true)"}},"required":["name"]}`),
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
				Explain   *bool  `json:"explain"`
			}
			if err := decodeMCPArgs(args, &p); err != nil {
				return "", err
			}
			if strings.TrimSpace(p.Name) == "" {
				return "", fmt.Errorf("name is required")
			}
			local := cloneMCPOptions(opts, p.Namespace)
			local.Output = "json"
			if p.Explain == nil || *p.Explain {
				local.Explain = true
				local.Interpret = true
			}
			var buf bytes.Buffer
			req := snapshotStatusRequest(&local, []string{p.Name})
			if err := executeStatusRequest(ctx, &buf, req); err != nil && !mcpAllowWarningJSON(err, buf.Bytes()) {
				return "", err
			}
			return buf.String(), nil
		},
	}
}

func mcpDoctorTool(opts *options) mcp.Tool {
	return mcp.Tool{
		Name:        "hpa_doctor",
		Description: "Run full HPA diagnostics across metrics, workload readiness, pods, events, and KEDA/VPA (read-only).",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"HPA name (or namespace/name)"},"namespace":{"type":"string","description":"Kubernetes namespace"}},"required":["name"]}`),
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			}
			if err := decodeMCPArgs(args, &p); err != nil {
				return "", err
			}
			if strings.TrimSpace(p.Name) == "" {
				return "", fmt.Errorf("name is required")
			}
			local := cloneMCPOptions(opts, p.Namespace)
			local.Output = "json"
			var buf bytes.Buffer
			if err := runDoctor(ctx, &buf, &local, []string{p.Name}); err != nil && !mcpAllowWarningJSON(err, buf.Bytes()) {
				return "", err
			}
			return buf.String(), nil
		},
	}
}

func mcpListTool(opts *options) mcp.Tool {
	return mcp.Tool{
		Name:        "hpa_list",
		Description: "List HPAs and their health scores across a namespace or all namespaces (read-only).",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"namespace":{"type":"string","description":"Kubernetes namespace"},"allNamespaces":{"type":"boolean","description":"List HPAs across all namespaces"},"problemOnly":{"type":"boolean","description":"Return only HPAs with detected issues"},"selector":{"type":"string","description":"Label selector (e.g. app=web)"}}}`),
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Namespace     string `json:"namespace"`
				AllNamespaces bool   `json:"allNamespaces"`
				ProblemOnly   bool   `json:"problemOnly"`
				Selector      string `json:"selector"`
			}
			if err := decodeMCPArgs(args, &p); err != nil {
				return "", err
			}
			local := cloneMCPOptions(opts, p.Namespace)
			local.Output = "json"
			local.AllNamespaces = p.AllNamespaces
			local.Problem = p.ProblemOnly
			local.Selector = p.Selector
			var buf bytes.Buffer
			req := snapshotListRequest(&local)
			if err := executeListRequest(ctx, &buf, req); err != nil {
				return "", err
			}
			return buf.String(), nil
		},
	}
}

func mcpTimelineTool(opts *options) mcp.Tool {
	return mcp.Tool{
		Name:        "hpa_timeline",
		Description: "Build a retrospective scaling timeline from Kubernetes events or a recorded JSONL file (read-only).",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"HPA name (or namespace/name)"},"namespace":{"type":"string","description":"Kubernetes namespace"},"since":{"type":"string","description":"Lookback duration (e.g. 30m, 1h; default 1h)"},"replay":{"type":"boolean","description":"Include control-cycle replay analysis"},"fromRecord":{"type":"string","description":"Optional recorded JSONL snapshot path"}},"required":["name"]}`),
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Name       string `json:"name"`
				Namespace  string `json:"namespace"`
				Since      string `json:"since"`
				Replay     bool   `json:"replay"`
				FromRecord string `json:"fromRecord"`
			}
			if err := decodeMCPArgs(args, &p); err != nil {
				return "", err
			}
			if strings.TrimSpace(p.Name) == "" {
				return "", fmt.Errorf("name is required")
			}
			local := cloneMCPOptions(opts, p.Namespace)
			local.Output = "json"
			var buf bytes.Buffer
			if p.FromRecord != "" {
				return "", fmt.Errorf("fromRecord is disabled in MCP mode for security; query live cluster events with since instead")
			}
			since := time.Hour
			if strings.TrimSpace(p.Since) != "" {
				parsed, err := time.ParseDuration(p.Since)
				if err != nil || parsed <= 0 {
					return "", fmt.Errorf("invalid since duration %q: must be a positive duration", p.Since)
				}
				since = parsed
			}
			if err := runRetrospectiveTimeline(ctx, &buf, &local, p.Name, since, p.Replay); err != nil {
				return "", err
			}
			return buf.String(), nil
		},
	}
}

func mcpSimulateTool(opts *options) mcp.Tool {
	return mcp.Tool{
		Name:        "hpa_simulate",
		Description: "Run a read-only what-if simulation for an HPA with metric or target overrides.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"HPA name (or namespace/name)"},"namespace":{"type":"string","description":"Kubernetes namespace"},"setMetric":{"type":"array","items":{"type":"string"},"description":"Metric overrides (e.g. cpu=90%)"},"setTarget":{"type":"array","items":{"type":"string"},"description":"Target overrides (e.g. cpu=60)"},"tolerance":{"type":"string","description":"HPA tolerance override (e.g. 0.1)"},"durationSeconds":{"type":"integer","description":"Time-series projection duration in seconds"}},"required":["name"]}`),
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Name            string   `json:"name"`
				Namespace       string   `json:"namespace"`
				SetMetric       []string `json:"setMetric"`
				SetTarget       []string `json:"setTarget"`
				Tolerance       string   `json:"tolerance"`
				DurationSeconds int32    `json:"durationSeconds"`
			}
			if err := decodeMCPArgs(args, &p); err != nil {
				return "", err
			}
			if strings.TrimSpace(p.Name) == "" {
				return "", fmt.Errorf("name is required")
			}
			local := cloneMCPOptions(opts, p.Namespace)
			local.Output = "json"
			var buf bytes.Buffer
			if err := runSimulate(ctx, &buf, &local, p.Name, p.SetMetric, p.SetTarget, p.Tolerance, false, p.DurationSeconds); err != nil {
				return "", err
			}
			return buf.String(), nil
		},
	}
}

func mcpAllowWarningJSON(err error, payload []byte) bool {
	return exitCodeForError(err) == ExitWarning && json.Valid(payload)
}

func cloneMCPOptions(opts *options, namespace string) options {
	var local options
	if opts != nil {
		local = opts.Copy()
	} else {
		local = defaultRootOptions()
	}
	if strings.TrimSpace(namespace) != "" {
		local.Namespace = namespace
	}
	return local
}

func decodeMCPArgs(raw json.RawMessage, dst any) error {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	return json.Unmarshal(raw, dst)
}
