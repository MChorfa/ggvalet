package lifecycle

import (
	"context"
	"fmt"
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/MChorfa/ggvalet/internal/vector"
	"github.com/google/uuid"
)

// Engine orchestrates deterministic onboarding and offboarding for all governed personas.
type Engine struct {
	Store *state.Store
}

// NewEngine creates a new persona lifecycle engine.
func NewEngine(store *state.Store) *Engine {
	return &Engine{Store: store}
}

// Onboard bootstraps an entity into the governed delivery ecosystem under an explicit baseline vector.
func (e *Engine) Onboard(ctx context.Context, req OnboardingRequest) (*OnboardingResult, error) {
	if req.Identifier == "" {
		return nil, fmt.Errorf("lifecycle: identifier is required for onboarding")
	}

	res := &OnboardingResult{
		ID:         "onboard-" + uuid.NewString()[:8],
		Persona:    req.Persona,
		Identifier: req.Identifier,
		Status:     "ONBOARDED",
		Artifacts:  make(map[string]string),
		CreatedAt:  time.Now().UTC(),
	}

	ttl := req.TTL
	if ttl <= 0 {
		ttl = 8 * time.Hour
	}

	// 1. Configure persona-specific authority and artifacts
	switch req.Persona {
	case PersonaUser:
		role := req.Role
		if role == "" {
			role = authority.RoleObserver // Least authority by default
		}
		scope := req.Scope
		if scope == "" {
			scope = "*"
		}
		lease := authority.NewLease(req.Identifier, role, scope, ttl)
		res.Lease = lease
		res.Artifacts["journal_path"] = fmt.Sprintf("~/.local/share/ggvalet/state.db (user: %s)", req.Identifier)
		res.Artifacts["standing_role"] = string(role)

	case PersonaProject:
		// Scaffold Golden Pipeline v7.0.0 and branch protection
		res.Artifacts[".gitlab-ci.yml"] = CanonicalGoldenPipelineV7
		res.Artifacts["branch_protection.json"] = CanonicalBranchProtectionConfig
		res.Artifacts["canonical_version"] = "v7.0.0"

	case PersonaAgent:
		// Zero standing privilege: ephemeral lease strictly attenuated
		role := req.Role
		if role == "" {
			role = authority.RoleObserver
		}
		if ttl > 1*time.Hour {
			ttl = 15 * time.Minute // Bound autonomous cognitive agents to 15m default
		}
		scope := req.Scope
		if scope == "" {
			scope = req.Identifier
		}
		lease := authority.NewLease(req.Identifier, role, scope, ttl)
		res.Lease = lease
		res.Artifacts["autonomy_level"] = "GAL-1"
		res.Artifacts["fencing"] = "RateLimited:20req/s, CircuitBreaker:5-strike"

	case PersonaService:
		role := authority.RoleReconciler
		if req.Role != "" {
			role = req.Role
		}
		lease := authority.NewLease(req.Identifier, role, "*", ttl)
		res.Lease = lease
		res.Artifacts["runner_config.toml"] = CanonicalRunnerConfigSnippet
		res.Artifacts["tags"] = "runner:hardened-linux, runner:airgap-transfer"

	case PersonaAuditor:
		role := authority.RoleTrustwall // Read-only / verification authority
		if req.Role != "" {
			role = req.Role
		}
		lease := authority.NewLease(req.Identifier, role, "*", ttl)
		res.Lease = lease
		res.Artifacts["scope"] = "READ_PROJECT, READ_PIPELINE, READ_ARTIFACT, READ_POLICY, EVALUATE_TRUSTWALL"
		res.Artifacts["enclave_mode"] = "READ_ONLY_FORENSIC"

	default:
		return nil, fmt.Errorf("lifecycle: unknown persona kind %q", req.Persona)
	}

	// 2. Formulate Baseline Vector State S_0
	s0 := vector.StateVector{
		EntityID:  fmt.Sprintf("%s:%s", req.Persona, req.Identifier),
		Presence:  vector.PresencePresent,
		Valence:   vector.ValencePositive,
		Anti:      vector.AntiNone,
		Coherence: vector.Coherent,
		Evidence:  vector.EvidenceVerified,
		Mode:      vector.ModeNormal,
		Epoch:     1,
		Timestamp: res.CreatedAt,
		Summary:   fmt.Sprintf("Onboarded persona %s (%s) into governed fabric", req.Persona, req.Identifier),
	}
	res.InitialVector = s0
	res.EvidenceDigest = res.Digest()

	// 3. Persist Vector State in SQLite store
	if e.Store != nil {
		_ = e.Store.RecordVectorState(ctx, s0)
	}

	return res, nil
}

