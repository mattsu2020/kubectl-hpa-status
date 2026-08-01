// Package buildinfo resolves the version, commit, and build date reported by
// the `version` command and the root --version flag.
//
// Release builds inject these through -ldflags (see .goreleaser.yml). When
// they are still at their defaults the binary was built without release
// metadata — typically `go install module@version` — and the values are
// recovered from the Go build info the toolchain embeds.
//
// Lifted from cmd/version_info.go as part of the cmd/ sub-package split. It has
// no dependency on cobra or the cmd option struct.
package buildinfo

import "runtime/debug"

// ldflags defaults. Exported so the cmd layer seeds its package-level
// version/commit/date variables from the same constants this package compares
// against; a drift between the two would silently disable the fallback.
const (
	DefaultVersion = "v2.0.0-dev"
	DefaultCommit  = "unknown"
	DefaultDate    = "unknown"
)

// Resolve fills in version, commit, and build date from the Go build info when
// the ldflags defaults were not overridden. Values already set via ldflags
// always win. readBuildInfo is injectable for tests.
func Resolve(v, c, d string, readBuildInfo func() (*debug.BuildInfo, bool)) (string, string, string) {
	if v != DefaultVersion && c != DefaultCommit && d != DefaultDate {
		return v, c, d
	}
	info, ok := readBuildInfo()
	if !ok || info == nil {
		return v, c, d
	}
	if v == DefaultVersion && info.Main.Version != "" && info.Main.Version != "(devel)" {
		v = info.Main.Version
	}
	c, d = ResolveVCSSettings(c, d, info.Settings)
	return v, c, d
}

// ResolveVCSSettings fills commit and build date from vcs build settings when
// the ldflags defaults are still in place.
func ResolveVCSSettings(c, d string, settings []debug.BuildSetting) (string, string) {
	for _, s := range settings {
		if s.Value == "" {
			continue
		}
		switch s.Key {
		case "vcs.revision":
			if c == DefaultCommit {
				c = s.Value
			}
		case "vcs.time":
			if d == DefaultDate {
				d = s.Value
			}
		}
	}
	return c, d
}
