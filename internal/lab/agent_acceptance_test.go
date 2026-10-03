package lab

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/MChorfa/ggvalet/internal/agent"
	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/controlplane"
	"github.com/MChorfa/ggvalet/internal/policy"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/MChorfa/ggvalet/internal/syncindex"
	"github.com/MChorfa/ggvalet/internal/vector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgentGovernanceAcceptance formally verifies the constitutional invariants of the
// Next-Gen Governed Agent operating over ggvalet as its trusted execution substrate.
func TestAgentGovernanceAcceptance(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "agent_acceptance.db")
	store, err := state.Open(dbPath)
	require.NoError(t, err)
	defer store.Close()

	model := policy.DefaultCanonicalModel()
	cpEngine := controlplane.NewEngine(nil, store, model)
	fedEngine := syncindex.NewEngine(nil, nil, store)
	ag := agent.NewAgent("agent-acceptance-v1", cpEngine, fedEngine, store)

	// Seed adversarial project state (Team B with 3 deviations)
	ag.ObservedOverride = map[string]any{
		"project":           "gitlab-shared/team-b",
		"protected":         false,
		"unmasked_variable": true,
		"mutable_image_tag": true,
	}

	// ─── GATE 1: Zero Standing Privilege & Refusal Pivot ───────────
	// Agent requests intent under valet-observer (read-only)
	intentObserver := agent.NewIntent("agent-cortaix-v3", "Assure Golden Pipeline v7", "gitlab-shared/team-b", authority.RoleObserver, 15*time.Minute)
	reportObserver, err := ag.Run(ctx, intentObserver, false)
	require.NoError(t, err)
	require.NotNil(t, reportObserver)

	assert.Equal(t, "SAFE_HOLD", reportObserver.Status)
	assert.NotEmpty(t, reportObserver.Refusals, "must refuse mutations under observer")
	assert.Equal(t, 3, len(reportObserver.Refusals))

	// Verify signed refusal receipts in SQLite
	refReceipts, err := store.QueryRefusalReceipts(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(refReceipts), 3)

	var hasPivot bool
	for _, step := range reportObserver.Steps {
		if step.Kind == agent.StepRefusedPivot {
			hasPivot = true
			assert.Equal(t, "PIVOT_ADVISORY", step.Disposition)
			assert.Contains(t, step.Obligations, "create_advisory_mr")
		}
	}
	assert.True(t, hasPivot, "agent must pivot to advisory remediation upon refusal")

	// ─── GATE 2: Authorized Mutation & State Convergence ───────────
	// Agent requests intent under valet-reconciler (mutation permitted)
	intentReconciler := agent.NewIntent("agent-cortaix-v3", "Assure Golden Pipeline v7", "gitlab-shared/team-b", authority.RoleReconciler, 15*time.Minute)
	reportReconciler, err := ag.Run(ctx, intentReconciler, false)
	require.NoError(t, err)
	require.NotNil(t, reportReconciler)

	assert.Equal(t, "CONVERGED", reportReconciler.Status)
	assert.Empty(t, reportReconciler.Refusals)
	assert.Equal(t, vector.ValencePositive, reportReconciler.FinalVector.Valence)
	assert.Equal(t, vector.AntiNone, reportReconciler.FinalVector.Anti)
	assert.Equal(t, vector.Coherent, reportReconciler.FinalVector.Coherence)
	assert.Equal(t, vector.ModeNormal, reportReconciler.FinalVector.Mode)

	// ─── GATE 3: Federated Drift & Conflict Quarantine ──────────────
	// Pre-seed baseline marker
	marker := syncindex.SyncMarker{
		Version:       syncindex.MarkerVersion,
		SrcHost:       "gitlab-shared.local",
		SrcProject:    "cortaix/platform",
		EntityType:    "issue",
		SrcIID:        103,
		ContentDigest: syncindex.ComputeContentDigest("Runner token", "Baseline body", nil, "opened"),
		SyncEpoch:     1,
	}
	_ = store.RecordSyncIndexEntry(ctx, state.SyncIndexRecord{
		ID:            "idx-cortaix-103",
		SrcHost:       "gitlab-shared.local",
		SrcProject:    "cortaix/platform",
		EntityType:    "issue",
		SrcIID:        103,
		DstHost:       "gitlab-dedicated.local",
		DstProject:    "cortaix/dedicated-repo",
		DstIID:        203,
		ContentDigest: marker.ContentDigest,
		SyncEpoch:     1,
	})

	// Concurrent edits on both source and destination
	srcIssues := []provider.Issue{
		{IID: 103, Title: "Runner token updated upstream", Body: "Upstream change", State: "opened"},
	}
	dstIssues := []provider.Issue{
		{IID: 203, Title: "Runner token updated downstream", Body: syncindex.EmbedMarker("Downstream change", marker), State: "opened"},
	}

	intentSync := agent.NewIntent("sync-agent", "Reconcile cross-instance drift", "cortaix/dedicated-repo", authority.RoleObserver, 15*time.Minute)
	syncReport, err := ag.RunFederatedSync(ctx, intentSync, "gitlab-shared.local", "cortaix/platform", "gitlab-dedicated.local", "cortaix/dedicated-repo", false, srcIssues, dstIssues)
	require.NoError(t, err)
	require.NotNil(t, syncReport)

	assert.Equal(t, "SAFE_HOLD", syncReport.Status)

	// Verify quarantine record in SQLite with intact forensic snapshots
	qRecords, err := store.ListQuarantineRecords(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(qRecords), 1)
	assert.Contains(t, qRecords[0].QuarantineReason, "CONFLICT")
	assert.NotEmpty(t, qRecords[0].SrcSnapshot)
	assert.NotEmpty(t, qRecords[0].DstSnapshot)

	// ─── GATE 4: Crash-Safe Checkpointing & Resumption ──────────────
	cpMgr, err := syncindex.NewCheckpointManager(store, "run-acceptance-cp", "gitlab-shared.local", "cortaix/platform", "gitlab-dedicated.local", "cortaix/dedicated-repo")
	require.NoError(t, err)

	// Advance batch 1
	err = cpMgr.Advance(ctx, 1, 101, 1, 0, 0)
	require.NoError(t, err)
	err = cpMgr.Advance(ctx, 1, 102, 1, 0, 0)
	require.NoError(t, err)

	// Simulate crash & resume from store
	cp, err := store.GetSyncCheckpoint(ctx, "run-acceptance-cp")
	require.NoError(t, err)
	require.NotNil(t, cp)
	assert.Equal(t, 102, cp.LastProcessedIID)
	assert.Equal(t, 2, cp.ItemsProcessed)

	// ─── GATE 5: Deep Diagnostics & SQLite Integrity Census ─────────
	err = store.IntegrityCheck(ctx)
	require.NoError(t, err, "SQLite WAL database must pass integrity check")

	census, err := store.EvidenceCensus(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, census["vector_states"], 2)
	assert.GreaterOrEqual(t, census["refusal_receipts"], 3)
	assert.GreaterOrEqual(t, census["quarantine_records"], 1)
	assert.GreaterOrEqual(t, census["sync_checkpoints"], 1)
}
