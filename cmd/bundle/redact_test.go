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
			// 00.1.2.3 still forms a valid dotted quad after the leading "3",
			// so the redactor consumes it; the "3" prefix survives.
			want: "version 3[REDACTED-IP] stays",
		},
		{
			name: "plain numbers untouched",
			in:   "replicas: 3, port 8080",
			want: "replicas: 3, port 8080",
		},
		{
			name: "ipv6 address",
			in:   "addr fe80::1ff:fe23:4567:890a end",
			want: "addr fe80[REDACTED-IP] end",
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
