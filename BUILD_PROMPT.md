# GitLab Valet — Zero-to-Hero Build Prompt

> A complete system prompt for an AI coding agent (or a developer) to build
> the GitLab Valet CLI (`glv`) from an empty directory to a shipping tool.
> Hand this to Claude Code, Cursor, or any capable coding agent and it will
> reconstruct the entire project. Each phase is independently buildable and
> testable.

---

## Role & objective

You are a senior Go engineer building **GitLab Valet** (`glv`): a multi-instance
GitLab management CLI with durable operation receipts and resumable work-plan
reconciliation. Every provider operation records intent and outcome in SQLite;
the JSONL journal remains the report/standup compatibility surface.

Build it in phases. After each phase, the project must compile (`go build ./...`)
and the new commands must run. Do not move to the next phase until the current
one compiles cleanly with `gofmt -l .` reporting no files.

---

## Non-negotiable design principles

1. **Receipt-first.** A provider decorator persists intent before every remote
   call and outcome afterward in `~/.gitlabvalet/state.db`. A failed intent
   write blocks the call; a failed outcome write produces an uncertain result.
2. **glab config native.** Read credentials, TLS settings, and API hosts from
   the existing `glab` CLI config (`~/.config/glab-cli/config.yml`). Never
   invent a parallel config format. Support `GLVALET_*` env vars as overrides.
3. **Per-host correctness.** Each GitLab instance may set `skip_tls_verify`
   (a string `"true"`, not a bool) and `api_host` (which may contain a path
   segment like `host.example.com/gitlab`). Both must be honored exactly.
4. **One binary, no runtime deps.** Single `go build` produces a portable
   binary. Local state lives under `~/.gitlabvalet/`.
5. **Two clients when needed.** Cross-instance features (sync, report push)
   build a second, independent client on demand from the loaded hosts map via
   a `config.ForHost()` factory — never mutate global state.
6. **Idempotent sync and reconciliation.** Managed items carry a hidden HTML marker
   (`<!-- glv-sync-src: URL -->`) so re-runs skip already-synced items.
7. **Prose over bullets in output; minimal formatting; warm, terse tone.**

---

## Tech stack

- Go 1.24+, module path `github.com/ckodex/gitlabvalet`
- `github.com/xanzy/go-gitlab` — GitLab API client
- `github.com/spf13/cobra` — CLI framework
- `github.com/spf13/viper` — env/config (used lightly)
- `gopkg.in/yaml.v3` — glab config parsing
- `github.com/fatih/color` — terminal color
- `github.com/olekukonko/tablewriter` — tables
- `github.com/google/uuid` — journal entry IDs
- `modernc.org/sqlite` — CGo-free durable state and receipt store
- `github.com/charmbracelet/bubbletea` + `bubbles` + `lipgloss` — TUI

---

## Repository layout (target)

```
gitlabvalet/
├── main.go                      # cmd.Execute()
├── go.mod
├── Makefile                     # build, install, test, cross
├── LICENSE                      # MIT
├── .gitignore
├── .gitlab-ci.yml               # verify + cross-compile
├── README.md
├── cmd/                         # one file per command group
│   ├── root.go                  # cobra root, --host flag, PersistentPreRunE, hosts cmd
│   ├── issue.go  epic.go  milestone.go  workitem.go  label.go
│   ├── mr.go                    # incl. colored unified diff
│   ├── sync.go                  # cross-instance, dedup marker
│   ├── search.go                # concurrent multi-host
│   ├── standup.go               # journal + open issues → Slack/Teams/issue
│   ├── timeline.go              # Gantt via lipgloss + parallel pool
│   ├── renovate.go              # bump detection, bulk approve/merge
│   ├── shields.go               # shields.io badges + lipgloss chips
│   ├── report.go                # markdown report + report push subcommand
│   ├── journal.go               # show / stats
│   ├── receipt.go               # export durable receipts
│   ├── plan.go                  # validate/diff/apply/status/explain/resume
│   ├── tui.go                   # bubbletea split-pane browser
│   └── cache.go                 # stats / flush
└── internal/
    ├── config/config.go         # glab config parse, Load(), ForHost()
    ├── client/client.go         # go-gitlab wrapper + Rec/RecErr + TLS skip
    ├── journal/journal.go       # JSONL ledger: Entry, Record, Query, Stats
    ├── state/store.go           # SQLite operations, receipts, plan checkpoints
    ├── observed/                # provider + raw-HTTP observation boundary
    ├── reconcile/reconcile.go   # idempotent stop-and-resume executor
    ├── plan/                    # v1/v2 schema, dependency graph, markers
    ├── report/report.go         # markdown + plain renderers, group by host
    ├── parallel/parallel.go     # bounded goroutine pool
    └── cache/cache.go           # SHA-256 keyed TTL disk cache
```

---

## Phase 1 — Foundation (config + journal + client + first command)

**Goal:** `glv issue list -p group/proj` works and the operation lands in the journal.

### 1a. `internal/config/config.go`

Parse the glab config and resolve an active host. Schema to support exactly:

