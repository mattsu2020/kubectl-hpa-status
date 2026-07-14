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

total="$(go tool cover -func="$coverage_file" | awk '/^total:/ {gsub(/%/, "", $3); print $3}')"
check "Total" "$total" 65
check "pkg/hpa" "$(statement_coverage '/pkg/hpa/')" 70
check "cmd" "$(statement_coverage '/cmd/')" 55
check "cmd/replaylab" "$(statement_coverage '/cmd/replaylab/')" 60
check "internal/enrichment" "$(statement_coverage '/internal/enrichment/')" 50
check "pkg/hpa/render" "$(statement_coverage '/pkg/hpa/render/')" 50
