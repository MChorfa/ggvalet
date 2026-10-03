package authority

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUnauthorized  = errors.New("authority: action unauthorized by effective lease")
	ErrLeaseExpired  = errors.New("authority: capability lease expired")
	ErrScopeExceeded = errors.New("authority: requested scope exceeds lease boundary")
)

type Capability string

const (
	CapReadProject           Capability = "READ_PROJECT"
	CapReadPipeline          Capability = "READ_PIPELINE"
	CapReadArtifact          Capability = "READ_ARTIFACT"
	CapReadPolicy            Capability = "READ_POLICY"
	CapCreateIssue           Capability = "CREATE_ISSUE"
	CapCreateMR              Capability = "CREATE_MR"
	CapModifyConfig          Capability = "MODIFY_CONFIG"
	CapModifyProtectedBranch Capability = "MODIFY_PROTECTED_BRANCH"
	CapTriggerPipeline       Capability = "TRIGGER_PIPELINE"
	CapPromoteArtifact       Capability = "PROMOTE_ARTIFACT"
	CapSignRelease           Capability = "SIGN_RELEASE"
	CapEvaluateTrustwall     Capability = "EVALUATE_TRUSTWALL"
	CapAdmitArtifact         Capability = "ADMIT_ARTIFACT"
	CapQuarantineArtifact    Capability = "QUARANTINE_ARTIFACT"
	CapAdminInstance         Capability = "ADMIN_INSTANCE"
)

// RefusalReceipt records machine-verifiable evidence that an operation was
// evaluated and safely refused due to authority constraints.
type RefusalReceipt struct {
	ReceiptID          string     `json:"receipt_id"`
	ActionNotPerformed bool       `json:"action_not_performed"`
	Reason             string     `json:"reason"`
	RequiredCapability Capability `json:"required_capability"`
	AttemptedResource  string     `json:"attempted_resource"`
	SubjectRole        string     `json:"subject_role"`
	LeaseID            string     `json:"lease_id"`
	Timestamp          time.Time  `json:"timestamp"`
	EvidenceDigest     string     `json:"evidence_digest"`
}

// Lease models an explicit, bounded capability lease.
type Lease struct {
	ID           string       `json:"id"`
	Subject      string       `json:"subject"`
	Role         RoleName     `json:"role"`
	Capabilities []Capability `json:"capabilities"`
	Scope        string       `json:"scope"` // Glob pattern, e.g. "gitlab-shared/team-b/*" or "*"
	IssuedAt     time.Time    `json:"issued_at"`
	ExpiresAt    time.Time    `json:"expires_at"`
	Issuer       string       `json:"issuer"`
}

// NewLease creates a new capability lease with unique ID.
func NewLease(subject string, role RoleName, scope string, duration time.Duration) *Lease {
	now := time.Now().UTC()
	return &Lease{
		ID:           "lease-" + uuid.NewString(),
		Subject:      subject,
		Role:         role,
		Capabilities: DefaultCapabilities(role),
		Scope:        scope,
		IssuedAt:     now,
		ExpiresAt:    now.Add(duration),
		Issuer:       "valet-authority-root",
	}
}

// Authorize checks whether the requested capability on the target resource is
// permitted by this lease. If unauthorized or expired, it returns an explicit
// RefusalReceipt and a typed error.
func (l *Lease) Authorize(cap Capability, resource string) (*RefusalReceipt, error) {
	now := time.Now().UTC()
	if !l.ExpiresAt.IsZero() && now.After(l.ExpiresAt) {
		receipt := l.createRefusal("lease_expired", cap, resource, now)
		return receipt, ErrLeaseExpired
	}

	if l.Scope != "" && l.Scope != "*" {
		matched, err := filepath.Match(l.Scope, resource)
		if err != nil || !matched {
			receipt := l.createRefusal("scope_exceeded", cap, resource, now)
			return receipt, ErrScopeExceeded
		}
	}

	hasCap := false
	for _, c := range l.Capabilities {
		if c == cap || c == CapAdminInstance {
			hasCap = true
			break
		}
	}

	if !hasCap {
		receipt := l.createRefusal("insufficient_authority", cap, resource, now)
		return receipt, fmt.Errorf("%w: missing required capability %s", ErrUnauthorized, cap)
	}

	return nil, nil
}

func (l *Lease) createRefusal(reason string, cap Capability, resource string, t time.Time) *RefusalReceipt {
	r := &RefusalReceipt{
		ReceiptID:          "rcpt-refusal-" + uuid.NewString(),
		ActionNotPerformed: true,
		Reason:             reason,
		RequiredCapability: cap,
		AttemptedResource:  resource,
		SubjectRole:        string(l.Role),
		LeaseID:            l.ID,
		Timestamp:          t,
	}

	payload, _ := json.Marshal(struct {
		Reason   string     `json:"reason"`
		Cap      Capability `json:"cap"`
		Resource string     `json:"resource"`
		Lease    string     `json:"lease"`
		Time     time.Time  `json:"time"`
	}{
		Reason:   reason,
		Cap:      cap,
		Resource: resource,
		Lease:    l.ID,
		Time:     t,
	})
	hash := sha256.Sum256(payload)
	r.EvidenceDigest = hex.EncodeToString(hash[:])
	return r
}
