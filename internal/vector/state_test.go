package vector

import (
	"testing"
	"time"
)

func TestStateVectorInvariants(t *testing.T) {
	// Rule 1: A vector with positive valence and coherent state is conformant.
	vGood := StateVector{
		EntityID:  "proj-team-a",
		Presence:  PresencePresent,
		Valence:   ValencePositive,
		Anti:      AntiNone,
		Coherence: Coherent,
		Evidence:  EvidenceVerified,
		Mode:      ModeNormal,
		Epoch:     1,
		Timestamp: time.Now(),
	}
	if !vGood.IsConformant() {
		t.Errorf("expected vGood to be conformant")
	}

	// Rule 2: Invariant dominance — AntiRelation overrides high positive score!
	vAnti := StateVector{
		EntityID:  "proj-team-b",
		Presence:  PresencePresent,
		Valence:   ValencePositive,
		Anti:      AntiContradicts, // Structural opposition!
		Coherence: Coherent,
		Evidence:  EvidenceVerified,
		Mode:      ModeNormal,
		Epoch:     1,
		Timestamp: time.Now(),
	}
	if vAnti.IsConformant() {
		t.Errorf("ANTI relation MUST NOT be conformant")
	}

	// Rule 3: EMPTY is not NEGATIVE, but it is not conformant.
	vEmpty := StateVector{
		EntityID:  "proj-team-c",
		Presence:  PresenceEmpty,
		Valence:   ValenceNeutral,
		Anti:      AntiNone,
		Coherence: Coherent,
		Evidence:  EvidenceObserved,
		Mode:      ModeNormal,
		Epoch:     1,
		Timestamp: time.Now(),
	}
	if vEmpty.IsConformant() {
		t.Errorf("empty presence should not be conformant")
	}

	// Rule 4: Conformance Transition creation seals an evidence digest.
	trans := NewTransition(
		"policy-canonical-pipeline",
		"sha256-mock-digest",
		vGood,
		Stimulus{Type: "PROMOTE_ARTIFACT", Target: "pkg-123"},
		map[string]string{"env": "lab"},
		DispAdmit,
		[]string{"audit-log"},
		vGood,
		[]string{"on_hash_drift"},
	)
	if trans.EvidenceDigest == "" {
		t.Errorf("expected non-empty EvidenceDigest on transition")
	}
	if trans.Disposition != DispAdmit {
		t.Errorf("expected DispAdmit, got %s", trans.Disposition)
	}
}
