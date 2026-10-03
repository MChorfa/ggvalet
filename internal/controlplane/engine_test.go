package controlplane

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/policy"
	"github.com/MChorfa/ggvalet/internal/state"
)

func TestControlPlaneAuditAndReconcile(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "cp_test.db")
	store, err := state.Open(dbPath)
	if err != nil {
		t.Fatalf("state Open failed: %v", err)
	}
	defer store.Close()

	engine := NewEngine(nil, store, policy.DefaultCanonicalModel())

	adverseObserved := map[string]any{
		"protected":         false, // DEV-001 (Requires CapModifyProtectedBranch)
		"unmasked_variable": true,  // DEV-003 (Requires CapModifyConfig)
	}

	// 1. Audit
	assessment, err := engine.Audit(ctx, "gitlab-shared/team-b", adverseObserved)
	if err != nil {
		t.Fatalf("Audit failed: %v", err)
	}
	if len(assessment.Findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(assessment.Findings))
	}

	// Verify Vector State was saved in SQLite
	vSaved, err := store.GetLatestVectorState(ctx, "gitlab-shared/team-b")
	if err != nil || vSaved == nil {
		t.Fatalf("failed to retrieve saved vector state from SQLite: %v", err)
	}

	// 2. Reconcile under valet-observer authority (SHOULD REFUSE EVERYTHING)
	obsLease := authority.NewLease("obs-client", authority.RoleObserver, "*", time.Hour)
	obsRes, err := engine.Reconcile(ctx, "gitlab-shared/team-b", obsLease, false, adverseObserved)
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if len(obsRes.Applied) != 0 {
		t.Fatalf("valet-observer MUST NOT apply changes, applied: %v", obsRes.Applied)
	}
	if len(obsRes.Refused) != 2 {
		t.Fatalf("expected 2 refused actions for observer, got %d", len(obsRes.Refused))
	}

	// Verify RefusalReceipts exist in SQLite store
	refusals, err := store.QueryRefusalReceipts(ctx)
	if err != nil || len(refusals) != 2 {
		t.Fatalf("expected 2 refusal receipts in SQLite, got %d (err: %v)", len(refusals), err)
	}
	for _, r := range refusals {
		if r.Reason != "insufficient_authority" {
			t.Errorf("expected reason insufficient_authority, got %s", r.Reason)
		}
	}

	// 3. Reconcile under valet-reconciler with dryRun=true (SHOULD PLAN, NOT APPLY)
	recLease := authority.NewLease("rec-client", authority.RoleReconciler, "*", time.Hour)
	dryRes, err := engine.Reconcile(ctx, "gitlab-shared/team-b", recLease, true, adverseObserved)
	if err != nil {
		t.Fatalf("Reconcile dry-run failed: %v", err)
	}
	if len(dryRes.Planned) != 2 || len(dryRes.Applied) != 0 {
		t.Fatalf("expected 2 planned, 0 applied in dry run; got %d planned, %d applied", len(dryRes.Planned), len(dryRes.Applied))
	}

	// 4. Reconcile under valet-reconciler with dryRun=false (SHOULD APPLY)
	liveRes, err := engine.Reconcile(ctx, "gitlab-shared/team-b", recLease, false, adverseObserved)
	if err != nil {
		t.Fatalf("Reconcile live failed: %v", err)
	}
	if len(liveRes.Applied) != 2 {
		t.Fatalf("expected 2 applied reconciliations, got %d", len(liveRes.Applied))
	}
}
