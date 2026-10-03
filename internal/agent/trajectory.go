package agent

import (
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/vector"
)

type StepKind string

const (
	StepObserve            StepKind = "OBSERVE"
	StepAnalyze            StepKind = "ANALYZE"
	StepAcquireLease       StepKind = "ACQUIRE_LEASE"
	StepPlan               StepKind = "PLAN"
	StepExecuteAction      StepKind = "EXECUTE_ACTION"
	StepRefusedPivot       StepKind = "REFUSED_PIVOT"
	StepQuarantineConflict StepKind = "QUARANTINE_CONFLICT"
	StepAssure             StepKind = "ASSURE"
)

// TrajectoryStep records a single verifiable cognitive step taken by the agent.
type TrajectoryStep struct {
	StepIndex       int                `json:"step_index"`
	Kind            StepKind           `json:"kind"`
	Description     string             `json:"description"`
	InitialVector   vector.StateVector `json:"initial_vector"`
	ResultingVector vector.StateVector `json:"resulting_vector"`
	Disposition     string             `json:"disposition"`
	Obligations     []string           `json:"obligations,omitempty"`
	EvidenceDigest  string             `json:"evidence_digest"`
	Timestamp       time.Time          `json:"timestamp"`
}

// TrajectoryReport aggregates the full evidence ledger of an autonomous agent execution.
type TrajectoryReport struct {
	Intent        IntentEnvelope             `json:"intent"`
	LeaseID       string                     `json:"lease_id"`
	InitialVector vector.StateVector         `json:"initial_vector"`
	FinalVector   vector.StateVector         `json:"final_vector"`
	Steps         []TrajectoryStep           `json:"steps"`
	Receipts      []string                   `json:"receipts,omitempty"`
	Refusals      []authority.RefusalReceipt `json:"refusals,omitempty"`
	Status        string                     `json:"status"` // CONVERGED, SAFE_HOLD, ESCALATED, FAILED
	CompletedAt   time.Time                  `json:"completed_at"`
}
