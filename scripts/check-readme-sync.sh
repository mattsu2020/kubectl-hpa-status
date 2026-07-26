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

# The translated headings differ, but their relative order must remain the
# same. This enforces the structural contract described in CONTRIBUTING.md.
previous_en_line=0
previous_ja_line=0
for pair in "${expected_pairs[@]}"; do
  en_section="${pair%%|*}"
  ja_section="${pair##*|}"
  en_line="$(grep -n -F -m1 "## $en_section" "$en_file" | cut -d: -f1)"
  ja_line="$(grep -n -F -m1 "## $ja_section" "$ja_file" | cut -d: -f1)"
  if (( en_line <= previous_en_line || ja_line <= previous_ja_line )); then
    echo "README section order differs around: $en_section / $ja_section" >&2
    exit 1
  fi
  previous_en_line="$en_line"
  previous_ja_line="$ja_line"
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

# Command examples are intentionally language-independent. Keep their exact
# text and order synchronized so a flag or subcommand cannot drift in only one
# README while the heading-count check still passes.
extract_commands() {
  awk '/^(kubectl( |-)hpa|kubectl-hpa-status|brew |go (test|install|build)|make( |$)|git clone|docker )/' "$1"
}

en_commands="$(extract_commands "$en_file")"
ja_commands="$(extract_commands "$ja_file")"
if [[ "$en_commands" != "$ja_commands" ]]; then
  echo "README command examples differ between $en_file and $ja_file" >&2
  diff -u <(printf '%s\n' "$en_commands") <(printf '%s\n' "$ja_commands") >&2 || true
  exit 1
fi

# Status-only flags are not registered on the root command. Reject the common
# shorthand that looks plausible but fails with "unknown flag".
if grep -Eq '^kubectl hpa_status <[^>]+> .*--(suggest|explain|interpret|fix)([ =]|$)' "$en_file" "$ja_file"; then
  echo "status-only flag example is missing the 'status' subcommand" >&2
  exit 1
fi

echo "README sync check passed: $en_file and $ja_file"
