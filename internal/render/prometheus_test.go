package render

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

var prometheusSampleLinePattern = regexp.MustCompile(
	`^([a-zA-Z_:][a-zA-Z0-9_:]*)\{namespace="((?:\\.|[^"\\])*)",name="((?:\\.|[^"\\])*)"\} (-?[0-9]+(?:\.[0-9]+)?)$`,
)

type parsedPrometheusText struct {
	helpCount   map[string]int
	typeCount   map[string]int
	sampleCount map[string]int
}

// parsePrometheusText is a strict parser-equivalent check for the subset of
// the OpenMetrics text format emitted by this package. It rejects malformed
// metadata, unexpected lines, and invalid label escaping.
func parsePrometheusText(t *testing.T, text string) parsedPrometheusText {
	t.Helper()
	parsed := parsedPrometheusText{
		helpCount:   map[string]int{},
		typeCount:   map[string]int{},
		sampleCount: map[string]int{},
	}
	scanner := bufio.NewScanner(strings.NewReader(text))
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "# HELP "):
			fields := strings.Fields(line)
			if len(fields) < 4 || fields[0] != "#" || fields[1] != "HELP" {
				t.Fatalf("invalid HELP line %d: %q", lineNumber, line)
			}
			parsed.helpCount[fields[2]]++
		case strings.HasPrefix(line, "# TYPE "):
			fields := strings.Fields(line)
			if len(fields) != 4 || fields[0] != "#" || fields[1] != "TYPE" || fields[3] != "gauge" {
				t.Fatalf("invalid TYPE line %d: %q", lineNumber, line)
			}
			parsed.typeCount[fields[2]]++
		default:
			matches := prometheusSampleLinePattern.FindStringSubmatch(line)
			if matches == nil {
				t.Fatalf("invalid sample line %d: %q", lineNumber, line)
			}
			validatePrometheusLabelEscapes(t, matches[2])
			validatePrometheusLabelEscapes(t, matches[3])
			parsed.sampleCount[matches[1]]++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan Prometheus text: %v", err)
	}
	return parsed
}

func validatePrometheusLabelEscapes(t *testing.T, value string) {
	t.Helper()
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' {
			continue
		}
		if i+1 >= len(value) || !strings.ContainsRune(`\"n`, rune(value[i+1])) {
			t.Fatalf("invalid Prometheus label escape in %q at byte %d", value, i)
		}
		i++
	}
}

func TestPrometheusListWritesMetadataOnceAndEscapesLineBreaks(t *testing.T) {
	report := hpaanalysis.ListReport{
		APIVersion: hpaanalysis.SchemaVersion,
		Items: []hpaanalysis.ListItem{
			{
				Namespace:   "team\nblue",
				Name:        "api\rworker",
				HealthScore: 75,
				Current:     2,
				Desired:     3,
				Min:         1,
				Max:         10,
			},
			{
				Namespace:   `prod\west`,
				Name:        `quote"worker`,
				HealthScore: 90,
				Current:     4,
				Desired:     4,
				Min:         2,
				Max:         20,
			},
		},
	}

	var out bytes.Buffer
	if err := Prometheus(&out, report); err != nil {
		t.Fatalf("Prometheus: %v", err)
	}
	parsed := parsePrometheusText(t, out.String())
	for _, metric := range prometheusMetricDefinitions {
		t.Run(metric.name, func(t *testing.T) {
			if got := parsed.helpCount[metric.name]; got != 1 {
				t.Errorf("HELP count = %d, want 1", got)
			}
			if got := parsed.typeCount[metric.name]; got != 1 {
				t.Errorf("TYPE count = %d, want 1", got)
			}
			if got := parsed.sampleCount[metric.name]; got != len(report.Items) {
				t.Errorf("sample count = %d, want %d", got, len(report.Items))
			}
		})
	}

	text := out.String()
	for _, escaped := range []string{`team\nblue`, `api\nworker`, `prod\\west`, `quote\"worker`} {
		if !strings.Contains(text, escaped) {
			t.Errorf("output is missing escaped label %q:\n%s", escaped, text)
		}
	}
	for _, raw := range []string{"team\nblue", "api\rworker"} {
		if strings.Contains(text, raw) {
			t.Errorf("raw line break leaked through label %q:\n%s", fmt.Sprintf("%q", raw), text)
		}
	}
}

func TestPrometheusEmptyListWritesMetadataWithoutSamples(t *testing.T) {
	var out bytes.Buffer
	if err := Prometheus(&out, hpaanalysis.ListReport{Items: []hpaanalysis.ListItem{}}); err != nil {
		t.Fatalf("Prometheus: %v", err)
	}

	parsed := parsePrometheusText(t, out.String())
	for _, metric := range prometheusMetricDefinitions {
		t.Run(metric.name, func(t *testing.T) {
			if got := parsed.helpCount[metric.name]; got != 1 {
				t.Errorf("HELP count = %d, want 1", got)
			}
			if got := parsed.typeCount[metric.name]; got != 1 {
				t.Errorf("TYPE count = %d, want 1", got)
			}
			if got := parsed.sampleCount[metric.name]; got != 0 {
				t.Errorf("sample count = %d, want 0", got)
			}
		})
	}
}
