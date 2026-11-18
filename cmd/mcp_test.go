package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

func TestMCPCommandExposesReadOnlyToolsAndExecutesStatusListSimulate(t *testing.T) {
	web := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithMinMax(2, 10),
		testutil.WithResourceMetric("cpu", 80, 120),
		testutil.WithConditions(autoscalingv2.HorizontalPodAutoscalerCondition{
			Type: autoscalingv2.ScalingLimited, Status: corev1.ConditionTrue, Reason: "TooManyReplicas",
		}),
	)
	client := testutil.NewFakeClient(web)

	root := NewRootCommandWithDeps(AppDeps{Kubernetes: client})

	requests := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"hpa_status","arguments":{"name":"web","namespace":"default"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"hpa_list","arguments":{"namespace":"default"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"hpa_simulate","arguments":{"name":"web","setMetric":["cpu=100%"]}}}`,
	}, "\n") + "\n"

	var stdout bytes.Buffer
	root.SetIn(strings.NewReader(requests))
	root.SetOut(&stdout)
	root.SetArgs([]string{"mcp"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("mcp command failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 JSON-RPC responses, got %d:\n%s", len(lines), stdout.String())
	}

	var listResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &listResp); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	if len(listResp.Result.Tools) != 5 {
		t.Fatalf("expected 5 read-only MCP tools, got %d", len(listResp.Result.Tools))
	}

	for _, idx := range []int{2, 3, 4} {
		var callResp struct {
			Result struct {
				IsError bool `json:"isError"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(lines[idx]), &callResp); err != nil {
			t.Fatalf("decode tool response %d: %v", idx, err)
		}
		if idx == 2 && (len(callResp.Result.Content) == 0 || !strings.Contains(callResp.Result.Content[0].Text, "LIMITED")) {
			t.Fatalf("expected diagnosis for limited HPA, got %+v", callResp.Result)
		}
		if callResp.Result.IsError || len(callResp.Result.Content) == 0 || !strings.Contains(callResp.Result.Content[0].Text, "web") {
			t.Fatalf("unexpected tool response %d: %+v", idx, callResp.Result)
		}
	}
}

func TestMCPDoctorWarningAndTimelineFromRecordBlocked(t *testing.T) {
	web := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithMinMax(2, 10),
		testutil.WithResourceMetric("cpu", 80, 120),
		testutil.WithConditions(autoscalingv2.HorizontalPodAutoscalerCondition{
			Type: autoscalingv2.ScalingLimited, Status: corev1.ConditionTrue, Reason: "TooManyReplicas",
		}),
	)
	client := testutil.NewFakeClient(web)
	root := NewRootCommandWithDeps(AppDeps{Kubernetes: client})

	requests := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"hpa_doctor","arguments":{"name":"web","namespace":"default"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"hpa_timeline","arguments":{"name":"web","fromRecord":"/etc/passwd"}}}`,
	}, "\n") + "\n"

	var stdout bytes.Buffer
	root.SetIn(strings.NewReader(requests))
	root.SetOut(&stdout)
	root.SetArgs([]string{"mcp"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("mcp command failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d:\n%s", len(lines), stdout.String())
	}

	var docResp struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &docResp); err != nil {
		t.Fatalf("decode doctor response: %v", err)
	}
	if docResp.Result.IsError || len(docResp.Result.Content) == 0 || !strings.Contains(docResp.Result.Content[0].Text, "LIMITED") {
		t.Fatalf("expected valid JSON diagnosis for limited HPA, got %+v", docResp.Result)
	}

	var tlResp struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &tlResp); err != nil {
		t.Fatalf("decode timeline response: %v", err)
	}
	if !tlResp.Result.IsError || len(tlResp.Result.Content) == 0 || !strings.Contains(tlResp.Result.Content[0].Text, "disabled") {
		t.Fatalf("expected fromRecord to be rejected in MCP mode, got %+v", tlResp.Result)
	}
}