```yaml
host: gitlab.example.com          # top-level default host
hosts:
  gitlab.example.com:
    token: glpat-xxx
    user: alice
    api_protocol: https
    api_host: gitlab.example.com   # may contain a path: host.com/gitlab
    skip_tls_verify: "true"        # STRING, not bool
```

Implement:
- `HostConfig` struct with `SkipTLS() bool` and `APIURL(hostname) string`
  (APIURL uses `api_host` verbatim when set; handles `://` already present).
- `glabFileConfig` with top-level `host` field captured as the default.
- `Config` struct: `Host, GitLabURL, Token, User, SkipTLS, JournalPath,
  CachePath, DefaultProject, DefaultGroup, Hosts map[string]*HostConfig`.
- `Load(opts Options) (*Config, error)` with host resolution order:
  `--host flag` → `GLVALET_HOST` → glab top-level `host:` → `GLVALET_GITLAB_URL`
  stripped → first host alphabetically.
- `ForHost(hosts, hostname, journalPath) (*Config, error)` — builds a Config
  for any host from a pre-loaded map (used later by sync/push).
- Search config in: `$XDG_CONFIG_HOME/glab-cli`, `%AppData%\glab-cli` (Windows),
  `~/.config/glab-cli`, `~/.glab-cli`. Merge `hosts.yml` if present (older glab).
- Helpers: `journalPath()`, `cachePath()` honoring `GLVALET_JOURNAL` /
  `GLVALET_CACHE` env overrides.

### 1b. `internal/journal/journal.go`

Append-only JSONL ledger:
- `Op` (create/update/close/reopen/comment/list/assign/delete),
  `Entity` (issue/epic/milestone/workitem/mr/label/note), `Outcome` (ok/err).
- `Entry`: `ID, Timestamp, Host, Op, Entity, Project, Group, EntityID, IID,
  Title, URL, Outcome, Detail, Tags`.
- `Open(path)`, `Record(Entry)` (UUID + UTC timestamp auto-filled, O_APPEND).
- `Filter{Since, Until, Host, Ops, Entities, Project, Group, Outcome}` and
  `Query(Filter) ([]Entry, error)` (sequential JSONL scan, skip malformed lines).
- `Stats{Total, ByHost, ByEntity, ByOp, Errors}` and `ComputeStats(entries)`.

### 1c. `internal/client/client.go`

- `Client{GL *gl.Client, Journal *journal.Journal, Cache *cache.Cache, cfg}`.
- `New(cfg)`: open journal + cache; build `http.Client` with
  `InsecureSkipVerify` when `cfg.SkipTLS`; `gl.NewClient(token,
  WithBaseURL(cfg.GitLabURL), WithHTTPClient(...))`.
- `Rec(op, entity, project, group, entityID, iid, title, url, tags...)` and
  `RecErr(op, entity, project, group, detail)` — both stamp `cfg.Host`.
- `CacheKey(path, params)` helper.

### 1d. `cmd/root.go`

- Cobra root `glv`, persistent `--host`/`-H` flag.
- `PersistentPreRunE`: `config.Load` then `client.New`; print
  `→ host: <host> as <user> [tls-skip]` to **stderr** (never stdout — keeps
  piped output clean).
- `hosts` subcommand: table of hostname/user/API URL/TLS-skip/default. Override
  its `PersistentPreRunE` so it works even with an empty token.
- Output helpers: `ok()`, `fail()`, `info()` using fatih/color.

### 1e. `cmd/issue.go` (start with list/mine/create/update/close/comment)

Each handler: resolve project (flag → `cfg.DefaultProject`), call go-gitlab,
then `Rec()` on success / `RecErr()` on failure. Render lists with tablewriter.

**Checkpoint:** `go build .`, `glv hosts`, `glv issue list -p <proj>`,
confirm a line appears in `~/.gitlabvalet/journal.jsonl`.

---

## Phase 2 — Entity coverage

Add `cmd/epic.go` (group-level, ISOTime start/due dates), `cmd/milestone.go`
(stats subcommand with completion %), `cmd/workitem.go` (REST
`/projects/:id/work_items`, GitLab 15.1+, via direct HTTP with PRIVATE-TOKEN
header since go-gitlab may lack it), `cmd/label.go` (list/create). Every
handler journals. Register all in `root.go`.

**Checkpoint:** create, list, and close each entity type; verify journal entries.

---

## Phase 3 — Reports & journal viewing

### 3a. `internal/report/report.go`
- `Options{Since, Until, Format, Author}`, `Format` = markdown|plain.
- `Generate(w, entries, opts)` → group by host (only show per-host sections
  when >1 host present), then by entity. Markdown summary tables + sections.
- **Copyright-safe**: it generates original structured output, no external text.

### 3b. `cmd/journal.go` — `show` (--since 24h|7d|30d, --entity, --host,
--errors) and `stats`. Implement a `parseDuration` that handles `Nd` and `Nw`
suffixes plus standard Go durations. Add `shortHostname()` for display.

### 3c. `cmd/report.go` — print/write report, plus `report push` subcommand
that builds the markdown, then creates an issue on a target project (any host
via `ForHost`), auto-titling `Activity Report YYYY (Wxx)`.

