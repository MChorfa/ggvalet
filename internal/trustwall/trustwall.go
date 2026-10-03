package trustwall

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MChorfa/ggvalet/internal/vector"
	"github.com/google/uuid"
)

type AdmissionDecision string

const (
	Admit      AdmissionDecision = "ADMIT"
	AdmitOblig AdmissionDecision = "ADMIT_WITH_OBLIGATIONS"
	Deny       AdmissionDecision = "DENY"
	Quarantine AdmissionDecision = "QUARANTINE"
)

type ArtifactClaim struct {
	ArtifactID       string            `json:"artifact_id"`
	ArtifactDigest   string            `json:"artifact_digest"`
	SourceRepo       string            `json:"source_repo"`
	CommitSHA        string            `json:"commit_sha"`
	HasSignature     bool              `json:"has_signature"`
	SignatureValid   bool              `json:"signature_valid"`
	HasSBOM          bool              `json:"has_sbom"`
	HasProvenance    bool              `json:"has_provenance"`
	ProvenanceSHA    string            `json:"provenance_sha"`
	Vulnerabilities  []string          `json:"vulnerabilities,omitempty"`
	LicenseCompliant bool              `json:"license_compliant"`
	ApprovalsCount   int               `json:"approvals_count"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

type AdmissionReceipt struct {
	ReceiptID       string             `json:"receipt_id"`
	ArtifactID      string             `json:"artifact_id"`
	Decision        AdmissionDecision  `json:"decision"`
	Violations      []string           `json:"violations,omitempty"`
	Obligations     []string           `json:"obligations,omitempty"`
	ResultingVector vector.StateVector `json:"resulting_vector"`
	EvaluatedAt     time.Time          `json:"evaluated_at"`
	ReceiptDigest   string             `json:"receipt_digest"`
}

type Gate struct {
	RequiredApprovals int
}

func NewGate() *Gate {
	return &Gate{RequiredApprovals: 2}
}

// Admit evaluates the artifact against mandatory trustwall invariants.
func (g *Gate) Admit(claim ArtifactClaim) *AdmissionReceipt {
	now := time.Now().UTC()
	var violations []string
	var obligations []string

	isQuarantine := false

	// Invariant 1: Cryptographic signature
	if !claim.HasSignature || !claim.SignatureValid {
		violations = append(violations, "INV-SIG: Missing or invalid Cosign signature")
		isQuarantine = true
	}

	// Invariant 2: Provenance commit match
	if !claim.HasProvenance || (claim.ProvenanceSHA != "" && claim.ProvenanceSHA != claim.CommitSHA) {
		violations = append(violations, fmt.Sprintf("INV-PROV: Provenance commit mismatch (%s != %s)", claim.ProvenanceSHA, claim.CommitSHA))
		isQuarantine = true
	}

	// Invariant 3: License compliance
	if !claim.LicenseCompliant {
		violations = append(violations, "INV-LIC: Incompatible or prohibited open-source license detected")
		isQuarantine = true
	}

	// Invariant 4: Critical quarantined vulnerabilities
	if len(claim.Vulnerabilities) > 0 {
		violations = append(violations, fmt.Sprintf("INV-VULN: Quarantined vulnerability present (%v)", claim.Vulnerabilities))
		isQuarantine = true
	}

	// Invariant 5: Dual control approvals
	if claim.ApprovalsCount < g.RequiredApprovals {
		violations = append(violations, fmt.Sprintf("INV-APPR: Insufficient peer approvals (%d < %d)", claim.ApprovalsCount, g.RequiredApprovals))
	}

	// Invariant 6: SBOM presence
	if !claim.HasSBOM {
		violations = append(violations, "INV-SBOM: CycloneDX SBOM is missing")
	}

	var decision AdmissionDecision
	var anti vector.AntiRelation
	var val vector.Valence
	var mode vector.LifecycleMode

	if isQuarantine {
		decision = Quarantine
		anti = vector.AntiContradicts
		val = vector.ValenceNegative
		mode = vector.ModeQuarantined
	} else if len(violations) > 0 {
		decision = Deny
		anti = vector.AntiNone
		val = vector.ValenceNegative
		mode = vector.ModeSafeHold
	} else {
		decision = Admit
		anti = vector.AntiNone
		val = vector.ValencePositive
		mode = vector.ModeNormal
		obligations = append(obligations, "archive_immutable_provenance", "log_merkle_leaf")
	}

	vec := vector.StateVector{
		EntityID:  claim.ArtifactID,
		Presence:  vector.PresencePresent,
		Valence:   val,
		Anti:      anti,
		Coherence: vector.Coherent,
		Evidence:  vector.EvidenceVerified,
		Mode:      mode,
		Epoch:     1,
		Timestamp: now,
		Summary:   fmt.Sprintf("Trustwall gate evaluated with decision %s (%d violations)", decision, len(violations)),
	}

	receipt := &AdmissionReceipt{
		ReceiptID:       "rcpt-tw-" + uuid.NewString(),
		ArtifactID:      claim.ArtifactID,
		Decision:        decision,
		Violations:      violations,
		Obligations:     obligations,
		ResultingVector: vec,
		EvaluatedAt:     now,
	}

	payload, _ := json.Marshal(struct {
		Artifact string            `json:"artifact"`
		Dec      AdmissionDecision `json:"dec"`
		Viols    []string          `json:"viols"`
		Time     time.Time         `json:"time"`
	}{
		Artifact: claim.ArtifactID,
		Dec:      decision,
		Viols:    violations,
		Time:     now,
	})
	hash := sha256.Sum256(payload)
	receipt.ReceiptDigest = hex.EncodeToString(hash[:])

	return receipt
}
