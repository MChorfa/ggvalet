# ADR-001: Host-neutral Provider abstraction with feature-flagged GitHub adapter

- Status: Accepted
- Date: 2026-05-28
- Deciders: project owner

## Context

The original spec (`BUILD_PROMPT.md`) scoped `ggvalet` to GitLab only. All
`cmd/*` commands consumed the GitLab SDK directly through
`internal/client/client.go`, which holds a `*gl.Client` alongside the
journal and cache. That design couples every command to the `go-gitlab` SDK
types and makes adding a second host — GitHub or GitHub Enterprise — a
pervasive change touching every command file.

A secondary requirement emerged: support GitHub hosts via separate
credentials (`GLVALET_GITHUB_TOKEN`, `GLVALET_GITHUB_URL`) without
breaking any existing GitLab path, and without exposing the GitHub adapter
to callers that have not opted in.

## Decision

### P5 — introduce `internal/provider/provider.go`

A `Provider` interface was added at
`internal/provider/provider.go`. It defines six operations covering issues,
merge requests/pull requests, and labels. All types (`Issue`,
`MergeRequest`, `Label`, `User`, `Note`) are host-neutral; no SDK-specific
type leaks across the boundary.

```
type Provider interface {
    Kind()              Kind
    Host()              string
    ListIssues(...)     ([]Issue, error)
    GetIssue(...)       (Issue, error)
    CreateIssue(...)    (Issue, error)
    ListMergeRequests(...) ([]MergeRequest, error)
    GetMergeRequest(...)   (MergeRequest, error)
    ListLabels(...)     ([]Label, error)
}
```

`ErrUnsupported` is the sentinel for operations a given host does not
implement. Callers branch on `errors.Is(err, provider.ErrUnsupported)`.

The existing GitLab code was extracted into
`internal/provider/gitlab/gitlab.go` implementing this interface.
`internal/client/client.go` now holds a `provider.Provider` field alongside
the `*gl.Client`, so commands that already use the SDK can migrate
incrementally.

### P6 — add `internal/provider/github/github.go`, feature-flagged off

A GitHub adapter was added at `internal/provider/github/github.go` backed
by `github.com/google/go-github/v66`. It implements the full `Provider`
interface. Epics and work items return `provider.ErrUnsupported`; GitHub
pull requests surface through `ListMergeRequests`/`GetMergeRequest`.

The adapter is gated by an exact env-var check:

```go
if os.Getenv("GLVALET_GITHUB_ENABLED") != "true" {
    return nil, ErrFeatureDisabled
}
```

`ErrFeatureDisabled` is a named sentinel in the `github` package. The
default-off posture limits blast radius: an unset or mis-spelled flag
produces a hard error at construction time rather than silent fallback.

GitHub Enterprise is supported via `cfg.GitHubURL`; when the URL resolves
to `github.com` or `api.github.com` the default API base is used unchanged.

`client.New` was **not** modified to route by flag. It continues to
construct only the GitLab provider. Flag-based routing is a follow-up
decision (see Consequences — negative).

## Alternatives considered

**1. Mint per-host clients ad hoc inside `cmd/*` commands.**
Rejected. Each command would need to import both SDKs, duplicate
construction logic, and handle the feature-flag itself. A change to
authentication or TLS configuration would need to be applied N times.

**2. Modify `client.New` to route by flag now.**
Deferred. GitLab is the verified path (P0–P4). Routing requires touching
every `cmd/*` consumer that holds a `*client.Client`. That change warrants
its own decision record. The current split — interface defined, adapter
compiled, routing deferred — is a deliberate incremental approach.

**3. Read the GitHub host from `glab-cli` config the way GitLab does.**
Rejected. `glab` does not model GitHub hosts. GitHub credentials are
supplied via `GLVALET_GITHUB_TOKEN` and `GLVALET_GITHUB_URL`, which are
separate from the GitLab config file path.

**4. Make the GitHub adapter default-on.**
Rejected. GitLab is the only path exercised end-to-end at this point.
Default-on would force every user to either set a GitHub token or suppress
the init error. Feature-flag default-off is the lower-risk choice.

## Consequences (positive)

- `cmd/*` commands can be migrated to call `client.Provider` instead of
  `client.GL` without touching the interface definition. [C]
- Adding a third host (e.g. Gitea, Bitbucket) requires only a new package
  under `internal/provider/<kind>/` implementing `Provider`. No `cmd/*`
  changes are needed for the interface layer. [S]
- `ErrFeatureDisabled` and `ErrUnsupported` are named sentinels; call-sites
  use `errors.Is` and can branch without string matching. [C]
- The GitHub adapter compiles and passes unit tests today with a
  `NewWithClient` test-injection constructor that bypasses the flag check.
  [C]
- `internal/client/client.go` exposes `provider.Provider` as a public
  field, so any future routing change in `New` does not alter the field
  type seen by `cmd/*`. [C]

## Consequences (negative / accepted costs)

- ~~`client.New` still hard-wires the GitLab provider.~~ **Resolved (P8,
  2026-06-01):** `client.New` (`internal/client/client.go:56-63`) now routes via
  `providerfactory.NewFromConfig`, honoring `GLVALET_PROVIDER`. Residual cost: the
  command layer is **fully migrated for host-neutral surfaces** — 42 raw `client.GL.*`
  sites remain, all in **10 GitLab-only `cmd/` files** that have no GitHub equivalent and
  are correctly guarded by `cmd/hostguard.go` under `GLVALET_PROVIDER=github`.
  Tracked as RES-01 in `docs/VWP-ATTESTATION.md`; GitHub host remains `[S]` experimental
  until a live-instance integration job runs in CI. [S]
- `Client` carries both `GL *gl.Client` and `Provider provider.Provider`.
  The `GL` field remains until `cmd/*` commands are migrated off it. During
  the migration window there are two access paths to GitLab data in the same
  struct.
- The GitHub adapter has no end-to-end test against a live GitHub instance
  in CI. Integration tests in
  `internal/provider/github/github_integration_test.go` are guarded by
  `GLVALET_GITHUB_ENABLED=true` and require a real token. [S]

## Compliance notes

- Capability claims above are classified per CKODEX VWP §26.C (P-SC-003).
  `[C]` = typed, tested, enforceable today.
  `[S]` = design locked, partial implementation.
- `ErrFeatureDisabled` default-off reduces blast radius consistent with the
  DAL blast-radius table (localized scope, DAL ≥ 1).
- No credentials, tokens, or PII appear in any type or log surface defined
  in `internal/provider/`.

## References

- `internal/provider/provider.go` — interface and shared types
- `internal/provider/gitlab/gitlab.go` — GitLab implementation
- `internal/provider/github/github.go` — GitHub implementation + feature flag
- `internal/client/client.go` — `Client.Provider` field, current routing
- `BUILD_PROMPT.md` — original GitLab-only spec
