# VWP Self-Attestation — GitLab Valet (`glv`)

- **Generated:** 2026-07-15 (supersedes the 2026-06-02 P0–P18 attestation)
- **Source spec:** CKODEX VWP v0.1 §26.E (`CLAUDE.md`)
- **Scope:** Phases P0–P19 of the GitLab Valet implementation, including the
  GitLab-first work reconciler and durable receipt substrate.
- **Self-signing:** unsigned in-repo; `make attest-sign` cosign-signs this file
  (keyless in CI via `SIGSTORE_ID_TOKEN`). Signing executes at the GA tag, alongside
  the release artifacts (DEF-02, P17).

> **Truth note (P11, updated P19).** This document corrects two stale claims in the 2026-05-28
> version: (1) DEF-01 (`client.New` flag routing) was claimed `[A]` deferred but was
> **completed in P8** — `client.New` now selects the provider via
> `providerfactory.NewFromConfig` (`internal/client/client.go:56-63`); it is reclassified
> `[C]`. (2) the old "Stubs remaining: 0" claim was false. P19 removes the
> plan-linkage deferral; four intentional GitHub `ErrUnsupported` TODOs remain
> surfaced in *Stubs remaining* (P-VW-004).

---

## Summary

| Phase | Capability | Classification |
|---|---|---|
| P0 | `go build ./...` and `go mod tidy` succeed on the GitLab-only baseline | C |
| P1 | `internal/config` host resolution covered by unit tests | C |
| P2 | `internal/journal` JSONL ledger covered by unit tests | C |
| P3 | `internal/cache`, `internal/parallel`, and `internal/report` covered by unit tests | C |
| P4 | `httptest`-based integration test exercises the real GitLab API path via go-gitlab SDK | C |
| P5 | Host-neutral `provider.Provider` interface introduced; GitLab adapter wraps the existing client | C |
| P6 | GitHub adapter introduced under `GLVALET_GITHUB_ENABLED=true` feature flag using the real go-github SDK | C |
| P7 | `httptest`-based integration test exercises the GitHub adapter wire path; `github` package coverage is 88.3% | C |
| P8 | `client.New` routes via `providerfactory` (honors `GLVALET_PROVIDER`); `cmd/issue.go` create/update/close/comment migrated to `Provider` | C |
| P9 | `Provider.CreateLabel` added (both adapters + TDD tests); `cmd/label.go` list+create migrated | C |
| P10 | `Provider.CreateMergeRequest`/`UpdateMergeRequest` added (both adapters + TDD tests); `cmd/mr.go` create+close migrated | C |
| P11 | GA honesty baseline: build/vet/fmt clean; 7 non-loopback pkgs pass (84.8–100% cover); marker + raw-`.GL.` inventory captured | C |
| P12 | Fail-loud host guard (`cmd/hostguard.go`): under a non-GitLab provider, not-yet-migrated commands error instead of silently querying GitLab; 3 unit tests pass in-sandbox | C |
| P13 | `Provider.ListMyIssues` added (both adapters + both mocks + TDD httptest tests); `cmd/issue.go` `mine` migrated; TODO removed | C |
| P14 | MR surface: `ListMyMergeRequests`/`ApproveMergeRequest`/`MergeMergeRequest` + `MergeRequest.Pipeline` added; GitLab full, GitHub merge + 2 `ErrUnsupported`; `cmd/mr.go` list/mine/approve/merge migrated (diff deferred) | C |
| P15 | `provider.FileDiff` + `GetMergeRequestDiff` added (both adapters + mocks + TDD tests); `cmd/mr.go` diff migrated and the GitLab SDK import removed — `mr` is now 100% Provider-based | C |
| P16 | `ListIssuesOptions.Milestone` (title) added; `cmd/issue.go` list + `cmd/label.go` sync migrated to `Provider`; both files drop the GitLab SDK import. **All host-neutral command surfaces (issue/mr/label) are now 100% Provider-based; zero command-layer migration TODOs remain.** | C |
| P17 | Release pipeline: `make checksums`/`sbom`/`release` targets + tag-driven CI `release` stage (cosign keyless via GitLab OIDC). Local assembly verified (5 cross binaries + verified SHA256SUMS + valid CycloneDX SBOM, 220 components); cosign sign/verify runs on first tag | C (assembly) / S (signing) |
| P18 | GA cutover: `glv --version` wired (`main.version` ldflag now has a symbol; Cobra `--version`/`-v`); README host-capability matrix + cosign-verify docs + stale "not routed" claim corrected; `journal_test.go` root-skip removed (de-skipped via ENOTDIR, runs everywhere); `docs/milestone-ga.md` + `make attest-sign` | C |
| P19 | SQLite intent/outcome receipts, one-time JSONL import/export, plan-v2 dependency graph, GitLab epic linkage, and stop-and-resume reconciliation | C |
| DEF-01 | `client.New` flag/env routing to the selected provider | **C (done in P8)** |
| DEF-02 | Signed release artifacts (cosign + SBOM) | **pipeline C / signature S (P17)** |
| DEF-03 | BPL (back-propagation lineage) for promotion-critical artifacts | A |
| RES-01 | Command-layer provider migration **complete** for host-neutral surfaces — 42 raw `.GL.` sites remain across 10 GitLab-only `cmd/` files; GitHub host is therefore `[S]` experimental | S |

