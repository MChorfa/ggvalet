// Package state owns ggvalet's transactional operational state.
package state

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type Operation struct {
	Provider, Host, Action, Resource, Target, InputDigest string
}

type Receipt struct {
	ID, OperationID, Phase, Provider, Host, Action, Resource, Target string
	Status, Detail, InputDigest                                      string
	OccurredAt                                                       time.Time
}

type PlanRun struct {
	ID, TargetKey, Provider, PlanJSON, Status, Error string
	CreatedAt, UpdatedAt                             time.Time
}

type PlanStep struct {
	RunID, StepID, Kind, Status, WebURL, Error string
	RemoteID, RemoteIID                        int
	UpdatedAt                                  time.Time
}

const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusBlocked   = "blocked"
	StatusUncertain = "uncertain"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("state mkdir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("state open: %w", err)
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("state %s: %w", pragma, err)
		}
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("state chmod: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS operations (
 id TEXT PRIMARY KEY, provider TEXT NOT NULL, host TEXT NOT NULL,
 action TEXT NOT NULL, resource TEXT NOT NULL, target TEXT NOT NULL,
 input_digest TEXT NOT NULL, status TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '',
 started_at TEXT NOT NULL, completed_at TEXT
);
CREATE TABLE IF NOT EXISTS receipt_events (
 id TEXT PRIMARY KEY, operation_id TEXT NOT NULL, phase TEXT NOT NULL,
 provider TEXT NOT NULL, host TEXT NOT NULL, action TEXT NOT NULL,
 resource TEXT NOT NULL, target TEXT NOT NULL, status TEXT NOT NULL,
 detail TEXT NOT NULL DEFAULT '', input_digest TEXT NOT NULL,
 occurred_at TEXT NOT NULL,
 FOREIGN KEY(operation_id) REFERENCES operations(id)
);
CREATE INDEX IF NOT EXISTS receipt_events_operation ON receipt_events(operation_id, occurred_at);
CREATE TABLE IF NOT EXISTS journal_entries (
 id TEXT PRIMARY KEY, host TEXT NOT NULL, op TEXT NOT NULL, entity TEXT NOT NULL,
 project TEXT NOT NULL DEFAULT '', group_path TEXT NOT NULL DEFAULT '',
 entity_id INTEGER NOT NULL DEFAULT 0, iid INTEGER NOT NULL DEFAULT 0,
 title TEXT NOT NULL DEFAULT '', url TEXT NOT NULL DEFAULT '',
 outcome TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '', tags TEXT NOT NULL DEFAULT '[]',
 timestamp TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS journal_entries_time ON journal_entries(timestamp);
CREATE INDEX IF NOT EXISTS journal_entries_host ON journal_entries(host);
CREATE INDEX IF NOT EXISTS journal_entries_outcome ON journal_entries(outcome);
CREATE TABLE IF NOT EXISTS plan_runs (
 id TEXT PRIMARY KEY, target_key TEXT NOT NULL, provider TEXT NOT NULL,
 plan_json TEXT NOT NULL, status TEXT NOT NULL, error TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS one_running_plan_per_target
 ON plan_runs(target_key) WHERE status = 'running';
CREATE TABLE IF NOT EXISTS plan_steps (
 run_id TEXT NOT NULL, step_id TEXT NOT NULL, kind TEXT NOT NULL,
 status TEXT NOT NULL, remote_id INTEGER NOT NULL DEFAULT 0,
 remote_iid INTEGER NOT NULL DEFAULT 0, web_url TEXT NOT NULL DEFAULT '',
 error TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL,
 PRIMARY KEY(run_id, step_id),
 FOREIGN KEY(run_id) REFERENCES plan_runs(id)
);
CREATE TABLE IF NOT EXISTS rotation_state (
 host TEXT PRIMARY KEY, last_rotated_at TEXT NOT NULL
);`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("state migrate: %w", err)
	}
	return nil
}

func Digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func (s *Store) Begin(ctx context.Context, op Operation) (string, error) {
	id, now := uuid.NewString(), time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("receipt begin transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO operations
 (id,provider,host,action,resource,target,input_digest,status,started_at)
 VALUES(?,?,?,?,?,?,?,?,?)`, id, op.Provider, op.Host, op.Action, op.Resource,
		op.Target, op.InputDigest, StatusRunning, now.Format(time.RFC3339Nano)); err != nil {
		return "", fmt.Errorf("receipt begin operation: %w", err)
	}
	if err = insertReceipt(ctx, tx, id, "intent", op, StatusRunning, "", now); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", fmt.Errorf("receipt begin commit: %w", err)
	}
	return id, nil
}

