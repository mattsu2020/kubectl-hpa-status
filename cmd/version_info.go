package cmd

import (
	"runtime/debug"
)

// ldflags defaults; when these are still in place the binary was built
// without release metadata (e.g. via `go install module@version`) and we
// fall back to Go build info embedded by the toolchain.
const (
	defaultVersion = "v2.0.0-dev"
	defaultCommit  = "unknown"
	defaultDate    = "unknown"
)

// resolveBuildInfo fills in version, commit, and build date from the Go
// build info when the ldflags defaults were not overridden. Values already
// set via ldflags always win. readBuildInfo is injectable for tests.
func resolveBuildInfo(v, c, d string, readBuildInfo func() (*debug.BuildInfo, bool)) (string, string, string) {
	if v != defaultVersion && c != defaultCommit && d != defaultDate {
		return v, c, d
	}
	info, ok := readBuildInfo()
	if !ok || info == nil {
		return v, c, d
	}
	if v == defaultVersion && info.Main.Version != "" && info.Main.Version != "(devel)" {
		v = info.Main.Version
	}
	for _, s := range info.Settings {
		if s.Value == "" {
			continue
		}
		switch s.Key {
		case "vcs.revision":
			if c == defaultCommit {
				c = s.Value
			}
		case "vcs.time":
			if d == defaultDate {
				d = s.Value
			}
		}
	}
	return v, c, d
}