---

## Capability attestations

```yaml
capability_id:    p0-baseline-build
claim:            "go build ./..." and "go mod tidy" complete without errors on the GitLab-only baseline.
classification:   C
evidence_refs:
  - kind:  test_output
    hash:  7c348c8fb0faf1343d873717113d2d3996ffc28050136bd4a8add39427f12945
    path:  docs/evidence/p7-full-suite.txt
  - kind:  artifact
    hash:  1ca4a13146b2891c0e462123ac0cb848f92e6221f18b870a1b78c232d1956c35
    path:  docs/evidence/p7-vet.txt
stubs_remaining:  0
deferred:         []
```

```yaml
capability_id:    p1-config-unit-tests
claim:            internal/config host-resolution logic is covered by unit tests that pass in the full suite run.
classification:   C
evidence_refs:
  - kind:  test_output
    hash:  7c348c8fb0faf1343d873717113d2d3996ffc28050136bd4a8add39427f12945
    path:  docs/evidence/p7-full-suite.txt
  - kind:  artifact
    hash:  ~
    path:  internal/config/
stubs_remaining:  0
deferred:         []
```

```yaml
capability_id:    p2-journal-unit-tests
claim:            internal/journal JSONL ledger logic is covered by unit tests that pass in the full suite run.
classification:   C
evidence_refs:
  - kind:  test_output
    hash:  7c348c8fb0faf1343d873717113d2d3996ffc28050136bd4a8add39427f12945
    path:  docs/evidence/p7-full-suite.txt
  - kind:  artifact
    hash:  ~
    path:  internal/journal/
stubs_remaining:  0
deferred:         []
```

```yaml
capability_id:    p3-cache-parallel-report-unit-tests
claim:            internal/cache, internal/parallel, and internal/report packages are covered by unit tests that pass in the full suite run.
classification:   C
evidence_refs:
  - kind:  test_output
    hash:  7c348c8fb0faf1343d873717113d2d3996ffc28050136bd4a8add39427f12945
    path:  docs/evidence/p7-full-suite.txt
  - kind:  artifact
    hash:  ~
    path:  internal/cache/
  - kind:  artifact
    hash:  ~
    path:  internal/parallel/
  - kind:  artifact
    hash:  ~
    path:  internal/report/
stubs_remaining:  0
deferred:         []
```

```yaml
capability_id:    p4-gitlab-httptest-integration
claim:            An httptest-based integration test exercises the real go-gitlab SDK path end-to-end and passes in the full suite run.
classification:   C
evidence_refs:
  - kind:  test_output
    hash:  7c348c8fb0faf1343d873717113d2d3996ffc28050136bd4a8add39427f12945
    path:  docs/evidence/p7-full-suite.txt
  - kind:  artifact
    hash:  ~
    path:  internal/client/client.go
stubs_remaining:  0
deferred:         []
```

