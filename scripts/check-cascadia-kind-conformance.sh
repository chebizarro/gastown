#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

retired_pattern='30318|30319|30320|30321|30322|30323|30324|30325'
active_paths=(cmd internal npm-package plugins gt-model-eval docs)
allow_docs='^(docs/NOSTR\.md|docs/issues/nostr-migration-import\.jsonl):'

scan_retired() {
  if command -v rg >/dev/null 2>&1; then
    rg -n -o "$retired_pattern" "$@" \
      --glob '!**/vendor/**' \
      --glob '!**/node_modules/**'
  else
    grep -RInEo "$retired_pattern" "$@" 2>/dev/null \
      | grep -Ev '(^|/)(vendor|node_modules)/'
  fi
}

active_hits="$(
  scan_retired "${active_paths[@]}" \
    | grep -Ev '^(docs/issues/nostr-migration-import\.jsonl|docs/NOSTR\.md|internal/nostr/canonical_test\.go):' \
    | sort -u \
    || true
)"

doc_hits="$(
  scan_retired docs \
    | sort -u \
    || true
)"

unexpected_doc_hits=""
if [[ -n "$doc_hits" ]]; then
  unexpected_doc_hits="$(
    printf '%s\n' "$doc_hits" | grep -Ev "$allow_docs" || true
  )"
fi

if [[ -n "$active_hits" || -n "$unexpected_doc_hits" ]]; then
  echo "cascadia kind conformance: FAIL"
  if [[ -n "$active_hits" ]]; then
    echo
    echo "active retired-kind references:"
    printf '%s\n' "$active_hits"
  fi
  if [[ -n "$unexpected_doc_hits" ]]; then
    echo
    echo "unexpected documentation references:"
    printf '%s\n' "$unexpected_doc_hits"
  fi
  exit 1
fi

echo "cascadia kind conformance: PASS"
echo "canonical constants: internal/nostr aliases are generated cascadia-go bindings"
echo "active retired-kind references: 0"

if [[ -n "$doc_hits" ]]; then
  echo "allowed documentation/fixture retired-kind references:"
  printf '%s\n' "$doc_hits"
else
  echo "allowed documentation/fixture retired-kind references: 0"
fi
