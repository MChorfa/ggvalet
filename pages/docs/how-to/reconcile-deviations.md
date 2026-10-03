# How-To: Reconcile Compliance Deviations

This guide explains how to audit and reconcile repository deviations against canonical delivery standards using `ggvalet audit` and `ggvalet reconcile`.

---

## 1. Auditing a Project

Run a non-destructive audit to detect deviations across security, supply chain, reliability, and configuration:

```bash
ggvalet audit --project my-group/my-project
```

To export findings for downstream processing or SIEM ingestion:
```bash
ggvalet audit --project my-group/my-project --json > audit-report.json
```

---

## 2. Dry-Run Reconciliation

Before executing mutations, always run with `--dry-run` to preview planned transitions:

```bash
ggvalet reconcile \
  --project my-group/my-project \
  --role valet-reconciler \
  --dry-run
```

Output details:
- **Planned Transitions**: Specific mutations authorized by the role (e.g. configuring branch protection, masking variables).
- **Refused Actions**: Any deviations requiring capabilities outside `valet-reconciler`.

---

## 3. Applying Authorized Transitions

To apply the planned transitions, specify `--yes`:

```bash
ggvalet reconcile \
  --project my-group/my-project \
  --role valet-reconciler \
  --yes
```

This performs:
1. Reversible API calls against the target provider.
2. Commits an execution receipt into SQLite WAL with input and output digests.
3. Updates the entity's VectorState from `DECOHERENT` to `COHERENT`.

---

## 4. Opening an Advisory Remediation Merge Request

If you are operating under `valet-advisor` or do not wish to mutate project settings directly, use `--remediate` to create a Merge Request containing the proposed fixes:

```bash
ggvalet reconcile \
  --project my-group/my-project \
  --role valet-advisor \
  --remediate
```

Output:
```text
✓ Advisory Remediation: Created Merge Request '!42 chore(ci): reconcile canonical compliance'
```
Developers can review the MR, run CI validation, and merge through standard code review channels.
