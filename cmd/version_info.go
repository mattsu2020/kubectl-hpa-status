package cmd

import (
	"runtime/debug"

	"github.com/mattsu2020/kubectl-hpa-status/cmd/internal/buildinfo"
)

// ldflags defaults; when these are still in place the binary was built
// without release metadata (e.g. via `go install module@version`) and we
// fall back to Go build info embedded by the toolchain.
//
// The values live in cmd/internal/buildinfo so the resolver compares against
// the same constants the -ldflags targets are seeded from.
const (
	defaultVersion = buildinfo.DefaultVersion
	defaultCommit  = buildinfo.DefaultCommit
	defaultDate    = buildinfo.DefaultDate
)

// resolveBuildInfo re-exports buildinfo.Resolve under the unexported name used
// by cmd/. buildVersion in root.go is the only caller; the resolver's own
// behavior is tested in cmd/internal/buildinfo.
func resolveBuildInfo(v, c, d string, readBuildInfo func() (*debug.BuildInfo, bool)) (string, string, string) {
	return buildinfo.Resolve(v, c, d, readBuildInfo)
}
