package syncindex

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/google/uuid"
)

// QuarantineConflict isolates conflicting federated entities and records forensic evidence.
func QuarantineConflict(
	ctx context.Context,
	store *state.Store,
	analysis DriftAnalysis,
	src EntitySnapshot,
	dst EntitySnapshot,
) (*state.QuarantineRecord, error) {
	srcJSON, _ := json.Marshal(src)
	dstJSON, _ := json.Marshal(dst)
	now := time.Now().UTC()

	record := state.QuarantineRecord{
		ID:               "quar-" + uuid.NewString()[:8],
		EntityKey:        analysis.EntityKey,
		EntityType:       analysis.EntityType,
		SrcHost:          analysis.SrcHost,
		SrcProject:       analysis.SrcProject,
		SrcIID:           analysis.SrcIID,
		DstHost:          analysis.DstHost,
		DstProject:       analysis.DstProject,
		DstIID:           analysis.DstIID,
		SrcSnapshot:      string(srcJSON),
		DstSnapshot:      string(dstJSON),
		BaselineDigest:   analysis.IndexDigest,
		QuarantineReason: "CONFLICT: concurrent uncoordinated modification on source and destination",
		ResolutionStatus: "QUARANTINED",
		QuarantinedAt:    now,
	}

	if store != nil {
		if err := store.RecordQuarantine(ctx, record); err != nil {
			return nil, fmt.Errorf("record quarantine: %w", err)
		}
		// Persist Vector State
		_ = store.RecordVectorState(ctx, analysis.Vector)
	}

	return &record, nil
}
