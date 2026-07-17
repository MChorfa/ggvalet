<!--
CKODEX DoD² + VWP §26 merge-request template.
The `vwp` and `test` CI jobs enforce the mechanical subset; this template enforces
the human subset. Do not delete sections — fill or write "n/a" with a reason.
-->

## Summary

<!-- What changed and why. State FACT vs ASSUMPTION vs SPECULATION where ambiguous. -->

## Capability classification (P-SC-003 — required)

<!-- One row per capability claim. C = typed+tested+enforceable today;
     S = design-locked, partial impl; A = aspirational. -->

| Capability | Class (C/S/A) | Evidence ref (path#hash / job artifact) |
| ---------- | ------------- | --------------------------------------- |
|            |               |                                         |

## Evidence refs (P-VW-001 — required, non-empty for any C claim)

<!-- Paths under docs/evidence/ or CI artifacts (cover.out, sbom.cdx.json) that
     prove each C claim. A claim of "implemented/tested/X%" with no ref is blocked. -->

-

## Stubs surfaced (P-VW-004)

<!-- Every TODO/FIXME added in this MR, by file:line. "None" if none.
     The `vwp` CI job fails if a Go stub is not referenced in docs/VWP-ATTESTATION.md. -->

-

## Checklist (DoD²)

- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .` clean
- [ ] `go test ./...` passes; coverage ≥ 80% (CI `test` job green)
- [ ] Dependencies resolve and `go.mod`/`go.sum` tidy (CI `deps` job green)
- [ ] No new hedge / marketing language; stubs surfaced (CI `vwp` job green)
- [ ] ADRs updated if an architecture decision changed
- [ ] No secrets, tokens, or PII in code, logs, or journal entries

## Risks & failure modes

<!-- What could break, and how it fails (fail-loud per Rule 12). -->

-
