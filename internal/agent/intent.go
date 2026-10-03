package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/google/uuid"
)

// IntentEnvelope represents the authoritative unit of work requested of the agent.
type IntentEnvelope struct {
	ID             string             `json:"id"`
	Actor          string             `json:"actor"`
	Goal           string             `json:"goal"`
	Resource       string             `json:"resource"`
	RequestedRole  authority.RoleName `json:"requested_role"`
	BudgetDuration time.Duration      `json:"budget_duration"`
	Context        map[string]string  `json:"context,omitempty"`
	Timestamp      time.Time          `json:"timestamp"`
}

// NewIntent creates a validated IntentEnvelope with a unique ID and UTC timestamp.
func NewIntent(actor, goal, resource string, role authority.RoleName, budget time.Duration) IntentEnvelope {
	if role == "" {
		role = authority.RoleObserver
	}
	if budget <= 0 {
		budget = 15 * time.Minute
	}
	return IntentEnvelope{
		ID:             "intent-" + uuid.NewString()[:8],
		Actor:          actor,
		Goal:           goal,
		Resource:       resource,
		RequestedRole:  role,
		BudgetDuration: budget,
		Context:        make(map[string]string),
		Timestamp:      time.Now().UTC(),
	}
}

// Digest returns the SHA-256 content digest of the intent envelope.
func (i IntentEnvelope) Digest() string {
	b, _ := json.Marshal(i)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
