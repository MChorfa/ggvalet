# AGENTS.md — ggvalet

Welcome to `ggvalet` (`github.com/MChorfa/ggvalet`). This repository implements a CKODEX-governed multi-instance GitLab agent, Day-2 control plane, and federated delivery coordinator.

---

## 1. Architectural Signature & Core Principles

All code in this repository MUST comply with the **CKODEX Codex Constitution**:
1. **Pure Kernel**: Pure execution semantics in `internal/agent`, `internal/vector`, `internal/authority`, `internal/policy` do not depend outward on transport or database engines.
2. **Zero Standing Privilege**: All operations are gated by explicit `authority.Lease` capabilities (`valet-observer`, `valet-advisor`, `valet-reconciler`, `valet-admin-test`).
3. **Proof Before / Receipt After**: Mutations require pre-execution proof of authority and preconditions; post-execution evidence is committed to SQLite WAL (`internal/state`).
4. **Vector State, Not Booleans**: Governed reality is modeled as a 6-tuple product: Presence, Valence, Coherence, Evidence, Lifecycle, and Monotonic Epoch. Anti-invariants dominate aggregate scores.
5. **Bounded Resilience**: Remote operations are bound by circuit breakers (`internal/resilience/circuit_breaker.go`), token-bucket rate limiters (`rate_limiter.go`), and jittered retries (`retry.go`).
6. **Anti-State Conflict Quarantine**: Divergent concurrent states are isolated into conflict quarantine (`internal/syncindex/quarantine.go`) rather than silently overwritten.

---

## 2. Milestone Architecture Map

- **Milestone 0 (`internal/lab`, `internal/trustwall`, `internal/transfer`)**: Delivery laboratory simulation, Trustwall invariant admission, and unidirectional diode air-gap packaging with Merkle roots.
- **Milestone 1 (`internal/controlplane`, `internal/policy`)**: Control plane engine, SQLite WAL schema, `ggvalet audit`, `ggvalet reconcile`, and progressive policy rules engine (`ggvalet rules`).
- **Milestone 2 (`internal/resilience`, `internal/syncindex`)**: Bounded resilience, cryptographic SHA-256 entity markers (`<!-- ggvalet-sync-src: URL | sha256:... -->`), drift evaluation, and conflict quarantine.
- **Milestone 3 (`internal/agent`)**: Next-gen autonomous agent loop, typed `IntentEnvelope`, refusal pivoting, and immutable trajectory ledger.
- **Milestone 4 (`internal/lifecycle`)**: Persona lifecycle engine for 24/7 Day-2 operations across 5 personas (`user`, `project`, `agent`, `service`, `auditor`), Golden Pipeline v7.0.0 template generation, branch protection JSON, and runner limits.

---

## 3. Developer & Agent Workflow

### Verification Commands
- `make test`: Run unit tests across all 28 packages.
- `make cover`: Run full test suite with coverage profile and enforce `.coverage-floors` and total $\ge 80\%$ gate (`scripts/coverage-check.sh`).
- `make cover-p19`: Run critical path coverage gate ($\ge 80\%$).
- `make vwp`: Run VWP §26 governance linter (ensures zero hedge words, zero marketing vocab, and all stubs surfaced).
- `make docs`: Regenerate `pages/docs/commands.md` and rebuild the documentation site. Always run after adding or modifying CLI flags.
- `make regression`: Complete verification pass (`test`, `cover`, `cover-p19`, `vwp`, `govulncheck`, `cross`).

---

## 4. Release Flow

**Do NOT run `make release-publish` locally.** The `.github/workflows/release.yml` workflow is the canonical release path. It builds, cosign-signs (keyless via GitHub Actions OIDC identity), and publishes all assets to GitHub Releases.

### Canonical Release Sequence
1. `make regression` — verify all gates pass locally.
2. `git commit` + `git tag -a v0.4.x -m "..."`.
3. `git push origin main && git push origin v0.4.x`.
4. Watch CI: `gh run watch <run-id> --repo MChorfa/ggvalet --exit-status`.
5. Verify: `gh release view v0.4.x --repo MChorfa/ggvalet`.
