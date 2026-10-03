# How-To: Synchronize Across Federated GitLab Instances

This guide covers bidirectional synchronization of issues, epics, and milestones across disparate GitLab instances with cryptographic markers and conflict quarantine.

---

## 1. Indexing Cryptographic Sync Markers

Scan the destination project to catalog existing entities and their SHA-256 markers:

```bash
ggvalet sync index scan \
  --src-host gitlab.thalesdigital.io \
  --src-project group/repo-a \
  --dst-host sc01-trt.thales-systems.ca/gitlab \
  --dst-project group/repo-b
```

View the indexed entities:
```bash
ggvalet sync index list
```

---

## 2. Analyzing Cross-Instance Drift

Detect whether entities have been modified on the source, destination, or both:

```bash
ggvalet sync drift \
  --src-host gitlab.thalesdigital.io \
  --src-project group/repo-a \
  --dst-host sc01-trt.thales-systems.ca/gitlab \
  --dst-project group/repo-b
```

Possible drift states:
- `IN_SYNC`: Identical SHA-256 digests.
- `SOURCE_MODIFIED`: Source changed since last sync; destination can be fast-forwarded.
- `DEST_MODIFIED`: Destination changed independently.
- `CONFLICT`: Both endpoints modified concurrently without coordination.

---

## 3. Reconciling Drift Safely

Always run with `--dry-run` first:

```bash
ggvalet sync reconcile \
  --src-host gitlab.thalesdigital.io \
  --src-project group/repo-a \
  --dst-host sc01-trt.thales-systems.ca/gitlab \
  --dst-project group/repo-b \
  --dry-run
```

Execute live reconciliation:
```bash
ggvalet sync reconcile \
  --src-host gitlab.thalesdigital.io \
  --src-project group/repo-a \
  --dst-host sc01-trt.thales-systems.ca/gitlab \
  --dst-project group/repo-b \
  --yes
```

---

## 4. Inspecting Conflict Quarantine

When concurrent conflicting edits occur, `ggvalet` does not overwrite either endpoint. Instead, it quarantines the entity in `SAFE_HOLD`:

```bash
# List quarantined conflicts
ggvalet sync quarantine list

# Inspect forensic snapshot of conflicting sides
ggvalet sync quarantine inspect --id quar-c70f785b
```

Output includes:
- Source and destination entity keys.
- Timestamp of conflict.
- Raw JSON snapshots of both versions.
- Baseline SHA-256 digest prior to divergence.
