# Reference: Naming Taxonomy & Rules of Engagement

This document details the naming conventions and operational rules of engagement governing `ggvalet`.

---

## 1. Naming Taxonomy (`CKX-TAX-001`)

All governed entities follow strict kebab-case and namespacing rules:

```text
<tenant>-<domain>-<component>[-<subsystem>]
```

### Entity Prefixes

| Entity Type | Regex Pattern | Example |
| :--- | :--- | :--- |
| **Persona ID** | `^[a-z0-9._-]+$` | `alice.developer`, `reconciler-bot` |
| **Project ID** | `^[a-z0-9_-]+\/[a-z0-9._-]+$` | `cortaix/space-defense`, `infra/runner-cluster` |
| **Candidate Rule** | `^cand-[a-f0-9]{8}$` | `cand-8b12f490` |
| **Receipt ID** | `^rcpt-[a-z]+-[a-f0-9-]+$` | `rcpt-refusal-1102`, `rcpt-tw-8d4290` |
| **Quarantine ID**| `^quar-[a-f0-9-]+$` | `quar-c70f785b` |
| **Bundle ID** | `^bundle-[a-f0-9-]+$` | `bundle-c1ae5d8b` |
| **Trajectory ID**| `^traj-[a-f0-9-]+$` | `traj-e92a812b` |

---

## 2. Rules of Engagement & Governed Autonomy Levels (GAL)

Autonomous agents operating with `ggvalet` are classified under 4 Governed Autonomy Levels:

### GAL 0: Passive Observer
- **Role**: `valet-observer`
- **Capabilities**: `READ_PROJECT`, `READ_CONFIG`, `AUDIT`
- **Side Effects**: Forbidden. Any attempt immediately produces a `RefusalReceipt`.
- **Use Case**: Status queries, compliance auditing, documentation generation.

### GAL 1: Governed Co-Pilot (Default)
- **Role**: `valet-advisor`
- **Capabilities**: `READ_PROJECT`, `AUDIT`, `CREATE_MR`, `PROPOSE_REMEDIATION`
- **Side Effects**: Non-destructive. All changes are submitted as Merge Requests for human approval.
- **Use Case**: Standard day-to-day coding assistant and CI/CD maintenance bot.

### GAL 2: Bounded Autonomous Operator
- **Role**: `valet-reconciler`
- **Capabilities**: `READ_PROJECT`, `AUDIT`, `MODIFY_PROTECTED_BRANCH`, `MODIFY_CONFIG`, `MODIFY_PIPELINE`
- **Side Effects**: Permitted only within declared project scope. Must produce dry-run plan before execution. All mutations are recorded in SQLite WAL.
- **Use Case**: Autonomous Day-2 drift reconciliation engine, scheduled compliance enforcer.

### GAL 3: Sovereign Controller
- **Role**: `valet-admin-test`
- **Capabilities**: All capabilities + bypass check.
- **Side Effects**: High blast radius.
- **Use Case**: Restricted exclusively to isolated Delivery Laboratory environments and automated CI test rigs. Prohibited on production remotes.

---

## 3. Golden Pipeline v7.0.0 Stage Matrix

Every project onboarded via `ggvalet lifecycle onboard project` receives `.gitlab-ci.yml` conforming to:

```yaml
stages:
  - build
  - test
  - security-sast
  - security-secrets
  - package
  - sign-attest
```

- Mandatory templates included:
  - `Jobs/Secret-Detection.gitlab-ci.yml`
  - `Jobs/SAST.gitlab-ci.yml`
  - `Jobs/Dependency-Scanning.gitlab-ci.yml`
- Syft CycloneDX SBOM generation in `package` stage.
- Cosign keyless or KMS container signing in `sign-attest` stage.