func (s *Store) Complete(ctx context.Context, id, status, detail string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("receipt complete transaction: %w", err)
	}
	defer tx.Rollback()
	var op Operation
	err = tx.QueryRowContext(ctx, `SELECT provider,host,action,resource,target,input_digest
 FROM operations WHERE id=?`, id).Scan(&op.Provider, &op.Host, &op.Action,
		&op.Resource, &op.Target, &op.InputDigest)
	if err != nil {
		return fmt.Errorf("receipt operation lookup: %w", err)
	}
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, `UPDATE operations SET status=?,detail=?,completed_at=? WHERE id=?`,
		status, detail, now.Format(time.RFC3339Nano), id); err != nil {
		return fmt.Errorf("receipt operation update: %w", err)
	}
	if err = insertReceipt(ctx, tx, id, "outcome", op, status, detail, now); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("receipt complete commit: %w", err)
	}
	return nil
}

func insertReceipt(ctx context.Context, tx *sql.Tx, operationID, phase string, op Operation, status, detail string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO receipt_events
 (id,operation_id,phase,provider,host,action,resource,target,status,detail,input_digest,occurred_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, uuid.NewString(), operationID, phase, op.Provider,
		op.Host, op.Action, op.Resource, op.Target, status, detail, op.InputDigest,
		at.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("receipt event insert: %w", err)
	}
	return nil
}

