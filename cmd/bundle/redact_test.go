package bundle

import (
	"strings"
	"testing"
)

func TestRedactBytes_Empty(t *testing.T) {
	t.Parallel()
	if got := RedactBytes(nil); got != nil {
		t.Errorf("RedactBytes(nil) = %q, want nil", got)
	}
	if got := RedactBytes([]byte{}); len(got) != 0 {
		t.Errorf("RedactBytes(empty) = %q, want empty", got)
	}
}

func TestRedactString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "ipv4 address",
			in:   "podIP: 10.0.1.23 hostIP: 192.168.1.1",
			want: "podIP: [REDACTED-IP] hostIP: [REDACTED-IP]",
		},
		{
			name: "ipv4 octet over 255 untouched",
			in:   "version 300.1.2.3 stays",
			want: "version 300.1.2.3 stays",
		},
		{
			name: "plain numbers untouched",
			in:   "replicas: 3, port 8080",
			want: "replicas: 3, port 8080",
		},
		{
			name: "ipv6 address",
			in:   "addr fe80::1ff:fe23:4567:890a end",
			want: "addr [REDACTED-IP] end",
		},
		{
			name: "node name after keyword",
			in:   "node: worker-1\nrest",
			want: "node: [REDACTED-NODE]\nrest",
		},
		{
			name: "NodeName keyword",
			in:   "NodeName: ip-node-a rest",
			want: "NodeName: [REDACTED-NODE] rest",
		},
		{
			name: "uuid",
			in:   "uid: 123e4567-e89b-12d3-a456-426614174000 done",
			want: "uid: [REDACTED-UID] done",
		},
		{
			name: "non-uuid similar string untouched",
			in:   "id: 123e4567-e89b-12d3-a456-42661417400Z",
			want: "id: 123e4567-e89b-12d3-a456-42661417400Z",
		},
		{
			name: "ec2 hostname",
			in:   "host ec2-3-91-1-1.compute-1.amazonaws.com up",
			want: "host [REDACTED-HOSTNAME] up",
		},
		{
			name: "gke hostname",
			in:   "gke-cluster-pool-abc123 ready",
			want: "[REDACTED-HOSTNAME] ready",
		},
		{
			name: "no sensitive content",
			in:   "HPA web scaled to 5 replicas",
			want: "HPA web scaled to 5 replicas",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := RedactString(tt.in); got != tt.want {
				t.Errorf("RedactString(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRedactString_RedactsEveryNodeOccurrence(t *testing.T) {
	t.Parallel()
	got := RedactString("node: worker-a, node: worker-b\nNodeName: worker-c")
	for _, leaked := range []string{"worker-a", "worker-b", "worker-c"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("node name %q leaked in %q", leaked, got)
		}
	}
	if count := strings.Count(got, "[REDACTED-NODE]"); count != 3 {
		t.Fatalf("redacted node count = %d, want 3: %q", count, got)
	}
}

func TestRedactStructuredBytes_RemovesSensitiveValues(t *testing.T) {
	t.Parallel()
	input := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    customer: acme
  annotations:
    internal-url: https://service.corp.example
spec:
  template:
    spec:
      containers:
      - name: app
        env:
        - name: API_TOKEN
          value: super-secret-token
        envFrom:
        - secretRef:
            name: production-credentials
`)
	got := string(RedactStructuredBytes(input))
	for _, leaked := range []string{"acme", "service.corp.example", "super-secret-token", "production-credentials"} {
		if strings.Contains(got, leaked) {
			t.Errorf("sensitive value %q leaked in:\n%s", leaked, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected redaction placeholders in:\n%s", got)
	}
}

// TestRedactStructuredBytes_RedactsValueFromRefs locks in redaction of Secret
// and ConfigMap references under env[].valueFrom and envFrom[]. Without this
// coverage a refactor of shouldRedactStructuredField could silently start
// leaking referenced object names (reconnaissance material for attackers).
func TestRedactStructuredBytes_RedactsValueFromRefs(t *testing.T) {
	t.Parallel()
	input := []byte(`apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: app
        env:
        - name: FROM_SECRET
          valueFrom:
            secretKeyRef:
              name: prod-db-credentials
              key: password
        - name: FROM_CM
          valueFrom:
            configMapKeyRef:
              name: prod-app-config
              key: endpoint
        envFrom:
        - secretRef:
            name: bulk-secret
        - configMapRef:
            name: bulk-config
`)
	got := string(RedactStructuredBytes(input))
	for _, leaked := range []string{"prod-db-credentials", "prod-app-config", "bulk-secret", "bulk-config"} {
		if strings.Contains(got, leaked) {
			t.Errorf("reference name %q leaked in:\n%s", leaked, got)
		}
	}
}

func TestRedactStructuredBytes_RedactsCredentialsEmbeddedInCommandAndArgs(t *testing.T) {
	t.Parallel()
	input := []byte(`apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: app
        command:
        - /bin/sh
        - -c
        - 'curl -H "Authorization: Bearer bearer-token" -H "Cookie: session=cookie-session; csrf=cookie-csrf" --password shell-password https://user:uri-password@service.invalid/login'
        args:
        - --token=flag-token
        - --password
        - separate-password
        - --api-key api-key-value
        - --credential=credential-value
        - --client-secret=client-secret-value
        - AWS_SECRET_ACCESS_KEY=aws-secret-value
        - --google-application-credentials
        - google-credentials-value
        - --cookie
        - separate-cookie-value
        - 'Authorization="ApiKey authorization-api-key"'
        - '{"apiKey":"json-api-key"}'
        - https://metrics.invalid/read?token=url-token&mode=safe&api_key=url-api-key
        - --config=/etc/app/config.yaml
`)

	got := string(RedactStructuredBytes(input))
	for _, leaked := range []string{
		"bearer-token",
		"cookie-session",
		"cookie-csrf",
		"shell-password",
		"uri-password",
		"flag-token",
		"separate-password",
		"api-key-value",
		"credential-value",
		"client-secret-value",
		"aws-secret-value",
		"google-credentials-value",
		"separate-cookie-value",
		"authorization-api-key",
		"json-api-key",
		"url-token",
		"url-api-key",
	} {
		if strings.Contains(got, leaked) {
			t.Errorf("credential %q leaked in:\n%s", leaked, got)
		}
	}
	for _, preserved := range []string{"mode=safe", "--config=/etc/app/config.yaml", "user:"} {
		if !strings.Contains(got, preserved) {
			t.Errorf("non-secret context %q was not preserved in:\n%s", preserved, got)
		}
	}
	if count := strings.Count(got, "[REDACTED]"); count < 13 {
		t.Fatalf("redaction placeholder count = %d, want at least 13:\n%s", count, got)
	}
}

func TestRedactStructuredBytes_RedactsSensitiveURLQueryWithoutDroppingOtherParameters(t *testing.T) {
	t.Parallel()
	input := []byte(`endpoint: https://service.invalid/api?access_token=access-value&limit=20&client_secret=secret-value#results
`)
	got := string(RedactStructuredBytes(input))
	for _, leaked := range []string{"access-value", "secret-value"} {
		if strings.Contains(got, leaked) {
			t.Errorf("URL credential %q leaked in %q", leaked, got)
		}
	}
	if !strings.Contains(got, "limit=20") || !strings.Contains(got, "#results") {
		t.Fatalf("non-secret URL components were not preserved: %q", got)
	}
}

// TestRedactString_HostnameWithIP verifies that combined patterns (an ip-
// style hostname containing dashes) redact as a hostname, not partially.
func TestRedactString_HostnameWithIP(t *testing.T) {
	t.Parallel()
	got := RedactString("pod on ip-10-0-1-23.ec2.internal restarted")
	if strings.Contains(got, "ip-10") {
		t.Errorf("hostname not fully redacted: %q", got)
	}
	if !strings.Contains(got, "[REDACTED-HOSTNAME]") {
		t.Errorf("expected hostname placeholder, got %q", got)
	}
}
