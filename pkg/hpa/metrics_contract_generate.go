package hpa

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"

	"sigs.k8s.io/yaml"
)

// GenerateContractCommands produces kubectl verification commands for each
// metric in the report. Each command uses kubectl get --raw to directly query
// the metrics API endpoint.
func GenerateContractCommands(report *MetricContractReport) []string {
	if report == nil || len(report.Checks) == 0 {
		return nil
	}

	var commands []string
	for _, check := range report.Checks {
		cmd := generateMetricCommand(report.Namespace, check)
		if cmd != "" {
			commands = append(commands, cmd)
		}
	}
	return commands
}

// generateMetricCommand produces a single kubectl --raw command for a metric check.
func generateMetricCommand(namespace string, check MetricContractCheck) string {
	switch check.MetricType {
	case MetricTypeResource, "ContainerResource":
		return fmt.Sprintf(
			"kubectl get --raw \"/apis/metrics.k8s.io/v1beta1/namespaces/%s/pods\"",
			namespace,
		)
	case MetricTypePods, "Object":
		return fmt.Sprintf(
			"kubectl get --raw \"/apis/custom.metrics.k8s.io/v1beta1/namespaces/%s/%s%s\"",
			namespace,
			strings.ToLower(check.MetricType),
			metricSelectorSuffix(check.Selector),
		)
	case MetricTypeExternal:
		metricName := url.PathEscape(check.MetricName)
		return fmt.Sprintf(
			"kubectl get --raw \"/apis/external.metrics.k8s.io/v1beta1/namespaces/%s/%s%s\"",
			namespace,
			metricName,
			metricSelectorSuffix(check.Selector),
		)
	default:
		return ""
	}
}

// metricSelectorSuffix returns a URL-encoded label selector suffix if a selector is present.
func metricSelectorSuffix(selector string) string {
	if selector == "" {
		return ""
	}
	return "?labelSelector=" + url.QueryEscape(selector)
}

// contractTestYAML is a lightweight struct for YAML test contract generation.
type contractTestYAML struct {
	APIVersion string               `json:"apiVersion" yaml:"apiVersion"`
	Kind       string               `json:"kind" yaml:"kind"`
	Metadata   contractTestMetadata `json:"metadata" yaml:"metadata"`
	Spec       contractTestSpec     `json:"spec" yaml:"spec"`
}

type contractTestMetadata struct {
	Namespace string `json:"namespace" yaml:"namespace"`
	HPAName   string `json:"hpaName" yaml:"hpaName"`
	Target    string `json:"target" yaml:"target"`
}

type contractTestSpec struct {
	Metrics []contractTestMetric `json:"metrics" yaml:"metrics"`
}

type contractTestMetric struct {
	Name           string `json:"name" yaml:"name"`
	Type           string `json:"type" yaml:"type"`
	APIService     string `json:"apiService" yaml:"apiService"`
	Selector       string `json:"selector,omitempty" yaml:"selector,omitempty"`
	ExpectedStatus string `json:"expectedStatus" yaml:"expectedStatus"`
	Verification   string `json:"verification" yaml:"verification"`
}

// GenerateContractYAML produces a YAML test contract document from the report.
func GenerateContractYAML(report *MetricContractReport) ([]byte, error) {
	if report == nil {
		return nil, fmt.Errorf("report is nil")
	}

	contract := contractTestYAML{
		APIVersion: "hpa-status.io/v1alpha1",
		Kind:       "MetricContractTest",
		Metadata: contractTestMetadata{
			Namespace: report.Namespace,
			HPAName:   report.Name,
			Target:    report.Target,
		},
		Spec: contractTestSpec{
			Metrics: make([]contractTestMetric, 0, len(report.Checks)),
		},
	}

	for _, check := range report.Checks {
		expectedStatus := "ok"
		if check.Status != "ok" {
			expectedStatus = check.Status
		}

		contract.Spec.Metrics = append(contract.Spec.Metrics, contractTestMetric{
			Name:           check.MetricName,
			Type:           check.MetricType,
			APIService:     check.APIService,
			Selector:       check.Selector,
			ExpectedStatus: expectedStatus,
			Verification:   generateMetricCommand(report.Namespace, check),
		})
	}

	return yaml.Marshal(contract)
}

