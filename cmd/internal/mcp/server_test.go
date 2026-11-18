package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestServerLifecycleAndToolDispatch(t *testing.T) {
	srv := NewServer("kubectl-hpa-status", "v5.0.0",
		Tool{
			Name:        "hpa_status",
			Description: "Inspect HPA status",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
			Handler: func(_ context.Context, args json.RawMessage) (string, error) {
				var p struct {
					Name string `json:"name"`
				}
				if err := json.Unmarshal(args, &p); err != nil {
					return "", err
				}
				if p.Name == "broken" {
					return "", errors.New("hpa not found")
				}
				return `{"name":"` + p.Name + `"}`, nil
			},
		},
	)

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"hpa_status","arguments":{"name":"web"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"hpa_status","arguments":{"name":"broken"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"unknown_tool","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"unknown/method"}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	if err := srv.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 6 {
		t.Fatalf("expected 6 responses (notification produces none), got %d:\n%s", len(lines), out.String())
	}

	var initResp struct {
		ID     int `json:"id"`
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &initResp); err != nil {
		t.Fatalf("decode initialize response: %v", err)
	}
	if initResp.Result.ProtocolVersion != "2024-11-05" || initResp.Result.ServerInfo.Name != "kubectl-hpa-status" {
		t.Fatalf("unexpected initialize result: %+v", initResp.Result)
	}

	var listResp struct {
		Result struct {
			Tools []Tool `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &listResp); err != nil {
		t.Fatalf("decode tools/list response: %v", err)
	}
	if len(listResp.Result.Tools) != 1 || listResp.Result.Tools[0].Name != "hpa_status" {
		t.Fatalf("unexpected tools/list result: %+v", listResp.Result.Tools)
	}

	var callOK struct {
		Result callToolResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &callOK); err != nil {
		t.Fatalf("decode tools/call ok: %v", err)
	}
	if callOK.Result.IsError || len(callOK.Result.Content) != 1 || callOK.Result.Content[0].Text != `{"name":"web"}` {
		t.Fatalf("unexpected call ok result: %+v", callOK.Result)
	}

	var callErr struct {
		Result callToolResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[3]), &callErr); err != nil {
		t.Fatalf("decode tools/call tool error: %v", err)
	}
	if !callErr.Result.IsError || !strings.Contains(callErr.Result.Content[0].Text, "hpa not found") {
		t.Fatalf("expected isError=true for handler error, got %+v", callErr.Result)
	}

	var unknownToolResp rpcResponse
	if err := json.Unmarshal([]byte(lines[4]), &unknownToolResp); err != nil {
		t.Fatalf("decode unknown tool resp: %v", err)
	}
	if unknownToolResp.Error == nil || unknownToolResp.Error.Code != -32602 {
		t.Fatalf("expected -32602 error, got %+v", unknownToolResp.Error)
	}

	var unknownMethodResp rpcResponse
	if err := json.Unmarshal([]byte(lines[5]), &unknownMethodResp); err != nil {
		t.Fatalf("decode unknown method resp: %v", err)
	}
	if unknownMethodResp.Error == nil || unknownMethodResp.Error.Code != -32601 {
		t.Fatalf("expected -32601 error, got %+v", unknownMethodResp.Error)
	}
}

func TestHandleMessageParseError(t *testing.T) {
	srv := NewServer("kubectl-hpa-status", "v5.0.0")
	respBytes, ok := srv.HandleMessage(context.Background(), []byte(`{bad json`))
	if !ok {
		t.Fatal("expected parse error response")
	}
	var resp rpcResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("decode parse error response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != -32700 {
		t.Fatalf("expected -32700 parse error, got %+v", resp.Error)
	}
}

func TestHandleMessageInvalidRequest(t *testing.T) {
	srv := NewServer("kubectl-hpa-status", "v5.0.0")
	respBytes, ok := srv.HandleMessage(context.Background(), []byte(`{"id":1,"method":""}`))
	if !ok {
		t.Fatal("expected invalid request response")
	}
	var resp rpcResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("decode invalid request response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != -32600 {
		t.Fatalf("expected -32600 invalid request, got %+v", resp.Error)
	}
}
