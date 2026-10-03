package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/policy"
	"github.com/MChorfa/ggvalet/internal/trustwall"
	"github.com/MChorfa/ggvalet/internal/vector"
)

// RecordRefusalReceipt persists a signed refusal receipt when an action is rejected by policy/authority.
func (s *Store) RecordRefusalReceipt(ctx context.Context, r *authority.RefusalReceipt) error {
	const query = `
INSERT INTO refusal_receipts (id, lease_id, subject_role, attempted_action, attempted_resource, reason, evidence_digest, occurred_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query,
		r.ReceiptID,
		r.LeaseID,
		r.SubjectRole,
		string(r.RequiredCapability),
		r.AttemptedResource,
		r.Reason,
		r.EvidenceDigest,
		r.Timestamp.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("state record refusal receipt: %w", err)
	}
	return nil
}

// QueryRefusalReceipts returns all recorded refusal receipts ordered by time descending.
func (s *Store) QueryRefusalReceipts(ctx context.Context) ([]authority.RefusalReceipt, error) {
	const query = `
SELECT id, lease_id, subject_role, attempted_action, attempted_resource, reason, evidence_digest, occurred_at
FROM refusal_receipts ORDER BY occurred_at DESC`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query refusal receipts: %w", err)
	}
	defer rows.Close()

	var list []authority.RefusalReceipt
	for rows.Next() {
		var r authority.RefusalReceipt
		var capStr, timeStr string
		if err := rows.Scan(&r.ReceiptID, &r.LeaseID, &r.SubjectRole, &capStr, &r.AttemptedResource, &r.Reason, &r.EvidenceDigest, &timeStr); err != nil {
			return nil, err
		}
		r.ActionNotPerformed = true
		r.RequiredCapability = authority.Capability(capStr)
		r.Timestamp, _ = time.Parse(time.RFC3339, timeStr)
		list = append(list, r)
	}
	return list, nil
}

// RecordTrustwallReceipt stores an admission decision receipt.
func (s *Store) RecordTrustwallReceipt(ctx context.Context, r *trustwall.AdmissionReceipt) error {
	violsJSON, _ := json.Marshal(r.Violations)
	obligsJSON, _ := json.Marshal(r.Obligations)
	const query = `
INSERT INTO trustwall_receipts (id, artifact_id, decision, violations, obligations, receipt_digest, evaluated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query,
		r.ReceiptID,
		r.ArtifactID,
		string(r.Decision),
		string(violsJSON),
		string(obligsJSON),
		r.ReceiptDigest,
		r.EvaluatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("state record trustwall receipt: %w", err)
	}
	return nil
}

// RecordVectorState persists an entity's vector state evolution.
func (s *Store) RecordVectorState(ctx context.Context, v vector.StateVector) error {
	const query = `
INSERT INTO vector_states (entity_id, presence, valence, anti, coherence, evidence, mode, epoch, timestamp)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(entity_id, epoch) DO UPDATE SET
 presence=excluded.presence, valence=excluded.valence, anti=excluded.anti,
 coherence=excluded.coherence, evidence=excluded.evidence, mode=excluded.mode,
 timestamp=excluded.timestamp`
	_, err := s.db.ExecContext(ctx, query,
		v.EntityID,
		string(v.Presence),
		string(v.Valence),
		string(v.Anti),
		string(v.Coherence),
		string(v.Evidence),
		string(v.Mode),
		v.Epoch,
		v.Timestamp.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("state record vector state: %w", err)
	}
	return nil
}