// Offboard safely decommissions, revokes leases, archives state, and records terminal vector state.
func (e *Engine) Offboard(ctx context.Context, req OffboardingRequest) (*OffboardingResult, error) {
	if req.Identifier == "" {
		return nil, fmt.Errorf("lifecycle: identifier is required for offboarding")
	}

	res := &OffboardingResult{
		ID:            "offboard-" + uuid.NewString()[:8],
		Persona:       req.Persona,
		Identifier:    req.Identifier,
		Status:        "OFFBOARDED",
		RevokedLeases: make([]string, 0),
		Obligations:   make([]string, 0),
		CompletedAt:   time.Now().UTC(),
	}

	entityID := fmt.Sprintf("%s:%s", req.Persona, req.Identifier)

	// Fetch current epoch if present
	var currentEpoch int64 = 1
	if e.Store != nil {
		latest, _ := e.Store.GetLatestVectorState(ctx, entityID)
		if latest != nil {
			currentEpoch = latest.Epoch
		}
	}

	// 1. Persona-specific teardown obligations
	switch req.Persona {
	case PersonaUser:
		res.Status = "REVOKED"
		res.RevokedLeases = append(res.RevokedLeases, fmt.Sprintf("lease-user-%s", req.Identifier))
		res.Obligations = []string{
			"revoke_active_pats_and_ssh_keys",
			"reassign_in_flight_merge_requests",
			"archive_activity_journal",
		}

	case PersonaProject:
		res.Status = "ARCHIVED"
		res.Obligations = []string{
			"set_repo_read_only",
			"sever_pipeline_schedules_and_webhooks",
			"purge_unregistered_runners",
			"freeze_release_content_digests",
		}

	case PersonaAgent:
		res.Status = "TERMINATED"
		res.RevokedLeases = append(res.RevokedLeases, fmt.Sprintf("lease-agent-%s", req.Identifier))
		res.Obligations = []string{
			"immediate_lease_revocation",
			"fence_in_flight_mutations_to_safe_hold",
			"seal_step_trajectory_ledger",
		}

	case PersonaService:
		res.Status = "DECOMMISSIONED"
		res.Obligations = []string{
			"unregister_ci_runner",
			"drain_executing_jobs",
			"secure_wipe_workspace_and_cache_volumes",
		}

	case PersonaAuditor:
		res.Status = "SESSION_CLOSED"
		res.Obligations = []string{
			"expire_audit_session_lease",
			"sign_and_emit_attestation_certificate",
		}

	default:
		return nil, fmt.Errorf("lifecycle: unknown persona kind %q", req.Persona)
	}

	// 2. Terminal Vector State S_term (placed in SAFE_HOLD / ARCHIVED)
	sTerm := vector.StateVector{
		EntityID:  entityID,
		Presence:  vector.PresencePresent,
		Valence:   vector.ValenceNeutral,
		Anti:      vector.AntiNone,
		Coherence: vector.Coherent,
		Evidence:  vector.EvidenceVerified,
		Mode:      vector.ModeSafeHold,
		Epoch:     currentEpoch + 1,
		Timestamp: res.CompletedAt,
		Summary:   fmt.Sprintf("Offboarded persona %s (%s): %s", req.Persona, req.Identifier, req.Reason),
	}
	res.TerminalVector = sTerm
	res.EvidenceDigest = res.Digest()

	// 3. Persist terminal vector in SQLite
	if e.Store != nil {
		_ = e.Store.RecordVectorState(ctx, sTerm)
	}

	return res, nil
}

// ListStatus queries recent lifecycle vector states across all personas.
func (e *Engine) ListStatus(ctx context.Context, limit int) ([]vector.StateVector, error) {
	if e.Store == nil {
		return nil, fmt.Errorf("lifecycle: store unavailable")
	}
	return e.Store.ListVectorStates(ctx, limit)
}
