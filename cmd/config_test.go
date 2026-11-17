package cmd

import (
	"strings"
	"testing"
)

func TestValidateConfig(t *testing.T) {
	boolPtr := func(v bool) *bool { return &v }
	intPtr := func(v int) *int { return &v }
	int64Ptr := func(v int64) *int64 { return &v }

	tests := []struct {
		name    string
		cfg     configFile
		wantErr string
	}{
		{name: "empty config is valid", cfg: configFile{}},
		{name: "negative chunkSize rejected", cfg: configFile{ChunkSize: int64Ptr(-1)}, wantErr: "chunkSize"},
		{name: "zero chunkSize valid", cfg: configFile{ChunkSize: int64Ptr(0)}},
		{name: "minScore below range rejected", cfg: configFile{MinScore: intPtr(-1)}, wantErr: "minScore"},
		{name: "minScore above range rejected", cfg: configFile{MinScore: intPtr(101)}, wantErr: "minScore"},
		{name: "maxScore below range rejected", cfg: configFile{MaxScore: intPtr(-1)}, wantErr: "maxScore"},
		{name: "healthScore below range rejected", cfg: configFile{HealthScore: intPtr(-1)}, wantErr: "healthScore"},
		{name: "valid boundary scores accepted", cfg: configFile{MinScore: intPtr(0), MaxScore: intPtr(100), HealthScore: intPtr(50)}},
		{name: "invalid color rejected", cfg: configFile{Color: "rainbow"}, wantErr: "color"},
		{name: "valid color always accepted", cfg: configFile{Color: "always"}},
		{name: "valid color never accepted", cfg: configFile{Color: "never"}},
		{name: "invalid output rejected", cfg: configFile{Output: "xml"}, wantErr: "output"},
		{name: "valid output json accepted", cfg: configFile{Output: "json"}},
		{name: "valid output go-template normalized accepted", cfg: configFile{Output: "go-template"}},
		{name: "invalid lang rejected", cfg: configFile{Lang: "fr"}, wantErr: "lang"},
		{name: "valid lang en accepted", cfg: configFile{Lang: "en"}},
		{name: "valid lang ja accepted", cfg: configFile{Lang: "ja"}},
		{name: "bool field accepted", cfg: configFile{Wide: boolPtr(true)}},
		{name: "configVersion v1 accepted", cfg: configFile{ConfigVersion: "v1"}},
		{name: "empty configVersion accepted (backward compat)", cfg: configFile{ConfigVersion: ""}},
		{name: "unknown configVersion rejected", cfg: configFile{ConfigVersion: "v2"}, wantErr: "configVersion"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateConfig(tc.cfg)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidOutputValuesCanonical(t *testing.T) {
	// All canonical output values must survive normalization and remain in the
	// accepted set. This guards against accidental drift between
	// validOutputValues and normalizeSelector.
	for _, v := range validOutputValues {
		if normalizeSelector(v) != v {
			t.Errorf("validOutputValues entry %q is not in canonical form (normalized to %q)", v, normalizeSelector(v))
		}
	}
}
