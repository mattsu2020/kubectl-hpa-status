package cmd

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

type junitTestsuite struct {
	XMLName  xml.Name        `xml:"testsuite"`
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Cases    []junitTestcase `xml:"testcase"`
}

type junitTestcase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

func writeListJUnit(out io.Writer, report hpaanalysis.ListReport) error {
	suite := junitTestsuite{Name: "kubectl-hpa-status", Tests: len(report.Items)}
	for _, item := range report.Items {
		tc := junitTestcase{Name: item.Namespace + "/" + item.Name, Classname: "hpa.health"}
		if listItemFailed(item) {
			suite.Failures++
			msg := item.Issue
			if msg == "" {
				msg = fmt.Sprintf("health=%s score=%d", item.Health, item.HealthScore)
			}
			tc.Failure = &junitFailure{Message: msg, Text: item.Summary}
		}
		suite.Cases = append(suite.Cases, tc)
	}
	data, err := xml.MarshalIndent(suite, "", "  ")
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, xml.Header+string(data)); err != nil {
		return err
	}
	return nil
}

func writeListSARIF(out io.Writer, report hpaanalysis.ListReport) error {
	type sarifResult struct {
		RuleID  string `json:"ruleId"`
		Level   string `json:"level"`
		Message struct {
			Text string `json:"text"`
		} `json:"message"`
		Locations []struct {
			PhysicalLocation struct {
				ArtifactLocation struct {
					URI string `json:"uri"`
				} `json:"artifactLocation"`
			} `json:"physicalLocation"`
		} `json:"locations,omitempty"`
	}
	type sarifRun struct {
		Tool struct {
			Driver struct {
				Name  string `json:"name"`
				Rules []struct {
					ID string `json:"id"`
				} `json:"rules"`
			} `json:"driver"`
		} `json:"tool"`
		Results []sarifResult `json:"results"`
	}
	doc := struct {
		Version string     `json:"version"`
		Schema  string     `json:"$schema"`
		Runs    []sarifRun `json:"runs"`
	}{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
	}
	run := sarifRun{}
	run.Tool.Driver.Name = "kubectl-hpa-status"
	run.Tool.Driver.Rules = []struct {
		ID string `json:"id"`
	}{{ID: "hpa-health"}}
	for _, item := range report.Items {
		if !listItemFailed(item) {
			continue
		}
		result := sarifResult{RuleID: "hpa-health", Level: "warning"}
		result.Message.Text = fmt.Sprintf("%s/%s health=%s score=%d issue=%s", item.Namespace, item.Name, item.Health, item.HealthScore, item.Issue)
		location := struct {
			PhysicalLocation struct {
				ArtifactLocation struct {
					URI string `json:"uri"`
				} `json:"artifactLocation"`
			} `json:"physicalLocation"`
		}{}
		location.PhysicalLocation.ArtifactLocation.URI = "kubernetes://" + item.Namespace + "/horizontalpodautoscalers/" + item.Name
		result.Locations = append(result.Locations, location)
		run.Results = append(run.Results, result)
	}
	doc.Runs = []sarifRun{run}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(doc)
}

func listItemFailed(item hpaanalysis.ListItem) bool {
	return item.Health != string(hpaanalysis.HealthOK) || item.HealthScore < 80 || item.Issue != ""
}
