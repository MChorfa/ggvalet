# Explanation: Vector State vs Booleans in Governed Engineering

This article explains why simple boolean `PASS / FAIL` flags are insufficient for governed software delivery, and how `ggvalet` uses typed state vectors.

---

## The Illusion of the Boolean

Most CI/CD platforms collapse pipeline reality into a binary result:
```text
exit_code == 0 ? PASS : FAIL
```

This collapses critically distinct real-world situations:
- Did a security test pass because the vulnerability was fixed, or because the scanning tool timed out?
- Is an unverified configuration an acceptable temporary risk, or a mandatory security violation?
- Is absence of telemetry data proof that nothing went wrong?

Treating `UNKNOWN` as `PASS` (or as `FAIL`) creates security holes or false alerts.

---

## The 6-Dimensional State Vector

In `ggvalet`, entity state is modeled as:
\[
S(e,t) = \langle P, V, A, C, E, L, \tau \rangle
\]

### 1. Presence: Empty is Not Negative
- `PRESENT`: A fact or measurement was positively observed.
- `EMPTY`: No data exists (e.g. no test results uploaded).
- `UNKNOWN`: The property cannot presently be determined.
- `REDACTED`: The property exists but was hidden due to privacy or confidentiality constraints.

Absence of evidence is never treated as evidence of success.

### 2. Valence: The Direction of Evidence
- `POSITIVE`: Supports the target invariant (e.g. branch protection enabled).
- `NEGATIVE`: Valid adverse evidence (e.g. unmasked API token).
- `NEUTRAL`: Informational telemetry.
- `MIXED`: Partially conforming signals.

### 3. Anti: Structural Opposition
`ANTI` is not merely "a worse negative score." It represents a non-negotiable invariant violation:
- An artifact marked `TRUSTED` when its SLSA provenance was revoked.
- An agent marked `EXECUTING` when its capability lease expired.

### 4. Coherence: When Realities Diverge
- `COHERENT`: Observed state matches canonical standard.
- `DECOHERENT`: Observed state has diverged from canonical definition (e.g. manual Web UI edit bypassing GitOps).
- `RECONCILING`: Currently transitioning back into alignment.

---

## Invariant Dominance

A core rule of the CKODEX Constitution is:

> **Mandatory anti-invariant violations dominate scores. Never average away a contradiction.**

Consider an evaluation with:
- 99 passing unit and lint tests (`POSITIVE`).
- 1 unmasked production credential (`ANTI: CONTRADICTS`).

In a naive percentage-based scorecard, this would be $99\%$ conformant.
In `ggvalet`, the composite vector is:
```text
S⟨P:PRESENT, V:NEGATIVE, A:CONTRADICTS, C:DECOHERENT, E:VERIFIED, L:DEGRADED, τ:1⟩
```
The entity is classified as `DECOHERENT` and blocked from promotion until the anti-invariant is resolved.
