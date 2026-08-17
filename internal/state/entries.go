package state

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/google/uuid"
)

// RecordEntry persists a journal-compatible entry to the SQLite projection.
func (s *Store) RecordEntry(ctx context.Context, e journal.Entry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("journal entry begin: %w", err)
	}
	defer tx.Rollback()
	if err := recordEntryTx(ctx, tx, e); err != nil {
		return err
	}
	return tx.Commit()
}

func recordEntryTx(ctx context.Context, tx *sql.Tx, e journal.Entry) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	tags, err := json.Marshal(e.Tags)
	if err != nil {
		tags = []byte("[]")
	}
	_, err = tx.ExecContext(ctx, `
INSERT OR IGNORE INTO journal_entries
(id, host, op, entity, project, group_path, entity_id, iid, title, url, outcome, detail, tags, timestamp)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.Host, string(e.Op), string(e.Entity), e.Project, e.Group, e.EntityID, e.IID,
		e.Title, e.URL, string(e.Outcome), e.Detail, string(tags), e.Timestamp.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("journal entry insert: %w", err)
	}
	return nil
}

// HasEntries reports whether the SQLite journal projection contains any rows.
func (s *Store) HasEntries(ctx context.Context) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM journal_entries LIMIT 1)`).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("journal entries check: %w", err)
	}
	return exists, nil
}

// QueryEntries returns journal entries from the SQLite projection.
func (s *Store) QueryEntries(ctx context.Context, f journal.Filter) ([]journal.Entry, error) {
	var args []any
	query := `SELECT id, host, op, entity, project, group_path, entity_id, iid, title, url, outcome, detail, tags, timestamp FROM journal_entries WHERE 1=1`

	if !f.Since.IsZero() {
		query += ` AND timestamp >= ?`
		args = append(args, f.Since.UTC().Format(time.RFC3339Nano))
	}
	if !f.Until.IsZero() {
		query += ` AND timestamp <= ?`
		args = append(args, f.Until.UTC().Format(time.RFC3339Nano))
	}
	if f.Host != "" {
		query += ` AND host = ?`
		args = append(args, f.Host)
	}
	if f.Project != "" {
		query += ` AND project = ?`
		args = append(args, f.Project)
	}
	if f.Group != "" {
		query += ` AND group_path = ?`
		args = append(args, f.Group)
	}
	if f.Outcome != "" {
		query += ` AND outcome = ?`
		args = append(args, string(f.Outcome))
	}
	if len(f.Ops) > 0 {
		ph, vals := inClause(len(f.Ops))
		query += ` AND op IN (` + ph + `)`
		for _, op := range f.Ops {
			vals = append(vals, string(op))
		}
		args = append(args, vals...)
	}
	if len(f.Entities) > 0 {
		ph, vals := inClause(len(f.Entities))
		query += ` AND entity IN (` + ph + `)`
		for _, ent := range f.Entities {
			vals = append(vals, string(ent))
		}
		args = append(args, vals...)
	}
	query += ` ORDER BY timestamp, id`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("journal entries query: %w", err)
	}
	defer rows.Close()

	var out []journal.Entry
	for rows.Next() {
		var e journal.Entry
		var ts, tags string
		if err := rows.Scan(&e.ID, &e.Host, (*string)(&e.Op), (*string)(&e.Entity), &e.Project, &e.Group,
			&e.EntityID, &e.IID, &e.Title, &e.URL, (*string)(&e.Outcome), &e.Detail, &tags, &ts); err != nil {
			return nil, fmt.Errorf("journal entry scan: %w", err)
		}
		e.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		_ = json.Unmarshal([]byte(tags), &e.Tags)
		out = append(out, e)
	}
	return out, rows.Err()
}

func inClause(n int) (string, []any) {
	placeholders := make([]string, n)
	for i := range placeholders {
		placeholders[i] = "?"
	}
	return strings.Join(placeholders, ","), nil
}

// BackfillJournalEntries imports legacy JSONL journal entries into the SQLite projection.
func (s *Store) BackfillJournalEntries(ctx context.Context, path string) error {
	var done string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM metadata WHERE key='journal_projection_import'`).Scan(&done)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("journal projection import status: %w", err)
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		_, err = s.db.ExecContext(ctx, `INSERT INTO metadata(key,value) VALUES('journal_projection_import','absent')`)
		return err
	}
	if err != nil {
		return fmt.Errorf("journal projection open: %w", err)
	}
	defer f.Close()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	count, err := importJournalEntriesTx(ctx, tx, f)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO metadata(key,value) VALUES('journal_projection_import',?)`, fmt.Sprintf("%d", count)); err != nil {
		return err
	}
	return tx.Commit()
}

func importJournalEntriesTx(ctx context.Context, tx *sql.Tx, reader io.Reader) (int, error) {
	scanner, count := bufio.NewScanner(reader), 0
	for scanner.Scan() {
		var e journal.Entry
		if json.Unmarshal(scanner.Bytes(), &e) != nil || e.ID == "" {
			continue
		}
		if err := recordEntryTx(ctx, tx, e); err != nil {
			return count, err
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("journal projection scan: %w", err)
	}
	return count, nil
}