```yaml
capability_id:    p5-provider-interface
claim:            A host-neutral provider.Provider interface is defined in internal/provider/provider.go and the GitLab adapter under internal/provider/gitlab/ satisfies it.
classification:   C
evidence_refs:
  - kind:  test_output
    hash:  7c348c8fb0faf1343d873717113d2d3996ffc28050136bd4a8add39427f12945
    path:  docs/evidence/p7-full-suite.txt
  - kind:  artifact
    hash:  ~
    path:  internal/provider/provider.go
  - kind:  artifact
    hash:  ~
    path:  internal/provider/gitlab/
stubs_remaining:  0
deferred:         []
```

```yaml
capability_id:    p6-github-adapter-feature-flag
claim:            A GitHub adapter in internal/provider/github/github.go uses the real go-github SDK and is gated behind the GLVALET_GITHUB_ENABLED=true feature flag.
classification:   C
evidence_refs:
  - kind:  test_output
    hash:  6ccea4eb2fa9b832635d3be8aece65db80bf6b084a157b20fef73b4ddf608218
    path:  docs/evidence/p7-test-output.txt
  - kind:  test_output
    hash:  7c348c8fb0faf1343d873717113d2d3996ffc28050136bd4a8add39427f12945
    path:  docs/evidence/p7-full-suite.txt
  - kind:  artifact
    hash:  ~
    path:  internal/provider/github/github.go
stubs_remaining:  0
deferred:         []
```

```yaml
capability_id:    p7-github-httptest-coverage
claim:            An httptest-based integration test in internal/provider/github/github_integration_test.go (273 LOC) exercises the GitHub adapter wire path; go tool cover reports 88.3% coverage for the github package; all per-function LOC values are at or below 50 (max observed: 43); go vet ./... produces no output.
classification:   C
evidence_refs:
  - kind:  test_output
    hash:  ad5c06e1e0f9b9d1950fdcdde25e616efd066f712c78f687dff5ba984d5ffd15
    path:  docs/evidence/p7-coverage.txt
  - kind:  test_output
    hash:  8be5473822fe22bb2a9a7da9ca9d27a5ce4d6d87b443d922bfdb67c60a6e0b23
    path:  docs/evidence/p7-file-loc.txt
  - kind:  test_output
    hash:  7c348c8fb0faf1343d873717113d2d3996ffc28050136bd4a8add39427f12945
    path:  docs/evidence/p7-full-suite.txt
  - kind:  test_output
    hash:  be54657e4535a45747e5161fa37a7755a7bfd4937a0b6bc4825164928fc0fe17
    path:  docs/evidence/p7-function-loc.txt
  - kind:  test_output
    hash:  6ccea4eb2fa9b832635d3be8aece65db80bf6b084a157b20fef73b4ddf608218
    path:  docs/evidence/p7-test-output.txt
  - kind:  proof
    hash:  1ca4a13146b2891c0e462123ac0cb848f92e6221f18b870a1b78c232d1956c35
    path:  docs/evidence/p7-vet.txt
  - kind:  artifact
    hash:  ~
    path:  internal/provider/github/github_integration_test.go
  - kind:  artifact
    hash:  ~
    path:  internal/provider/github/github_test.go
  - kind:  artifact
    hash:  ~
    path:  internal/provider/github/github_wire_test.go
stubs_remaining:  0
deferred:         []
```

```yaml
capability_id:    p8-client-routing-and-issue-migration
claim:            client.New selects the provider via providerfactory.NewFromConfig (honoring GLVALET_PROVIDER); cmd/issue.go create/update/close/comment call glClient.Provider.* instead of glClient.GL.*. Build, vet, and gofmt are clean; ./cmd and ./internal/provider tests pass (no httptest).
classification:   C
evidence_refs:
  - kind:  test_output
    hash:  a40052187a6ba44575c69f6c266fef2e052ce2e5a0305dafb0ad935e59c0b99b
    path:  docs/evidence/p8-cli-migration.txt
  - kind:  artifact
    hash:  ~
    path:  internal/client/client.go
stubs_remaining:  2   # cmd/issue.go:41 (list), cmd/issue.go:129 (mine) — surfaced below
deferred:         ["issue list milestone-title filter", "cross-project ListMyIssues"]
```

