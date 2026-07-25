# AGENTS.md — ggvalet

## Release flow

**Do NOT run `make release-publish` locally.** The `.github/workflows/release.yml`
workflow is the canonical release path. It builds, cosign-signs (keyless via the
GitHub Actions OIDC identity), and publishes all assets to the GitHub Release.

Local publish races with CI: the tag push triggers the workflow, which tries to
upload assets that already exist → 422 "already_exists" → red CI.

### Correct release sequence

1. `make regression` — verify all gates pass locally
2. `git commit` + `git tag -a v0.4.x -m "..."`
3. `git push origin main && git push origin v0.4.x`
4. Watch CI: `gh run watch <run-id> --repo MChorfa/ggvalet --exit-status`
5. Verify: `gh release view v0.4.x --repo MChorfa/ggvalet`

CI-produced cosign signatures are attested by the workflow identity
(`https://github.com/MChorfa/ggvalet/.github/workflows/release.yml@refs/tags/vX.Y.Z`),
which is stronger than browser-OIDC local signing.

## Build / test commands

- `make regression` — full suite: tests, coverage (gate 80%), VWP lint, govulncheck, cross-compile
- `make test` — `go test ./...`
- `make coverage` — coverage report only
- `make release-dry` — goreleaser dry-run (no publish)
- `make build` — local platform binary
