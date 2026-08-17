# Wave 2 Milestone — Coverage gates, SQLite journal projection, and project hygiene

- **Date:** 2026-07-26
- **Scope:** Harden the per-package coverage gate, migrate journal/report/standup/tui
  queries to a SQLite projection with JSONL backfill and fallback, add generated
  command-reference docs, and establish dependency + security hygiene.
- **Governance bar:** CKODEX VWP §26 evidence gates + DoD².

---

## Verdict

All Wave 2 targets are implemented and mechanically verified.

- Per-package coverage floors are now enforced in `make cover` and CI via
  `scripts/coverage-check.sh` + `.coverage-floors`.
- `internal/state` stores a `journal_entries` projection; `internal/client` writes
  to both SQLite and the legacy JSONL journal and queries SQLite first with JSONL
  fallback. `cmd/journal`, `cmd/report`, `cmd/standup`, and `cmd/tui` consume the
  new `QueryEntries` abstraction.
- `make docs` generates `pages/docs/commands.md` from live `ggvalet --help` output
  and builds the docs site; `make site-lint` proves the build-site script parses
  and executes.
- `.github/dependabot.yml` automates Go module and GitHub Actions dependency PRs.
- `docs/SECURITY.md` documents supported versions, vulnerability reporting, and the
  existing security checks.

## Verification contract

The release gate is `make regression`:

- `go test ./...` passes
- `make cover` passes per-package floors + 80% total
- `make cover-p19` passes 80% P19 slice
- `make vwp` passes
- `govulncheck ./...` reports no called vulnerabilities
- `make cross` builds for five targets
- `make docs` builds the docs site and regenerates `commands.md`

Detailed coverage outputs are recorded in:

- `docs/evidence/p20-wave2-coverage.txt` (sha256: `686293686ce63deef9ce52c082f9a5fa5181e549531a0485d2776300f352a63a`)
- `docs/evidence/p20-wave2-p19-coverage.txt` (sha256: `8bccc87dd952455d75459289e9ceef2b93b0fb13e36cb56970834bcbd326bec9`)

## Boundaries

- The generated command reference covers top-level commands only. Subcommand detail
  is available via each command's own `--help` output, which the page includes.
- Dependabot is configured for GitHub; the project also uses GitLab CI, where the
  same dependency updates are picked up by Renovate or manual `go mod tidy` drift
  checks.
- The security policy uses a generic private-channel reporting path; operators
  should replace it with a project-specific contact if one exists.

## Next

1. Tag `v0.4.2` once the Wave 2 attestation is cosign-signed.
2. GitHub path → GA: add a live-instance CI integration job, then bump to `v1.0.0`.
