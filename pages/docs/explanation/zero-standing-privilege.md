# Explanation: Zero Standing Privilege & Capability Leases

This article explains why ambient administrative credentials fail in automated delivery environments, and how `ggvalet` implements zero standing privilege through attenuated capability leases.

---

## The Failure of Ambient Authority

In conventional CI/CD setups, tools and runner jobs inherit static, long-lived API tokens (`GLPAT`, `GITHUB_TOKEN`). These credentials provide broad ambient authority:
- A pipeline needing only to upload test results holds permissions to modify branch protection rules.
- An automated dependency bot compromised via supply chain confusion can delete releases or push rogue commits.
- If an agent makes an unintended API call, the ambient credential permits it unconditionally.

Possession of a bearer token is not authorization.

---

## Capability Leases: Attenuated and Time-Bounded

`ggvalet` decouples identity from capability. Even when a valid GitLab token is configured in `glab` CLI or env vars, `ggvalet` executes under an explicit **Capability Lease**:

```text
Root Authority
     │
     ▼
CapabilityLease (Subject, Role, TargetScope, Expiry)
     │
     ├── CapReadProject
     ├── CapAudit
     └── [CapModifyProtectedBranch ── DENIED for valet-observer]
```

### Properties of a Capability Lease
1. **Explicit**: Every operation must declare the lease under which it acts.
2. **Attenuated**: A child lease cannot exceed the capability boundaries of its parent role.
3. **Time-Bounded**: Leases expire automatically (e.g. 1 hour default), preventing credential hoarding.
4. **Observable**: Every lease issuance, evaluation, and denial is committed to the immutable SQLite WAL.

---

## Refusal as a First-Class System Response

In traditional CLI tools, an unauthorized operation results in an unhandled crash or a generic HTTP 403 error. 

In `ggvalet`, an unauthorized action is a governed transition. When an entity attempts an operation exceeding its active lease:
1. The engine rejects the action before making any remote network call.
2. It generates a cryptographically signed `RefusalReceipt` containing the required capability, actor identity, and SHA-256 evidence digest.
3. It logs the refusal to `refusal_receipts` in SQLite.
4. It enables programmatic fallback: under `--remediate`, the refused change is converted into an advisory Merge Request.

This ensures automated agents fail safely, never hallucinate unauthorized actions, and provide complete auditability for security teams.
