package vector

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Disposition models canonical admission / conformance decisions.
type Disposition string

const (
	DispAdmit                Disposition = "ADMIT"
	DispAdmitWithObligations Disposition = "ADMIT_WITH_OBLIGATIONS"
	DispDeny                 Disposition = "DENY"
	DispEscalate             Disposition = "ESCALATE"
	DispSafeHold             Disposition = "SAFE_HOLD"
	DispQuarantine           Disposition = "QUARANTINE"
)

// Stimulus represents the stimulus / operation / event X.
type Stimulus struct {
	Type       string         `json:"type"`
	Target     string         `json:"target"`
	Payload    map[string]any `json:"payload,omitempty"`
	Actor      string         `json:"actor,omitempty"`
	OccurredAt time.Time      `json:"occurred_at"`
}

// ConformanceTransition models delta(Pe, S0, X, C) = <d, O, S1, E, R>.
type ConformanceTransition struct {
	ID               string            `json:"id"`
	PolicyName       string            `json:"policy_name"`
	PolicyDigest     string            `json:"policy_digest"`
	InitialState     StateVector       `json:"initial_state"`
	Stimulus         Stimulus          `json:"stimulus"`
	Context          map[string]string `json:"context,omitempty"`
	Disposition      Disposition       `json:"disposition"`
	Obligations      []string          `json:"obligations,omitempty"`
	ResultingState   StateVector       `json:"resulting_state"`
	EvidenceDigest   string            `json:"evidence_digest"`
	RecoveryTriggers []string          `json:"recovery_triggers,omitempty"`
	EvaluatedAt      time.Time         `json:"evaluated_at"`
}

// NewTransition constructs and seals a conformance transition with an immutable evidence digest.
func NewTransition(
	policyName string,
	policyDigest string,
	s0 StateVector,
	x Stimulus,
	ctx map[string]string,
	d Disposition,
	obligations []string,
	s1 StateVector,
	triggers []string,
) *ConformanceTransition {
	now := time.Now().UTC()
	t := &ConformanceTransition{
		ID:               "trans-" + uuid.NewString(),
		PolicyName:       policyName,
		PolicyDigest:     policyDigest,
		InitialState:     s0,
		Stimulus:         x,
		Context:          ctx,
		Disposition:      d,
		Obligations:      obligations,
		ResultingState:   s1,
		RecoveryTriggers: triggers,
		EvaluatedAt:      now,
	}

	payload, _ := json.Marshal(struct {
		Pol  string      `json:"pol"`
		S0   StateVector `json:"s0"`
		X    Stimulus    `json:"x"`
		D    Disposition `json:"d"`
		S1   StateVector `json:"s1"`
		Time time.Time   `json:"time"`
	}{
		Pol:  policyName,
		S0:   s0,
		X:    x,
		D:    d,
		S1:   s1,
		Time: now,
	})
	hash := sha256.Sum256(payload)
	t.EvidenceDigest = hex.EncodeToString(hash[:])
	return t
}
