# Tutorial: Your First Governed Delivery Audit & Onboarding

This tutorial guides you through your first hands-on session with `ggvalet`. You will spin up the local delivery laboratory, execute a compliance audit, observe capability lease enforcement, and onboard a new project under canonical delivery standards.

---

## Prerequisites
- `ggvalet` binary built and available on your `$PATH` (see [Installation](../install.md)).
- Local terminal environment (Linux or macOS).

---

## Step 1: Verify Installation & Preflight Environment

Check the operational health of your local SQLite WAL ledger and active provider configurations:

```bash
ggvalet doctor
```

Expected output:
```text
=== GG VALET DOCTOR ===
Configuration:      VALID
SQLite Store:       OPERATIONAL (~/.local/share/ggvalet/state.db)
Journal:            OPERATIONAL
Providers Detected: 2 (gitlab.thalesdigital.io, sc01-trt.thales-systems.ca/gitlab)
Status:             READY
```

Confirm which instances are registered in your configuration:
```bash
ggvalet hosts
```

---

## Step 2: Spin Up the Delivery Laboratory

`ggvalet` includes a built-in simulation environment for testing governance and high-assurance workflows without touching production infrastructure:

```bash
# 1. Spin up the simulated delivery topology
ggvalet lab up

# 2. Inspect active laboratory domains
ggvalet lab status
```

You will observe 5 isolated domains:
- `gitlab-shared.local`: Multi-tenant developer environment (Teams A, B, and C).
- `gitlab-dedicated.local`: High-assurance controlled project environment.
- `trustwall.local`: Mandatory admission and verification station.
- `transfer-station.local`: Unidirectional hardware diode station.
- `gitlab-airgap.local`: Physically isolated destination registry.

Now, populate the laboratory with 30 realistic compliance and security deviations:
```bash
ggvalet lab seed
```

---

## Step 3: Run Your First Compliance Audit

Audit project `gitlab-shared/team-b` against canonical standard v7.0.0:

```bash
ggvalet audit --project gitlab-shared/team-b
```

Output:
```text
=== GG VALET AUDIT: gitlab-shared/team-b ===
Canonical Standard: v7.0.0
Vector State: S⟨P:PRESENT, V:NEGATIVE, A:CONTRADICTS, C:DECOHERENT, E:VERIFIED, L:DEGRADED, τ:1⟩

+--------------+------------------------+--------------+--------------------------------+-------------------------+------------+
|      ID      |          KIND          |   CATEGORY   |             TITLE              |      REQUIRED CAP       | REVERSIBLE |
+--------------+------------------------+--------------+--------------------------------+-------------------------+------------+
| FND-1b56eb54 | DOCUMENTED_REQUIREMENT | SECURITY     | Main branch unprotected        | MODIFY_PROTECTED_BRANCH | Yes        |
| FND-d32cc5f5 | DOCUMENTED_REQUIREMENT | SECURITY     | Unmasked API token in CI       | MODIFY_CONFIG           | Yes        |
| FND-9fdbabca | INFERRED_OPTIMIZATION  | SUPPLY_CHAIN | Mutable container image tag    | CREATE_MR               | Yes        |
+--------------+------------------------+--------------+--------------------------------+-------------------------+------------+

Recommended Actions:
  • [FND-1b56eb54] Main branch unprotected
    Action: Configure protected branch rules for main to require MR and maintainer approval
  • [FND-d32cc5f5] Unmasked API token in CI variable
    Action: Mask the DEPLOY_KEY variable in project settings
```

Notice that the project's vector state is `DECOHERENT` and `DEGRADED` because mandatory security invariants (`A:CONTRADICTS`) dominate the score.

---

## Step 4: Reconcile Under Capability Lease Authority

`ggvalet` operates under zero standing privilege. Let's observe what happens when we attempt to reconcile under the default read-only `valet-observer` role:

```bash
ggvalet reconcile --project gitlab-shared/team-b --role valet-observer
```

Output:
```text
=== GG VALET RECONCILIATION: gitlab-shared/team-b ===
Effective Role: valet-observer (Lease: lease-8f19...)
Mode: DRY-RUN

Refused Actions (2) — Insufficient Authority:
+-------------------+-------------------------+------------------------+-----------------+
|    RECEIPT ID     |      REQUIRED CAP       |         REASON         | EVIDENCE DIGEST |
+-------------------+-------------------------+------------------------+-----------------+
| rcpt-refusal-1102 | MODIFY_PROTECTED_BRANCH | insufficient_authority | 4ac463f7844f... |
| rcpt-refusal-1103 | MODIFY_CONFIG           | insufficient_authority | a92be14108cb... |
+-------------------+-------------------------+------------------------+-----------------+
```

Because `valet-observer` lacks mutation capabilities, `ggvalet` refused the side effects and committed immutable refusal receipts to SQLite.

Now, execute reconciliation under the authorized `valet-reconciler` role:
```bash
# Preview planned changes first
ggvalet reconcile --project gitlab-shared/team-b --role valet-reconciler --dry-run

# Apply authorized transitions
ggvalet reconcile --project gitlab-shared/team-b --role valet-reconciler --yes
```

Output:
```text
=== GG VALET RECONCILIATION: gitlab-shared/team-b ===
Effective Role: valet-reconciler
Mode: LIVE EXECUTION

Applied Mutations (3):
  ✓ RECONCILED: Main branch unprotected -> Configure protected branch rules for main
  ✓ RECONCILED: Unmasked API token in CI variable -> Mask the DEPLOY_KEY variable
  ✓ RECONCILED: Mutable container image tag :latest -> Pin container image by immutable SHA256
```

---

## Step 5: Onboard a New Project Persona

Automate Day-2 baseline setup for a new repository `cortaix/space-defense`:

```bash
ggvalet lifecycle onboard project --id cortaix/space-defense --output-dir ./demo-repo
```

This generates:
1. Canonical `.gitlab-ci.yml` (Golden Pipeline v7.0.0 with SAST, Secret-Detection, Dependency-Scanning, Syft SBOM generation, and Cosign signing).
2. Branch protection rule JSON enforcing 2 required maintainer reviews.
3. Runner concurrency bounds (max 4 parallel jobs).
4. An immutable onboarding receipt recorded in SQLite.

Verify active personas:
```bash
ggvalet lifecycle status
```

---

## Next Steps
- Learn how to run autonomous planning loops in the [Next-Gen Agent Loop Tutorial](autonomous-agent-loop.md).
- Explore persona management in the [How-To Onboard Personas Guide](../how-to/onboard-personas.md).
