# How-To: Onboard and Offboard Personas

This guide provides step-by-step instructions for managing the 5 canonical delivery personas (`user`, `project`, `agent`, `service`, `auditor`) using `ggvalet lifecycle`.

---

## 1. Onboarding a Human Developer (`user`)

When a developer joins a project, onboard them to grant least-privilege access and enforce cryptographic commit signing:

```bash
ggvalet lifecycle onboard user \
  --id "alice.developer" \
  --role valet-observer \
  --authority "project-lead"
```

What this does:
- Validates the identity against naming taxonomy `CKX-TAX-001`.
- Grants an attenuated `valet-observer` capability lease.
- Records required GPG/SSH key verification obligations.
- Emits an onboarding evidence receipt to SQLite WAL.

---

## 2. Onboarding a Repository (`project`)

When creating a new repository or bringing an existing repository under canonical compliance:

```bash
ggvalet lifecycle onboard project \
  --id "cortaix/space-defense" \
  --output-dir ./my-repo
```

Generated artifacts:
- `.gitlab-ci.yml`: Canonical Golden Pipeline v7.0.0 with 6 stages (`build`, `test`, `security-sast`, `security-secrets`, `package`, `sign-attest`).
- `branch-rules.json`: Main branch protection template (zero direct pushes, 2 required maintainer approvals).
- `runner-limits.json`: Maximum concurrent jobs bounded to 4 to prevent queue starvation.

---

## 3. Onboarding an Autonomous Coding Agent (`agent`)

Autonomous agents must be registered with an explicit Governed Autonomy Level (GAL):

```bash
ggvalet lifecycle onboard agent \
  --id "reconciler-daemon" \
  --role valet-reconciler \
  --authority "sre-lead"
```

What this does:
- Enforces GAL 1 (Autonomous execution bounded by capability leases).
- Pins the agent to specific namespaces.
- Configures mandatory trajectory ledger logging in SQLite.

---

## 4. Onboarding a Service or Webhook (`service`)

For automated bots, webhooks, or event listeners:

```bash
ggvalet lifecycle onboard service \
  --id "gitlab-webhook-handler" \
  --authority "platform-security"
```

What this does:
- Provisions short-lived credentials.
- Configures HMAC-SHA256 signature verification on inbound payloads.
- Binds operations to the circuit breaker and rate limiter.

---

## 5. Onboarding a Compliance Auditor (`auditor`)

For internal or third-party regulatory assessors:

```bash
ggvalet lifecycle onboard auditor \
  --id "auditor-kpmg-2026" \
  --role valet-observer \
  --authority "compliance-officer"
```

What this does:
- Grants read-only access to SQLite receipts, refusal records, and trajectory ledgers.
- Prohibits all mutation capabilities.

---

## 6. Inspecting Persona State and Leases

View all active personas, their roles, and lease statuses:

```bash
# Formatted table
ggvalet lifecycle status

# Machine-readable JSON
ggvalet lifecycle status --json
```

---

## 7. Offboarding and Decommissioning

When a user leaves, an agent is decommissioned, or a project is archived, offboarding revokes standing credentials and freezes state into `SAFE_HOLD`:

```bash
# Offboard human user
ggvalet lifecycle offboard user \
  --id "alice.developer" \
  --reason "Contract completed"

# Offboard decommissioned project
ggvalet lifecycle offboard project \
  --id "cortaix/space-defense" \
  --reason "Project merged into main platform"
```

Result:
- All active capability leases are immediately revoked.
- Entity state transitions to `SAFE_HOLD` or `QUARANTINED`.
- An immutable exit receipt is recorded in the SQLite ledger.
