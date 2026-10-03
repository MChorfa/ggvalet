package vector

import (
	"fmt"
	"time"
)

// Presence captures existence semantics.
// Rules: EMPTY != NEGATIVE, UNKNOWN != NEGATIVE, REDACTED != EMPTY.
type Presence string

const (
	PresenceEmpty    Presence = "EMPTY"
	PresencePresent  Presence = "PRESENT"
	PresenceUnknown  Presence = "UNKNOWN"
	PresenceRedacted Presence = "REDACTED"
)

// Valence captures directional effect of evidence relative to an invariant.
type Valence string

const (
	ValencePositive   Valence = "POSITIVE"
	ValenceNegative   Valence = "NEGATIVE"
	ValenceNeutral    Valence = "NEUTRAL"
	ValenceMixed      Valence = "MIXED"
	ValenceUnresolved Valence = "UNRESOLVED"
)

// AntiRelation models structural opposition.
// Anti invariants dominate scores; structural contradiction cannot be averaged away.
type AntiRelation string

const (
	AntiNone        AntiRelation = "NONE"
	AntiContradicts AntiRelation = "CONTRADICTS"
	AntiAttacks     AntiRelation = "ATTACKS"
	AntiInvalidates AntiRelation = "INVALIDATES"
)

// Coherence captures whether divergent representations of reality form one trustworthy state.
type Coherence string

const (
	Coherent    Coherence = "COHERENT"
	Partial     Coherence = "PARTIAL"
	Decoherent  Coherence = "DECOHERENT"
	Reconciling Coherence = "RECONCILING"
)

// EvidenceStatus represents the epistemic status of state claims.
type EvidenceStatus string

const (
	EvidenceObserved     EvidenceStatus = "OBSERVED"
	EvidenceVerified     EvidenceStatus = "VERIFIED"
	EvidenceInferred     EvidenceStatus = "INFERRED"
	EvidenceContradicted EvidenceStatus = "CONTRADICTED"
)

// LifecycleMode represents the operational/containment mode.
type LifecycleMode string

const (
	ModeNormal      LifecycleMode = "NORMAL"
	ModeDegraded    LifecycleMode = "DEGRADED"
	ModeSafeHold    LifecycleMode = "SAFE_HOLD"
	ModeQuarantined LifecycleMode = "QUARANTINED"
	ModeRecovering  LifecycleMode = "RECOVERING"
	ModeFailed      LifecycleMode = "FAILED"
)

// StateVector encapsulates the typed vector state S(e,t) = <P, V, A, C, E, L, tau>.
type StateVector struct {
	EntityID  string         `json:"entity_id"`
	Presence  Presence       `json:"presence"`
	Valence   Valence        `json:"valence"`
	Anti      AntiRelation   `json:"anti"`
	Coherence Coherence      `json:"coherence"`
	Evidence  EvidenceStatus `json:"evidence"`
	Mode      LifecycleMode  `json:"mode"`
	Epoch     int64          `json:"epoch"`
	Timestamp time.Time      `json:"timestamp"`
	Summary   string         `json:"summary,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

// String returns a compact representation of the vector.
func (v StateVector) String() string {
	return fmt.Sprintf("S⟨P:%s, V:%s, A:%s, C:%s, E:%s, L:%s, τ:%d⟩",
		v.Presence, v.Valence, v.Anti, v.Coherence, v.Evidence, v.Mode, v.Epoch)
}

// IsConformant returns true if and only if presence is PRESENT, valence is POSITIVE,
// coherence is COHERENT, evidence is VERIFIED or OBSERVED, and there is NO Anti relation.
// Invariant: Mandatory ANTI violations dominate scores.
func (v StateVector) IsConformant() bool {
	if v.Anti != AntiNone && v.Anti != "" {
		return false
	}
	if v.Presence != PresencePresent {
		return false
	}
	if v.Valence != ValencePositive {
		return false
	}
	if v.Coherence != Coherent {
		return false
	}
	return v.Mode == ModeNormal || v.Mode == ModeDegraded
}
