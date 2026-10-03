# Explanation: Bounded Resilience & Day-2 Reconciliation

This article explains why unmanaged retries cause systemic outages, and how `ggvalet` achieves bounded resilience across federated infrastructure.

---

## "Retry" Is Not Resilience

When network degradation or API rate limits occur, standard scripts and CI jobs retry immediately:
- Hundreds of parallel pipeline jobs encounter an HTTP 429 (Too Many Requests).
- Each job retries in a tight loop or with static delays.
- The destination API experiences a thundering herd, crashing under compounded load.

Unbounded retries amplify failure.

---

## The 3-Tier Bounded Resilience Engine

All outbound operations in `ggvalet` (`internal/resilience`) must pass through three deterministic bounds:

```text
Outbound Request
       │
       ▼
[1. Token-Bucket Rate Limiter] ──→ Enforces max requests/sec (e.g. 10 req/s)
       │
       ▼
[2. Circuit Breaker]          ──→ Trips to OPEN on 5 consecutive failures
       │
       ▼
[3. Jittered Exponential]     ──→ Decorrelated jitter backoff with budget cap
```

### 1. Token-Bucket Rate Limiter
Prevents sudden request spikes by metered token replenishment. Bursts are bounded, and callers wait cooperatively.

### 2. Tri-State Circuit Breaker
Transitions between `CLOSED` (normal operation), `OPEN` (calls fail fast without network traffic during outages), and `HALF_OPEN` (probes connectivity with limited trial requests). This prevents cascading failure across federated instances.

### 3. Jittered Exponential Backoff
Transient errors (HTTP 429, 502, 503, 504, connection timeouts) are retried using Full Jitter:
\[
t_{\text{wait}} = \text{random}(0, \min(M, B \times 2^{\text{attempt}}))
\]
The randomness breaks retry synchronization and eliminates thundering herds.

---

## The Day-2 Control Loop

Day-2 operations cannot rely on "restart and hope." `ggvalet` models continuous lifecycle maintenance as a closed reconciliation loop:

```text
OBSERVE ──→ DETECT ──→ DIAGNOSE ──→ DEGRADE ──→ CONTAIN ──→ RECOVER ──→ VERIFY ──→ RECONCILE
```

1. **Observe**: Continuously inspect observed state against canonical declarations.
2. **Detect**: Calculate drift and classify deviations.
3. **Diagnose**: Formulate minimal reversible transition vectors.
4. **Degrade & Contain**: Isolate conflicting or unverified entities into `SAFE_HOLD` or `QUARANTINED`.
5. **Recover & Verify**: Reconcile authorized mutations under attenuated capability leases.
6. **Reconcile**: Commit durable SHA-256 evidence to SQLite WAL, restoring system coherence.
