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
check "Total" "$total" 74
check "pkg/hpa" "$(statement_coverage '/pkg/hpa/')" 78
check "cmd" "$(statement_coverage '/cmd/')" 64
check "cmd/replaylab" "$(statement_coverage '/cmd/replaylab/')" 92
check "internal/enrichment" "$(statement_coverage '/internal/enrichment/')" 83
check "pkg/hpa/render" "$(statement_coverage '/pkg/hpa/render/')" 91

# Safety-sensitive boundaries get their own floors so strong coverage in large
# presentation packages cannot hide a regression in mutation, policy, archive,
# or persistence code.
check "mutating command paths" "$(statement_coverage '/cmd/(apply|config_apply|list_apply)\.go:')" 67
check "internal/patch" "$(statement_coverage '/internal/patch/')" 92
check "internal/history" "$(statement_coverage '/internal/history/')" 66
check "pkg/hpa/policy" "$(statement_coverage '/pkg/hpa/policy/')" 93
check "pkg/hpa/lint" "$(statement_coverage '/pkg/hpa/lint/')" 90
check "cmd/bundle" "$(statement_coverage '/cmd/bundle/')" 91
