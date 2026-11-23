#!/usr/bin/env bash
# Runs golangci-lint with a Go toolchain it can type-check with.
#
# golangci-lint lists packages through the `go` command on PATH but type-checks
# them with the Go version it was itself built with. When the developer's
# toolchain is newer, `go list` selects build-tagged dependency files (for
# example `//go:build go1.27` in golang.org/x/net) that the older type-checker
# rejects outright:
#
#   panic: file requires newer Go version go1.27 (application built with go1.26)
#
# CI does not hit this because setup-go installs the go.mod version, which
# matches the linter's build version. Locally, pin GOTOOLCHAIN to the linter's
# own build version so `make lint` / `make fmt` behave like CI. A linter older
# than the go.mod requirement is rejected instead: no toolchain choice makes it
# work, it has to be upgraded.
#
# Usage: scripts/run-golangci-lint.sh <golangci-lint arguments...>
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: $0 <golangci-lint arguments...>" >&2
  exit 2
fi
bin="$(command -v golangci-lint)" || {
  echo "::error::golangci-lint not found in PATH" >&2
  exit 1
}

# ver_lt A B succeeds when version A is lower than B (1.26.4 < 1.27).
ver_lt() {
  awk -v a="$1" -v b="$2" 'BEGIN {
    n = split(a, A, "."); m = split(b, B, ".")
    for (i = 1; i <= (n > m ? n : m); i++) {
      if ((A[i] + 0) < (B[i] + 0)) exit 0
      if ((A[i] + 0) > (B[i] + 0)) exit 1
    }
    exit 1
  }'
}

lint_go="$(go version "$bin" | awk '{print $2}')"
active_go="$(go env GOVERSION)"
mod_go="$(go list -m -f '{{.GoVersion}}')"

# Not a recognizable Go binary (a wrapper script, say): run it as-is.
if ! [[ "${lint_go#go}" =~ ^[0-9]+\.[0-9]+ ]]; then
  exec "$bin" "$@"
fi
lint_go="${lint_go#go}"
active_go="${active_go#go}"

if ver_lt "$lint_go" "$mod_go"; then
  echo "::error::golangci-lint was built with go${lint_go}, older than the go ${mod_go} required by go.mod; upgrade golangci-lint" >&2
  exit 1
fi

# Only "auto" lets us pick a toolchain; an explicit user setting is respected.
if ver_lt "$lint_go" "$active_go" && [ "${GOTOOLCHAIN:-auto}" = "auto" ]; then
  echo "::notice::golangci-lint was built with go${lint_go} while the active toolchain is go${active_go}; running it with GOTOOLCHAIN=go${lint_go} so every selected file type-checks" >&2
  export GOTOOLCHAIN="go${lint_go}"
fi

exec "$bin" "$@"
