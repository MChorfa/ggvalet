package state

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MChorfa/ggvalet/internal/journal"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrPermission }

func TestStoreReceiptsAndPlanCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.Begin(context.Background(), Operation{Provider: "gitlab", Host: "gitlab.test", Action: "create", Resource: "issue", Target: "g/p", InputDigest: Digest("input")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Complete(context.Background(), id, StatusSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	receipts, err := s.Receipts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 2 || receipts[0].Phase != "intent" || receipts[1].Phase != "outcome" {
		t.Fatalf("receipts = %#v", receipts)
	}
	var exported bytes.Buffer
	if err := s.ExportJSONL(context.Background(), &exported); err != nil || exported.Len() == 0 {
		t.Fatalf("export len=%d err=%v", exported.Len(), err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	runID, err := s.StartRun(context.Background(), "gitlab:test:1:2", "gitlab", []byte(`{"Version":"2","Target":{"Provider":"gitlab"}}`), []PlanStep{{StepID: "issue:i1", Kind: "issue"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStep(context.Background(), runID, "issue:i1", StatusSucceeded, 1, 2, "url", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRunStatus(context.Background(), runID, StatusSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	run, steps, err := s.Run(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != StatusSucceeded || steps[0].RemoteIID != 2 {
		t.Fatalf("run=%#v steps=%#v", run, steps)
	}
}

func TestStoreImportsLegacyOnce(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "journal.jsonl")
	e := journal.Entry{ID: "one", Host: "gitlab.test", Op: journal.OpCreate, Entity: journal.EntityIssue, Project: "g/p", Outcome: journal.OutcomeOK}
	b, _ := json.Marshal(e)
	if err := os.WriteFile(legacy, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ImportLegacy(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	if err := s.ImportLegacy(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	receipts, err := s.Receipts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 1 || receipts[0].Phase != "legacy" {
		t.Fatalf("receipts = %#v", receipts)
	}
}

func TestStoreFailureAndExclusivityPaths(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.ImportLegacy(ctx, filepath.Join(dir, "missing.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := s.Complete(ctx, "missing", StatusSucceeded, ""); err == nil {
		t.Fatal("expected missing operation error")
	}
	if _, _, err := s.Run(ctx, "missing"); err == nil {
		t.Fatal("expected missing run error")
	}
	runID, err := s.StartRun(ctx, "gitlab:test:1:2", "gitlab", []byte(`{}`), []PlanStep{{StepID: "issue:i", Kind: "issue"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRun(ctx, "gitlab:test:1:2", "gitlab", []byte(`{}`), nil); err == nil {
		t.Fatal("expected active-run conflict")
	}
	if err := s.SetRunStatus(ctx, runID, StatusFailed, "stop"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Begin(ctx, Operation{Provider: "gitlab", Host: "test", Action: "list", Resource: "issue", Target: "p"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ExportJSONL(ctx, failingWriter{}); err == nil {
		t.Fatal("expected writer failure")
	}
}

func TestStoreImportsLegacyErrorsAndMalformedLines(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "journal.jsonl")
	entry := journal.Entry{ID: "failed", Host: "gitlab.test", Op: journal.OpCreate, Entity: journal.EntityIssue, Outcome: journal.OutcomeErr}
	b, _ := json.Marshal(entry)
	data := append([]byte("not-json\n"), append(b, '\n')...)
	if err := os.WriteFile(legacy, data, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ImportLegacy(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	receipts, err := s.Receipts(context.Background())
	if err != nil || len(receipts) != 1 || receipts[0].Status != StatusFailed {
		t.Fatalf("receipts=%#v err=%v", receipts, err)
	}
}
