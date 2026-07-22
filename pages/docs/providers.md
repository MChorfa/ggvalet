# Providers

`ggvalet` exposes a host-neutral `Provider` interface in `internal/provider` and
switches adapters at startup based on `GLVALET_PROVIDER`.

| Provider | Selection | Config source |
|---|---|---|
| `gitlab` | default | `~/.config/glab-cli/config.yml` or `GLVALET_TOKEN` + `GLVALET_GITLAB_URL` |
| `github` | `GLVALET_PROVIDER=github` + `GLVALET_GITHUB_ENABLED=true` | `GLVALET_GITHUB_TOKEN` + `GLVALET_GITHUB_URL` |
| `gitea` | `GLVALET_PROVIDER=gitea` | `~/.config/tea/config.yml`, `~/.tea/tea.yml`, or `GLVALET_TOKEN` + `GLVALET_GITEA_URL` |

## Capability matrix

| Command surface | GitLab | GitHub | Gitea |
|---|---|---|---|
| `issue` (list/mine/get/create/update/close/comment) | GA | `list --milestone` unsupported | GA |
| `mr` (list/mine/create/approve/merge/diff/close) | GA | `mine`, `approve` unsupported | `mine`, `diff` unsupported |
| `label` (list/create/sync) | GA | GA | GA |
| `epic`, group `milestone` | GA | no equivalent | no equivalent |
| `sync`, `search`, `standup`, `timeline`, `renovate`, `shields`, `tui` | GA | GitLab-only | GitLab-only |
| `report`, `plan`, `journal`, `hosts`, `doctor` | GA | GA | GA |

"unsupported" returns `provider.ErrUnsupported` (a clear error), never a silent
fallback.

## Gitea / tea setup

Install [tea](https://gitea.com/gitea/tea) and log in:

```bash
tea login add --name try --url https://gitea.example.com --token gitea_xxx
tea logins list
```

`ggvalet` will read `~/.config/tea/config.yml` automatically when
`GLVALET_PROVIDER=gitea` is set. You can override the URL or bypass `tea` with
environment variables:

```bash
export GLVALET_PROVIDER=gitea
export GLVALET_GITEA_URL=https://gitea.example.com
export GLVALET_TOKEN=gitea_xxx
```