**Checkpoint:** `glv report --since 7d`, `glv journal show --since 7d`.

---

## Phase 4 — Cross-instance sync

`cmd/sync.go` with subcommands `issues`, `epics`, `milestones`:
- `syncClients(srcHost, dstHost)` builds two clients via `config.ForHost`.
- Dedup: append `syncFooter(srcURL)` (`<!-- glv-sync-src: URL -->`) to created
  item descriptions; before creating, scan destination for the marker and skip
  matches. Milestones dedup by title.
- Always support `--dry-run` and `--limit`. Journal both the source read and
  destination create (tag `sync-src` / `sync-dst`).
- Add `label sync` to `cmd/label.go` (copy labels with colors, skip existing).

**Checkpoint:** `glv sync issues --src-... --dst-... --dry-run` then real run;
re-run and confirm everything is skipped (idempotent).

---

## Phase 5 — Intelligence & visuals

- `internal/parallel/parallel.go`: bounded pool (`Go`, `GoErr`, `Wait`).
- `internal/cache/cache.go`: SHA-256 keyed TTL JSON files; `Get/Set/Unmarshal/
  Flush/Stats`. Wire `--no-cache` into list commands; add `cmd/cache.go`.
- `cmd/search.go`: concurrent multi-host search (`--all-hosts`) via the pool.
- `cmd/standup.go`: journal (yesterday) + open issues (today) + blocked label;
  output to stdout/file/Slack webhook/Teams Adaptive Card/GitLab issue.
- `cmd/mr.go`: list/mine/create/approve/merge/close/**diff** (colored unified
  diff in terminal: `+` green, `-` red, `@@` teal, file headers lavender).
- `cmd/timeline.go`: Gantt chart with lipgloss block chars, auto date range,
  today marker, completion-colored fill; epics+milestones fetched in parallel.
- `cmd/renovate.go`: detect Renovate MRs (author/branch/title), classify
  major/minor/patch by regex, `list/approve/merge/stats` with bulk + pool;
  default `--bump patch`; always `--dry-run` available.
- `cmd/shields.go`: `badge` (shields.io markdown from live project data) +
  `chips` (lipgloss colored label chips, luminance-based text color).
- `cmd/tui.go`: bubbletea split-pane (list + viewport), four tabs
  (Issues/Epics/Milestones/Journal), lazy-load per tab, `Tab/1-4/j/k///o/r/q`
  keybindings, CKODEX palette (deep blue, teal, lavender, yellow).

**Checkpoint:** `glv tui`, `glv timeline -p ... -g ...`, `glv renovate list`,
`glv standup`, `glv search "x" --all-hosts`.

---

## Definition of done

- `go build ./...` clean; `gofmt -l .` empty; `go vet ./...` clean.
- `glv hosts` lists every glab-configured instance with correct TLS/default flags.
- Every provider call has durable intent/outcome receipts; JSONL reports remain compatible.
- Plan v2 validates dependencies, rejects cycles, links issues to GitLab epics,
  stops on failure, and resumes without duplicating completed resources.
- `glv sync` is idempotent across runs.
- `make cross` produces darwin/linux/windows binaries.

## Phase 6 — Durable receipts and work reconciliation

- Add `GLVALET_STATE` with default `~/.gitlabvalet/state.db`; use SQLite WAL,
  foreign keys, a busy timeout, `0600` database mode, and idempotent migrations.
- Import the legacy journal once. Keep JSONL for existing reports and expose
  `glv receipt export` for portable receipt output.
- Decorate `provider.Provider` so intent is committed before execution and
  outcome afterward. Observe raw GitLab SDK traffic at the HTTP transport.
- Extend plan schema v2 with `depends_on`, issue `epic`, and `weight`. Build a
  dependency DAG with cycle detection and deterministic topological order.
- Persist plan runs and step checkpoints. Support `validate`, `diff`, `apply`,
  `status`, `explain`, and `resume`. Stop on the first failure; do not implement
  automatic delete rollback.
- Re-find managed resources by their embedded identity marker before creation.
  Link issues to epics idempotently through the GitLab adapter.

**Checkpoint:** inject a failure after milestone and epic creation, then resume;
the completed resources are not duplicated and the issue is created and linked once.

## Critical gotchas (learned the hard way)

- `skip_tls_verify` is a **quoted string** in glab config — parse as `string`,
  compare case-insensitively to `"true"`.
- `api_host` can contain a path (`sc01.example.com/gitlab`) → use verbatim in
  `APIURL`, do not URL-encode.
- The startup host indicator goes to **stderr**, so `glv report ... | pbcopy`
  stays clean.
- go-gitlab work-items support is thin — use raw HTTP for `/work_items`.
- In bubbletea, let the list component consume keys while
  `list.FilterState() == list.Filtering`, otherwise `/` search breaks.
- `c-*` lipgloss/SVG color classes use direct-child selectors — don't nest.

---

## Style for any prose the tool emits

Warm, terse, high-signal. Prose over bullets. Minimal bold. Disclaimers brief.
Decline destructive actions softly and always offer `--dry-run` first.
