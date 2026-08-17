# ggvalet credential rotation — design

Date: 2026-08-15
Status: approved design, not yet implemented
Scope: monthly PAT rotation across configured GitLab hosts; SSH inventory (read-only) in v1

## 1. Problem

ggvalet reads credentials from glab's config (`~/.config/glab-cli/config.yml`) and
never writes them. It owns no credential storage: `~/.ggvalet/` holds a journal,
a state database, and a cache. There is no rotation, no schedule, and no
inventory of SSH key material.

Rotation inverts the read-only relationship — ggvalet must write a secret it did
not previously own — so the design is shaped almost entirely by one hazard
described in §4.

## 2. Observed state (2026-08-15)

Established by probing, not assumed. These facts drive several decisions.

| Fact | Value |
|---|---|
| `gitlab.thalesdigital.io` version | 19.1.4-ee — well past the 16.0 floor for token self-rotation |
| Active PAT | id 51629, `MC_GITHUB_TRUSTNEST_SA_DEV_OSX_TDS_FULL`, expires 2027-05-18 |
| PAT scopes | include **`self_rotate`** — self-service rotation needs no admin token |
| `sc01-trt.thales-systems.ca` | **401 Unauthorized** — credential already dead |
| `doctor` | probes only the *default* host, which is why it reports "all checks passed" while sc01-trt is broken |
| `config.yml` | mode 0600, 2200 bytes, **contains comments**, 4-space indent, one `token:` per host block |
| SSH: registered on thalesdigital | 1 key, `MNC_GITLAB_CORTAIX_SA_DEV_MBP_ED25519_KEY`, expires 2027-02-03 |
| SSH: `~/.ssh/config` | 3 of 7 `IdentityFile` entries point at files that do not exist |
| SSH: `~/.ssh` | a corrupt-named keypair exists — `MNC_GITLAB_CORTAIX_SA_DEV_MBP_ED25519_KEY \t` (literal space+tab) plus its `.pub` |
| SSH on sc01-trt | no Host block in `~/.ssh/config` — PAT only |

Two consequences worth stating plainly. First, the token is valid until 2027, so
monthly rotation is a **policy choice, not an expiry necessity** — failure
handling should be proportionate. Second, the SSH drift that exists is on
`gitlab.com` and GitHub blocks, not on a Thales host; the one managed SSH key is
healthy.

## 3. Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Custody | Write back to `config.yml` | glab stays the single source of truth; no new dependency and no second credential path to drift |
| Trigger | `ggvalet rotate` + launchd agent | ggvalet stays stateless and non-daemonised; the schedule is inspectable outside the tool, matching the existing `com.mchorfa.cargo-sweep` pattern |
| SSH in v1 | Inventory and report only | The estate is already drifted; rotating on top of it would automate a mess |
| Safety model | Write-ahead escrow | Puts complexity exactly at the hazard and nowhere else |
| Host capability | Declared, not detected | A 401 host cannot be probed to discover whether it uses SSH — the host most likely to be mis-scoped is the broken one |

Rejected: a `plan`/`reconcile` integration (that engine models multi-target
GitLab object graphs; rotation is a 3-step linear sequence over 1–2 hosts, so it
would bend an unrelated abstraction for resumability the escrow already
provides), and a built-in scheduler (duplicates launchd).

## 4. The hazard

`POST /personal_access_tokens/self/rotate` revokes the old token server-side and
returns its replacement **exactly once**. GitLab offers no self-service
create-PAT API for non-admins, so rotation is the only available path and this
property cannot be designed away.

Therefore: **the new secret is persisted before anything else happens to it.**

## 5. Command surface

```
ggvalet rotate              # rotate PATs on hosts that are due
ggvalet rotate --check      # report only; exit 1 if anything is due or broken
ggvalet rotate --host H     # single host
ggvalet rotate --force      # ignore cadence
ggvalet rotate --recover    # replay an uncommitted escrow

ggvalet ssh audit           # inventory + drift report; --json supported
```

Both groups are host-specific and are registered as guarded leaves in
`hostguard`; neither may be added to `hostNeutralPrefixes`.

Deliberately excluded: `rotate --revoke-all` (a footgun with no monthly use
case), and any write path in `ssh audit`.

## 6. Configuration

