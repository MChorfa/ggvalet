# Providers

`ggvalet` exposes a host-neutral `Provider` interface in `internal/provider` and
switches adapters at startup based on `GLVALET_PROVIDER`.

| Provider | Selection | Default config source | Override env |
|---|---|---|---|
| `gitlab` | default | `~/.config/glab-cli/config.yml` | `GLVALET_TOKEN`, `GLVALET_GITLAB_URL` |
| `github` | `GLVALET_PROVIDER=github` + `GLVALET_GITHUB_ENABLED=true` | none (PAT required) | `GLVALET_GITHUB_TOKEN`, `GLVALET_GITHUB_URL` |
| `gitea` | `GLVALET_PROVIDER=gitea` | `~/.config/tea/config.yml` or `~/.tea/tea.yml` | `GLVALET_TOKEN`, `GLVALET_GITEA_URL` |

- `GLVALET_HOST` scopes a single command to one configured host.
- `GLVALET_DEFAULT_PROJECT` and `GLVALET_DEFAULT_GROUP` work for any provider
  that supports the command.

## Capability matrix

| Command surface | GitLab | GitHub | Gitea |
|---|---|---|---|
| `issue` (list/mine/get/create/update/close/comment) | GA | `list --milestone` unsupported | `list --milestone` unsupported |
| `mr` (list/mine/create/approve/merge/diff/close) | GA | `mine`, `approve` unsupported | `mine` unsupported |
| `label` (list/create/sync) | GA | GA | GA |
| `epic`, group `milestone` | GA | no equivalent | no equivalent |
| `report`, `journal`, `hosts`, `doctor`, `cache`, `receipt` | GA | GA | GA |
| `plan` (validate/diff/apply/status/resume) | GA | issues work; group milestones/epics unsupported | issues work; group milestones/epics unsupported |
| `sync`, `search`, `timeline`, `renovate`, `shields badge`, `tui` | GA | GitLab-only | GitLab-only |
| `standup` | GA | GA | GA |
| `shields chips` | GA | GA | GA |

Notes:

- "unsupported" means the command returns `provider.ErrUnsupported` (a clear
  error), never a silent fallback to another host.
- GitHub and Gitea are `[S]` experimental; full support requires live-instance
  integration tests in CI.
- `plan` is provider-driven, but GitHub and Gitea cannot create group-level
  milestones or epics, so plans containing those operations will fail.

## GitLab setup

Zero-config when `glab` is configured:

```bash
glab auth login --hostname gitlab.example.com --token glpat-xxx
ggvalet hosts
ggvalet issue mine
```

Override the URL or token at runtime:

```bash
export GLVALET_HOST=gitlab.example.com
export GLVALET_TOKEN=glpat-xxx
export GLVALET_GITLAB_URL=https://gitlab.example.com
```

## GitHub setup

GitHub support is opt-in:

```bash
export GLVALET_PROVIDER=github
export GLVALET_GITHUB_ENABLED=true
export GLVALET_GITHUB_TOKEN=ghp_xxx
export GLVALET_GITHUB_URL=https://api.github.com
```

## Gitea / tea setup

Install [tea](https://gitea.com/gitea/tea) and add a login:

```bash
tea login add --name try --url https://gitea.example.com --token gitea_xxx
tea logins list
```

`ggvalet` will read `~/.config/tea/config.yml` (or `~/.tea/tea.yml`) when
`GLVALET_PROVIDER=gitea` is set. With a single login that instance is the
default; with multiple logins, use `GLVALET_HOST` to pick one by host.

Override or bypass `tea` with environment variables:

```bash
export GLVALET_PROVIDER=gitea
export GLVALET_GITEA_URL=https://gitea.example.com
export GLVALET_TOKEN=gitea_xxx
export GLVALET_DEFAULT_PROJECT=owner/repo
```

Example workflow:

```bash
export GLVALET_PROVIDER=gitea
ggvalet hosts
ggvalet issue list --project owner/repo
ggvalet mr create --project owner/repo --source-branch feature --target-branch main
```