```yaml
capability_id:    p9-label-migration
claim:            provider.Provider gained CreateLabel (both adapters) and provider.Label gained OpenIssuesCount; 4 TDD tests added; cmd/label.go list+create migrated to glClient.Provider.*. Build/vet/gofmt clean; ./cmd and ./internal/provider tests pass.
classification:   C
evidence_refs:
  - kind:  test_output
    hash:  a559ddfab7bda5b2d3fcad7b6e3889a80a4c350261603867f845bbda30291513
    path:  docs/evidence/p9-label-migration.txt
  - kind:  artifact
    hash:  ~
    path:  internal/provider/provider.go
stubs_remaining:  1   # cmd/label.go:115 (cross-host sync) — surfaced below
deferred:         ["cross-host label sync via per-host providers"]
```

```yaml
capability_id:    p10-mr-migration
claim:            provider.Provider gained CreateMergeRequest and UpdateMergeRequest with option types (both adapters); 4 TDD tests added; cmd/mr.go create+close migrated. Build/vet/gofmt clean; ./cmd and ./internal/provider tests pass.
classification:   C
evidence_refs:
  - kind:  test_output
    hash:  87ae087f80bcea10d47c12805c77941f3221a88969323b00e59ccef3638c4b3b
    path:  docs/evidence/p10-mr-migration.txt
  - kind:  artifact
    hash:  ~
    path:  internal/provider/provider.go
stubs_remaining:  1   # cmd/mr.go:31 (list/mine/approve/merge/diff) — surfaced below
deferred:         ["MR list/mine/approve/merge/diff neutral methods"]
```

```yaml
capability_id:    p11-ga-honesty-baseline
claim:            On 2026-06-02, go build ./... exits 0, go vet ./... exits 0, and gofmt -l . is empty. The 7 packages that do not use httptest loopback servers pass with coverage 84.8%-100% (config 84.8, plan 86.7, cache 92.9, journal 95.7, report 98.0, parallel 100.0, providerfactory 100.0). The remaining 3 packages (internal/client, internal/provider/gitlab, internal/provider/github) use httptest.NewServer loopback listeners which this sandbox denies (bind: operation not permitted); they are CI-verified, not sandbox-verified. A marker inventory records 0 cmd-layer TODOs + 4 GitHub ErrUnsupported TODOs and 42 raw .GL. call sites across 10 GitLab-only cmd files.
classification:   C
evidence_refs:
  - kind:  test_output
    hash:  328dde5f2bf9b6fba47b0a0df85c8f0b4bf2f5984f039b602c8d5b4e475b7ece
    path:  docs/evidence/p11-baseline-build.txt
  - kind:  test_output
    hash:  6d19ab3f46e6e439ab52b0369bc705970071f479c5edca4d5a3e378aa583e83e
    path:  docs/evidence/p11-baseline-vet-fmt.txt
  - kind:  test_output
    hash:  162bb4b7e6791e2fa40c5ba6a4bb604cc9bcf396dd04287218c99b3a26214427
    path:  docs/evidence/p11-baseline-coverage-sandbox.txt
  - kind:  test_output
    hash:  3a834f19ad5df579a4ba99e985062a3827e49ccf2113e83892f8517078e050e1
    path:  docs/evidence/p11-marker-inventory.txt
stubs_remaining:  9   # see Stubs remaining
deferred:         ["full command-layer provider migration (RES-01)", "DEF-02 cosign", "DEF-03 BPL"]
```

---

## Deferred items

- **DEF-01 — `client.New` provider routing. ✅ DONE (P8).** `client.New`
  (`internal/client/client.go:56-63`) now selects the provider via
  `providerfactory.NewFromConfig`, honoring `GLVALET_PROVIDER` (`gitlab`|`github`).
  Reclassified `[C]`. ADR-001 Consequences updated accordingly.
- **DEF-02 — Signed release artifacts. pipeline [C] / signature [S] (P17).** A tag-driven
  CI `release` stage now builds 5 cross-platform binaries, writes a verified `SHA256SUMS`,
  generates a CycloneDX SBOM (Syft), and cosign-signs the checksums **keyless** via GitLab
  OIDC (`id_tokens.SIGSTORE_ID_TOKEN`), then `cosign verify-blob`s in-job. The artifact
  assembly (`make release`) is verified locally (`docs/evidence/p17-release-pipeline.txt`);
  the cosign signature itself executes on the first tag pipeline (needs the OIDC token).
