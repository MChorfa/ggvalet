# Reference: System Architecture & Schemas

This document defines the formal schemas, mathematical definitions, and database structures that govern `ggvalet`.

---

## 1. Vector State Schema

Governed reality is represented as a typed 6-tuple product:
\[
S(e,t) = \langle P, V, A, C, E, L, \tau \rangle
\]

| Dimension | Type | Values | Semantics |
| :--- | :--- | :--- | :--- |
| **P (Presence)** | `string` | `PRESENT`, `EMPTY`, `UNKNOWN`, `REDACTED` | Distinguishes absence of value from negative truth. |
| **V (Valence)** | `string` | `POSITIVE`, `NEGATIVE`, `NEUTRAL`, `MIXED` | Direction of evidence relative to requirement. |
| **A (Anti)** | `string` | `NONE`, `CONTRADICTS`, `ATTACKS`, `INVALIDATES` | Structural opposition. Dominates aggregate scores. |
| **C (Coherence)** | `string` | `COHERENT`, `PARTIAL`, `DECOHERENT`, `RECONCILING` | Divergence between observed and canonical reality. |
| **E (Evidence)** | `string` | `UNVERIFIED`, `SELF_ASSERTED`, `OBSERVED`, `VERIFIED`, `ATTESTED`, `TAMPERED` | Cryptographic epistemic standing of facts. |
| **L (Lifecycle)** | `string` | `UNINITIALIZED`, `ADMITTED`, `ACTIVE`, `DEGRADED`, `SAFE_HOLD`, `QUARANTINED`, `TERMINATED` | Current operational phase. |
| **$\tau$ (Epoch)** | `int64` | Monotonically increasing integer | State version / temporal position. |

### Serialized JSON Representation
```json
{
  "entity_id": "gitlab-shared/team-b:branch_protection",
  "presence": "PRESENT",
  "valence": "NEGATIVE",
  "anti": "CONTRADICTS",
  "coherence": "DECOHERENT",
  "evidence": "VERIFIED",
  "mode": "DEGRADED",
  "epoch": 1,
  "timestamp": "2026-10-03T21:30:00Z",
  "summary": "Branch rules allow direct pushes to main"
}
```

---

## 2. Capability Lease Schema

Execution permissions are granted through time-bounded, attenuated leases:

```json
{
  "lease_id": "lease-8f1902bc-4401",
  "subject": "alice.developer",
  "role": "valet-observer",
  "target_scope": "gitlab-shared/*",
  "capabilities": [
    "READ_PROJECT",
    "READ_CONFIG",
    "AUDIT"
  ],
  "issued_at": "2026-10-03T21:00:00Z",
  "expires_at": "2026-10-03T22:00:00Z"
}
```

---

## 3. SQLite WAL Database Schema

Local durable state is maintained in `~/.local/share/ggvalet/state.db` under SQLite WAL mode:

### Key Tables
1. **`vector_states`**:
   - `entity_id TEXT PRIMARY KEY`, `presence TEXT`, `valence TEXT`, `anti TEXT`, `coherence TEXT`, `evidence TEXT`, `mode TEXT`, `epoch INTEGER`, `timestamp DATETIME`, `summary TEXT`.
2. **`refusal_receipts`**:
   - `receipt_id TEXT PRIMARY KEY`, `actor TEXT`, `role TEXT`, `required_capability TEXT`, `resource TEXT`, `reason TEXT`, `evidence_digest TEXT`, `timestamp DATETIME`.
3. **`sync_index`**:
   - `id TEXT PRIMARY KEY`, `src_host TEXT`, `src_project TEXT`, `entity_type TEXT`, `src_iid INTEGER`, `src_url TEXT`, `dst_host TEXT`, `dst_project TEXT`, `dst_iid INTEGER`, `dst_url TEXT`, `content_digest TEXT`, `sync_epoch INTEGER`, `status TEXT`, `last_synced_at DATETIME`.
4. **`quarantine_records`**:
   - `id TEXT PRIMARY KEY`, `entity_key TEXT`, `entity_type TEXT`, `src_host TEXT`, `src_project TEXT`, `src_iid INTEGER`, `dst_host TEXT`, `dst_project TEXT`, `dst_iid INTEGER`, `src_snapshot TEXT`, `dst_snapshot TEXT`, `baseline_digest TEXT`, `quarantine_reason TEXT`, `resolution_status TEXT`, `resolved_by TEXT`, `quarantined_at DATETIME`, `resolved_at DATETIME`.
5. **`candidate_rules`**:
   - `id TEXT PRIMARY KEY`, `title TEXT`, `description TEXT`, `pattern_observed TEXT`, `stage TEXT`, `historical_runs INTEGER`, `match_frequency REAL`, `estimated_savings TEXT`, `simulated_pass_rate REAL`, `approved_by TEXT`, `created_at DATETIME`, `updated_at DATETIME`.
6. **`persona_states`**:
   - `id TEXT PRIMARY KEY`, `persona_type TEXT`, `authority TEXT`, `lease_id TEXT`, `gal_level TEXT`, `status TEXT`, `onboarded_at DATETIME`, `offboarded_at DATETIME`, `evidence_digest TEXT`.
