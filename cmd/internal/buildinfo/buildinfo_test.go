package buildinfo

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

func TestResolve(t *testing.T) {
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
			v:    DefaultVersion, c: DefaultCommit, d: DefaultDate,
			read:  buildInfoWith("v2.1.3", map[string]string{"vcs.revision": "deadbeef", "vcs.time": "2026-07-01T00:00:00Z"}),
			wantV: "v2.1.3", wantC: "deadbeef", wantD: "2026-07-01T00:00:00Z",
		},
		{
			name: "devel module version is ignored but vcs metadata still applies",
			v:    DefaultVersion, c: DefaultCommit, d: DefaultDate,
			read:  buildInfoWith("(devel)", map[string]string{"vcs.revision": "deadbeef"}),
			wantV: DefaultVersion, wantC: "deadbeef", wantD: DefaultDate,
		},
		{
			name: "no build info keeps defaults",
			v:    DefaultVersion, c: DefaultCommit, d: DefaultDate,
			read:  func() (*debug.BuildInfo, bool) { return nil, false },
			wantV: DefaultVersion, wantC: DefaultCommit, wantD: DefaultDate,
		},
		{
			name: "available but nil build info keeps defaults",
			v:    DefaultVersion, c: DefaultCommit, d: DefaultDate,
			read:  func() (*debug.BuildInfo, bool) { return nil, true },
			wantV: DefaultVersion, wantC: DefaultCommit, wantD: DefaultDate,
		},
		{
			name: "empty settings values are skipped",
			v:    DefaultVersion, c: DefaultCommit, d: DefaultDate,
			read:  buildInfoWith("", map[string]string{"vcs.revision": "", "vcs.time": ""}),
			wantV: DefaultVersion, wantC: DefaultCommit, wantD: DefaultDate,
		},
		{
			name: "partial ldflags: only unset fields are filled",
			v:    "v2.1.0", c: DefaultCommit, d: DefaultDate,
			read:  buildInfoWith("v9.9.9", map[string]string{"vcs.revision": "deadbeef", "vcs.time": "2026-07-01T00:00:00Z"}),
			wantV: "v2.1.0", wantC: "deadbeef", wantD: "2026-07-01T00:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotV, gotC, gotD := Resolve(tt.v, tt.c, tt.d, tt.read)
			if gotV != tt.wantV || gotC != tt.wantC || gotD != tt.wantD {
				t.Errorf("Resolve() = (%q, %q, %q), want (%q, %q, %q)",
					gotV, gotC, gotD, tt.wantV, tt.wantC, tt.wantD)
			}
		})
	}
}

func TestResolveSkipsBuildInfoWhenFullyStamped(t *testing.T) {
	t.Parallel()

	// A full release build must never consult the Go build info.
	called := false
	read := func() (*debug.BuildInfo, bool) {
		called = true
		return nil, false
	}

	Resolve("v2.3.0", "abc123", "2026-01-02", read)

	if called {
		t.Error("expected build info not to be read when all ldflags are set")
	}
}

func TestResolveVCSSettings(t *testing.T) {
	t.Parallel()

	// An ldflags-supplied commit must survive a vcs.revision setting.
	c, _ := ResolveVCSSettings("ldflag-commit", DefaultDate, []debug.BuildSetting{{Key: "vcs.revision", Value: "deadbeef"}})
	if c != "ldflag-commit" {
		t.Errorf("an ldflags commit must win over vcs.revision, got %q", c)
	}

	// Unrelated settings must be ignored.
	c, d := ResolveVCSSettings(DefaultCommit, DefaultDate, []debug.BuildSetting{{Key: "GOARCH", Value: "arm64"}})
	if c != DefaultCommit || d != DefaultDate {
		t.Errorf("unrelated settings must not change the values, got %q %q", c, d)
	}
}
