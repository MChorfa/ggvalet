package syncindex

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/MChorfa/ggvalet/internal/state"
)

func TestFederatedEngine_DriftAndQuarantine(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "engine_test.db")
	store, err := state.Open(dbPath)
	if err != nil {
		t.Fatalf("state open: %v", err)
	}
	defer store.Close()

	engine := NewEngine(nil, nil, store)

	srcHost := "gitlab.shared.local"
	srcProj := "cortaix/src"
	dstHost := "gitlab.dedicated.local"
	dstProj := "cortaix/dst"

	// Baseline issue #1
	body1 := "Original Description"
	digest1 := ComputeContentDigest("Issue 1", body1, nil, "opened")
	_ = store.RecordSyncIndexEntry(ctx, state.SyncIndexRecord{
		ID:            "idx-001",
		SrcHost:       srcHost,
		SrcProject:    srcProj,
		EntityType:    "issue",
		SrcIID:        1,
		DstHost:       dstHost,
		DstProject:    dstProj,
		DstIID:        101,
		ContentDigest: digest1,
		SyncEpoch:     1,
		Status:        string(StatusInSync),
		LastSyncedAt:  time.Now().UTC(),
	})

	// Source has issue 1 (diverged) and issue 2 (new)
	srcIssues := []provider.Issue{
		{ID: 1, IID: 1, Title: "Issue 1 - Source Edit", Body: "Source edited body", State: "opened"},
		{ID: 2, IID: 2, Title: "Issue 2 - New", Body: "New issue", State: "opened"},
	}

	// Destination has issue 101 with independent edit (causes conflict with issue 1)
	dstMarker1 := FormatMarker(SyncMarker{
		Version:       MarkerVersion,
		SrcHost:       srcHost,
		SrcProject:    srcProj,
		EntityType:    "issue",
		SrcIID:        1,
		ContentDigest: digest1,
		SyncEpoch:     1,
	})
	dstIssues := []provider.Issue{
		{ID: 101, IID: 101, Title: "Issue 1 - Dst Edit", Body: "Dst edited body\n" + dstMarker1, State: "opened"},
	}

	analyses, err := engine.EvaluateDrift(ctx, srcHost, srcProj, dstHost, dstProj, srcIssues, dstIssues)
	if err != nil {
		t.Fatalf("EvaluateDrift failed: %v", err)
	}

	if len(analyses) != 2 {
		t.Fatalf("expected 2 analyses, got %d", len(analyses))
	}

	// Issue 1 must be CONFLICT
	if analyses[0].Status != StatusConflict {
		t.Errorf("expected Issue 1 to be CONFLICT, got %s", analyses[0].Status)
	}

	// Issue 2 must be DESTINATION_MISSING
	if analyses[1].Status != StatusDestinationMissing {
		t.Errorf("expected Issue 2 to be DESTINATION_MISSING, got %s", analyses[1].Status)
	}

	// Verify Quarantine record was saved in SQLite for Issue 1
	quarantines, err := store.ListQuarantineRecords(ctx)
	if err != nil || len(quarantines) != 1 {
		t.Fatalf("expected 1 quarantine record, got %v (len %d)", err, len(quarantines))
	}
	if quarantines[0].SrcIID != 1 || quarantines[0].ResolutionStatus != "QUARANTINED" {
		t.Errorf("unexpected quarantine record: %+v", quarantines[0])
	}
}

func TestFederatedEngine_ReconcileAuthorityGating(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "auth_test.db")
	store, err := state.Open(dbPath)
	if err != nil {
		t.Fatalf("state open: %v", err)
	}
	defer store.Close()

	engine := NewEngine(nil, nil, store)

	srcHost := "gitlab.shared.local"
	srcProj := "cortaix/src"
	dstHost := "gitlab.dedicated.local"
	dstProj := "cortaix/dst"

	srcIssues := []provider.Issue{
		{ID: 10, IID: 10, Title: "Missing on Dest", Body: "Need sync", State: "opened"},
	}

	// 1. Reconcile under valet-observer: must be refused with RefusalReceipt
	obsLease := authority.NewLease("obs-cli", authority.RoleObserver, "*", time.Hour)
	obsRes, err := engine.Reconcile(ctx, srcHost, srcProj, dstHost, dstProj, obsLease, false, srcIssues, nil)
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if len(obsRes.Refused) != 1 {
		t.Fatalf("expected 1 refused action, got %d", len(obsRes.Refused))
	}
	if obsRes.Refused[0].Reason != "insufficient_authority" {
		t.Errorf("expected reason insufficient_authority, got %s", obsRes.Refused[0].Reason)
	}

	// Verify RefusalReceipt in SQLite
	receipts, err := store.QueryRefusalReceipts(ctx)
	if err != nil || len(receipts) != 1 {
		t.Fatalf("expected 1 refusal receipt in store, got %d", len(receipts))
	}

	// 2. Reconcile under valet-reconciler: must be applied
	recLease := authority.NewLease("rec-cli", authority.RoleReconciler, "*", time.Hour)
	recRes, err := engine.Reconcile(ctx, srcHost, srcProj, dstHost, dstProj, recLease, false, srcIssues, nil)
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if len(recRes.Applied) != 1 {
		t.Fatalf("expected 1 applied reconciliation, got %d", len(recRes.Applied))
	}

	// Verify sync index was updated
	idx, err := store.GetSyncIndexEntry(ctx, srcHost, srcProj, "issue", 10, dstHost, dstProj)
	if err != nil || idx == nil {
		t.Fatalf("expected sync index entry for issue 10, got %v", idx)
	}
	if idx.Status != string(StatusInSync) {
		t.Errorf("expected status InSync, got %s", idx.Status)
	}
}
