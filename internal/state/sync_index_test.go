package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncIndexAndResilienceStorage(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test_sync_store.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC().Truncate(time.Second)

	// 1. Sync Index Record
	idx := SyncIndexRecord{
		ID:            "idx-001",
		SrcHost:       "gitlab.shared.local",
		SrcProject:    "group/src",
		EntityType:    "issue",
		SrcIID:        10,
		SrcURL:        "http://shared/10",
		DstHost:       "gitlab.dedicated.local",
		DstProject:    "group/dst",
		DstIID:        20,
		DstURL:        "http://dedicated/20",
		ContentDigest: "sha256:abc1234",
		SyncEpoch:     1,
		Status:        "SYNCHRONIZED",
		LastSyncedAt:  now,
	}

	if err := store.RecordSyncIndexEntry(ctx, idx); err != nil {
		t.Fatalf("RecordSyncIndexEntry failed: %v", err)
	}

	fetched, err := store.GetSyncIndexEntry(ctx, "gitlab.shared.local", "group/src", "issue", 10, "gitlab.dedicated.local", "group/dst")
	if err != nil || fetched == nil {
		t.Fatalf("GetSyncIndexEntry failed: %v, fetched=%v", err, fetched)
	}
	if fetched.DstIID != 20 || fetched.ContentDigest != "sha256:abc1234" {
		t.Errorf("unexpected index entry: %+v", fetched)
	}

	list, err := store.ListSyncIndexEntries(ctx, "gitlab.dedicated.local", "group/dst")
	if err != nil || len(list) != 1 {
		t.Fatalf("ListSyncIndexEntries failed: %v, len=%d", err, len(list))
	}

	// 2. Sync Checkpoint
	cp := SyncCheckpoint{
		RunID:            "run-sync-001",
		SrcHost:          "gitlab.shared.local",
		SrcProject:       "group/src",
		DstHost:          "gitlab.dedicated.local",
		DstProject:       "group/dst",
		CursorPage:       2,
		LastProcessedIID: 45,
		Status:           "PAUSED",
		ItemsProcessed:   15,
		ItemsFailed:      0,
		ItemsSkipped:     2,
		UpdatedAt:        now,
	}

	if err := store.SaveSyncCheckpoint(ctx, cp); err != nil {
		t.Fatalf("SaveSyncCheckpoint failed: %v", err)
	}

	fetchedCP, err := store.GetSyncCheckpoint(ctx, "run-sync-001")
	if err != nil || fetchedCP == nil {
		t.Fatalf("GetSyncCheckpoint failed: %v, cp=%v", err, fetchedCP)
	}
	if fetchedCP.CursorPage != 2 || fetchedCP.LastProcessedIID != 45 {
		t.Errorf("unexpected checkpoint: %+v", fetchedCP)
	}

	// 3. Quarantine Record
	qr := QuarantineRecord{
		ID:               "quar-001",
		EntityKey:        "issue:gitlab.shared.local/group/src#10",
		EntityType:       "issue",
		SrcHost:          "gitlab.shared.local",
		SrcProject:       "group/src",
		SrcIID:           10,
		DstHost:          "gitlab.dedicated.local",
		DstProject:       "group/dst",
		DstIID:           20,
		SrcSnapshot:      `{"title":"Src Edited"}`,
		DstSnapshot:      `{"title":"Dst Edited"}`,
		BaselineDigest:   "sha256:abc1234",
		QuarantineReason: "CONFLICT: dual independent modification",
		ResolutionStatus: "QUARANTINED",
		QuarantinedAt:    now,
	}

	if err := store.RecordQuarantine(ctx, qr); err != nil {
		t.Fatalf("RecordQuarantine failed: %v", err)
	}

	qList, err := store.ListQuarantineRecords(ctx)
	if err != nil || len(qList) != 1 {
		t.Fatalf("ListQuarantineRecords failed: %v, len=%d", err, len(qList))
	}
	if qList[0].ResolutionStatus != "QUARANTINED" {
		t.Errorf("unexpected quarantine status: %s", qList[0].ResolutionStatus)
	}
}
