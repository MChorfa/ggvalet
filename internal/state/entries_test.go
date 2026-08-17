package state

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MChorfa/ggvalet/internal/journal"
)

func TestStore_RecordAndQueryEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	now := time.Now().UTC()
	e1 := journal.Entry{
		ID: "e1", Host: "gitlab.test", Op: journal.OpCreate, Entity: journal.EntityIssue,
		Project: "g/p", Group: "", EntityID: 10, IID: 1,
		Title: "Issue 1", URL: "http://example.com/1", Outcome: journal.OutcomeOK,
		Tags:      []string{"tag1"},
		Timestamp: now.Add(-2 * time.Hour),
	}
	e2 := journal.Entry{
		ID: "e2", Host: "gitlab.test", Op: journal.OpList, Entity: journal.EntityMR,
		Project: "g/p2", EntityID: 0, IID: 0,
		Title: "list 1 mr", URL: "", Outcome: journal.OutcomeOK,
		Timestamp: now.Add(-30 * time.Minute),
	}
	e3 := journal.Entry{
		ID: "e3", Host: "github.test", Op: journal.OpCreate, Entity: journal.EntityIssue,
		Project: "owner/repo", Title: "GH Issue", Outcome: journal.OutcomeErr, Detail: "boom",
		Timestamp: now.Add(-10 * time.Minute),
	}

	for _, e := range []journal.Entry{e1, e2, e3} {
		if err := s.RecordEntry(ctx, e); err != nil {
			t.Fatalf("RecordEntry %s: %v", e.ID, err)
		}
	}

	// all
	all, err := s.QueryEntries(ctx, journal.Filter{})
	if err != nil || len(all) != 3 {
		t.Fatalf("Query all = %d, err=%v", len(all), err)
	}

	// by host
	host, err := s.QueryEntries(ctx, journal.Filter{Host: "gitlab.test"})
	if err != nil || len(host) != 2 {
		t.Fatalf("Query host = %d, err=%v", len(host), err)
	}

	// by project
	proj, err := s.QueryEntries(ctx, journal.Filter{Project: "g/p"})
	if err != nil || len(proj) != 1 || proj[0].ID != "e1" {
		t.Fatalf("Query project = %d, err=%v", len(proj), err)
	}

	// by ops
	ops, err := s.QueryEntries(ctx, journal.Filter{Ops: []journal.Op{journal.OpCreate}})
	if err != nil || len(ops) != 2 {
		t.Fatalf("Query ops = %d, err=%v", len(ops), err)
	}

	// by entities
	ents, err := s.QueryEntries(ctx, journal.Filter{Entities: []journal.Entity{journal.EntityMR}})
	if err != nil || len(ents) != 1 || ents[0].ID != "e2" {
		t.Fatalf("Query entities = %d, err=%v", len(ents), err)
	}

	// by outcome
	errs, err := s.QueryEntries(ctx, journal.Filter{Outcome: journal.OutcomeErr})
	if err != nil || len(errs) != 1 || errs[0].ID != "e3" {
		t.Fatalf("Query outcome = %d, err=%v", len(errs), err)
	}

	// by time window
	win, err := s.QueryEntries(ctx, journal.Filter{Since: now.Add(-time.Hour), Until: now})
	if err != nil || len(win) != 2 {
		t.Fatalf("Query window = %d, err=%v", len(win), err)
	}

	// verify round-trip fields
	first := all[0]
	if first.ID != "e1" || first.Host != "gitlab.test" || first.Op != journal.OpCreate ||
		first.Entity != journal.EntityIssue || first.Project != "g/p" || first.IID != 1 ||
		first.Title != "Issue 1" || first.URL != "http://example.com/1" ||
		len(first.Tags) != 1 || first.Tags[0] != "tag1" {
		t.Fatalf("entry round-trip mismatch: %#v", first)
	}
}

func TestStore_RecordEntry_GeneratesIDAndTimestamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	e := journal.Entry{Host: "gitlab.test", Op: journal.OpCreate, Entity: journal.EntityIssue, Outcome: journal.OutcomeOK}
	if err := s.RecordEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	entries, err := s.QueryEntries(ctx, journal.Filter{})
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries = %d, err=%v", len(entries), err)
	}
	if entries[0].ID == "" || entries[0].Timestamp.IsZero() {
		t.Fatalf("generated fields missing: %#v", entries[0])
	}
}

func TestStore_HasEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	has, err := s.HasEntries(ctx)
	if err != nil || has {
		t.Fatalf("HasEntries empty = %v, err=%v", has, err)
	}
	if err := s.RecordEntry(ctx, journal.Entry{ID: "x", Host: "h", Op: journal.OpCreate, Entity: journal.EntityIssue, Outcome: journal.OutcomeOK}); err != nil {
		t.Fatal(err)
	}
	has, err = s.HasEntries(ctx)
	if err != nil || !has {
		t.Fatalf("HasEntries populated = %v, err=%v", has, err)
	}
}

func TestStore_BackfillJournalEntries(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "journal.jsonl")
	e1 := journal.Entry{ID: "old-1", Host: "gitlab.test", Op: journal.OpCreate, Entity: journal.EntityIssue, Project: "g/p", Outcome: journal.OutcomeOK}
	e2 := journal.Entry{ID: "old-2", Host: "gitlab.test", Op: journal.OpUpdate, Entity: journal.EntityIssue, Project: "g/p", Outcome: journal.OutcomeOK}
	b1, _ := json.Marshal(e1)
	b2, _ := json.Marshal(e2)
	data := append(append(b1, '\n'), append(b2, '\n')...)
	if err := os.WriteFile(legacy, data, 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	if err := s.BackfillJournalEntries(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	entries, err := s.QueryEntries(ctx, journal.Filter{})
	if err != nil || len(entries) != 2 {
		t.Fatalf("backfill entries = %d, err=%v", len(entries), err)
	}

	// idempotent
	if err := s.BackfillJournalEntries(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	entries2, err := s.QueryEntries(ctx, journal.Filter{})
	if err != nil || len(entries2) != 2 {
		t.Fatalf("second backfill entries = %d, err=%v", len(entries2), err)
	}
}

func TestStore_BackfillJournalEntries_MissingFile(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	if err := s.BackfillJournalEntries(ctx, filepath.Join(dir, "missing.jsonl")); err != nil {
		t.Fatal(err)
	}
	has, err := s.HasEntries(ctx)
	if err != nil || has {
		t.Fatalf("HasEntries after missing file = %v, err=%v", has, err)
	}
}

func TestStore_RecordEntry_DBClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	ctx := context.Background()

	e := journal.Entry{ID: "x", Host: "h", Op: journal.OpCreate, Entity: journal.EntityIssue, Outcome: journal.OutcomeOK}
	if err := s.RecordEntry(ctx, e); err == nil {
		t.Fatal("expected RecordEntry error after close")
	}
	if _, err := s.HasEntries(ctx); err == nil {
		t.Fatal("expected HasEntries error after close")
	}
	if _, err := s.QueryEntries(ctx, journal.Filter{}); err == nil {
		t.Fatal("expected QueryEntries error after close")
	}
}

func TestStore_BackfillJournalEntries_MalformedLine(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "journal.jsonl")
	if err := os.WriteFile(legacy, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.BackfillJournalEntries(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	has, err := s.HasEntries(context.Background())
	if err != nil || has {
		t.Fatalf("HasEntries after malformed backfill = %v, err=%v", has, err)
	}
}
