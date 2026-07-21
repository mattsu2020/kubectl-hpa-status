package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootRejectsInvalidEffectiveOptionsBeforeRun(t *testing.T) {
	missingConfig := filepath.Join(t.TempDir(), "missing.yaml")
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "explicit missing config is fatal",
			args:    []string{"--config", missingConfig, "version"},
			wantErr: "failed to load config",
		},
		{
			name:    "malformed health weight is fatal",
			args:    []string{"--health-weight", "not-a-pair", "version"},
			wantErr: "expected name=value",
		},
		{
			name:    "negative qps is fatal",
			args:    []string{"--qps=-1", "version"},
			wantErr: "--qps must be",
		},
		{
			name:    "zero concurrency is fatal",
			args:    []string{"--concurrency=0", "version"},
			wantErr: "--concurrency must be",
		},
		{
			name:    "conflicting namespace scopes are fatal",
			args:    []string{"--namespace", "team-a", "--all-namespaces", "version"},
			wantErr: "cannot be used together",
		},
		{
			name:    "unknown enrichment mode is fatal",
			args:    []string{"status", "--keda=typo", "web"},
			wantErr: "--keda must be",
		},
		{
			name:    "unknown output mode is fatal",
			args:    []string{"--output=xml", "version"},
			wantErr: "unsupported --output",
		},
		{
			name:    "unbounded list apply is rejected before client creation",
			args:    []string{"list", "--apply", "--filter=all", "--yes"},
			wantErr: "rejects --filter=all",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := NewRootCommand()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs(tc.args)
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
			}
			if strings.Contains(out.String(), "kubectl-hpa-status version") {
				t.Fatalf("command body ran after validation failure: %s", out.String())
			}
		})
	}
}

func TestStatusFlagRegistrationDoesNotLeakEnrichmentIntoList(t *testing.T) {
	opts := defaultRootOptions()
	_ = newStatusCommand(&opts)

	if opts.KEDA != "" || opts.VPA != "" {
		t.Fatalf("status flag registration leaked enrichment defaults: keda=%q vpa=%q", opts.KEDA, opts.VPA)
	}
	if opts.Events.Enabled {
		t.Fatal("plain status default unexpectedly enables Events")
	}
	if !canStreamList(&opts) {
		t.Fatal("default list options should remain eligible for streaming")
	}
}

func TestExplainEnablesEventsUnlessExplicitlyConfigured(t *testing.T) {
	t.Run("implicit", func(t *testing.T) {
		opts := defaultRootOptions()
		cmd := newStatusCommand(&opts)
		opts.Explain = true
		opts.Normalize()
		applyStatusDepthDefaults(cmd, &opts)
		if !opts.Events.Enabled || opts.Events.Limit != 5 {
			t.Fatalf("explain events = %+v, want enabled limit 5", opts.Events)
		}
	})

	t.Run("explicit false wins", func(t *testing.T) {
		opts := defaultRootOptions()
		cmd := newStatusCommand(&opts)
		opts.Explain = true
		opts.EventsConfigured = true
		opts.Events.Enabled = false
		opts.Normalize()
		applyStatusDepthDefaults(cmd, &opts)
		if opts.Events.Enabled {
			t.Fatal("explicit events=false was overridden")
		}
	})
}

func TestValidateListApplyAcceptsExplicitHealthScoreZero(t *testing.T) {
	opts := &options{
		Common: commonOptions{
			ApplyOptions: ApplyOptions{
				Apply: true,
			},
		},
		List: listOptions{
			HealthScoreMin:           -1,
			HealthScoreMax:           0,
			HealthScoreMaxConfigured: true,
		},
	}
	if err := validateListApply(opts, ""); err != nil {
		t.Fatalf("explicit --health-score=0 should bound list apply: %v", err)
	}
}

func TestLoadConfigFileIsStrictAndCanonical(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "unknown field",
			content: "configVersion: v1\nunknownField: true\n",
			wantErr: "unknown field",
		},
		{
			name:    "duplicate field",
			content: "namespace: first\nnamespace: second\n",
			wantErr: "namespace",
		},
		{
			name:    "invalid enrichment mode",
			content: "keda: typo\n",
			wantErr: "config keda",
		},
		{
			name:    "negative health weight",
			content: "healthWeights:\n  churn: -1\n",
			wantErr: "non-negative",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := loadConfigFile(path)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "output: names\ntemplates:\n  names:\n    type: go-template\n    template: '{{ .Analysis.Name }}'\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfigFile(path)
	if err != nil {
		t.Fatalf("named template config: %v", err)
	}
	if cfg.Output != "names" {
		t.Fatalf("named output = %q, want names", cfg.Output)
	}

	path = filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("output: gotemplate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = loadConfigFile(path)
	if err != nil {
		t.Fatalf("gotemplate config: %v", err)
	}
	if cfg.Output != "go-template" {
		t.Fatalf("canonical output = %q, want go-template", cfg.Output)
	}
}
