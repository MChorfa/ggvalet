package syncindex

import (
	"context"
	"fmt"
	"time"

	"github.com/MChorfa/ggvalet/internal/state"
)

// CheckpointManager tracks high-water mark progress for crash recovery.
type CheckpointManager struct {
	store *state.Store
	cp    state.SyncCheckpoint
}

// NewCheckpointManager initializes or loads an existing checkpoint.
func NewCheckpointManager(store *state.Store, runID, srcHost, srcProject, dstHost, dstProject string) (*CheckpointManager, error) {
	if store == nil {
		return &CheckpointManager{
			cp: state.SyncCheckpoint{
				RunID:      runID,
				SrcHost:    srcHost,
				SrcProject: srcProject,
				DstHost:    dstHost,
				DstProject: dstProject,
				Status:     "RUNNING",
				UpdatedAt:  time.Now().UTC(),
			},
		}, nil
	}

	existing, err := store.GetSyncCheckpoint(context.Background(), runID)
	if err != nil {
		return nil, fmt.Errorf("load checkpoint: %w", err)
	}
	if existing != nil {
		return &CheckpointManager{store: store, cp: *existing}, nil
	}

	cp := state.SyncCheckpoint{
		RunID:      runID,
		SrcHost:    srcHost,
		SrcProject: srcProject,
		DstHost:    dstHost,
		DstProject: dstProject,
		Status:     "RUNNING",
		UpdatedAt:  time.Now().UTC(),
	}
	_ = store.SaveSyncCheckpoint(context.Background(), cp)
	return &CheckpointManager{store: store, cp: cp}, nil
}

// Checkpoint returns the current state.
func (cm *CheckpointManager) Checkpoint() state.SyncCheckpoint {
	return cm.cp
}

// Advance updates the cursor and persists to SQLite.
func (cm *CheckpointManager) Advance(ctx context.Context, page, lastIID, processed, failed, skipped int) error {
	cm.cp.CursorPage = page
	cm.cp.LastProcessedIID = lastIID
	cm.cp.ItemsProcessed += processed
	cm.cp.ItemsFailed += failed
	cm.cp.ItemsSkipped += skipped
	cm.cp.UpdatedAt = time.Now().UTC()

	if cm.store != nil {
		return cm.store.SaveSyncCheckpoint(ctx, cm.cp)
	}
	return nil
}

// Complete marks the run finished.
func (cm *CheckpointManager) Complete(ctx context.Context) error {
	cm.cp.Status = "COMPLETED"
	cm.cp.UpdatedAt = time.Now().UTC()
	if cm.store != nil {
		return cm.store.SaveSyncCheckpoint(ctx, cm.cp)
	}
	return nil
}
