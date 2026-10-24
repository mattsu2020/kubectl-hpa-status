package bundle

import (
	"encoding/json"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestStructuredRedactionPreservesSerializableShape(t *testing.T) {
	input := []byte(`{"token":"key-secret","replicas":3,"enabled":true,"nullable":null,"spec":{"nodeName":"worker-private-01","containers":[{"name":"web","args":["--token=arg-secret","--password","separate-secret","https://user:uri-secret@service.invalid/api?api_key=query-secret"],"env":[{"name":"TOKEN","value":"env-secret"}]}]},"addresses":["10.0.1.2","2001:db8::1"],"uid":"12345678-1234-1234-1234-123456789abc"}`)
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			data := input
			if format == "yaml" {
				var err error
				data, err = yaml.JSONToYAML(data)
				if err != nil {
					t.Fatal(err)
				}
			}
			out := RedactStructuredBytes(data)
			for _, secret := range []string{"key-secret", "arg-secret", "separate-secret", "uri-secret", "query-secret", "env-secret", "worker-private-01", "10.0.1.2", "2001:db8::1", "12345678-1234-1234-1234-123456789abc"} {
				if strings.Contains(string(out), secret) {
					t.Errorf("redacted output contains %q", secret)
				}
			}
			if format == "yaml" {
				var err error
				out, err = yaml.YAMLToJSON(out)
				if err != nil {
					t.Fatalf("redacted YAML is invalid: %v\n%s", err, out)
				}
			}
			var decoded map[string]any
			if err := json.Unmarshal(out, &decoded); err != nil {
				t.Fatalf("redacted JSON is invalid: %v\n%s", err, out)
			}
			if decoded["replicas"] != float64(3) || decoded["enabled"] != true || decoded["nullable"] != nil || decoded["token"] != "[REDACTED]" {
				t.Fatalf("scalar shape changed: %#v", decoded)
			}
			spec := decoded["spec"].(map[string]any)
			container := spec["containers"].([]any)[0].(map[string]any)
			args := container["args"].([]any)
			if len(args) != 4 || args[0] != "--token=[REDACTED]" || args[1] != "--password" || args[2] != "[REDACTED]" {
				t.Fatalf("command shape changed: %#v", args)
			}
		})
	}
}

func TestStructuredRedactionHandlesRootString(t *testing.T) {
	out := RedactStructuredBytes([]byte(`"token=secret at 10.0.1.2"`))
	var decoded string
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != "token=[REDACTED] at [REDACTED-IP]" {
		t.Fatalf("unexpected redaction: %q", decoded)
	}
}