// GenerateContractMarkdown produces a Markdown document describing the metric
// contract with status, verification commands, and remediation steps.
func GenerateContractMarkdown(report *MetricContractReport) ([]byte, error) {
	if report == nil {
		return nil, fmt.Errorf("report is nil")
	}

	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("# Metric Contract: %s/%s (%s)\n\n",
		report.Namespace, report.Name, report.Target))
	buf.WriteString(fmt.Sprintf("**Overall Status:** %s\n\n", report.OverallStatus))
	buf.WriteString(fmt.Sprintf("**Summary:** %s\n\n", report.Summary))

	if len(report.Checks) > 0 {
		buf.WriteString("## Metrics\n\n")
		buf.WriteString("| Metric | Type | API Service | Status | Verification |\n")
		buf.WriteString("|--------|------|-------------|--------|---------------|\n")

		for _, check := range report.Checks {
			statusEmoji := "OK"
			if check.Status != "ok" {
				statusEmoji = "FAIL"
			}
			cmd := generateMetricCommand(report.Namespace, check)
			buf.WriteString(fmt.Sprintf("| %s | %s | `%s` | %s %s | `%s` |\n",
				check.MetricName,
				check.MetricType,
				check.APIService,
				statusEmoji,
				check.Status,
				cmd,
			))
		}
		buf.WriteString("\n")
	}

	if len(report.Remediation) > 0 {
		buf.WriteString("## Remediation\n\n")
		for _, step := range report.Remediation {
			buf.WriteString(fmt.Sprintf("- %s\n", step))
		}
		buf.WriteString("\n")
	}

	return []byte(buf.String()), nil
}

// GenerateContractJUnit produces JUnit XML output suitable for CI integration.
// Each metric check becomes a testcase; status "ok" passes, anything else fails.
func GenerateContractJUnit(report *MetricContractReport) ([]byte, error) {
	if report == nil {
		return nil, fmt.Errorf("report is nil")
	}

	suite := xmlJUnitTestSuite{
		Name: fmt.Sprintf("hpa-metrics-contract.%s.%s", report.Namespace, report.Name),
	}

	if report.Target != "" {
		suite.Properties = []xmlJUnitProperty{
			{Name: "target", Value: report.Target},
		}
	}

	for _, check := range report.Checks {
		tc := xmlJUnitTestCase{
			Classname: fmt.Sprintf("%s.%s", report.Namespace, report.Name),
			Name:      fmt.Sprintf("%s/%s", check.MetricType, check.MetricName),
		}

		if check.Status != "ok" {
			failureText := check.Detail
			if check.Remediation != "" {
				failureText = fmt.Sprintf("%s\nRemediation: %s", check.Detail, check.Remediation)
			}
			tc.Failure = &xmlJUnitFailure{
				Message: fmt.Sprintf("%s: %s", check.Status, check.APIService),
				Text:    failureText,
			}
			suite.Failures++
		}
		suite.Tests++
		suite.TestCases = append(suite.TestCases, tc)
	}

	return xml.MarshalIndent(suite, "", "  ")
}

// xmlJUnit types for structured JUnit XML output.

type xmlJUnitTestSuite struct {
	XMLName    xml.Name           `xml:"testsuite"`
	Name       string             `xml:"name,attr"`
	Tests      int                `xml:"tests,attr"`
	Failures   int                `xml:"failures,attr"`
	Properties []xmlJUnitProperty `xml:"properties>property,omitempty"`
	TestCases  []xmlJUnitTestCase `xml:"testcase"`
}

type xmlJUnitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type xmlJUnitTestCase struct {
	Classname string           `xml:"classname,attr"`
	Name      string           `xml:"name,attr"`
	Failure   *xmlJUnitFailure `xml:"failure,omitempty"`
}

type xmlJUnitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}