- **DEF-03 — BPL (back-propagation lineage) for promotion-critical artifacts. [A]** No
  promotion pipeline exists. Deferred until a staging/prod promotion path is established.
- **RES-01 — Command-layer provider migration. [S] → host-neutral surfaces complete.**
  The **entire `issue`, `mr`, and `label` command surfaces are now 100% `Provider`-based**
  (P8–P16); `cmd/issue.go`, `cmd/mr.go`, and `cmd/label.go` no longer import the GitLab SDK,
  and **zero command-layer migration TODOs remain**. **42** raw `client.GL.*` sites remain
  (was 53), all in **GitLab-only commands** that have no GitHub equivalent and are correctly
  GitLab-native: `sync.go`(9), `shields.go`(6), `epic.go`(5), `tui.go`(4), `search.go`(4),
  `renovate.go`(4), `milestone.go`(4), `standup.go`(3), `timeline.go`(2), `report.go`(1).
  Under `GLVALET_PROVIDER=github` these **fail loud** via the P12 host guard
  (`cmd/hostguard.go`); on GitHub, host-neutral commands that lack a GitHub analogue
  (`mr mine`/`mr approve`, `issue list --milestone`) dispatch through `Provider` and return
  `ErrUnsupported`. **GitHub remains `[S]` experimental** until a live-instance integration
  job runs in CI (currently flag-guarded, token-required).

---

```yaml
capability_id:    p19-work-reconciler
claim:            SQLite intent/outcome receipts and GitLab-first plan-v2 reconciliation with durable stop-and-resume checkpoints are implemented and tested.
classification:   C
evidence_refs:
  - kind:  test_output
    hash:  8111e08ad7278cd3a0d6e79d20e6ae62d37e8ca5b6126944f399d64d3da8cdd2
    path:  docs/evidence/p19-reconciler.txt
  - kind:  artifact
    hash:  99851e9556ee3017caa19ac8a0f39923e5df93d8ee1dc8786e4d1540facb794a
    path:  docs/milestone-reconciler.md
stubs_remaining:  4
deferred:         ["GitHub reconciliation", "automatic rollback", "autonomous convergence"]
```

## Stubs remaining

Count: **4** (was incorrectly reported as 0 on 2026-05-28; 9 at P11). All are surfaced
here per P-VW-004. None are silent. **Zero remain in the command-migration class** — all
cleared P13–P16.

**Intentional GitHub host-capability limits (4) — `ErrUnsupported`, not migration debt:**

| File:line | Marker | Intent |
|---|---|---|
| `internal/provider/github/github.go:410` | `TODO(ckodex)` | repo-scoped milestone creation (GitHub milestones are repo-, not group-scoped) |
| `internal/provider/github/github.go:416` | `TODO(ckodex)` | optional epic→tracking-issue mapping |
| `internal/provider/github/github.go:423` | `TODO(ckodex)` | repo-scoped milestone listing |
| `internal/provider/github/github.go:429` | `TODO(ckodex)` | optional epic→label-filtered-issues mapping |

> ✅ Command-migration TODOs cleared: `issue mine` (P13); `mr` list/mine/approve/merge (P14);
> `mr diff` (P15); `issue list` + `label sync` (P16). `cmd/issue.go`, `cmd/mr.go`,
> `cmd/label.go` no longer import the GitLab SDK.

**GitHub host-capability limits (4) — intentional `ErrUnsupported`, not migration debt:**

| File:line | Marker | Intent |
|---|---|---|
| `internal/provider/github/github.go:410` | `TODO(ckodex)` | repo-scoped milestone creation (GitHub milestones are repo-, not group-scoped) |
| `internal/provider/github/github.go:416` | `TODO(ckodex)` | optional epic→tracking-issue mapping |
| `internal/provider/github/github.go:423` | `TODO(ckodex)` | repo-scoped milestone listing |
| `internal/provider/github/github.go:429` | `TODO(ckodex)` | optional epic→label-filtered-issues mapping |

These 4 return `provider.ErrUnsupported` by design (no GitHub equivalent); they are
correct behavior on a GitHub host, not incomplete work, and do not block GitLab GA.
