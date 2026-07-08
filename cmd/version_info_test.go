package cmd

import (
	"runtime/debug"
	"testing"
)

func buildInfoWith(version string, settings map[string]string) func() (*debug.BuildInfo, bool) {
	info := &debug.BuildInfo{}
	info.Main.Version = version
	for k, v := range settings {
		info.Settings = append(info.Settings, debug.BuildSetting{Key: k, Value: v})
	}
	return func() (*debug.BuildInfo, bool) { return info, true }
}

func TestResolveBuildInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		v, c, d             string
		read                func() (*debug.BuildInfo, bool)
		wantV, wantC, wantD string
	}{
		{
			name: "ldflags values win untouched",
			v:    "v2.1.0", c: "abc1234", d: "2026-07-07",
			read:  buildInfoWith("v9.9.9", map[string]string{"vcs.revision": "zzz", "vcs.time": "1999"}),
			wantV: "v2.1.0", wantC: "abc1234", wantD: "2026-07-07",
		},
		{
			name: "go install fills version and vcs metadata",
			v:    defaultVersion, c: defaultCommit, d: defaultDate,
			read:  buildInfoWith("v2.1.3", map[string]string{"vcs.revision": "deadbeef", "vcs.time": "2026-07-01T00:00:00Z"}),
			wantV: "v2.1.3", wantC: "deadbeef", wantD: "2026-07-01T00:00:00Z",
		},
		{
			name: "devel module version is ignored but vcs metadata still applies",
			v:    defaultVersion, c: defaultCommit, d: defaultDate,
			read:  buildInfoWith("(devel)", map[string]string{"vcs.revision": "deadbeef"}),
			wantV: defaultVersion, wantC: "deadbeef", wantD: defaultDate,
		},
		{
			name: "no build info keeps defaults",
			v:    defaultVersion, c: defaultCommit, d: defaultDate,
			read:  func() (*debug.BuildInfo, bool) { return nil, false },
			wantV: defaultVersion, wantC: defaultCommit, wantD: defaultDate,
		},
		{
			name: "empty settings values are skipped",
			v:    defaultVersion, c: defaultCommit, d: defaultDate,
			read:  buildInfoWith("", map[string]string{"vcs.revision": "", "vcs.time": ""}),
			wantV: defaultVersion, wantC: defaultCommit, wantD: defaultDate,
		},
		{
			name: "partial ldflags: only unset fields are filled",
			v:    "v2.1.0", c: defaultCommit, d: defaultDate,
			read:  buildInfoWith("v9.9.9", map[string]string{"vcs.revision": "deadbeef", "vcs.time": "2026-07-01T00:00:00Z"}),
			wantV: "v2.1.0", wantC: "deadbeef", wantD: "2026-07-01T00:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotV, gotC, gotD := resolveBuildInfo(tt.v, tt.c, tt.d, tt.read)
			if gotV != tt.wantV || gotC != tt.wantC || gotD != tt.wantD {
				t.Errorf("resolveBuildInfo() = (%q, %q, %q), want (%q, %q, %q)",
					gotV, gotC, gotD, tt.wantV, tt.wantC, tt.wantD)
			}
		})
	}
}
