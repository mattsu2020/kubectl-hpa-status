// Package mcp implements a minimal, zero-dependency Model Context Protocol
// (MCP) JSON-RPC 2.0 server over stdio for read-only HPA diagnostics.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const defaultProtocolVersion = "2024-11-05"

// ToolHandler executes an MCP tool call and returns a UTF-8 text payload
// (typically structured JSON) or an error.
type ToolHandler func(ctx context.Context, args json.RawMessage) (string, error)

// Tool describes one read-only MCP tool exposed by the server.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Handler     ToolHandler     `json:"-"`
}

// Server dispatches MCP JSON-RPC 2.0 requests over stdio.
type Server struct {
	name    string
	version string
	tools   map[string]Tool
	order   []string
}

// NewServer constructs an MCP server with the given server identity and tools.
func NewServer(name, version string, tools ...Tool) *Server {
	s := &Server{
		name:    name,
		version: version,
		tools:   make(map[string]Tool, len(tools)),
		order:   make([]string, 0, len(tools)),
	}
	for _, t := range tools {
		if t.Name == "" {
			continue
		}
		if _, exists := s.tools[t.Name]; !exists {
			s.order = append(s.order, t.Name)
		}
		s.tools[t.Name] = t
	}
	return s
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callToolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// Serve reads newline-delimited JSON-RPC 2.0 requests from in and writes
// responses to out until EOF or ctx cancellation.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		respBytes, shouldReply := s.HandleMessage(ctx, []byte(line))
		if !shouldReply {
			continue
		}
		if _, err := out.Write(append(respBytes, '\n')); err != nil {
			return fmt.Errorf("write MCP response: %w", err)
		}
	}
	return scanner.Err()
}

// HandleMessage processes one JSON-RPC 2.0 message and returns the encoded
// response and whether a response should be emitted (false for notifications).
func (s *Server) HandleMessage(ctx context.Context, raw []byte) ([]byte, bool) {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return mustMarshal(rpcResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &rpcError{Code: -32700, Message: fmt.Sprintf("parse error: %v", err)},
		}), true
	}

	isNotification := len(req.ID) == 0 || string(req.ID) == "null"
	if req.Method == "notifications/initialized" || (isNotification && strings.HasPrefix(req.Method, "notifications/")) {
		return nil, false
	}

	var result any
	var rpcErr *rpcError

	switch req.Method {
	case "initialize":
		result = s.handleInitialize(req.Params)
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result = s.handleToolsList()
	case "tools/call":
		result, rpcErr = s.handleToolCall(ctx, req.Params)
	default:
		rpcErr = &rpcError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)}
	}

	if isNotification {
		return nil, false
	}
	id := req.ID
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return mustMarshal(rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
		Error:   rpcErr,
	}), true
}

func (s *Server) handleInitialize(params json.RawMessage) map[string]any {
	var p initializeParams
	if len(params) > 0 {
		_ = json.Unmarshal(params, &p)
	}
	proto := p.ProtocolVersion
	if proto == "" {
		proto = defaultProtocolVersion
	}
	return map[string]any{
		"protocolVersion": proto,
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    s.name,
			"version": s.version,
		},
	}
}

func (s *Server) handleToolsList() map[string]any {
	listed := make([]Tool, 0, len(s.order))
	for _, name := range s.order {
		listed = append(listed, s.tools[name])
	}
	return map[string]any{"tools": listed}
}

func (s *Server) handleToolCall(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var p callToolParams
	if err := json.Unmarshal(params, &p); err != nil || p.Name == "" {
		return nil, &rpcError{Code: -32602, Message: "invalid params: tool name is required"}
	}
	tool, ok := s.tools[p.Name]
	if !ok || tool.Handler == nil {
		return nil, &rpcError{Code: -32602, Message: fmt.Sprintf("unknown tool %q", p.Name)}
	}
	text, err := tool.Handler(ctx, p.Arguments)
	if err != nil {
		return callToolResult{
			Content: []toolContent{{Type: "text", Text: err.Error()}},
			IsError: true,
		}, nil
	}
	return callToolResult{
		Content: []toolContent{{Type: "text", Text: text}},
	}, nil
}

func mustMarshal(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal marshal error"}}`)
	}
	return data
}
