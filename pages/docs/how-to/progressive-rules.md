# How-To: Discover, Simulate, and Promote Progressive Rules

This guide demonstrates how to use `ggvalet rules` to advance candidate policies through the 5 progressive governance stages without breaking engineering velocity:

```text
DISCOVERED ──→ SIMULATED ──→ SHADOW ──→ WARN ──→ ENFORCE
```

---

## 1. Discovering Optimization Candidates

Analyze pipeline execution traces to spot recurring patterns (e.g. repeated unchanged nightly integration reruns):

```bash
ggvalet rules discover \
  --pattern "Unchanged nightly integration rerun without dependency drift" \
  --runs 100 \
  --matches 83
```

Output:
```text
✓ Formulated new candidate rule: cand-8b12f490
  Title: Skip unchanged nightly integration rerun
  Stage: DISCOVERED
  Match Frequency: 83.0% across 100 runs
  Estimated Savings: 54.0% reduction in runner minutes
```

---

## 2. Listing Active and Candidate Rules

List all rules stored in your local ledger:

```bash
ggvalet rules list
```

---

## 3. Simulating Against Historical Traces

Before promoting a candidate, verify that it does not introduce false positives by simulating it against historical traces:

```bash
ggvalet rules simulate --id cand-8b12f490
```

Output:
```text
✓ Simulated candidate rule: cand-8b12f490
  Pass Rate in Simulation: 99.40%
  Stage advanced to: SIMULATED
  Status: Rule is safe to promote to SHADOW or WARN upon human approval.
```

---

## 4. Promoting Through Progressive Stages

### Stage 1: SHADOW (Telemetry-Only)
Collect live telemetry without altering developer workflows:
```bash
ggvalet rules promote --id cand-8b12f490 --stage SHADOW
```

### Stage 2: WARN (Educational Feedback)
Emit non-blocking warnings in MR comments or terminal output:
```bash
ggvalet rules promote --id cand-8b12f490 --stage WARN
```

### Stage 3: ENFORCE (Hard Gate)
Block non-conformant pipelines at the merge gate. Requires explicit approver sign-off:
```bash
ggvalet rules promote \
  --id cand-8b12f490 \
  --stage ENFORCE \
  --approver "sec-lead@company.internal"
```
Output:
```text
✓ Promoted candidate rule: cand-8b12f490
  New Stage: ENFORCE
  Approved By: sec-lead@company.internal
  Updated At: 2026-10-03 21:35:00 UTC
```
