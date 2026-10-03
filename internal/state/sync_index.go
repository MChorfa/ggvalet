package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type SyncIndexRecord struct {
	ID            string    `json:"id"`
	SrcHost       string    `json:"src_host"`
	SrcProject    string    `json:"src_project"`
	EntityType    string    `json:"entity_type"`
	SrcIID        int       `json:"src_iid"`
	SrcURL        string    `json:"src_url"`
	DstHost       string    `json:"dst_host"`
	DstProject    string    `json:"dst_project"`
	DstIID        int       `json:"dst_iid"`
	DstURL        string    `json:"dst_url"`
	ContentDigest string    `json:"content_digest"`
	SyncEpoch     int       `json:"sync_epoch"`
	Status        string    `json:"status"`
	LastSyncedAt  time.Time `json:"last_synced_at"`
}

type SyncCheckpoint struct {
	RunID            string    `json:"run_id"`
	SrcHost          string    `json:"src_host"`
	SrcProject       string    `json:"src_project"`
	DstHost          string    `json:"dst_host"`
	DstProject       string    `json:"dst_project"`
	CursorPage       int       `json:"cursor_page"`
	LastProcessedIID int       `json:"last_processed_iid"`
	Status           string    `json:"status"`
	ItemsProcessed   int       `json:"items_processed"`
	ItemsFailed      int       `json:"items_failed"`
	ItemsSkipped     int       `json:"items_skipped"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type QuarantineRecord struct {
	ID               string    `json:"id"`
	EntityKey        string    `json:"entity_key"`
	EntityType       string    `json:"entity_type"`
	SrcHost          string    `json:"src_host"`
	SrcProject       string    `json:"src_project"`
	SrcIID           int       `json:"src_iid"`
	DstHost          string    `json:"dst_host"`
	DstProject       string    `json:"dst_project"`
	DstIID           int       `json:"dst_iid"`
	SrcSnapshot      string    `json:"src_snapshot"`
	DstSnapshot      string    `json:"dst_snapshot"`
	BaselineDigest   string    `json:"baseline_digest"`
	QuarantineReason string    `json:"quarantine_reason"`
	ResolutionStatus string    `json:"resolution_status"` // QUARANTINED, RESOLVED_SRC, RESOLVED_DST, FORKED
	ResolvedBy       string    `json:"resolved_by"`
	QuarantinedAt    time.Time `json:"quarantined_at"`
	ResolvedAt       time.Time `json:"resolved_at,omitempty"`
}

// RecordSyncIndexEntry inserts or updates a sync index mapping.
func (s *Store) RecordSyncIndexEntry(ctx context.Context, r SyncIndexRecord) error {
	const query = `
INSERT INTO sync_index (
  id, src_host, src_project, entity_type, src_iid, src_url,
  dst_host, dst_project, dst_iid, dst_url, content_digest,
  sync_epoch, status, last_synced_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(src_host, src_project, entity_type, src_iid, dst_host, dst_project) DO UPDATE SET
  dst_iid=excluded.dst_iid,
  dst_url=excluded.dst_url,
  content_digest=excluded.content_digest,
  sync_epoch=excluded.sync_epoch,
  status=excluded.status,
  last_synced_at=excluded.last_synced_at`

	_, err := s.db.ExecContext(ctx, query,
		r.ID, r.SrcHost, r.SrcProject, r.EntityType, r.SrcIID, r.SrcURL,
		r.DstHost, r.DstProject, r.DstIID, r.DstURL, r.ContentDigest,
		r.SyncEpoch, r.Status, r.LastSyncedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("state record sync index: %w", err)
	}
	return nil
}

// GetSyncIndexEntry queries a specific mapped entity between source and destination.
func (s *Store) GetSyncIndexEntry(ctx context.Context, srcHost, srcProject, entityType string, srcIID int, dstHost, dstProject string) (*SyncIndexRecord, error) {
	const query = `
SELECT id, src_host, src_project, entity_type, src_iid, src_url,
       dst_host, dst_project, dst_iid, dst_url, content_digest,
       sync_epoch, status, last_synced_at
FROM sync_index
WHERE src_host = ? AND src_project = ? AND entity_type = ? AND src_iid = ?
  AND dst_host = ? AND dst_project = ?`

	var r SyncIndexRecord
	var timeStr string
	err := s.db.QueryRowContext(ctx, query, srcHost, srcProject, entityType, srcIID, dstHost, dstProject).Scan(
		&r.ID, &r.SrcHost, &r.SrcProject, &r.EntityType, &r.SrcIID, &r.SrcURL,
		&r.DstHost, &r.DstProject, &r.DstIID, &r.DstURL, &r.ContentDigest,
		&r.SyncEpoch, &r.Status, &timeStr,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.LastSyncedAt, _ = time.Parse(time.RFC3339, timeStr)
	return &r, nil
}

// ListSyncIndexEntries returns all mapped entities for a destination project.
func (s *Store) ListSyncIndexEntries(ctx context.Context, dstHost, dstProject string) ([]SyncIndexRecord, error) {
	const query = `
SELECT id, src_host, src_project, entity_type, src_iid, src_url,
       dst_host, dst_project, dst_iid, dst_url, content_digest,
       sync_epoch, status, last_synced_at
FROM sync_index
WHERE dst_host = ? AND dst_project = ?
ORDER BY last_synced_at DESC`

	rows, err := s.db.QueryContext(ctx, query, dstHost, dstProject)
	if err != nil {
		return nil, fmt.Errorf("list sync index entries: %w", err)
	}
	defer rows.Close()

	var list []SyncIndexRecord
	for rows.Next() {
		var r SyncIndexRecord
		var timeStr string
		if err := rows.Scan(
			&r.ID, &r.SrcHost, &r.SrcProject, &r.EntityType, &r.SrcIID, &r.SrcURL,
			&r.DstHost, &r.DstProject, &r.DstIID, &r.DstURL, &r.ContentDigest,
			&r.SyncEpoch, &r.Status, &timeStr,
		); err != nil {
			return nil, err
		}
		r.LastSyncedAt, _ = time.Parse(time.RFC3339, timeStr)
		list = append(list, r)
	}
	return list, nil
}

// SaveSyncCheckpoint records a high-water mark during a sync run for crash resumption.
func (s *Store) SaveSyncCheckpoint(ctx context.Context, cp SyncCheckpoint) error {
	const query = `
INSERT INTO sync_checkpoints (
  run_id, src_host, src_project, dst_host, dst_project,
  cursor_page, last_processed_iid, status,
  items_processed, items_failed, items_skipped, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET
  cursor_page=excluded.cursor_page,
  last_processed_iid=excluded.last_processed_iid,
  status=excluded.status,
  items_processed=excluded.items_processed,
  items_failed=excluded.items_failed,
  items_skipped=excluded.items_skipped,
  updated_at=excluded.updated_at`

	_, err := s.db.ExecContext(ctx, query,
		cp.RunID, cp.SrcHost, cp.SrcProject, cp.DstHost, cp.DstProject,
		cp.CursorPage, cp.LastProcessedIID, cp.Status,
		cp.ItemsProcessed, cp.ItemsFailed, cp.ItemsSkipped,
		cp.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("save sync checkpoint: %w", err)
	}
	return nil
}

// GetSyncCheckpoint queries an active or paused checkpoint by run ID.
func (s *Store) GetSyncCheckpoint(ctx context.Context, runID string) (*SyncCheckpoint, error) {
	const query = `
SELECT run_id, src_host, src_project, dst_host, dst_project,
       cursor_page, last_processed_iid, status,
       items_processed, items_failed, items_skipped, updated_at
FROM sync_checkpoints WHERE run_id = ?`

	var cp SyncCheckpoint
	var timeStr string
	err := s.db.QueryRowContext(ctx, query, runID).Scan(
		&cp.RunID, &cp.SrcHost, &cp.SrcProject, &cp.DstHost, &cp.DstProject,
		&cp.CursorPage, &cp.LastProcessedIID, &cp.Status,
		&cp.ItemsProcessed, &cp.ItemsFailed, &cp.ItemsSkipped, &timeStr,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cp.UpdatedAt, _ = time.Parse(time.RFC3339, timeStr)
	return &cp, nil
}

// RecordQuarantine stores a forensic conflict record for operator review.
func (s *Store) RecordQuarantine(ctx context.Context, q QuarantineRecord) error {
	const query = `
INSERT INTO quarantine_records (
  id, entity_key, entity_type, src_host, src_project, src_iid,
  dst_host, dst_project, dst_iid, src_snapshot, dst_snapshot,
  baseline_digest, quarantine_reason, resolution_status, resolved_by,
  quarantined_at, resolved_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  resolution_status=excluded.resolution_status,
  resolved_by=excluded.resolved_by,
  resolved_at=excluded.resolved_at`

	var resTimeStr string
	if !q.ResolvedAt.IsZero() {
		resTimeStr = q.ResolvedAt.UTC().Format(time.RFC3339)
	}

	_, err := s.db.ExecContext(ctx, query,
		q.ID, q.EntityKey, q.EntityType, q.SrcHost, q.SrcProject, q.SrcIID,
		q.DstHost, q.DstProject, q.DstIID, q.SrcSnapshot, q.DstSnapshot,
		q.BaselineDigest, q.QuarantineReason, q.ResolutionStatus, q.ResolvedBy,
		q.QuarantinedAt.UTC().Format(time.RFC3339), resTimeStr,
	)
	if err != nil {
		return fmt.Errorf("state record quarantine: %w", err)
	}
	return nil
}

// ListQuarantineRecords returns all quarantine records ordered by quarantine time descending.
func (s *Store) ListQuarantineRecords(ctx context.Context) ([]QuarantineRecord, error) {
	const query = `
SELECT id, entity_key, entity_type, src_host, src_project, src_iid,
       dst_host, dst_project, dst_iid, src_snapshot, dst_snapshot,
       baseline_digest, quarantine_reason, resolution_status, resolved_by,
       quarantined_at, resolved_at
FROM quarantine_records ORDER BY quarantined_at DESC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list quarantine records: %w", err)
	}
	defer rows.Close()

	var list []QuarantineRecord
	for rows.Next() {
		var q QuarantineRecord
		var qTimeStr, rTimeStr string
		if err := rows.Scan(
			&q.ID, &q.EntityKey, &q.EntityType, &q.SrcHost, &q.SrcProject, &q.SrcIID,
			&q.DstHost, &q.DstProject, &q.DstIID, &q.SrcSnapshot, &q.DstSnapshot,
			&q.BaselineDigest, &q.QuarantineReason, &q.ResolutionStatus, &q.ResolvedBy,
			&qTimeStr, &rTimeStr,
		); err != nil {
			return nil, err
		}
		q.QuarantinedAt, _ = time.Parse(time.RFC3339, qTimeStr)
		if rTimeStr != "" {
			q.ResolvedAt, _ = time.Parse(time.RFC3339, rTimeStr)
		}
		list = append(list, q)
	}
	return list, nil
}
