#!/usr/bin/env bash
set -euo pipefail

en_file="${1:-README.md}"
ja_file="${2:-README.ja.md}"

if [[ ! -f "$en_file" ]]; then
  echo "missing English README: $en_file" >&2
  exit 1
fi

if [[ ! -f "$ja_file" ]]; then
  echo "missing Japanese README: $ja_file" >&2
  exit 1
fi

en_sections="$(grep -E '^## ' "$en_file" | sed 's/^## //')"
ja_sections="$(grep -E '^## ' "$ja_file" | sed 's/^## //')"
en_count="$(printf '%s\n' "$en_sections" | sed '/^$/d' | wc -l | tr -d ' ')"
ja_count="$(printf '%s\n' "$ja_sections" | sed '/^$/d' | wc -l | tr -d ' ')"

if [[ "$en_count" -ne "$ja_count" ]]; then
  echo "README section count differs: $en_file=$en_count $ja_file=$ja_count" >&2
  printf '%s\n%s\n' "--- $en_file sections ---" "$en_sections" >&2
  printf '%s\n%s\n' "--- $ja_file sections ---" "$ja_sections" >&2
  exit 1
fi

expected_pairs=(
  "Before / After|Before / After"
  "Demo|デモ"
  "Quick Start|5分で始める"
  "Install|インストール"
  "Representative Commands|代表コマンド"
  "Examples|例"
  "Documentation|ドキュメント"
  "Community and Promotion|コミュニティとプロモーション"
  "Roadmap|ロードマップ"
  "Development|開発"
  "License|ライセンス"
)

for pair in "${expected_pairs[@]}"; do
  en_section="${pair%%|*}"
  ja_section="${pair##*|}"

  if ! grep -Fxq "## $en_section" "$en_file"; then
    echo "$en_file is missing section: ## $en_section" >&2
    exit 1
  fi

  if ! grep -Fxq "## $ja_section" "$ja_file"; then
    echo "$ja_file is missing section: ## $ja_section" >&2
    exit 1
  fi
done

required_links=(
  "ROADMAP.md"
  "docs/social-promotion.md"
  "images/demo.png"
)

for link in "${required_links[@]}"; do
  if ! grep -Fq "$link" "$en_file"; then
    echo "$en_file is missing link/reference: $link" >&2
    exit 1
  fi

  if ! grep -Fq "$link" "$ja_file"; then
    echo "$ja_file is missing link/reference: $link" >&2
    exit 1
  fi
done

# Core subcommands must still be referenced in both READMEs so removals or
# renames do not silently leave the docs stale. Only commands documented in
# the Representative Commands / Quick Start sections are enforced.
required_command_refs=(
  "hpa_status status"
  "hpa_status list"
  "hpa_status doctor"
)

for ref in "${required_command_refs[@]}"; do
  if ! grep -Fq "$ref" "$en_file"; then
    echo "$en_file is missing command reference: $ref" >&2
    exit 1
  fi

  if ! grep -Fq "$ref" "$ja_file"; then
    echo "$ja_file is missing command reference: $ref" >&2
    exit 1
  fi
done

echo "README sync check passed: $en_file and $ja_file"