// GetLatestVectorState queries the highest epoch vector state for an entity.
func (s *Store) GetLatestVectorState(ctx context.Context, entityID string) (*vector.StateVector, error) {
	const query = `
SELECT entity_id, presence, valence, anti, coherence, evidence, mode, epoch, timestamp
FROM vector_states WHERE entity_id = ? ORDER BY epoch DESC LIMIT 1`
	var v vector.StateVector
	var pStr, vStr, aStr, cStr, eStr, mStr, tStr string
	err := s.db.QueryRowContext(ctx, query, entityID).Scan(
		&v.EntityID, &pStr, &vStr, &aStr, &cStr, &eStr, &mStr, &v.Epoch, &tStr,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v.Presence = vector.Presence(pStr)
	v.Valence = vector.Valence(vStr)
	v.Anti = vector.AntiRelation(aStr)
	v.Coherence = vector.Coherence(cStr)
	v.Evidence = vector.EvidenceStatus(eStr)
	v.Mode = vector.LifecycleMode(mStr)
	v.Timestamp, _ = time.Parse(time.RFC3339, tStr)
	return &v, nil
}

// ListVectorStates returns recent vector states across entities.
func (s *Store) ListVectorStates(ctx context.Context, limit int) ([]vector.StateVector, error) {
	if limit <= 0 {
		limit = 50
	}
	const query = `
SELECT entity_id, presence, valence, anti, coherence, evidence, mode, epoch, timestamp
FROM vector_states ORDER BY timestamp DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("state list vector states: %w", err)
	}
	defer rows.Close()

	var result []vector.StateVector
	for rows.Next() {
		var v vector.StateVector
		var pStr, vStr, aStr, cStr, eStr, mStr, tStr string
		if err := rows.Scan(&v.EntityID, &pStr, &vStr, &aStr, &cStr, &eStr, &mStr, &v.Epoch, &tStr); err != nil {
			return nil, fmt.Errorf("state scan vector state: %w", err)
		}
		v.Presence = vector.Presence(pStr)
		v.Valence = vector.Valence(vStr)
		v.Anti = vector.AntiRelation(aStr)
		v.Coherence = vector.Coherence(cStr)
		v.Evidence = vector.EvidenceStatus(eStr)
		v.Mode = vector.LifecycleMode(mStr)
		v.Timestamp, _ = time.Parse(time.RFC3339, tStr)
		result = append(result, v)
	}
	return result, rows.Err()
}

// RecordCandidateRule persists or updates a discovered progressive rule.
func (s *Store) RecordCandidateRule(ctx context.Context, r *policy.CandidateRule) error {
	const query = `
INSERT INTO candidate_rules (id, title, description, pattern, stage, match_frequency, estimated_savings, approved_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
 stage=excluded.stage, approved_by=excluded.approved_by, updated_at=excluded.updated_at`
	_, err := s.db.ExecContext(ctx, query,
		r.ID,
		r.Title,
		r.Description,
		r.PatternObserved,
		string(r.Stage),
		r.MatchFrequency,
		r.EstimatedSavings,
		r.ApprovedBy,
		r.CreatedAt.UTC().Format(time.RFC3339),
		r.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("state record candidate rule: %w", err)
	}
	return nil
}

// ListCandidateRules lists all progressive rules recorded.
func (s *Store) ListCandidateRules(ctx context.Context) ([]policy.CandidateRule, error) {
	const query = `
SELECT id, title, description, pattern, stage, match_frequency, estimated_savings, approved_by, created_at, updated_at
FROM candidate_rules ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query candidate rules: %w", err)
	}
	defer rows.Close()

	var list []policy.CandidateRule
	for rows.Next() {
		var r policy.CandidateRule
		var stageStr, createdStr, updatedStr string
		var approvedBy sql.NullString
		if err := rows.Scan(
			&r.ID, &r.Title, &r.Description, &r.PatternObserved,
			&stageStr, &r.MatchFrequency, &r.EstimatedSavings, &approvedBy,
			&createdStr, &updatedStr,
		); err != nil {
			return nil, err
		}
		r.ApprovedBy = approvedBy.String
		r.Stage = policy.RuleStage(stageStr)
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		list = append(list, r)
	}
	return list, nil
}

// GetCandidateRule queries a specific candidate rule by ID.
func (s *Store) GetCandidateRule(ctx context.Context, id string) (*policy.CandidateRule, error) {
	const query = `
SELECT id, title, description, pattern, stage, match_frequency, estimated_savings, approved_by, created_at, updated_at
FROM candidate_rules WHERE id = ?`
	var r policy.CandidateRule
	var stageStr, createdStr, updatedStr string
	var approvedBy sql.NullString
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&r.ID, &r.Title, &r.Description, &r.PatternObserved,
		&stageStr, &r.MatchFrequency, &r.EstimatedSavings, &approvedBy,
		&createdStr, &updatedStr,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.ApprovedBy = approvedBy.String
	r.Stage = policy.RuleStage(stageStr)
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	return &r, nil
}
