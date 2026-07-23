# ggvalet

A personal, multi-instance agent for GitLab, GitHub, and (experimental) Gitea.
Every provider operation produces durable intent and outcome receipts in
`~/.ggvalet/state.db`; the legacy JSONL journal remains available for reports
and backward compatibility.

- Latest release: https://github.com/MChorfa/ggvalet/releases/latest
- Docs site: https://mchorfa.github.io/ggvalet/

Reads your existing **glab CLI config** (`~/.config/glab-cli/config.yml`) or
**tea CLI config** (`~/.config/tea/config.yml`): tokens, `skip_tls_verify`, and
`api_host` / `url` are inherited per instance. No separate setup.

---

## Install

### From a release

```bash
# macOS (Apple Silicon)
curl -L -o ggvalet.tar.gz https://github.com/MChorfa/ggvalet/releases/latest/download/ggvalet_0.2.1_darwin_arm64.tar.gz
tar -xzf ggvalet.tar.gz
mv ggvalet ~/.local/bin/

# Or use the install script
curl -L https://mchorfa.github.io/ggvalet/install.sh | bash
```

Replace `darwin_arm64` with `darwin_amd64`, `linux_amd64`, `linux_arm64`, or
`windows_amd64` as needed. Pre-built binaries, SBOMs, and cosign signatures are
available on every [release](https://github.com/MChorfa/ggvalet/releases).

### From source

```bash
go mod tidy
make build           # → ./ggvalet
make install         # → $GOBIN/ggvalet
make cross           # → dist/ (darwin, linux, windows)
ggvalet --version    # prints the build tag
```

---

### Verify a signed release

Tagged releases ship cross-platform binaries, a `SHA256SUMS`, a CycloneDX SBOM
(`sbom.cdx.json`), and a cosign **keyless** signature over the checksums:

```bash
sha256sum -c SHA256SUMS                                   # checksums match

RELEASE_PROJECT_URL="https://github.com/MChorfa/ggvalet"
RELEASE_TAG="v0.2.1"
RELEASE_ISSUER="https://token.actions.githubusercontent.com"

cosign verify-blob SHA256SUMS \
  --signature SHA256SUMS.sig \
  --certificate SHA256SUMS.pem \
  --certificate-identity "${RELEASE_PROJECT_URL}/.github/workflows/release.yml@refs/tags/${RELEASE_TAG}" \
  --certificate-oidc-issuer "${RELEASE_ISSUER}"
```

Then confirm the smoke check:

```bash
ggvalet doctor
ggvalet hosts
```

---

## Feature map

| Group | Commands |
|---|---|
| **CRUD** | `issue`, `epic`, `milestone`, `wi` (work items), `label`, `mr` |
| **Sync** | `sync issues`, `sync epics`, `sync milestones`, `label sync` |
| **Intelligence** | `search` (multi-host), `renovate` (triage), `standup` |
| **Visual** | `tui`, `timeline` (Gantt), `shields` (badges + chips) |
| **Reporting** | `report`, `report push`, `journal show`, `journal stats`, `receipt export` |
| **Reconcile** | `plan validate`, `plan diff`, `plan apply`, `plan status`, `plan explain`, `plan resume` |
| **Ops** | `hosts`, `cache stats`, `cache flush`, `doctor` |

---

## Quick start

```bash
ggvalet hosts                              # list configured instances
ggvalet issue mine                         # my open issues, default host
ggvalet --host gitlab.example.com issue mine
ggvalet tui                                # interactive browser
ggvalet report --since 7d --author "Name"  # weekly report
ggvalet standup --since 24h                # daily standup
```

See the [docs site](https://mchorfa.github.io/ggvalet/) for the full command
reference and `BUILD_PROMPT.md` for a from-scratch build guide.

---

## Architecture

- `cmd/` — Cobra CLI surfaces and host-guard logic.
- `internal/config` — loads glab config, env overrides, and per-host client config.
- `internal/provider` — host-neutral `Provider` interface.
- `internal/provider/gitlab/` — GitLab REST adapter.
- `internal/provider/github/` — GitHub REST adapter (opt-in, experimental).
- `internal/provider/gitea/` — Gitea REST adapter (experimental).
- `internal/observed` — persists intent before each remote call and outcome
  after; failure to persist intent blocks the call.
- `internal/state` — SQLite store for receipts and plan runs (authoritative).
- `internal/journal` — legacy JSONL journal, imported once and kept for reports.
- `internal/cache` — TTL disk cache for API responses.
- `internal/plan`, `internal/reconcile` — resumable, idempotent plan engine.

Provider calls route through `internal/observed`, which writes an intent before
the remote call and an outcome afterward. Failure to persist intent prevents the
call; failure to persist an outcome marks it uncertain. The legacy journal is
imported once and remains the report/standup compatibility surface.

---

## Resumable plans

Plan version 2 adds explicit dependencies and resumable, idempotent application.
The first release targets GitLab milestones, epics, issues, weights, milestone
assignment, and epic linkage. It stops at the first failed step and never
auto-deletes remote resources.

```yaml
version: "2"
target: { provider: gitlab, group_id: 42, project_id: 84 }
milestones:
  - { id: m1, title: "Release 1" }
epics:
  - { id: e1, title: "Trust substrate", depends_on: [m1] }
issues:
  - id: i1
    title: "Persist operation receipts"
    milestone: m1
    epic: e1
    weight: 3
```

```bash
ggvalet plan validate plan.yaml
ggvalet plan diff plan.yaml
ggvalet plan apply plan.yaml                         # dry-run
ggvalet plan apply plan.yaml --dry-run=false --yes   # starts a durable run
ggvalet plan status RUN_ID
ggvalet plan explain RUN_ID
ggvalet plan resume RUN_ID
ggvalet receipt export -o receipts.jsonl
```

---

## Providers

The codebase exposes a host-neutral `Provider` interface at
`internal/provider/provider.go`. Adapters implement that interface; the active
adapter is selected at startup.

- `client.New(cfg)` selects the adapter via `providerfactory.NewFromConfig`.
- `GLVALET_PROVIDER` selects the provider: `gitlab` (default), `github`, or `gitea`.
- `GLVALET_HOST` scopes commands to a single configured host.

**GitLab adapter** (`internal/provider/gitlab/`) — exercised by every `ggvalet`
command. The `issue`, `mr`, and `label` surfaces are fully host-neutral; the
remaining commands (`epic`, `milestone`, `sync`, `search`, `standup`,
`timeline`, `renovate`, `shields`, `tui`, `report`) are GitLab-specific.

**GitHub adapter** (`internal/provider/github/`) — opt-in **`[S]` experimental**.
Requires both `GLVALET_PROVIDER=github` and `GLVALET_GITHUB_ENABLED=true`
(otherwise `github.New` returns `ErrFeatureDisabled`). A command that is not yet
host-neutral **fails loud** under a non-GitLab provider rather than silently
querying GitLab (`cmd/hostguard.go`).

### Host-capability matrix

| Command surface | GitLab | GitHub | Gitea |
|---|---|---|---|
| `issue` (list/mine/get/create/update/close/comment) | GA | `list --milestone` unsupported | `list --milestone` unsupported |
| `mr` (list/mine/create/approve/merge/diff/close) | GA | `mine`, `approve` unsupported | `mine` unsupported |
| `label` (list/create/sync) | GA | GA | GA |
| `epic`, group `milestone` | GA | no equivalent | no equivalent |
| `report`, `journal`, `hosts`, `doctor`, `cache`, `receipt` | GA | GA | GA |
| `plan` (validate/diff/apply/status/resume) | GA | issues work; group milestones/epics unsupported | issues work; group milestones/epics unsupported |
| `sync`, `search`, `standup`, `timeline`, `renovate`, `shields`, `tui` | GA | GitLab-only | GitLab-only |

Notes:

- "unsupported" returns `provider.ErrUnsupported` (a clear error), never a silent
  fallback.
- GitHub and Gitea stay `[S]` experimental until live-instance integration jobs
  run in CI.
- `plan` is provider-driven, but GitHub and Gitea cannot create group-level
  milestones or epics, so plans containing those operations will fail.

---

## Configuration

Zero-config when glab (or tea, for Gitea) is set up. Optional overrides:

```bash
export GLVALET_DEFAULT_PROJECT="group/project"
export GLVALET_DEFAULT_GROUP="group"
export GLVALET_HOST="gitlab.example.com"   # override default host
export GLVALET_TOKEN="glpat-xxx"           # backfill an empty token
export GLVALET_JOURNAL="/custom/journal.jsonl"
export GLVALET_CACHE="/custom/cache"
export GLVALET_STATE="/custom/state.db"
```

### GitHub provider (opt-in, `[S]` experimental)

```bash
export GLVALET_PROVIDER=github                          # select the adapter
export GLVALET_GITHUB_ENABLED=true                      # exact "true" required
export GLVALET_GITHUB_TOKEN="ghp_xxx"                   # GitHub PAT
export GLVALET_GITHUB_URL="https://api.github.com"      # or enterprise base URL
```

`GLVALET_PROVIDER` selects the adapter; `GLVALET_GITHUB_ENABLED=true` arms it
(otherwise `github.New` returns `ErrFeatureDisabled`). See the host-capability
matrix above for what each surface supports.

### Gitea provider (opt-in, `[S]` experimental)

```bash
export GLVALET_PROVIDER=gitea                           # select the adapter
export GLVALET_GITEA_URL="https://gitea.example.com"    # optional: override tea URL
export GLVALET_TOKEN="gitea_xxx"                        # PAT or token from tea
export GLVALET_DEFAULT_PROJECT="owner/repo"             # optional: default project
```

`ggvalet` reads `tea login` credentials from `~/.config/tea/config.yml` or
`~/.tea/tea.yml` automatically. If `tea` has a single login, that instance is
used as the default; with multiple logins, `GLVALET_HOST` can select one by its
host.

Supported surfaces: `issue`, `mr` (including `mr diff`), `label`, `report`, `journal`, `hosts`,
`doctor`, `cache`, `receipt`, and issue-only `plan` operations. Group
milestones, group epics, `mr mine`, `sync`, `search`, `standup`,
`timeline`, `renovate`, `shields`, and `tui` return `provider.ErrUnsupported`.

---

## Documentation

- [Latest release notes](https://github.com/MChorfa/ggvalet/releases/latest)
- [Docs site (GitHub Pages)](https://mchorfa.github.io/ggvalet/)
- `BUILD_PROMPT.md` — from-scratch build guide

---

## License

MIT — see `LICENSE`. Personal project, unaffiliated with any employer.
