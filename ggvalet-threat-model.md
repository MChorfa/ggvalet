# ggvalet Threat Model

Version: 0.1 (Wave 2)  
Scope: CLI tool `ggvalet` as used both on individual workstations and in CI automation.

## 1. Context and assumptions

| Item | Assumption | Evidence |
|---|---|---|
| Deployment | Single-user local CLI **and** CI/automation runner. | `README.md` § "GitHub and Gitea stay [S] experimental ...", `cmd/standup.go --push-issue`, `cmd/report.go --dst-project` |
| Auth | Token-based (GitLab `PRIVATE-TOKEN`, GitHub `Authorization`, Gitea `token`). Tokens live in `glab`/`tea` config or env vars `GLVALET_TOKEN` / `GLVALET_GITHUB_TOKEN`. | `internal/config/config.go:32, :79-81`, `internal/provider/gitlab/gitlab.go:374-378`, `internal/provider/github/github.go:54-60` |
| Data sensitivity | API tokens, host metadata, issue/MR metadata, user content (titles, descriptions, comments, standup text), and locally cached API responses. PII/PHI/PCI is not a design target, but user content may contain any of these. | `internal/state/entries.go` `RecordEntry` schema, `internal/cache/cache.go` envelope struct |
| Network exposure | Outbound-only HTTPS to GitLab/GitHub/Gitea. No listener, no server surface. | `internal/client/client.go:73-91` |
| Trust anchor | User workstation / CI runner. The glab/tea config files are outside ggvalet's control. | `internal/config/config.go:442-451, :557-566` |

## 2. System model

```
┌──────────────────────────────────────────────────────────────────────┐
│  User workstation / CI runner                                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────────┐   │
│  │ glab config  │  │ ggvalet CLI  │  │ ~/.ggvalet/              │   │
│  │ ~/.config/   │  │              │  │  state.db  (0o600)      │   │
│  │ glab-cli/    │  │  cmd/*.go    │  │  journal.jsonl (0o600)  │   │
│  │  config.yml  │──│  client.go   │──│  cache/*.json (0o600)   │   │
│  └──────────────┘  └──────┬───────┘  └──────────────────────────┘   │
│                           │                                          │
│                           ▼                                          │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Provider: gitlab / github / gitea                          │    │
│  │  go-gitlab / go-github SDK over TLS                         │    │
│  └──────────────────────┬──────────────────────────────────────┘    │
└─────────────────────────┼──────────────────────────────────────────┘
                          │
                          ▼
              GitLab / GitHub / Gitea REST API
```

## 3. Trust boundaries

| Boundary | Protocol / Mechanism | Notes |
|---|---|---|
| User ↔ `ggvalet` process | Local shell, env vars, flags | Tokens may enter via env or flags; shell history and CI logs are a risk. |
| `ggvalet` ↔ glab/tea config | Local file read | Tokens in plaintext in glab config; permissions depend on glab. |
| `ggvalet` ↔ `~/.ggvalet/` | Local SQLite / JSONL / JSON cache | Created with `0o700` dirs and `0o600` files. Standup output is an outlier at `0o644`. |
| `ggvalet` ↔ Git provider API | HTTPS, token header, optional `InsecureSkipVerify` | TLS is default; `skip_tls_verify: true` (per-host) disables cert validation. |
| Build pipeline ↔ release artifacts | GitHub Actions / GitLab CI, GoReleaser, cosign keyless | SHA256SUMS + .sig + .pem + SBOM. Install script only checks SHA256, not cosign. |
| Docsify site ↔ user browser | `https://MChorfa.github.io/ggvalet/` loads `cdn.jsdelivr.net/npm/docsify` | CDN script supply-chain risk; no PII in site. |

## 4. Assets

| Asset | Value | Location |
|---|---|---|
| GitLab/GitHub/Gitea personal access tokens | **Critical** — full provider access | glab/tea config, env `GLVALET_TOKEN`, `GLVALET_GITHUB_TOKEN`, in-memory `Config` |
| Local state DB (`~/.ggvalet/state.db`) | **High** — journal, operation receipts, plan runs | `internal/state/store.go:56-82` |
| Local journal (`~/.ggvalet/journal.jsonl`) | **High** — immutable audit log | `internal/journal/journal.go:88-117` |
| Cache (`~/.ggvalet/cache/*.json`) | **Medium** — API response bodies, possibly project/issue content | `internal/cache/cache.go:24-29` |
| Release artifacts | **High** — binary is installed with `install -m 755` to `/usr/local/bin` | `scripts/install-ggvalet.sh:65`, `dist/` |
| Install script (`install.sh`) | **High** — curl\|bash surface | `scripts/install-ggvalet.sh`, site install link |

## 5. Attacker capabilities

- **Workstation / CI runner compromise**: read `~/.ggvalet/`, `~/.config/`, env, replace binary, modify `PATH`.
- **Network MITM / malicious host**: intercept if `SkipTLS` enabled or if DNS is poisoned; attacker-controlled GitLab/GitHub instance harvests tokens.
- **Supply chain**: compromise Go dependency, GitHub Actions / GitLab runner, GoReleaser, cosign, CDN, release tarball, install script.
- **Malicious API content**: attacker controls issue/MR titles, descriptions, labels, web URLs; content is rendered in TUI/markdown and opened via `open`/`xdg-open`.
- **Social / misconfiguration**: user runs `ggvalet sync` to the wrong destination, or sets `GLVALET_GITHUB_ENABLED=true` without understanding limitations.

## 6. Threats / abuse paths

