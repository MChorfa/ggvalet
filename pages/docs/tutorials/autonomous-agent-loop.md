# Tutorial: Formulating Intents & Executing the Autonomous Agent Loop

This tutorial teaches you how to operate `ggvalet agent`, the autonomous cognitive harness for safe, governed repository maintenance. You will learn the 6-phase cognitive loop, dry-run intent planning, and refusal pivoting.

---

## What You Will Learn
1. How `ggvalet` converts natural language or structural goals into typed `IntentEnvelope` primitives.
2. The 6-phase cognitive control loop (`ADMIT → OBSERVE → PLAN → CONFORMANCE-GATE → EXECUTE → EMIT-RECEIPT`).
3. How to inspect plans before side effects occur.
4. How to audit execution trajectories using the SQLite flight recorder.

---

## The 6-Phase Cognitive Loop

```text
       IntentEnvelope
             │
             ▼
      ┌─────────────┐
1.    │    ADMIT    │ ──→ Verify CapabilityLease and lease expiration
      └──────┬──────┘
             ▼
      ┌─────────────┐
2.    │   OBSERVE   │ ──→ Query project state, variables, branch configuration
      └──────┬──────┘
             ▼
      ┌─────────────┐
3.    │    PLAN     │ ──→ Formulate minimal transition path
      └──────┬──────┘
             ▼
      ┌─────────────┐
4.    │ CONFORMANCE │ ──→ Hard invariant check (Anti-state dominance)
      └──────┬──────┘
             ▼
      ┌─────────────┐
5.    │   EXECUTE   │ ──→ Apply changes (or emit RefusalReceipt if unprivileged)
      └──────┬──────┘
             ▼
      ┌─────────────┐
6.    │EMIT-RECEIPT │ ──→ Append trajectory step to SQLite ledger
      └─────────────┘
```

---

## Step 1: Formulate an Intent & Generate a Plan

Run `ggvalet agent plan` to dry-run an intent without executing any side effects:

```bash
ggvalet agent plan \
  --intent "Ensure compliance with canonical v7.0.0 for team-b" \
  --project gitlab-shared/team-b
```

Output:
```text
=== GG VALET AGENT PLAN (DRY-RUN) ===
Trajectory ID: traj-e92a812b
Intent: Ensure compliance with canonical v7.0.0 for team-b
Target: gitlab-shared/team-b
Evaluated Standard: v7.0.0

Planned Transitions (3):
  1. [MODIFY_PROTECTED_BRANCH] Protect main branch (push_level=0, merge_level=40)
  2. [MODIFY_CONFIG] Mask DEPLOY_KEY variable
  3. [CREATE_MR] Pin container image :latest to immutable SHA256 digest

No side effects were applied. Run 'ggvalet agent run --yes' under an authorized role to execute.
```

---

## Step 2: Execute Under an Attenuated Capability Lease

Now, execute the intent under the `valet-reconciler` role with `--yes`:

```bash
ggvalet agent run \
  --intent "Ensure compliance with canonical v7.0.0 for team-b" \
  --project gitlab-shared/team-b \
  --role valet-reconciler \
  --yes
```

Output:
```text
=== GG VALET AGENT EXECUTION ===
Trajectory ID: traj-a012bc44
Role: valet-reconciler (Lease: lease-9844-reconciler)
Status: COMPLETED_SUCCESSFULLY

Applied Transitions:
  ✓ [MODIFY_PROTECTED_BRANCH] Branch rules configured for main
  ✓ [MODIFY_CONFIG] Variable DEPLOY_KEY masked
  ✓ [CREATE_MR] Image digest pinned

Emitted Evidence:
  Receipt ID:       rcpt-exec-7821-ab90
  Evidence Digest:  c482098ea4f9c1048bb019e...
  Trajectory Steps: 6 recorded to SQLite WAL
```

---

## Step 3: Inspect Trajectory Flight Recorder

Every autonomous action leaves an immutable forensic trace in the SQLite ledger. Inspect the execution history:

```bash
ggvalet agent status --trajectory-id traj-a012bc44
```

Output:
```text
=== TRAJECTORY RECORD: traj-a012bc44 ===
Intent:             Ensure compliance with canonical v7.0.0 for team-b
Target:             gitlab-shared/team-b
Initial Vector:     S⟨P:PRESENT, V:NEGATIVE, A:CONTRADICTS, C:DECOHERENT, E:VERIFIED, L:DEGRADED, τ:1⟩
Resulting Vector:   S⟨P:PRESENT, V:POSITIVE, A:NONE, C:COHERENT, E:VERIFIED, L:ACTIVE, τ:2⟩
Total Steps:        6
Terminal Status:    SUCCEEDED
```

---

## Summary
You have verified that the agent:
- Operates under strict capability bounds.
- Plans deterministically before touching remote systems.
- Transitions the target entity from an adverse `DECOHERENT` state into a verified `COHERENT` state.
- Emits forensic proof that survives process crashes.
