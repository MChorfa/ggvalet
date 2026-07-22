#!/usr/bin/env bash
# vwp-lint.sh — lightweight CKODEX VWP v0.1 (§26) governance gate for ggvalet.
#
# Classification: [S] tool / [A] impl. This is a deliberately-bounded grep
# heuristic, NOT a complete NLP classifier. It enforces the mechanically-cheap
# subset of VWP §26.B/§26.C on shippable surfaces:
#   - P-SC-001 NoHedgedCompletion   (hedge words in code comments / README)
#   - P-SC-002 NoMarketingVocabulary (marketing vocab in code comments / README)
#   - P-VW-004 NoSilentStubs        (TODO/FIXME present but not surfaced in the attestation)
#
# Scope: cmd/ and internal/ Go sources + README.md. docs/ is EXEMPT because the
# attestation and ADRs legitimately quote the blocklists and enumerate TODOs.
#
# Exit non-zero on any violation so CI fails closed. Budget: well under §26.D's 30s p99.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

ATTESTATION="docs/VWP-ATTESTATION.md"
status=0

# Surfaces to scan: Go sources under cmd/ and internal/, plus README.md.
# Portable file collection (bash 3.2-compatible; no mapfile).
GO_FILES=()
while IFS= read -r f; do GO_FILES+=("$f"); done \
  < <(find cmd internal -name '*.go' ! -name '*_test.go' 2>/dev/null)
SCAN_FILES=("${GO_FILES[@]}")
[[ -f README.md ]] && SCAN_FILES+=("README.md")

# ── P-SC-001 NoHedgedCompletion ────────────────────────────────────────────
HEDGES='should work|should be correct|probably|i think this|this might|in theory|in principle|tentatively|appears to'
echo "── P-SC-001 hedged-completion lint ──"
if grep -Ein "$HEDGES" "${SCAN_FILES[@]}" 2>/dev/null; then
  echo "VWP P-SC-001 VIOLATION: hedge language above. Replace with a verified claim or a fenced [S]/[A] block." >&2
  status=1
else
  echo "ok: no hedge words"
fi

# ── P-SC-002 NoMarketingVocabulary ─────────────────────────────────────────
MARKETING='robust|scalable|enterprise-grade|production-ready|battle-tested|rock-solid|bulletproof|world-class|cutting-edge|next-generation|revolutionary|seamless|powerful|comprehensive|elegant'
echo "── P-SC-002 marketing-vocab lint ──"
if grep -Eiw "$MARKETING" "${SCAN_FILES[@]}" 2>/dev/null; then
  echo "VWP P-SC-002 VIOLATION: marketing vocabulary above. Substitute a measurable property." >&2
  status=1
else
  echo "ok: no marketing vocab"
fi

# ── P-VW-004 NoSilentStubs ─────────────────────────────────────────────────
# Every TODO/FIXME in shippable Go source must be surfaced (its file:line path or
# its enclosing file) in the attestation's "Stubs remaining" section.
echo "── P-VW-004 stub-surfacing lint ──"
todo_violation=0
while IFS=: read -r file line _; do
  [[ -z "$file" ]] && continue
  # Surfaced if the attestation references the file (by path) anywhere.
  if ! grep -qF "$file" "$ATTESTATION" 2>/dev/null; then
    echo "  UNSURFACED: $file:$line — not referenced in $ATTESTATION" >&2
    todo_violation=1
  fi
done < <(grep -Ein 'TODO|FIXME|unimplemented' "${GO_FILES[@]}" 2>/dev/null || true)
if [[ "$todo_violation" -ne 0 ]]; then
  echo "VWP P-VW-004 VIOLATION: stub(s) above are not surfaced in the attestation." >&2
  status=1
else
  echo "ok: all stubs surfaced in $ATTESTATION"
fi

echo
if [[ "$status" -eq 0 ]]; then
  echo "VWP lint: PASS"
else
  echo "VWP lint: FAIL" >&2
fi
exit "$status"
