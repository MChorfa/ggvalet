package lifecycle

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/MChorfa/ggvalet/internal/vector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) *state.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lifecycle_test.db")
	store, err := state.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestLifecycle_OnboardAndOffboard_User(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	engine := NewEngine(store)

	// 1. Onboard User
	onboardReq := OnboardingRequest{
		Persona:    PersonaUser,
		Identifier: "alice.engineer",
		Role:       authority.RoleObserver,
		TTL:        8 * time.Hour,
	}
	res, err := engine.Onboard(ctx, onboardReq)
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "ONBOARDED", res.Status)
	assert.Equal(t, PersonaUser, res.Persona)
	assert.Equal(t, "alice.engineer", res.Identifier)
	assert.NotNil(t, res.Lease)
	assert.Equal(t, authority.RoleObserver, res.Lease.Role)
	assert.Equal(t, vector.ModeNormal, res.InitialVector.Mode)
	assert.NotEmpty(t, res.EvidenceDigest)

	// 2. Offboard User
	offboardReq := OffboardingRequest{
		Persona:    PersonaUser,
		Identifier: "alice.engineer",
		Reason:     "Transitioned to another unit",
	}
	offRes, err := engine.Offboard(ctx, offboardReq)
	require.NoError(t, err)
	require.NotNil(t, offRes)

	assert.Equal(t, "REVOKED", offRes.Status)
	assert.Equal(t, vector.ModeSafeHold, offRes.TerminalVector.Mode)
	assert.Contains(t, offRes.Obligations, "revoke_active_pats_and_ssh_keys")
	assert.NotEmpty(t, offRes.RevokedLeases)
}

func TestLifecycle_OnboardAndOffboard_Project(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	engine := NewEngine(store)

	// 1. Onboard Project
	res, err := engine.Onboard(ctx, OnboardingRequest{
		Persona:    PersonaProject,
		Identifier: "cortaix/space-rad",
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "ONBOARDED", res.Status)
	assert.Contains(t, res.Artifacts, ".gitlab-ci.yml")
	assert.Contains(t, res.Artifacts[".gitlab-ci.yml"], "v7.0.0")
	assert.Contains(t, res.Artifacts, "branch_protection.json")

	// 2. Offboard Project
	offRes, err := engine.Offboard(ctx, OffboardingRequest{
		Persona:    PersonaProject,
		Identifier: "cortaix/space-rad",
		Reason:     "Project lifecycle completed",
	})
	require.NoError(t, err)
	assert.Equal(t, "ARCHIVED", offRes.Status)
	assert.Equal(t, vector.ModeSafeHold, offRes.TerminalVector.Mode)
	assert.Contains(t, offRes.Obligations, "set_repo_read_only")
}

func TestLifecycle_OnboardAndOffboard_Agent(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	engine := NewEngine(store)

	// 1. Onboard Agent (Zero standing privilege, bounded TTL)
	res, err := engine.Onboard(ctx, OnboardingRequest{
		Persona:    PersonaAgent,
		Identifier: "agent-cortaix-reconciler",
		Role:       authority.RoleAdvisor,
		TTL:        2 * time.Hour, // Engine will clamp to 15m default
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "ONBOARDED", res.Status)
	assert.NotNil(t, res.Lease)
	assert.Equal(t, authority.RoleAdvisor, res.Lease.Role)
	assert.Equal(t, 15*time.Minute, res.Lease.ExpiresAt.Sub(res.Lease.IssuedAt))

	// 2. Offboard Agent
	offRes, err := engine.Offboard(ctx, OffboardingRequest{
		Persona:    PersonaAgent,
		Identifier: "agent-cortaix-reconciler",
		Reason:     "Task trajectory concluded",
	})
	require.NoError(t, err)
	assert.Equal(t, "TERMINATED", offRes.Status)
	assert.Equal(t, vector.ModeSafeHold, offRes.TerminalVector.Mode)
	assert.Contains(t, offRes.Obligations, "immediate_lease_revocation")
}

func TestLifecycle_OnboardAndOffboard_Service(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	engine := NewEngine(store)

	res, err := engine.Onboard(ctx, OnboardingRequest{
		Persona:    PersonaService,
		Identifier: "runner-airgap-cluster-01",
	})
	require.NoError(t, err)
	assert.Equal(t, "ONBOARDED", res.Status)
	assert.Contains(t, res.Artifacts, "runner_config.toml")

	offRes, err := engine.Offboard(ctx, OffboardingRequest{
		Persona:    PersonaService,
		Identifier: "runner-airgap-cluster-01",
		Reason:     "Runner hardware decommissioning",
	})
	require.NoError(t, err)
	assert.Equal(t, "DECOMMISSIONED", offRes.Status)
	assert.Contains(t, offRes.Obligations, "unregister_ci_runner")
}

func TestLifecycle_OnboardAndOffboard_Auditor(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	engine := NewEngine(store)

	res, err := engine.Onboard(ctx, OnboardingRequest{
		Persona:    PersonaAuditor,
		Identifier: "auditor.external.soc2",
	})
	require.NoError(t, err)
	assert.Equal(t, "ONBOARDED", res.Status)
	assert.Equal(t, authority.RoleTrustwall, res.Lease.Role)

	offRes, err := engine.Offboard(ctx, OffboardingRequest{
		Persona:    PersonaAuditor,
		Identifier: "auditor.external.soc2",
		Reason:     "Annual assessment completed",
	})
	require.NoError(t, err)
	assert.Equal(t, "SESSION_CLOSED", offRes.Status)
	assert.Contains(t, offRes.Obligations, "sign_and_emit_attestation_certificate")
}

func TestLifecycle_ValidationErrors(t *testing.T) {
	engine := NewEngine(nil)

	_, err := engine.Onboard(context.Background(), OnboardingRequest{})
	assert.Error(t, err)

	_, err = engine.Onboard(context.Background(), OnboardingRequest{Identifier: "test", Persona: "unknown"})
	assert.Error(t, err)

	_, err = engine.Offboard(context.Background(), OffboardingRequest{})
	assert.Error(t, err)

	_, err = engine.Offboard(context.Background(), OffboardingRequest{Identifier: "test", Persona: "unknown"})
	assert.Error(t, err)

	_, err = engine.ListStatus(context.Background(), 10)
	assert.Error(t, err)
}
