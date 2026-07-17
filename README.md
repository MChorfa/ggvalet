# GitLab Valet (`glv`)

Your personal multi-instance GitLab agent. Every provider operation produces
durable intent and outcome receipts in `~/.gitlabvalet/state.db`. The existing
JSONL journal remains available for reports and backward compatibility.

```
╔═══════════════════════════════════════════════════╗
║          GitLab Valet  (ckodex/gitlabvalet)       ║
║  Manage · Record · Report  — never miss a thing  ║
╚═══════════════════════════════════════════════════╝
```

Reads your existing **glab CLI config** (`~/.config/glab-cli/config.yml`):
tokens, `skip_tls_verify`, and `api_host` are all inherited per instance. No
separate setup.

---

## Install

```bash
go mod tidy
make build           # → ./glv
make install         # → $GOBIN/glv
make cross           # → dist/ (darwin, linux, windows)
glv --version        # prints the build tag
```

### Verify a signed release

Tagged releases ship cross-platform binaries, a `SHA256SUMS`, a CycloneDX SBOM
(`sbom.cdx.json`), and a cosign **keyless** signature over the checksums:

```bash
sha256sum -c SHA256SUMS                                   # checksums match

RELEASE_PROJECT_URL="https://gitlab.example.com/group/gitlabvalet"
RELEASE_TAG="v0.2.0"
RELEASE_ISSUER="https://gitlab.example.com"

cosign verify-blob SHA256SUMS \
  --signature SHA256SUMS.sig \
  --certificate SHA256SUMS.pem \
  --certificate-identity "${RELEASE_PROJECT_URL}//.gitlab-ci.yml@refs/tags/${RELEASE_TAG}" \
  --certificate-oidc-issuer "${RELEASE_ISSUER}"
```

Then confirm the smoke check:
```bash
glv hosts
```

---

## Feature map

| Group | Commands |
|-------|----------|
| **CRUD** | `issue`, `epic`, `milestone`, `wi` (work items), `label`, `mr` |
| **Sync** | `sync issues`, `sync epics`, `sync milestones`, `label sync` |
| **Intelligence** | `search` (multi-host), `renovate` (triage), `standup` |
| **Visual** | `tui`, `timeline` (Gantt), `shields` (badges + chips) |
| **Reporting** | `report`, `report push`, `journal show`, `journal stats`, `receipt export` |
| **Reconcile** | `plan validate`, `plan diff`, `plan apply`, `plan status`, `plan explain`, `plan resume` |
| **Ops** | `hosts`, `cache stats`, `cache flush` |

---

## Quick start

```bash
glv hosts                              # list configured instances
glv issue mine                         # my open issues, default host
glv --host sc01-trt.thales-systems.ca/gitlab issue mine
glv tui                                # interactive browser
glv report --since 7d --author "Name"  # weekly report
glv standup --since 24h                # daily standup
```

See `BUILD_PROMPT.md` for a complete from-scratch build guide, and the
companion skill (`gitlabvalet-skill/`) for full command + recipe documentation.

---

## Architecture

```
glab config ──► Config.Hosts ──► active client (+ sync client via ForHost)
                                      │
            ┌─────────────────────────┼─────────────────────────┐
     State + receipts             Cache                    Parallel pool
    (SQLite, authoritative)     (TTL disk)               (bounded goroutines)
                                      │
                                  Commands
                                      │
       GitLab REST API · plan runs · JSONL export · TUI / reports
```

Provider calls route through `internal/observed`, which persists an intent before
the remote call and an outcome afterward. Failure to persist intent prevents the
call; failure to persist an outcome marks it uncertain. The legacy journal is
imported once and remains the report/standup compatibility surface.

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
glv plan validate plan.yaml
glv plan diff plan.yaml
glv plan apply plan.yaml                         # dry-run
glv plan apply plan.yaml --dry-run=false --yes   # starts a durable run
glv plan status RUN_ID
glv plan explain RUN_ID
glv plan resume RUN_ID
glv receipt export -o receipts.jsonl
```

---

## Providers

The codebase exposes a host-neutral `Provider` interface at
`internal/provider/provider.go`. Adapters implement that interface; the active
adapter is selected at startup.

```
                 provider.Provider (interface)
                        │
          ┌─────────────┴─────────────┐
          │                           │
  internal/provider/gitlab/    internal/provider/github/
  (GitLab REST adapter)         (GitHub REST adapter — opt-in)
          │
     client.New(cfg)
          │
     all glv commands
```

`client.New` selects the adapter via `providerfactory.NewFromConfig`, keyed on
`GLVALET_PROVIDER` (`gitlab` default, or `github`).

**GitLab adapter** (`internal/provider/gitlab/`) — exercised by every `glv`
command. The `issue`, `mr`, and `label` surfaces are fully host-neutral; the
remaining commands (`epic`, `milestone`, `sync`, `search`, `standup`,
`timeline`, `renovate`, `shields`, `tui`, `report`) are GitLab-specific.

**GitHub adapter** (`internal/provider/github/`) — opt-in **`[S]` experimental**.
Requires both `GLVALET_PROVIDER=github` and `GLVALET_GITHUB_ENABLED=true`
(otherwise `github.New` returns `ErrFeatureDisabled`). A command that is not yet
host-neutral **fails loud** under a non-GitLab provider rather than silently
querying GitLab (`cmd/hostguard.go`).

### Host-capability matrix

| Command surface | GitLab | GitHub |
|-----------------|:------:|:------:|
| `issue` (list/mine/get/create/update/close/comment) | ✅ GA | ✅ (`list --milestone` → unsupported) |
| `mr` (list/mine/create/approve/merge/diff/close) | ✅ GA | ✅ (`mine`, `approve` → unsupported) |
| `label` (list/create/sync) | ✅ GA | ✅ |
| `epic`, group `milestone` | ✅ GA | ⛔ no equivalent |
| `sync`, `search`, `standup`, `timeline`, `renovate`, `shields`, `tui`, `report` | ✅ GA | ⛔ GitLab-only |

"unsupported" returns `provider.ErrUnsupported` (a clear error), never a silent
fallback. GitHub stays `[S]` experimental until a live-instance integration job
runs in CI.

---

## Configuration

Zero-config when glab is set up. Optional overrides:

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

---

## License

MIT — see `LICENSE`. Personal project, unaffiliated with any employer.
