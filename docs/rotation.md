# Credential rotation — operator runbook

This is the document to read when a rotation has failed and a credential is
sitting in escrow. Read the "unrecoverable window" section first if you are
here because something already went wrong.

`ggvalet rotate` rotates GitLab personal access tokens (PATs) on the hosts you
list in `rotation.yaml`. SSH keys are never rotated automatically — see
[SSH keys](#ssh-keys) below.

---

## The one unrecoverable window

GitLab's `POST /personal_access_tokens/self/rotate` endpoint **revokes the old
token and returns the replacement exactly once**, in the HTTP response. There
is no way to ask GitLab for that value again. Once the call succeeds, the only
copy of the new secret is whatever `ggvalet` does with it next.

`ggvalet` writes that response to a 0600 escrow file, in a 0700 directory,
before it does anything else — including verifying the new token or writing it
into your `glab` config. That ordering is what makes almost every failure
recoverable: as long as the escrow write landed on disk, `ggvalet rotate
--recover` can finish the job later.

There is exactly one gap this does not cover: **the old token is already
revoked, and the escrow write itself did not confirm.** If the process dies,
loses power, or the filesystem fails between the rotate call returning and the
escrow file being fsynced, the new secret may never have reached disk. In that
case it is genuinely gone — GitLab will not hand it out again, and there is no
local copy to recover from.

### How to tell which case you're in

Read the message. `ggvalet rotate` prints a `CRITICAL` line whenever a host was
rotated but not fully committed, and the wording is deliberately different for
the two cases:

- **"the replacement (token id N) is in escrow"** — the escrow write
  confirmed. The secret is on disk. Run:

  ```bash
  ggvalet rotate --recover
  ```

- **"the replacement (token id N) may be in escrow"** — the escrow write did
  not confirm cleanly. `ggvalet` still tells you to look before giving up
  (`look for <escrow-dir>/<host>-*.json`), because some of the ways this write
  can fail happen *after* the file is already synced — only the directory
  listing is unconfirmed. Check for the file:

  ```bash
  ls -la ~/.ggvalet/escrow/ | grep <host>
  ```

  - **File found** → treat it like the first case: run `ggvalet rotate
    --recover`.
  - **No file** → the credential is gone. Skip to
    [Manual recovery](#manual-recovery-when-the-credential-is-truly-gone).

`ggvalet doctor` will also show the host as unreachable (`401`/token invalid)
until this is resolved — that is expected, not a second problem.

### Manual recovery (when the credential is truly gone)

1. Log into the GitLab instance's web UI as the affected user.
2. Go to **User Settings → Access Tokens** and create a new personal access
   token with the same scopes the old one had (rotation requires
   `self_rotate`; add whatever your workflow needs beyond that, e.g. `api`).
3. Set an expiry consistent with your rotation cadence — see
   [Cadence and expiry](#cadence-and-expiry) below (65 days is the default).
4. Copy the token value once — GitLab shows it exactly once, same as the
   rotation endpoint.
5. Paste it into `~/.config/glab-cli/config.yml` (or wherever your `glab`
   config lives — see `ggvalet doctor` output, which reports the resolved
   config path) under that host's `token:` line.
6. Confirm it: `ggvalet doctor` should show the host healthy again.
7. There is no escrow file to clean up in this path — none was ever
   confirmed — so there is nothing further to commit.

---

## What an escrow file is

An escrow file is the durable record of a rotation that is in flight or that
died partway through. Location: `~/.ggvalet/escrow/` (override the whole
`ggvalet` home directory with `GLVALET_HOME`). Each file is named
`<host>-<unix-nano>.json`.

Properties that matter operationally:

- **The directory is 0700, the file is 0600.** Only the owning user can read
  it. Treat it exactly like you would treat a plaintext credential file,
  because that is what it is.
- **It contains a live, valid credential** — the new PAT's value, in
  `new_token`, in cleartext. It is not encrypted; the filesystem permissions
  are the only control. Do not copy it, back it up to a shared location, or
  attach it to a support ticket. If you need to hand it to someone for
  debugging, redact the `new_token` field first.
- **It is deleted only after the new token is verified working and written
  into your `glab` config.** `ggvalet rotate` and `ggvalet rotate --recover`
  both run the same verify → commit → journal → delete sequence
  (`internal/rotation/rotate.go`, the `finish` phase); the escrow file is the
  last thing removed, and only once every earlier step has succeeded. An
  escrow file present on disk always means "rotated at GitLab, not yet fully
  committed locally" — never "safe to ignore."
- `ggvalet rotate` refuses to start a new rotation for a host that still has
  an uncommitted escrow file. Recover it first.

## Duplicate journal entries after recovery

Recovery replays the commit, journal, and state-store writes against the
escrowed secret — it is not a new rotation, it is finishing the interrupted
one. Those writes are not idempotent. If a run was killed **between** the
journal write and the escrow-file delete, the escrow file is still there (the
delete never happened), so `--recover` runs the whole sequence again,
including the journal write.

The result: two `OpRotate` journal entries carrying the same new token id.
This is expected, not a bug. **Two entries with the same token id are one
rotation, not two** — do not read them as two separate credential changes when
building a report or an audit trail. `ggvalet journal show` will display both;
correlate by token id, not by entry count.

---

## Rotation phases

Every host goes through the same state machine (`internal/rotation/rotate.go`):

1. **preflight** — checks for a leftover escrow, confirms the current token is
   active and carries the `self_rotate` scope, and proves the config file is
   writable. Nothing is mutated remotely in this phase; every failure here is
   free.
2. **rotate** — calls GitLab's `self/rotate` endpoint. This is the
   irreversible step: the old token is revoked the instant this call
   succeeds.
3. **escrow** — writes and fsyncs the new secret to `~/.ggvalet/escrow/`
   before touching anything else. See [the unrecoverable
   window](#the-one-unrecoverable-window) above for what happens if this
   fails.
4. **verify** — confirms the new token actually authenticates against the
   host.
5. **commit** — writes the new token into your `glab`/`tea` config
   (`~/.config/glab-cli/config.yml` by default).
6. **journal / store / done** — records the rotation in the journal and the
   state database, then deletes the escrow file.

`ggvalet rotate --check` only reports what phase a host *would* need to enter
(due / not due, or an existing uncommitted escrow) — it never contacts a host
and never mutates anything. It cannot tell you a token is revoked or has lost
its `self_rotate` scope; only `ggvalet doctor` checks that remotely.

## Cadence and expiry

`rotation.yaml` (`~/.ggvalet/rotation.yaml`, or `$GLVALET_HOME/rotation.yaml`)
declares which credential types each host uses and the timing defaults:

```yaml
version: 1
defaults:
  cadence_days: 30       # rotate.go's DefaultCadenceDays
  pat_expiry_days: 65    # rotate.go's DefaultPATExpiryDays
hosts:
  gitlab.thalesdigital.io:
    credentials: [pat, ssh]
  sc01-trt.thales-systems.ca:
    credentials: [pat]
```

- A host with `pat` in `credentials` is rotated by `ggvalet rotate`.
- A host with `ssh` in `credentials` is only ever *audited* — see
  [SSH keys](#ssh-keys).
- If `rotation.yaml` does not exist, `ggvalet rotate` writes a proposed one
  (every observed host gets `pat`) and stops. Review and edit it — declaring
  a credential type is a statement about what the host actually uses; ggvalet
  will not guess.
- The 65/30 day defaults leave 35 days of slack between a missed rotation and
  actual token expiry. `ggvalet doctor` starts warning once a token's
  remaining life drops below one full rotation cycle
  (`pat_expiry_days - cadence_days`), because that means at least one
  scheduled rotation has already been missed.

---

## Verifying the schedule is running

`ggvalet rotate --check` is local-only: it reads `rotation.yaml` and the local
state database and never makes a network call. It can tell you a host is
*due*; it cannot tell you a token is broken. Use `ggvalet doctor` for that —
it is the only command that checks token health remotely (active/revoked,
near-expiry, and whether the token has lost the `self_rotate` scope it needs
to rotate itself).

To confirm the monthly schedule is actually firing:

```bash
launchctl list | grep ggvalet-rotate     # agent is loaded
tail -n 50 ~/.local/log/ggvalet-rotate.log   # last run's output
ggvalet doctor                           # remote token health, right now
```

If the schedule silently stopped running, `doctor` is what will catch it —
tokens will show as approaching expiry (or already past the point where
`self_rotate` cadence should have refreshed them) even though `rotate --check`
looked fine on its own. Check, in order:

1. `launchctl list | grep ggvalet-rotate` — if this prints nothing, the agent
   isn't loaded. Reload it (see [Installing the schedule](#installing-the-schedule)).
2. The log file at `StandardOutPath`/`StandardErrorPath` in the plist
   (`~/.local/log/ggvalet-rotate.log`) — check the timestamp of the last
   entry against the schedule.
3. `ggvalet doctor` — if a PAT-declaring host shows near-expiry or missing
   `self_rotate`, rotation has not been running (or has been failing) for
   that host regardless of what the scheduler reports.

### A note on missed runs

`StartCalendarInterval` fires launchd's calendar trigger; it does **not**
queue up one run per missed occurrence. If the machine is asleep or off at
04:00 on the 1st, macOS runs the job **once**, at the next wake — it does not
run it once for every day it was missed. Do not read one catch-up run as "we
only missed one occurrence"; check `doctor` for the actual token age rather
than inferring it from the log.

---

## SSH keys

SSH keys are audited, never rotated automatically:

```bash
ggvalet ssh audit          # table
ggvalet ssh audit --json   # machine-readable
```

This checks for drift between your `ssh_config`, the key files that actually
exist on disk, and what `rotation.yaml` declares — a dangling `IdentityFile`
reference (`DANGLING_REF`), a key filename that would break as an
`ssh_config` value (`CORRUPT_NAME`), and any mismatch against hosts
`rotation.yaml` marks as using `ssh`. It never writes to `~/.ssh`, never
generates a key, and never touches a remote host. A finding only fails the
command's exit code when it concerns a host `rotation.yaml` marks as ggvalet's
responsibility (`ssh` in that host's `credentials` list) — findings on
unmanaged hosts are reported but do not fail the run.

If `ggvalet ssh audit` reports a problem, a human generates the new key,
installs it on the remote host, updates `ssh_config`, and re-runs the audit to
confirm it's clean. `ggvalet` does not do any part of that for you.

---

## Installing the schedule

The plist at `deploy/launchd/com.mchorfa.ggvalet-rotate.plist` runs `ggvalet
rotate` at 04:00 on the 1st of every month. It assumes the binary is at
`/usr/local/bin/ggvalet` and logs to `~/.local/log/ggvalet-rotate.log` — edit
both paths if yours differ, then re-validate:

```bash
plutil -lint deploy/launchd/com.mchorfa.ggvalet-rotate.plist
```

**Rollout order — do not load the agent first.** An unattended rotation on a
host you have not verified is a worse failure mode than a manual one, because
nobody is watching it happen:

1. `ggvalet rotate --check` — confirms `rotation.yaml` exists and reads back
   what you expect, without touching a host.
2. One watched `ggvalet rotate --force --host <one-host>` — rotate a single
   host by hand and confirm the tool you actually use against that host
   (`glab api /version`, or equivalent) still works afterward.
3. Only once that passed, install and load the schedule:

   ```bash
   mkdir -p ~/Library/LaunchAgents ~/.local/log
   cp deploy/launchd/com.mchorfa.ggvalet-rotate.plist ~/Library/LaunchAgents/
   launchctl load ~/Library/LaunchAgents/com.mchorfa.ggvalet-rotate.plist
   launchctl list | grep ggvalet-rotate
   ```

   Expect a line for `com.mchorfa.ggvalet-rotate` in the `launchctl list`
   output once it is loaded.

To stop the schedule:

```bash
launchctl unload ~/Library/LaunchAgents/com.mchorfa.ggvalet-rotate.plist
```
