package journal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOpen_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	jdir := filepath.Join(dir, "subdir", "deeper")
	jpath := filepath.Join(jdir, "test.jsonl")

	j, err := Open(jpath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if j == nil {
		t.Fatal("Open returned nil journal")
	}

	if _, err := os.Stat(jdir); err != nil {
		t.Fatalf("directory not created: %v", err)
	}

	info, _ := os.Stat(jdir)
	if info.Mode().Perm() != 0o700 {
		t.Errorf("directory mode = %o, want 0700", info.Mode().Perm())
	}
}

func TestOpen_MkdirError(t *testing.T) {
	// Provoke the mkdir failure by placing a regular file where Open needs a
	// directory: MkdirAll under a non-directory component fails with ENOTDIR
	// for every euid (root included), so this needs no permission games and no
	// skip — it deterministically exercises Open's mkdir error path.
	dir := t.TempDir()
	notDir := filepath.Join(dir, "afile")
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	_, err := Open(filepath.Join(notDir, "subdir", "test.jsonl"))
	if err == nil {
		t.Fatal("Open under a non-directory path should error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("journal mkdir")) {
		t.Errorf("error message = %q, want to contain 'journal mkdir'", err)
	}
}

func TestRecord_AutoFillsID(t *testing.T) {
	j, _ := Open(filepath.Join(t.TempDir(), "test.jsonl"))
	e := Entry{Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK}

	if err := j.Record(e); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	entries, _ := j.Query(Filter{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ID == "" {
		t.Error("ID was not auto-filled")
	}
}

func TestRecord_AutoFillsTimestamp(t *testing.T) {
	j, _ := Open(filepath.Join(t.TempDir(), "test.jsonl"))
	before := time.Now().UTC()
	e := Entry{Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK}

	if err := j.Record(e); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	after := time.Now().UTC()
	entries, _ := j.Query(Filter{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	ts := entries[0].Timestamp
	if ts.IsZero() {
		t.Error("Timestamp was not auto-filled")
	}
	if ts.Before(before) || ts.After(after.Add(time.Second)) {
		t.Errorf("Timestamp out of bounds: before=%v, ts=%v, after=%v", before, ts, after)
	}
}

func TestRecord_PreservesExplicitID(t *testing.T) {
	j, _ := Open(filepath.Join(t.TempDir(), "test.jsonl"))
	customID := "my-custom-id"
	e := Entry{ID: customID, Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK}

	if err := j.Record(e); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	entries, _ := j.Query(Filter{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ID != customID {
		t.Errorf("ID = %q, want %q", entries[0].ID, customID)
	}
}

func TestRecord_PreservesExplicitTimestamp(t *testing.T) {
	j, _ := Open(filepath.Join(t.TempDir(), "test.jsonl"))
	customTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	e := Entry{Timestamp: customTime, Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK}

	if err := j.Record(e); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	entries, _ := j.Query(Filter{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if !entries[0].Timestamp.Equal(customTime) {
		t.Errorf("Timestamp = %v, want %v", entries[0].Timestamp, customTime)
	}
}

func TestRecord_CreatesFileWithCorrectMode(t *testing.T) {
	jpath := filepath.Join(t.TempDir(), "test.jsonl")
	j, _ := Open(jpath)
	e := Entry{Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK}

	if err := j.Record(e); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	info, _ := os.Stat(jpath)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestRecord_AppendMultipleEntries(t *testing.T) {
	jpath := filepath.Join(t.TempDir(), "test.jsonl")
	j, _ := Open(jpath)

	entries := []Entry{
		{Op: OpCreate, Entity: EntityIssue, Title: "issue-1", Outcome: OutcomeOK},
		{Op: OpUpdate, Entity: EntityIssue, Title: "issue-2", Outcome: OutcomeOK},
		{Op: OpClose, Entity: EntityMR, Title: "mr-1", Outcome: OutcomeOK},
	}

	for _, e := range entries {
		if err := j.Record(e); err != nil {
			t.Fatalf("Record failed: %v", err)
		}
	}

	results, _ := j.Query(Filter{})
	if len(results) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(results))
	}
	if results[0].Title != "issue-1" || results[1].Title != "issue-2" || results[2].Title != "mr-1" {
		t.Error("entries not in expected order")
	}
}

func TestRecord_ValidJSON(t *testing.T) {
	jpath := filepath.Join(t.TempDir(), "test.jsonl")
	j, _ := Open(jpath)
	e := Entry{Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK, Tags: []string{"tag1", "tag2"}}

	if err := j.Record(e); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	file, _ := os.Open(jpath)
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		line := scanner.Text()
		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d not valid JSON: %v", lineCount, err)
		}
	}
	if lineCount != 1 {
		t.Errorf("expected 1 line, got %d", lineCount)
	}
}

func TestRecord_ConcurrentAppends(t *testing.T) {
	jpath := filepath.Join(t.TempDir(), "test.jsonl")
	j, _ := Open(jpath)

	const n = 100
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			e := Entry{
				Op:      OpCreate,
				Entity:  EntityIssue,
				Title:   fmt.Sprintf("issue-%d", idx),
				Outcome: OutcomeOK,
			}
			if err := j.Record(e); err != nil {
				t.Errorf("Record failed: %v", err)
			}
		}(i)
	}
	wg.Wait()

	file, _ := os.Open(jpath)
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0
	validCount := 0
	for scanner.Scan() {
		lineCount++
		line := scanner.Text()
		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Logf("malformed line %d: %v", lineCount, err)
			continue
		}
		validCount++
	}

	if lineCount != n {
		t.Errorf("file has %d lines, want %d", lineCount, n)
	}
	if validCount != n {
		t.Errorf("%d valid entries, want %d", validCount, n)
	}
}

func TestQuery_MissingFile(t *testing.T) {
	j, _ := Open(filepath.Join(t.TempDir(), "nonexistent.jsonl"))

	results, err := j.Query(Filter{})
	if err != nil {
		t.Fatalf("Query should not error on missing file: %v", err)
	}
	if results != nil {
		t.Errorf("Query returned %v, want nil", results)
	}
}

func TestQuery_EmptyFilter(t *testing.T) {
	jpath := filepath.Join(t.TempDir(), "test.jsonl")
	j, _ := Open(jpath)

	entries := []Entry{
		{Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Op: OpUpdate, Entity: EntityMR, Outcome: OutcomeOK},
		{Op: OpClose, Entity: EntityEpic, Outcome: OutcomeErr},
	}
	for _, e := range entries {
		j.Record(e)
	}

	results, err := j.Query(Filter{})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("empty filter returned %d entries, want 3", len(results))
	}
}

func TestQuery_FilterBySince(t *testing.T) {
	jpath := filepath.Join(t.TempDir(), "test.jsonl")
	j, _ := Open(jpath)

	t1 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	t3 := time.Date(2025, 1, 3, 12, 0, 0, 0, time.UTC)

	entries := []Entry{
		{Timestamp: t1, Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Timestamp: t2, Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Timestamp: t3, Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
	}
	for _, e := range entries {
		j.Record(e)
	}

	results, _ := j.Query(Filter{Since: t2})
	if len(results) != 2 {
		t.Errorf("filter Since=%v returned %d entries, want 2", t2, len(results))
	}
	if !results[0].Timestamp.Equal(t2) || !results[1].Timestamp.Equal(t3) {
		t.Error("filtered entries not in expected time range")
	}
}

func TestQuery_FilterByUntil(t *testing.T) {
	jpath := filepath.Join(t.TempDir(), "test.jsonl")
	j, _ := Open(jpath)

	t1 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	t3 := time.Date(2025, 1, 3, 12, 0, 0, 0, time.UTC)

	entries := []Entry{
		{Timestamp: t1, Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Timestamp: t2, Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Timestamp: t3, Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
	}
	for _, e := range entries {
		j.Record(e)
	}

	results, _ := j.Query(Filter{Until: t2})
	if len(results) != 2 {
		t.Errorf("filter Until=%v returned %d entries, want 2", t2, len(results))
	}
	if !results[0].Timestamp.Equal(t1) || !results[1].Timestamp.Equal(t2) {
		t.Error("filtered entries not in expected time range")
	}
}

func TestQuery_FilterByHost(t *testing.T) {
	jpath := filepath.Join(t.TempDir(), "test.jsonl")
	j, _ := Open(jpath)

	entries := []Entry{
		{Host: "host1", Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Host: "host2", Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Host: "host1", Op: OpCreate, Entity: EntityMR, Outcome: OutcomeOK},
	}
	for _, e := range entries {
		j.Record(e)
	}

	results, _ := j.Query(Filter{Host: "host1"})
	if len(results) != 2 {
		t.Errorf("filter Host=host1 returned %d entries, want 2", len(results))
	}
	for _, e := range results {
		if e.Host != "host1" {
			t.Errorf("unexpected host %q in filtered results", e.Host)
		}
	}
}

func TestQuery_FilterByProject(t *testing.T) {
	jpath := filepath.Join(t.TempDir(), "test.jsonl")
	j, _ := Open(jpath)

	entries := []Entry{
		{Project: "proj-a", Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Project: "proj-b", Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Project: "proj-a", Op: OpCreate, Entity: EntityMR, Outcome: OutcomeOK},
	}
	for _, e := range entries {
		j.Record(e)
	}

	results, _ := j.Query(Filter{Project: "proj-a"})
	if len(results) != 2 {
		t.Errorf("filter Project=proj-a returned %d entries, want 2", len(results))
	}
	for _, e := range results {
		if e.Project != "proj-a" {
			t.Errorf("unexpected project %q in filtered results", e.Project)
		}
	}
}

func TestQuery_FilterByGroup(t *testing.T) {
	jpath := filepath.Join(t.TempDir(), "test.jsonl")
	j, _ := Open(jpath)

	entries := []Entry{
		{Group: "group-x", Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Group: "group-y", Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Group: "group-x", Op: OpUpdate, Entity: EntityMR, Outcome: OutcomeOK},
	}
	for _, e := range entries {
		j.Record(e)
	}

	results, _ := j.Query(Filter{Group: "group-x"})
	if len(results) != 2 {
		t.Errorf("filter Group=group-x returned %d entries, want 2", len(results))
	}
	for _, e := range results {
		if e.Group != "group-x" {
			t.Errorf("unexpected group %q in filtered results", e.Group)
		}
	}
}

func TestQuery_FilterByOutcome(t *testing.T) {
	jpath := filepath.Join(t.TempDir(), "test.jsonl")
	j, _ := Open(jpath)

	entries := []Entry{
		{Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeErr},
		{Op: OpCreate, Entity: EntityMR, Outcome: OutcomeOK},
	}
	for _, e := range entries {
		j.Record(e)
	}

	results, _ := j.Query(Filter{Outcome: OutcomeErr})
	if len(results) != 1 {
		t.Errorf("filter Outcome=err returned %d entries, want 1", len(results))
	}
	if results[0].Outcome != OutcomeErr {
		t.Errorf("filtered entry has outcome %q, want err", results[0].Outcome)
	}
}

func TestQuery_FilterByOps_ORSemantics(t *testing.T) {
	jpath := filepath.Join(t.TempDir(), "test.jsonl")
	j, _ := Open(jpath)

	entries := []Entry{
		{Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Op: OpUpdate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Op: OpClose, Entity: EntityIssue, Outcome: OutcomeOK},
		{Op: OpReopen, Entity: EntityMR, Outcome: OutcomeOK},
	}
	for _, e := range entries {
		j.Record(e)
	}

	results, _ := j.Query(Filter{Ops: []Op{OpCreate, OpUpdate}})
	if len(results) != 2 {
		t.Errorf("filter Ops=[Create,Update] returned %d entries, want 2", len(results))
	}
	for _, e := range results {
		if e.Op != OpCreate && e.Op != OpUpdate {
			t.Errorf("unexpected op %q in filtered results", e.Op)
		}
	}
}

func TestQuery_FilterByEntities_ORSemantics(t *testing.T) {
	jpath := filepath.Join(t.TempDir(), "test.jsonl")
	j, _ := Open(jpath)

	entries := []Entry{
		{Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Op: OpCreate, Entity: EntityMR, Outcome: OutcomeOK},
		{Op: OpCreate, Entity: EntityEpic, Outcome: OutcomeOK},
		{Op: OpCreate, Entity: EntityLabel, Outcome: OutcomeOK},
	}
	for _, e := range entries {
		j.Record(e)
	}

	results, _ := j.Query(Filter{Entities: []Entity{EntityIssue, EntityMR}})
	if len(results) != 2 {
		t.Errorf("filter Entities=[Issue,MR] returned %d entries, want 2", len(results))
	}
	for _, e := range results {
		if e.Entity != EntityIssue && e.Entity != EntityMR {
			t.Errorf("unexpected entity %q in filtered results", e.Entity)
		}
	}
}

func TestQuery_MalformedLineSkipped(t *testing.T) {
	jpath := filepath.Join(t.TempDir(), "test.jsonl")

	file, _ := os.Create(jpath)
	fmt.Fprint(file, "{\"op\": \"create\", \"entity\": \"issue\", \"outcome\": \"ok\"}\n")
	fmt.Fprint(file, "this is not json\n")
	fmt.Fprint(file, "{\"op\": \"update\", \"entity\": \"mr\", \"outcome\": \"ok\"}\n")
	file.Close()

	j, _ := Open(jpath)
	results, err := j.Query(Filter{})
	if err != nil {
		t.Fatalf("Query should not error on malformed lines: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Query returned %d entries, want 2 (malformed line skipped)", len(results))
	}
}

func TestQuery_BlankLinesSkipped(t *testing.T) {
	jpath := filepath.Join(t.TempDir(), "test.jsonl")

	file, _ := os.Create(jpath)
	fmt.Fprint(file, "{\"op\": \"create\", \"entity\": \"issue\", \"outcome\": \"ok\"}\n")
	fmt.Fprint(file, "\n")
	fmt.Fprint(file, "   \n")
	fmt.Fprint(file, "{\"op\": \"update\", \"entity\": \"mr\", \"outcome\": \"ok\"}\n")
	file.Close()

	j, _ := Open(jpath)
	results, _ := j.Query(Filter{})
	if len(results) != 2 {
		t.Errorf("Query returned %d entries, want 2 (blank lines skipped)", len(results))
	}
}

func TestQuery_EmptyFile(t *testing.T) {
	jpath := filepath.Join(t.TempDir(), "test.jsonl")
	os.Create(jpath)

	j, _ := Open(jpath)
	results, err := j.Query(Filter{})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Query on empty file returned %d entries, want 0", len(results))
	}
}

func TestQuery_CombinedFilters(t *testing.T) {
	jpath := filepath.Join(t.TempDir(), "test.jsonl")
	j, _ := Open(jpath)

	t1 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	t3 := time.Date(2025, 1, 3, 12, 0, 0, 0, time.UTC)

	entries := []Entry{
		{Timestamp: t1, Host: "host-a", Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Timestamp: t2, Host: "host-b", Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Timestamp: t2, Host: "host-a", Op: OpUpdate, Entity: EntityMR, Outcome: OutcomeOK},
		{Timestamp: t3, Host: "host-a", Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeErr},
	}
	for _, e := range entries {
		j.Record(e)
	}

	results, _ := j.Query(Filter{
		Since:   t2,
		Host:    "host-a",
		Ops:     []Op{OpCreate},
		Outcome: OutcomeOK,
	})
	if len(results) != 0 {
		t.Errorf("combined filter returned %d entries, want 0", len(results))
	}

	results, _ = j.Query(Filter{
		Since:    t1,
		Until:    t3,
		Host:     "host-a",
		Entities: []Entity{EntityIssue},
	})
	if len(results) != 2 {
		t.Errorf("combined filter returned %d entries, want 2", len(results))
	}
}

func TestComputeStats_Empty(t *testing.T) {
	s := ComputeStats([]Entry{})
	if s.Total != 0 {
		t.Errorf("Total = %d, want 0", s.Total)
	}
	if s.Errors != 0 {
		t.Errorf("Errors = %d, want 0", s.Errors)
	}
	if s.ByHost == nil {
		t.Error("ByHost is nil, want initialized map")
	}
	if s.ByEntity == nil {
		t.Error("ByEntity is nil, want initialized map")
	}
	if s.ByOp == nil {
		t.Error("ByOp is nil, want initialized map")
	}
}

func TestComputeStats_Total(t *testing.T) {
	entries := []Entry{
		{Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Op: OpCreate, Entity: EntityMR, Outcome: OutcomeOK},
		{Op: OpUpdate, Entity: EntityIssue, Outcome: OutcomeErr},
	}
	s := ComputeStats(entries)
	if s.Total != 3 {
		t.Errorf("Total = %d, want 3", s.Total)
	}
}

func TestComputeStats_ByHost(t *testing.T) {
	entries := []Entry{
		{Host: "host1", Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Host: "host1", Op: OpCreate, Entity: EntityMR, Outcome: OutcomeOK},
		{Host: "host2", Op: OpUpdate, Entity: EntityIssue, Outcome: OutcomeOK},
	}
	s := ComputeStats(entries)
	if s.ByHost["host1"] != 2 {
		t.Errorf("ByHost[host1] = %d, want 2", s.ByHost["host1"])
	}
	if s.ByHost["host2"] != 1 {
		t.Errorf("ByHost[host2] = %d, want 1", s.ByHost["host2"])
	}
}

func TestComputeStats_ByEntity(t *testing.T) {
	entries := []Entry{
		{Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Op: OpCreate, Entity: EntityMR, Outcome: OutcomeOK},
		{Op: OpUpdate, Entity: EntityIssue, Outcome: OutcomeOK},
	}
	s := ComputeStats(entries)
	if s.ByEntity[EntityIssue] != 2 {
		t.Errorf("ByEntity[issue] = %d, want 2", s.ByEntity[EntityIssue])
	}
	if s.ByEntity[EntityMR] != 1 {
		t.Errorf("ByEntity[mr] = %d, want 1", s.ByEntity[EntityMR])
	}
}

func TestComputeStats_ByOp(t *testing.T) {
	entries := []Entry{
		{Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Op: OpCreate, Entity: EntityMR, Outcome: OutcomeOK},
		{Op: OpUpdate, Entity: EntityIssue, Outcome: OutcomeOK},
	}
	s := ComputeStats(entries)
	if s.ByOp[OpCreate] != 2 {
		t.Errorf("ByOp[create] = %d, want 2", s.ByOp[OpCreate])
	}
	if s.ByOp[OpUpdate] != 1 {
		t.Errorf("ByOp[update] = %d, want 1", s.ByOp[OpUpdate])
	}
}

func TestComputeStats_Errors(t *testing.T) {
	entries := []Entry{
		{Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Op: OpCreate, Entity: EntityMR, Outcome: OutcomeErr},
		{Op: OpUpdate, Entity: EntityIssue, Outcome: OutcomeErr},
	}
	s := ComputeStats(entries)
	if s.Errors != 2 {
		t.Errorf("Errors = %d, want 2", s.Errors)
	}
}

func TestComputeStats_AllFields(t *testing.T) {
	entries := []Entry{
		{Host: "host1", Op: OpCreate, Entity: EntityIssue, Outcome: OutcomeOK},
		{Host: "host1", Op: OpCreate, Entity: EntityMR, Outcome: OutcomeErr},
		{Host: "host2", Op: OpUpdate, Entity: EntityIssue, Outcome: OutcomeErr},
	}
	s := ComputeStats(entries)

	if s.Total != 3 {
		t.Errorf("Total = %d, want 3", s.Total)
	}
	if s.Errors != 2 {
		t.Errorf("Errors = %d, want 2", s.Errors)
	}
	if len(s.ByHost) != 2 {
		t.Errorf("ByHost has %d entries, want 2", len(s.ByHost))
	}
	if len(s.ByEntity) != 2 {
		t.Errorf("ByEntity has %d entries, want 2", len(s.ByEntity))
	}
	if len(s.ByOp) != 2 {
		t.Errorf("ByOp has %d entries, want 2", len(s.ByOp))
	}
}
