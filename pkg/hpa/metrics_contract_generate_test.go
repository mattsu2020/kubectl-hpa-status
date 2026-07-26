package hpa

import (
	"fmt"
	"strings"
	"testing"
)

func TestGenerateContractCommands(t *testing.T) {
	tests := []struct {
		name     string
		report   *MetricContractReport
		wantLen  int
		contains []string
	}{
		{
			name:    "nil report returns nil",
			report:  nil,
			wantLen: 0,
		},
		{
			name: "resource metric produces pods endpoint",
			report: &MetricContractReport{
				Namespace:     "prod",
				Name:          "web",
				OverallStatus: "healthy",
				Checks: []MetricContractCheck{
					{MetricType: "Resource", MetricName: "cpu", Status: "ok"},
				},
			},
			wantLen:  1,
			contains: []string{"metrics.k8s.io", "prod"},
		},
		{
			name: "external metric produces external endpoint",
			report: &MetricContractReport{
				Namespace:     "prod",
				Name:          "web",
				OverallStatus: "healthy",
				Checks: []MetricContractCheck{
					{MetricType: "External", MetricName: "http_requests", Selector: "app=web", Status: "ok"},
				},
			},
			wantLen:  1,
			contains: []string{"external.metrics.k8s.io", "http_requests", "labelSelector"},
		},
		{
			name: "multiple metrics produce multiple commands",
			report: &MetricContractReport{
				Namespace:     "prod",
				Name:          "web",
				OverallStatus: "degraded",
				Checks: []MetricContractCheck{
					{MetricType: "Resource", MetricName: "cpu", Status: "ok"},
					{MetricType: "External", MetricName: "http_requests", Status: "missing-api"},
				},
			},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commands := GenerateContractCommands(tt.report)
			if len(commands) != tt.wantLen {
				t.Fatalf("expected %d commands, got %d", tt.wantLen, len(commands))
			}
			for _, want := range tt.contains {
				found := false
				for _, cmd := range commands {
					if strings.Contains(cmd, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected commands to contain %q, got %v", want, commands)
				}
			}
		})
	}
}

