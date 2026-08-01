#!/usr/bin/env bash
set -euo pipefail

coverage_file="${1:-coverage.out}"

statement_coverage() {
  awk -v p="$1" 'NR > 1 && $1 ~ p { total += $2; if ($3 > 0) covered += $2 } END { if (total > 0) printf "%.1f", covered * 100 / total; else print 0 }' "$coverage_file"
}

check() {
  local name="$1" value="$2" threshold="$3"
  echo "$name coverage: ${value}% (threshold ${threshold}%)"
  if awk -v value="$value" -v threshold="$threshold" 'BEGIN { exit !(value < threshold) }'; then
    echo "$name coverage ${value}% is below ${threshold}%" >&2
    exit 1
  fi
}

# Thresholds are ratchets, not aspirations: each one sits roughly 2 points
# below the coverage actually achieved on main, so a real regression fails the
# build while normal test-selection jitter does not. They had drifted 10+
# points below actual, which let a large regression pass unnoticed. When a
# target rises durably, raise its floor here in the same PR; never lower a
# floor to make a red build green.
total="$(go tool cover -func="$coverage_file" | awk '/^total:/ {gsub(/%/, "", $3); print $3}')"
check "Total" "$total" 76
check "pkg/hpa (tree)" "$(statement_coverage '/pkg/hpa/')" 78
check "cmd (tree)" "$(statement_coverage '/cmd/')" 69
check "cmd/replaylab" "$(statement_coverage '/cmd/replaylab/')" 92
check "internal/enrichment" "$(statement_coverage '/internal/enrichment/')" 83
check "pkg/hpa/render" "$(statement_coverage '/pkg/hpa/render/')" 91

# Root-package floors. The "(tree)" checks above use substring matches, so they
# also count every subpackage under that path. As the cmd/ and pkg/hpa/ splits
# progress, each freshly extracted subpackage lands with high coverage and
# lifts the tree aggregate, which can mask the large, weakly covered root
# package it was carved out of. When these two checks were added the cmd tree
# read 66.3% against a flat cmd package at 62.7%, and the pkg/hpa tree read
# 80.2% against a root at 75.5% — a regression confined to either root could
# have passed its tree gate. `[^/]+\.go:` anchors these checks to files sitting
# directly in the package directory, so a root cannot be carried by its own
# subpackages.
check "cmd (root package)" "$(statement_coverage '/cmd/[^/]+\.go:')" 67
check "pkg/hpa (root package)" "$(statement_coverage '/pkg/hpa/[^/]+\.go:')" 73

# Safety-sensitive boundaries get their own floors so strong coverage in large
# presentation packages cannot hide a regression in mutation, policy, archive,
# or persistence code.
check "mutating command paths" "$(statement_coverage '/cmd/(apply|config_apply|list_apply)\.go:')" 67
check "internal/patch" "$(statement_coverage '/internal/patch/')" 92
check "internal/history" "$(statement_coverage '/internal/history/')" 66
check "pkg/hpa/policy" "$(statement_coverage '/pkg/hpa/policy/')" 93
check "pkg/hpa/lint" "$(statement_coverage '/pkg/hpa/lint/')" 90
check "cmd/bundle" "$(statement_coverage '/cmd/bundle/')" 91
