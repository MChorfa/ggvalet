package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/controlplane"
	"github.com/MChorfa/ggvalet/internal/policy"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/MChorfa/ggvalet/internal/syncindex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) *state.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "agent_test.db")
	store, err := state.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestAgent_ConvergesUnderReconciler(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	model := policy.DefaultCanonicalModel()
	engine := controlplane.NewEngine(nil, store, model)

	ag := NewAgent("test-agent", engine, nil, store)
	ag.ObservedOverride = map[string]any{
		"project":           "gitlab-shared/team-b",
		"protected":         false,
		"unmasked_variable": true,
		"mutable_image_tag": true,
	}

	intent := NewIntent("agent-worker", "Converge team-b to canonical", "gitlab-shared/team-b", authority.RoleReconciler, 15*time.Minute)

	// Live run with valet-reconciler role
	report, err := ag.Run(ctx, intent, false)
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.Equal(t, "CONVERGED", report.Status)
	assert.Equal(t, intent.ID, report.Intent.ID)
	assert.NotEmpty(t, report.LeaseID)
	assert.Empty(t, report.Refusals)

	// Verify trajectory steps
	require.GreaterOrEqual(t, len(report.Steps), 5)
	assert.Equal(t, StepObserve, report.Steps[0].Kind)
	assert.Equal(t, StepAnalyze, report.Steps[1].Kind)
	assert.Equal(t, StepAcquireLease, report.Steps[2].Kind)
	assert.Equal(t, StepPlan, report.Steps[3].Kind)

	var hasExecute, hasAssure bool
	for _, s := range report.Steps {
		if s.Kind == StepExecuteAction {
			hasExecute = true
		}
		if s.Kind == StepAssure {
			hasAssure = true
		}
	}
	assert.True(t, hasExecute, "should have StepExecuteAction")
	assert.True(t, hasAssure, "should have StepAssure")
}

func TestAgent_RefusesAndPivotsUnderObserver(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	model := policy.DefaultCanonicalModel()
	engine := controlplane.NewEngine(nil, store, model)

	ag := NewAgent("test-observer-agent", engine, nil, store)
	ag.ObservedOverride = map[string]any{
		"project":           "gitlab-shared/team-b",
		"protected":         false,
		"unmasked_variable": true,
		"mutable_image_tag": true,
	}

	intent := NewIntent("agent-worker", "Audit and reconcile team-b", "gitlab-shared/team-b", authority.RoleObserver, 15*time.Minute)

	report, err := ag.Run(ctx, intent, false)
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.Equal(t, "SAFE_HOLD", report.Status)
	assert.NotEmpty(t, report.Refusals, "observer must receive refusal receipts")
	assert.NotEmpty(t, report.Receipts)

	var hasPivot bool
	for _, s := range report.Steps {
		if s.Kind == StepRefusedPivot {
			hasPivot = true
			assert.Equal(t, "PIVOT_ADVISORY", s.Disposition)
			assert.Contains(t, s.Obligations, "create_advisory_mr")
		}
	}
	assert.True(t, hasPivot, "should have recorded StepRefusedPivot")
}

func TestAgent_PlanOnly(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	model := policy.DefaultCanonicalModel()
	engine := controlplane.NewEngine(nil, store, model)

	ag := NewAgent("test-agent", engine, nil, store)
	ag.ObservedOverride = map[string]any{
		"project":   "gitlab-shared/team-b",
		"protected": false,
	}

	intent := NewIntent("agent-worker", "Plan changes", "gitlab-shared/team-b", authority.RoleReconciler, 15*time.Minute)

	report, err := ag.Plan(ctx, intent)
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.Equal(t, "PLAN_READY", report.Status)
}

func TestAgent_AlreadyConformant(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	model := policy.DefaultCanonicalModel()
	engine := controlplane.NewEngine(nil, store, model)

	ag := NewAgent("test-agent", engine, nil, store)
	ag.ObservedOverride = map[string]any{
		"project":           "gitlab-shared/team-a",
		"protected":         true,
		"unmasked_variable": false,
		"mutable_image_tag": false,
		"pipeline_version":  "v7",
	}

	intent := NewIntent("agent-worker", "Verify team-a", "gitlab-shared/team-a", authority.RoleObserver, 15*time.Minute)

	report, err := ag.Run(ctx, intent, false)
	require.NoError(t, err)
	assert.Equal(t, "CONVERGED", report.Status)
}

func TestAgent_FederatedSync_QuarantineConflict(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	fedEngine := syncindex.NewEngine(nil, nil, store)

	ag := NewAgent("test-agent", nil, fedEngine, store)

	intent := NewIntent("sync-worker", "Sync issues with conflict", "gitlab-dedicated/repo", authority.RoleObserver, 10*time.Minute)

	marker := syncindex.SyncMarker{
		Version:       syncindex.MarkerVersion,
		SrcHost:       "gitlab-shared",
		SrcProject:    "shared/repo",
		EntityType:    "issue",
		SrcIID:        10,
		ContentDigest: syncindex.ComputeContentDigest("Initial Title", "Initial Body", nil, "opened"),
		SyncEpoch:     1,
	}
	_ = store.RecordSyncIndexEntry(ctx, state.SyncIndexRecord{
		ID:            "idx-shared-10",
		SrcHost:       "gitlab-shared",
		SrcProject:    "shared/repo",
		EntityType:    "issue",
		SrcIID:        10,
		DstHost:       "gitlab-dedicated",
		DstProject:    "dedicated/repo",
		DstIID:        5,
		ContentDigest: marker.ContentDigest,
		SyncEpoch:     1,
	})

	// Both sides edited independently (Conflict!)
	srcIssues := []provider.Issue{
		{IID: 10, Title: "Issue 10 Upstream Modified", Body: "Upstream change", State: "opened"},
	}
	dstIssues := []provider.Issue{
		{IID: 5, Title: "Issue 10 Downstream Modified", Body: syncindex.EmbedMarker("Downstream change", marker), State: "opened"},
	}

	report, err := ag.RunFederatedSync(ctx, intent, "gitlab-shared", "shared/repo", "gitlab-dedicated", "dedicated/repo", false, srcIssues, dstIssues)
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.Equal(t, "SAFE_HOLD", report.Status)
	var hasQuarantine bool
	for _, s := range report.Steps {
		if s.Kind == StepQuarantineConflict {
			hasQuarantine = true
			assert.Equal(t, "QUARANTINED", s.Disposition)
		}
	}
	assert.True(t, hasQuarantine, "should record StepQuarantineConflict")
}

func TestIntent_DigestAndDefaults(t *testing.T) {
	intent := NewIntent("worker", "test goal", "res", "", 0)
	assert.Equal(t, authority.RoleObserver, intent.RequestedRole)
	assert.Equal(t, 15*time.Minute, intent.BudgetDuration)

	digest := intent.Digest()
	assert.NotEmpty(t, digest)
	assert.Contains(t, digest, "sha256:")

	ag := NewAgent("", nil, nil, nil)
	assert.Equal(t, "valet-governed-agent", ag.Name)

	_, err := ag.Run(context.Background(), intent, false)
	assert.Error(t, err)

	_, err = ag.RunFederatedSync(context.Background(), intent, "s", "p", "d", "dp", false, nil, nil)
	assert.Error(t, err)
}