func TestGenerateMetricCommand_CustomMetricResourcePaths(t *testing.T) {
	tests := []struct {
		name  string
		check MetricContractCheck
		want  string
	}{
		{
			name: "Pods metric",
			check: MetricContractCheck{
				MetricType:   MetricTypePods,
				MetricName:   "http_requests",
				Resource:     "pods",
				ResourceName: "*",
				Selector:     "app=web",
			},
			want: `kubectl get --raw "/apis/custom.metrics.k8s.io/v1beta1/namespaces/prod/pods/*/http_requests?metricLabelSelector=app%3Dweb"`,
		},
		{
			name: "Pods metric combines target and metric selectors",
			check: MetricContractCheck{
				MetricType:     MetricTypePods,
				MetricName:     "http_requests",
				Resource:       "pods",
				ResourceName:   "*",
				Selector:       "series=frontend",
				TargetSelector: "app=web",
			},
			want: `kubectl get --raw "/apis/custom.metrics.k8s.io/v1beta1/namespaces/prod/pods/*/http_requests?labelSelector=app%3Dweb&metricLabelSelector=series%3Dfrontend"`,
		},
		{
			name: "Object metric",
			check: MetricContractCheck{
				MetricType:   MetricTypeObject,
				MetricName:   "queue_depth",
				Resource:     "deployments.apps",
				ResourceName: "web",
			},
			want: `kubectl get --raw "/apis/custom.metrics.k8s.io/v1beta1/namespaces/prod/deployments.apps/web/queue_depth"`,
		},
		{
			name: "Object metric uses discovered custom metrics version",
			check: MetricContractCheck{
				MetricType:   MetricTypeObject,
				MetricName:   "queue_depth",
				Resource:     "deployments.apps",
				ResourceName: "web",
				APIService:   "custom.metrics.k8s.io/v1beta2",
			},
			want: `kubectl get --raw "/apis/custom.metrics.k8s.io/v1beta2/namespaces/prod/deployments.apps/web/queue_depth"`,
		},
		{
			name: "cluster-scoped Object metric",
			check: MetricContractCheck{
				MetricType:         MetricTypeObject,
				MetricName:         "node_load",
				Resource:           "nodes",
				ResourceName:       "worker-1",
				ResourceNamespaced: boolPtr(false),
			},
			want: `kubectl get --raw "/apis/custom.metrics.k8s.io/v1beta1/nodes/worker-1/node_load"`,
		},
		{
			name: "Namespace Object metric uses HPA namespace",
			check: MetricContractCheck{
				MetricType:         MetricTypeObject,
				MetricName:         "namespace_qps",
				Resource:           "namespaces",
				ResourceName:       "ignored-object-name",
				ResourceNamespaced: boolPtr(false),
			},
			want: `kubectl get --raw "/apis/custom.metrics.k8s.io/v1beta1/namespaces/prod/metrics/namespace_qps"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generateMetricCommand("prod", tt.check); got != tt.want {
				t.Fatalf("generateMetricCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateMetricCommand_ResourceMetricUsesTargetSelector(t *testing.T) {
	t.Parallel()

	check := MetricContractCheck{
		MetricType:     MetricTypeResource,
		MetricName:     "cpu",
		TargetSelector: "app=web,tier in (api,worker)",
	}
	want := `kubectl get --raw "/apis/metrics.k8s.io/v1beta1/namespaces/prod/pods?labelSelector=app%3Dweb%2Ctier+in+%28api%2Cworker%29"`
	if got := generateMetricCommand("prod", check); got != want {
		t.Fatalf("generateMetricCommand() = %q, want %q", got, want)
	}
}

func TestGenerateMetricCommand_SkipsUnresolvedTargetSelector(t *testing.T) {
	t.Parallel()

	check := MetricContractCheck{
		MetricType:                    MetricTypePods,
		MetricName:                    "requests",
		Resource:                      "pods",
		ResourceName:                  "*",
		TargetSelectorResolutionError: "scale target is unavailable",
	}
	if got := generateMetricCommand("prod", check); got != "" {
		t.Fatalf("generateMetricCommand() = %q, want no potentially broad query", got)
	}
}

func TestGenerateContractYAML(t *testing.T) {
	report := &MetricContractReport{
		Namespace:     "prod",
		Name:          "web-hpa",
		Target:        "Deployment/web",
		OverallStatus: "healthy",
		Checks: []MetricContractCheck{
			{MetricType: "Resource", MetricName: "cpu", APIService: "metrics.k8s.io/v1beta1", Status: "ok"},
			{MetricType: "External", MetricName: "http_requests", APIService: "external.metrics.k8s.io/v1beta1", Status: "ok"},
		},
	}

	yamlBytes, err := GenerateContractYAML(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := string(yamlBytes)
	for _, want := range []string{"MetricContractTest", "cpu", "http_requests", "prod", "web-hpa"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected YAML to contain %q", want)
		}
	}
}

func TestGenerateContractYAML_NilReport(t *testing.T) {
	_, err := GenerateContractYAML(nil)
	if err == nil {
		t.Fatal("expected error for nil report")
	}
}

func TestGenerateContractJUnit(t *testing.T) {
	tests := []struct {
		name         string
		report       *MetricContractReport
		wantTests    int
		wantFailures int
	}{
		{
			name: "all healthy",
			report: &MetricContractReport{
				Namespace:     "prod",
				Name:          "web",
				OverallStatus: "healthy",
				Checks: []MetricContractCheck{
					{MetricType: "Resource", MetricName: "cpu", Status: "ok"},
				},
			},
			wantTests:    1,
			wantFailures: 0,
		},
		{
			name: "broken metric produces failure",
			report: &MetricContractReport{
				Namespace:     "prod",
				Name:          "web",
				OverallStatus: "broken",
				Checks: []MetricContractCheck{
					{MetricType: "Resource", MetricName: "cpu", Status: "ok"},
					{MetricType: "External", MetricName: "rqps", Status: "missing-api",
						APIService:  "external.metrics.k8s.io/v1beta1",
						Detail:      "API service not available",
						Remediation: "Install Prometheus Adapter"},
				},
			},
			wantTests:    2,
			wantFailures: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			xmlBytes, err := GenerateContractJUnit(tt.report)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			output := string(xmlBytes)
			if !strings.Contains(output, "testsuite") {
				t.Error("expected XML to contain testsuite element")
			}
			if !strings.Contains(output, fmt.Sprintf(`tests="%d"`, tt.wantTests)) {
				t.Errorf("expected tests=%d in XML, got:\n%s", tt.wantTests, output)
			}
			if !strings.Contains(output, fmt.Sprintf(`failures="%d"`, tt.wantFailures)) {
				t.Errorf("expected failures=%d in XML, got:\n%s", tt.wantFailures, output)
			}
		})
	}
}

func TestGenerateContractJUnit_NilReport(t *testing.T) {
	_, err := GenerateContractJUnit(nil)
	if err == nil {
		t.Fatal("expected error for nil report")
	}
}

func TestGenerateContractMarkdown(t *testing.T) {
	report := &MetricContractReport{
		Namespace:     "prod",
		Name:          "web",
		Target:        "Deployment/web",
		OverallStatus: "healthy",
		Summary:       "All metrics available",
		Checks: []MetricContractCheck{
			{MetricType: "Resource", MetricName: "cpu", APIService: "metrics.k8s.io/v1beta1", Status: "ok"},
		},
		Remediation: []string{"Install metrics-server"},
	}

	mdBytes, err := GenerateContractMarkdown(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := string(mdBytes)
	for _, want := range []string{"Metric Contract", "Overall Status", "cpu", "metrics.k8s.io", "Remediation"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected Markdown to contain %q", want)
		}
	}
}

func TestGenerateContractMarkdown_NilReport(t *testing.T) {
	_, err := GenerateContractMarkdown(nil)
	if err == nil {
		t.Fatal("expected error for nil report")
	}
}
