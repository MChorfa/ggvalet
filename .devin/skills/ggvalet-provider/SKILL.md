---
name: ggvalet-provider
description: >
  Guide for adding, modifying, or debugging ggvalet provider adapters.
  Use when touching internal/provider, providerfactory, internal/config,
  internal/client, cmd/hostguard, or provider-related tests and docs.
namespace: default
metadata:
  version: 1.0.1
  status: active
license: MIT
---

# ggvalet Provider Skill

`ggvalet` abstracts GitLab, GitHub, and Gitea behind a host-neutral
`provider.Provider` interface.

## Key files

- `internal/provider/provider.go` — interface, options structs, `Kind` constants, `ErrUnsupported`.
- `internal/provider/<name>/` — concrete adapter (`New(cfg)`, `Kind()`, all interface methods).
- `internal/providerfactory/factory.go` — selects adapter from `cfg.Provider` / `GLVALET_PROVIDER`.
- `internal/client/client.go` — builds `Provider` and raw GitLab `go-gitlab` client (`GL` only for gitlab).
- `internal/config/config.go` — loads glab and tea config, resolves active host, URL, token, `Provider`.
- `cmd/hostguard.go` — blocks non-host-neutral commands under non-GitLab providers.

## Adding a new provider

1. Add a `Kind` constant in `internal/provider/provider.go`.
2. Create `internal/provider/<name>/<name>.go` implementing `provider.Provider`.
   - Return `provider.ErrUnsupported` for operations the backend cannot perform.
   - Map host-neutral state strings (`"opened"`, `"closed"`, `"all"`) to the SDK's native values.
   - Prefix wrapped errors with the provider name for clarity.
3. Add the adapter to `providerfactory.NewFromConfig`.
4. Update `internal/client/client.go` to only build the raw GitLab client when `cfg.Provider` is `gitlab` (or empty).
5. Update `internal/config/config.go`:
   - Add a `provider<Name>` constant.
   - Add a config-loading path for the new provider if it has its own CLI config file.
   - Set `HostConfig.Provider` on loaded hosts and make `ForHost` return the correct `Config.URL` field.
6. Update `cmd/hostguard.go` allowlists if the new provider supports host-neutral commands.
7. Update `cmd/hosts.go` / `cmd/doctor.go` hints and `cmd/root.go` display logic.
8. Add tests in `internal/provider/<name>/` and `internal/config/config_test.go`.
9. Update `README.md` and `pages/docs/providers.md` with the provider matrix.

## Conventions

- Keep imports grouped: stdlib, blank line, third-party SDK, blank line, project internal.
- Do not add feature flags unless the provider is opt-in/experimental; if you do, mirror `GLVALET_GITHUB_ENABLED`.
- `ErrUnsupported` must be returned as the wrapped cause so `errors.Is(err, provider.ErrUnsupported)` works.
- `provider.Kind` string values are load-bearing (tests check literal values).
- The `Hosts` map in `Config` carries the active provider's hosts; `ForHost` must preserve `HostConfig.Provider`.

## Testing

- `go test ./internal/provider/...` — unit + interface compile checks.
- `go test ./internal/config/...` — config parsing (glab, tea, env overrides).
- `make regression` before commit.