```yaml
# ~/.ggvalet/rotation.yaml
version: 1
defaults: { cadence_days: 30, pat_expiry_days: 65 }
hosts:
  gitlab.thalesdigital.io:      { credentials: [pat, ssh] }
  sc01-trt.thales-systems.ca:   { credentials: [pat] }
```

If the file is absent, `rotate --check` writes a **proposed** file derived from
observed state and refuses to rotate until the operator confirms it.

Cadence 30 days against a 65-day expiry tolerates exactly one missed cycle: a
laptop asleep on the 1st degrades to a warning rather than a lockout, while two
consecutive misses fail loudly with time still on the clock.

## 7. Rotation state machine

```
PREFLIGHT ──▶ ROTATED ──▶ VERIFIED ──▶ COMMITTED ──▶ done
   │             │            │             │
   ▼             ▼            ▼             ▼
 abort        escrow held — recoverable via --recover
(no-op)
```

**Phase 0 — Preflight.** Mutates nothing. Locate the host block; assert
`config.yml` is owner-writable; `GET /personal_access_tokens/self` and assert
`active && !revoked && scopes ∋ self_rotate`; assert no uncommitted escrow for
this host; check cadence in `state.db` unless `--force`; record the SHA-256 of
`config.yml`. Any failure aborts before a byte changes. An unauthenticated host
is a **hard failure**, never a skip — a monthly unattended job that quietly
skips a dead host is how the problem stays hidden for a year.

**Phase 1 — Rotate → escrow.** Call rotate with an explicit
`expires_at = today + defaults.pat_expiry_days` (65 days). The expiry is set by
ggvalet, never inherited — the existing token runs to 2027, which is the
condition this feature exists to end. Before logging, before touching
config, before any other work: write
`~/.ggvalet/escrow/<host>-<unix>.json` at mode 0600 and fsync **both the file
and its parent directory**. The escrow write is the literal next statement after
the response is parsed. Past this point the secret survives a crash.

**Phase 2 — Verify.** Authenticate against the host with the new token.

**Phase 3 — Commit.** Re-read `config.yml` and compare SHA-256 with the
preflight value; a mismatch means glab wrote underneath us, so **abort and keep
the escrow** — detect and stop, never merge. Locate the `token:` line by an
indentation-scoped walk (`hosts:` → 4-space host key → 8-space field), never a
global regex, because each host block has its own `token:`. Replace that line's
value only. Write a temp file in the same directory at 0600, fsync, `os.Rename`.
Re-read and confirm.

**Phase 4 — Confirm and record.** Reload config through ggvalet's normal path
and make a real API call, proving the credential works the way glab will consume
it. Record `last_rotated_at` in `state.db`, append a journal entry, delete the
escrow.

The escrow is deleted on success by design: `config.yml` now holds the same
secret at the same 0600, so retaining a second plaintext copy adds exposure
without adding recovery capability.

### Escrow record

```json
{ "version": 1, "host": "…", "state": "ROTATED|VERIFIED",
  "rotated_at": "…Z", "old_token_id": 51629, "new_token_id": 51630,
  "new_token": "glpat-…", "expires_at": "…", "scopes": ["api","self_rotate"],
  "config_path": "…", "config_sha256_before": "…" }
```

Directory 0700, files 0600. The secret is never logged and never journaled; the
journal records token id and a last-4 fingerprint only.

An escrow record only ever persists as `ROTATED` or `VERIFIED`. `COMMITTED` in
the diagram is a phase, not a stored state: reaching it deletes the record. A
recovery run therefore always finds `ROTATED` or `VERIFIED`, and is idempotent —
if it finds `config.yml` already carrying the escrowed token, it skips the write
and proceeds straight to recording and deletion.

### Concurrency

An advisory `flock` on `~/.ggvalet/rotate.lock` prevents overlapping ggvalet
rotations. It cannot stop `glab` from writing concurrently — that is what the
Phase 3 SHA-256 comparison is for.

## 8. Failure matrix

| Crash point | On-disk state | Recovery |
|---|---|---|
| During preflight | unchanged | re-run |
| **After rotate response, before escrow fsync** | **old revoked, new secret lost** | **none — manual PAT creation in the GitLab UI** |
| Escrow fsync'd, before verify | escrow `ROTATED` | `--recover` |
| Verified, before commit | escrow `VERIFIED` | `--recover` |
| Temp written, before rename | escrow + temp file | `--recover`; rename is atomic, config is old-or-new, never torn |
| Renamed, before state/journal | config updated, escrow present | `--recover` is idempotent — detects config already matches, records, deletes escrow |
| After escrow delete | complete | n/a |

