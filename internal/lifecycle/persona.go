package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/vector"
)

// PersonaKind identifies the operational entity type.
type PersonaKind string

const (
	PersonaUser    PersonaKind = "user"
	PersonaProject PersonaKind = "project"
	PersonaAgent   PersonaKind = "agent"
	PersonaService PersonaKind = "service"
	PersonaAuditor PersonaKind = "auditor"
)

// OnboardingRequest contains inputs required to bootstrap an entity into the governed fabric.
type OnboardingRequest struct {
	Persona    PersonaKind        `json:"persona"`
	Identifier string             `json:"identifier"` // Username, Project path, Agent ID, Service name
	TargetHost string             `json:"target_host,omitempty"`
	Scope      string             `json:"scope,omitempty"`
	Role       authority.RoleName `json:"role,omitempty"`
	TTL        time.Duration      `json:"ttl,omitempty"`
	Metadata   map[string]string  `json:"metadata,omitempty"`
}

// OnboardingResult captures the evidence and configuration generated during onboarding.
type OnboardingResult struct {
	ID             string             `json:"id"`
	Persona        PersonaKind        `json:"persona"`
	Identifier     string             `json:"identifier"`
	Status         string             `json:"status"` // ONBOARDED, SAFE_HOLD, REJECTED
	InitialVector  vector.StateVector `json:"initial_vector"`
	Lease          *authority.Lease   `json:"lease,omitempty"`
	Artifacts      map[string]string  `json:"artifacts,omitempty"` // Templates, configs, rules
	EvidenceDigest string             `json:"evidence_digest"`
	CreatedAt      time.Time          `json:"created_at"`
}

// Digest computes the SHA-256 evidence digest of an onboarding result.
func (r OnboardingResult) Digest() string {
	b, _ := json.Marshal(r)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// OffboardingRequest contains inputs required to safely terminate or archive an entity.
type OffboardingRequest struct {
	Persona    PersonaKind       `json:"persona"`
	Identifier string            `json:"identifier"`
	TargetHost string            `json:"target_host,omitempty"`
	Reason     string            `json:"reason"`
	Approver   string            `json:"approver,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// OffboardingResult captures the evidence and terminal vector state after offboarding.
type OffboardingResult struct {
	ID             string             `json:"id"`
	Persona        PersonaKind        `json:"persona"`
	Identifier     string             `json:"identifier"`
	Status         string             `json:"status"` // OFFBOARDED, ARCHIVED, TERMINATED
	TerminalVector vector.StateVector `json:"terminal_vector"`
	RevokedLeases  []string           `json:"revoked_leases,omitempty"`
	Obligations    []string           `json:"obligations,omitempty"`
	EvidenceDigest string             `json:"evidence_digest"`
	CompletedAt    time.Time          `json:"completed_at"`
}

// Digest computes the SHA-256 evidence digest of an offboarding result.
func (r OffboardingResult) Digest() string {
	b, _ := json.Marshal(r)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