| ID | Threat | STRIDE | Attacker | Likelihood | Impact | Priority | Existing mitigations | Recommended mitigations |
|---|---|---|---|---|---|---|---|---|
| T1 | **Token theft from local filesystem / env** | Information Disclosure, Elevation | Workstation, CI log reader | Medium | Critical | **High** | State/cache/journal use `0o700`/`0o600`; tokens never written to journal | Mask CI secrets; avoid `set -x`; warn if `~/.ggvalet/` is group/other-readable; document short-lived tokens |
| T2 | **TLS bypass / MITM to harvest token** | Tampering, Spoofing | Network, DNS | Medium | High | **High** | TLS default; `InsecureSkipVerify` only when `skip_tls_verify=true` | `doctor` should warn on `SkipTLS`; audit `skip_tls_verify: true` in glab config; restrict to explicit `--insecure` flag |
| T3 | **Release artifact / supply-chain tampering** | Tampering | Supply chain | Low | Critical | **High** | cosign keyless signing, SBOM, SHA256SUMS, gitleaks, govulncheck | Install script should also verify `SHA256SUMS.sig` + `.pem`; pin GitHub Actions; add SLSA provenance |
| T4 | **Arbitrary file overwrite / information leak via output flags** | Tampering, Information Disclosure | User/script injection | Medium | Medium | **Medium** | Paths are user-supplied | `report --output`, `standup --output`, `ci download --dest` should set `0o600`, reject `..` / absolute paths, sanitize filenames |
| T5 | **Cross-host data exfiltration via `sync` / `report push`** | Information Disclosure | Misconfig, malicious host | Medium | High | **High** | `--dry-run` exists; `report push` builds destination client from `cfg.Hosts` | Add `--yes` / confirmation prompt for `sync` writes; validate `--dst-host` is in known hosts; default `sync` to `--dry-run` until explicitly confirmed |
| T6 | **SSRF / webhook exfiltration via `standup --slack/--teams`** | Spoofing, Information Disclosure | Malicious config/script | Low | Medium | **Medium** | URL is user-supplied flag | Allowlist webhook hostnames or require env var `GLVALET_SLACK_WEBHOOK`; validate `https://` scheme; warn in docs |
| T7 | **Unbounded cache / state growth (DoS / leak)** | Denial of Service | Any user | Medium | Low | **Low** | Cache entries TTL and auto-prune on read | Add max cache size and journal rotation / `VACUUM`; bound `BackfillJournalEntries` memory |
| T8 | **Malicious content / URL opened by `tui` or `openURL`** | Spoofing, Tampering | Malicious issue/MR | Low | Medium | **Low** | `openURL` only opens browser on user key press | Validate `web_url` scheme is `http(s)` before opening; no `file://`/`javascript:`; consider URL preview |
| T9 | **Provider confusion / feature-flag bypass** | Elevation, Denial of Service | Misconfig | Low | Medium | **Low** | `hostguard.go` blocks raw GitLab SDK commands on non-GitLab hosts; `GLVALET_GITHUB_ENABLED` exact `"true"` | Keep GitHub `[S]` until live CI passes; add `--provider` override validation |
| T10 | **Unsafe install from curl \| bash** | Tampering | Network, compromised release | Low | Critical | **High** | install script checks SHA256SUMS | Add cosign verification step with fallback message; sign install script itself or ship with package managers |

## 7. Risk heat map

```
Impact
  Critical │  T3  T10
           │  T1  T2  T5
      High │
           │
    Medium │  T4  T6  T8  T9
           │
       Low │  T7
           └─────────────────────────────
             Low    Medium   High   Critical
                      Likelihood
```

## 8. Security findings with code references

1. **`cmd/standup.go:93` writes standup output with `0o644`**.  
   Standup text may include issue titles, blockers, and host metadata. This is more permissive than the `0o600` used by state, journal, and cache.  
   **Recommendation:** change `os.WriteFile(output, []byte(text), 0o644)` to `0o600`.

2. **`cmd/report.go:60` and `cmd/ci.go:438` create output directories/files with default / `0o755` permissions**.  
   `os.Create` follows umask; `os.MkdirAll(..., 0o755)` creates world-traversable directories.  
   **Recommendation:** set `0o600` for files and `0o700` for user-specific output directories.

3. **`scripts/install-ggvalet.sh:53-59` only verifies SHA256SUMS, not the cosign signature.**  
   A compromised GitHub release could be replaced with a tarball whose SHA256 matches a forged checksum file.  
   **Recommendation:** add `cosign verify-blob` or prompt the user if cosign is unavailable.

4. **`cmd/standup.go:98-108` sends standup text to any `--slack` / `--teams` URL without validation.**  
   This is an SSRF-like exfiltration surface if the URL is attacker-controlled.  
   **Recommendation:** validate `https://` scheme and, in CI, prefer env-var sourced webhooks with hostname allowlisting.

5. **`internal/client/client.go:76-77` sets `InsecureSkipVerify: true` when `cfg.SkipTLS` is true.**  
   This is a deliberate per-host opt-in, but there is no runtime warning.  
   **Recommendation:** `doctor` should flag active hosts with `skip_tls_verify: true` and emit a warning at runtime.

## 9. SAST-LLM gap

`sast-orchestrator` is not present in this repository and no `.sast-llm/` configuration exists.  
This threat model is the repository-grounded pass. A SAST-LLM pass should be run as soon as the orchestrator is available or wired in CI; until then the above manual findings plus `govulncheck` and VWP lint are the security evidence.

## 10. Open questions / assumptions

1. Is `ggvalet` ever run on shared CI runners with long-lived tokens, or only on self-hosted runners with short-lived project tokens?  
2. Should `standup`/`report` output files be readable by other users in a shared build environment?  
3. Is there a requirement to support installing `ggvalet` in environments where `cosign` is not available?
