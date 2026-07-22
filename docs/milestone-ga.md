# GA Milestone Report — ggvalet (`ggvalet`)

- **Date:** 2026-06-02
- **Scope:** GA readiness for `ggvalet` (GitLab path GA; GitHub `[S]` experimental).
- **Governance bar:** CKODEX VWP §26 evidence gates + DoD². CATM `[A]` threat
  detectors are out of scope (per the approved GA plan).

---

## Verdict

**GitLab path: GA-ready pending one CI run on a tag.** Every release-blocking
gate is mechanically enforced in CI, the provider abstraction is complete for
all host-neutral surfaces, the self-attestation is truthful (count verified
against `grep`), and the signed-release pipeline's artifact assembly is proven
locally. The only step not yet *executed* is the cosign signature, which runs on
the first tag pipeline (it requires a GitLab OIDC token unavailable off-CI).

**GitHub path: `[S]` experimental.** Adapter implemented and unit-tested; routed
via `GLVALET_PROVIDER=github`; non-neutral commands fail loud. Promotion to GA
needs a live-instance integration job (`GLVALET_GITHUB_ENABLED=true` + token).

---

## Artifacts & evidence

| Area | Artifact | Evidence |
|------|----------|----------|
| Honesty baseline | `docs/VWP-ATTESTATION.md` (P0–P17), ADR-001 corrected | `docs/evidence/p11-*` |
| CI evidence gates | `.gitlab-ci.yml` (test+coverage+deps+vuln+secrets+sbom+vwp), `scripts/vwp-lint.sh`, MR template | `docs/evidence/p12-ci-gates.txt` |
| Provider migration | `issue`/`mr`/`label` 100% `Provider`-based; `cmd/hostguard.go` fail-loud guard | `docs/evidence/p13–p16` |
| Signed release | `make checksums`/`sbom`/`release`, CI `release` stage (cosign keyless + SBOM) | `docs/evidence/p17-release-pipeline.txt` |
| GA cutover | `ggvalet --version` wired; README host matrix + verify docs; root-skip removed | this report |

## Freshness

- Historical GA baseline used Go 1.22. P19 raises the current baseline and CI image to Go 1.24.
- syft 1.44.0 / cosign v3.0.6 used locally; CI installs current in-job.

## Locks / gates compiled

- `test` (≥80% coverage), `deps` (verify+tidy), `vuln` (govulncheck), `secrets`
  (gitleaks), `sbom` (syft), `vwp` (§26 lint), `release` (cosign keyless) — all
  fail-closed.

---

## Risks

1. **GitHub is `[S]`, not GA** — 10 GitLab-only command surfaces have no GitHub
   analogue and fail loud under `GLVALET_PROVIDER=github` (by design).
2. **Release signature unexecuted** — proven only by local assembly until the
   first tag pipeline runs (honest `pipeline [C] / signature [S]` split).
3. **Loopback integration tests are CI-only** — httptest suites can't bind in
   the dev sandbox; they run in the CI container. Pure logic (e.g.
   `projectFromRef`) has sandbox-runnable unit tests.

## Next

1. **Cut the GA tag `v0.1.0`** (decided 2026-06-02; pre-1.0 GA — reserves
   `v1.0.0` for when the GitHub path also reaches GA). The Makefile derives the
   version from `git describe`, so the tag alone sets it; pushing it triggers the
   `release` pipeline → signed artifacts + SBOM.
2. Cosign-sign this attestation at release (`make attest-sign`).
3. GitHub → GA: add a live-instance CI integration job, then bump to `v1.0.0`.

## Residual stub surface (superseded by P19)

The P19 reconciler implements epic-to-issue linkage. Four intentional GitHub
`ErrUnsupported` host limits remain; zero command-migration TODOs remain.