func (s *Store) Receipts(ctx context.Context) ([]Receipt, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,operation_id,phase,provider,host,
 action,resource,target,status,detail,input_digest,occurred_at
 FROM receipt_events ORDER BY occurred_at,id`)
	if err != nil {
		return nil, fmt.Errorf("receipt query: %w", err)
	}
	defer rows.Close()
	var out []Receipt
	for rows.Next() {
		var r Receipt
		var at string
		if err := rows.Scan(&r.ID, &r.OperationID, &r.Phase, &r.Provider, &r.Host,
			&r.Action, &r.Resource, &r.Target, &r.Status, &r.Detail, &r.InputDigest, &at); err != nil {
			return nil, err
		}
		r.OccurredAt, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ExportJSONL(ctx context.Context, w io.Writer) error {
	receipts, err := s.Receipts(ctx)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	for _, r := range receipts {
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("receipt export: %w", err)
		}
	}
	return nil
}

func (s *Store) ImportLegacy(ctx context.Context, path string) error {
	var done string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM metadata WHERE key='legacy_journal_import'`).Scan(&done)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("legacy import status: %w", err)
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		_, err = s.db.ExecContext(ctx, `INSERT INTO metadata(key,value) VALUES('legacy_journal_import','absent')`)
		return err
	}
	if err != nil {
		return fmt.Errorf("legacy journal open: %w", err)
	}
	defer f.Close()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	count, err := importLegacyEntries(ctx, tx, f)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO metadata(key,value) VALUES('legacy_journal_import',?)`, fmt.Sprintf("%d", count)); err != nil {
		return err
	}
	return tx.Commit()
}

func importLegacyEntries(ctx context.Context, tx *sql.Tx, reader io.Reader) (int, error) {
	scanner, count := bufio.NewScanner(reader), 0
	for scanner.Scan() {
		var e journal.Entry
		if json.Unmarshal(scanner.Bytes(), &e) != nil || e.ID == "" {
			continue
		}
		if err := insertLegacyEntry(ctx, tx, e); err != nil {
			return count, err
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("legacy journal scan: %w", err)
	}
	return count, nil
}

func insertLegacyEntry(ctx context.Context, tx *sql.Tx, entry journal.Entry) error {
	status := StatusSucceeded
	if entry.Outcome == journal.OutcomeErr {
		status = StatusFailed
	}
	op := Operation{Provider: "gitlab", Host: entry.Host, Action: string(entry.Op), Resource: string(entry.Entity),
		Target: firstTarget(entry.Project, entry.Group), InputDigest: Digest(entry)}
	at := entry.Timestamp.UTC()
	if at.IsZero() {
		at = time.Now().UTC()
	}
	id := "legacy:" + entry.ID
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO operations
 (id,provider,host,action,resource,target,input_digest,status,detail,started_at,completed_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, op.Provider, op.Host, op.Action, op.Resource, op.Target,
		op.InputDigest, status, entry.Detail, at.Format(time.RFC3339Nano), at.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if err := insertReceipt(ctx, tx, id, "legacy", op, status, entry.Detail, at); err != nil && !strings.Contains(err.Error(), "UNIQUE") {
		return err
	}
	return nil
}

func firstTarget(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "all"
}

func (s *Store) StartRun(ctx context.Context, targetKey, provider string, planJSON []byte, steps []PlanStep) (string, error) {
	id, now := uuid.NewString(), time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO plan_runs
 (id,target_key,provider,plan_json,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		id, targetKey, provider, string(planJSON), StatusRunning, now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return "", fmt.Errorf("a reconciliation run is already active for %s", targetKey)
		}
		return "", err
	}
	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, `INSERT INTO plan_steps
 (run_id,step_id,kind,status,updated_at) VALUES(?,?,?,?,?)`, id, step.StepID,
			step.Kind, StatusPending, now); err != nil {
			return "", err
		}
	}
	return id, tx.Commit()
}

func (s *Store) Run(ctx context.Context, id string) (PlanRun, []PlanStep, error) {
	var run PlanRun
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,target_key,provider,plan_json,status,error,created_at,updated_at
 FROM plan_runs WHERE id=?`, id).Scan(&run.ID, &run.TargetKey, &run.Provider,
		&run.PlanJSON, &run.Status, &run.Error, &created, &updated)
	if err != nil {
		return run, nil, fmt.Errorf("plan run %q: %w", id, err)
	}
	run.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	run.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	rows, err := s.db.QueryContext(ctx, `SELECT run_id,step_id,kind,status,remote_id,remote_iid,web_url,error,updated_at
 FROM plan_steps WHERE run_id=? ORDER BY rowid`, id)
	if err != nil {
		return run, nil, err
	}
	defer rows.Close()
	var steps []PlanStep
	for rows.Next() {
		var step PlanStep
		var at string
		if err := rows.Scan(&step.RunID, &step.StepID, &step.Kind, &step.Status,
			&step.RemoteID, &step.RemoteIID, &step.WebURL, &step.Error, &at); err != nil {
			return run, nil, err
		}
		step.UpdatedAt, _ = time.Parse(time.RFC3339Nano, at)
		steps = append(steps, step)
	}
	return run, steps, rows.Err()
}

func (s *Store) SetRunStatus(ctx context.Context, id, status, detail string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE plan_runs SET status=?,error=?,updated_at=? WHERE id=?`,
		status, detail, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (s *Store) SetStep(ctx context.Context, runID, stepID, status string, remoteID, remoteIID int, webURL, detail string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE plan_steps SET status=?,remote_id=?,remote_iid=?,web_url=?,error=?,updated_at=?
 WHERE run_id=? AND step_id=?`, status, remoteID, remoteIID, webURL, detail,
		time.Now().UTC().Format(time.RFC3339Nano), runID, stepID)
	return err
}

// SetLastRotated records a successful rotation for host.
func (s *Store) SetLastRotated(ctx context.Context, host string, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO rotation_state (host, last_rotated_at) VALUES (?, ?)
		 ON CONFLICT(host) DO UPDATE SET last_rotated_at = excluded.last_rotated_at`,
		host, at.UTC().Format(time.RFC3339))
	return err
}

// LastRotated returns the last successful rotation for host. ok is false when
// the host has never been rotated.
func (s *Store) LastRotated(ctx context.Context, host string) (time.Time, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT last_rotated_at FROM rotation_state WHERE host = ?`, host).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false, err
	}
	return t, true, nil
}