The second row is the only unrecoverable window. It is stated rather than hidden
because it cannot be removed (§4); it is mitigated by doing no other work in
that path.

## 9. SSH audit

Matches on **SHA-256 public-key fingerprint, not filename** — filenames are
precisely what has drifted.

| Class | Meaning |
|---|---|
| `MATCHED` | local key ↔ config reference ↔ registered remotely |
| `DANGLING_REF` | `IdentityFile` points at a nonexistent file |
| `CORRUPT_NAME` | filename contains whitespace or control characters |
| `ORPHAN_LOCAL` | key on disk, unreferenced and unregistered |
| `ORPHAN_REMOTE` | registered on a host with no matching local private key |
| `EXPIRING` | remote key expiring within the configured window |

**Scope.** The audit reads the whole of `~/.ssh/config`, labelling each finding
`managed` (a host in `rotation.yaml` declaring `ssh`) or `unmanaged`, and exits
non-zero **only on managed drift**. A strictly-scoped audit would report nothing
today — the managed key is healthy and all real drift is on `gitlab.com` and
GitHub blocks — while a globally-failing audit would break the monthly job over
hosts ggvalet is not responsible for. Remote key listing is performed only for
hosts declaring `ssh`, so sc01-trt is never queried.

v1 never writes to `~/.ssh`.

**What v1 actually shipped (amended 2026-08-17, after the final branch review).**
The implemented audit is a **local inventory**: it matches on the
`IdentityFile` path, not on a SHA-256 fingerprint, and it does not list keys
registered on a remote host. So it produces `MATCHED`, `DANGLING_REF` and
`CORRUPT_NAME` only, where `MATCHED` means "the referenced file exists" rather
than the three-way match described above. `ORPHAN_LOCAL`, `ORPHAN_REMOTE` and
`EXPIRING` cannot arise without the remote listing and are **not** part of the
v1 `--json` contract; the constants and the unpopulated `Fingerprint` field
were removed rather than left declared, because a class a consumer can see but
that is never produced reads as "checked, nothing found". Fingerprint matching
and remote key listing are deferred to v1.1. The rest of this section — the
managed/unmanaged labelling, the exit-code rule, never querying sc01-trt, and
never writing to `~/.ssh` — is implemented as written.

## 10. `doctor` change

`doctor` iterates every configured host, reports per-host, and exits non-zero if
any host fails. `doctor --host H` preserves single-host behaviour. This is what
makes the sc01-trt 401 visible.

## 11. Scheduling

`com.mchorfa.ggvalet-rotate.plist`, modelled on the existing cargo-sweep agent:
monthly `StartCalendarInterval`, `ProcessType Background`, `LowPriorityIO`,
logging to `~/.local/log/ggvalet-rotate.log`. launchd coalesces missed calendar
events, so a machine asleep on the 1st rotates at next wake.

## 12. Testing

The fault-injection table test is the one that earns this design: abort at each
phase boundary and assert `--recover` converges to a working credential.

Alongside it:

- config write-back against fixtures with comments, two host blocks, and 4-space
  indent — asserting every byte outside the changed line is preserved
- regression: the indentation-scoped locator selects the correct host's `token:`
  when both hosts have one
- SHA-256 mismatch triggers abort with escrow retained
- escrow round-trip with 0600/0700 asserted
- SSH audit fixtures built from the shapes observed today, including the
  space+tab filename and the three dangling references
- `hostguard`: `rotate` and `ssh` are not host-neutral
- rotation integration against an `httptest` server; no real host in CI

New packages meet the repository's existing coverage gate.

## 13. Rollout

1. Ship `ssh audit` and `rotate --check` (read-only). Run a cycle; clear the
   sc01-trt 401 and the SSH drift they report.
2. One watched `rotate --force` against the primary host.
3. Install the launchd agent only after step 2 succeeds.

## 14. Out of scope for v1

SSH key rotation; repairing `~/.ssh/config`; regenerating the three missing key
files (those references may be dead config rather than missing keys — the intent
is unknown); CI-variable propagation; any host beyond the two configured.
