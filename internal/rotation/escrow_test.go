package rotation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteEscrow_PermissionsAndRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "escrow")
	e := &Escrow{
		Version: 1, Host: "gitlab.example.com", State: StateRotated,
		OldTokenID: 51629, NewTokenID: 51630, NewToken: "glpat-secret",
		ExpiresAt: "2026-10-20", Scopes: []string{"api", "self_rotate"},
		ConfigPath: "/tmp/config.yml", ConfigSHA256: "abc123",
	}
	path, err := WriteEscrow(dir, e)
	if err != nil {
		t.Fatalf("WriteEscrow: %v", err)
	}

	dfi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := dfi.Mode().Perm(); got != 0o700 {
		t.Errorf("dir mode = %o, want 700", got)
	}
	ffi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := ffi.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want 600", got)
	}

	got, err := LoadEscrow(path)
	if err != nil {
		t.Fatalf("LoadEscrow: %v", err)
	}
	if got.NewToken != "glpat-secret" || got.OldTokenID != 51629 || got.State != StateRotated {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func TestLoadEscrow_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	badFile := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badFile, []byte("not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadEscrow(badFile)
	if err == nil {
		t.Errorf("expected error for invalid JSON, got nil")
	}
}

func TestLoadEscrow_NotFound(t *testing.T) {
	_, err := LoadEscrow("/nonexistent/path/file.json")
	if err == nil {
		t.Errorf("expected error for nonexistent file, got nil")
	}
}

func TestLoadEscrow_SetsPath(t *testing.T) {
	dir := t.TempDir()
	e := &Escrow{
		Version: 1, Host: "test.com", State: StateVerified,
		OldTokenID: 1, NewTokenID: 2, NewToken: "token",
		ExpiresAt: "2026-12-31",
	}
	path, err := WriteEscrow(dir, e)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadEscrow(path)
	if err != nil {
		t.Fatalf("LoadEscrow: %v", err)
	}
	if loaded.Path != path {
		t.Errorf("loaded.Path = %q, want %q", loaded.Path, path)
	}
}

func TestListEscrows_Empty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent")
	list, err := ListEscrows(dir)
	if err != nil {
		t.Fatalf("ListEscrows: %v", err)
	}
	if list != nil {
		t.Errorf("expected nil for nonexistent dir, got %v", list)
	}
}

func TestListEscrows_Multiple(t *testing.T) {
	dir := t.TempDir()

	e1 := &Escrow{
		Version: 1, Host: "host1.com", State: StateRotated,
		OldTokenID: 1, NewTokenID: 2, NewToken: "token1",
		ExpiresAt: "2026-12-31",
	}
	e2 := &Escrow{
		Version: 1, Host: "host2.com", State: StateVerified,
		OldTokenID: 3, NewTokenID: 4, NewToken: "token2",
		ExpiresAt: "2026-12-31",
	}

	_, err := WriteEscrow(dir, e1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = WriteEscrow(dir, e2)
	if err != nil {
		t.Fatal(err)
	}

	list, err := ListEscrows(dir)
	if err != nil {
		t.Fatalf("ListEscrows: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 escrows, got %d", len(list))
	}
	if list[0].Host != "host1.com" || list[1].Host != "host2.com" {
		t.Errorf("unexpected escrow order or content")
	}
	for _, e := range list {
		if e.Path == "" {
			t.Errorf("ListEscrows should set Path field")
		}
	}
}

func TestListEscrows_IgnoresNonJSON(t *testing.T) {
	dir := t.TempDir()

	// Write an escrow
	e := &Escrow{
		Version: 1, Host: "test.com", State: StateRotated,
		OldTokenID: 1, NewTokenID: 2, NewToken: "token",
		ExpiresAt: "2026-12-31",
	}
	_, err := WriteEscrow(dir, e)
	if err != nil {
		t.Fatal(err)
	}

	// Write a non-JSON file
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a subdirectory
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	list, err := ListEscrows(dir)
	if err != nil {
		t.Fatalf("ListEscrows: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 escrow (non-JSON and dirs ignored), got %d", len(list))
	}
}

func TestDeleteEscrow(t *testing.T) {
	dir := t.TempDir()
	e := &Escrow{
		Version: 1, Host: "test.com", State: StateRotated,
		OldTokenID: 1, NewTokenID: 2, NewToken: "token",
		ExpiresAt: "2026-12-31",
	}
	path, err := WriteEscrow(dir, e)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist after WriteEscrow: %v", err)
	}

	if err := DeleteEscrow(path); err != nil {
		t.Fatalf("DeleteEscrow: %v", err)
	}

	if _, err := os.Stat(path); err == nil {
		t.Errorf("file should not exist after DeleteEscrow")
	}
}

func TestDeleteEscrow_NotFound(t *testing.T) {
	err := DeleteEscrow("/nonexistent/path/file.json")
	if err == nil {
		t.Errorf("expected error for nonexistent file, got nil")
	}
}

func TestWriteEscrow_SetsRotatedAtIfEmpty(t *testing.T) {
	dir := t.TempDir()
	e := &Escrow{
		Version: 1, Host: "test.com", State: StateRotated,
		OldTokenID: 1, NewTokenID: 2, NewToken: "token",
		ExpiresAt: "2026-12-31",
		// RotatedAt is empty
	}

	path, err := WriteEscrow(dir, e)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadEscrow(path)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.RotatedAt == "" {
		t.Errorf("RotatedAt should be set by WriteEscrow")
	}
}

func TestWriteEscrow_PreservesRotatedAtIfSet(t *testing.T) {
	dir := t.TempDir()
	timestamp := "2026-08-15T10:00:00Z"
	e := &Escrow{
		Version: 1, Host: "test.com", State: StateRotated,
		OldTokenID: 1, NewTokenID: 2, NewToken: "token",
		ExpiresAt: "2026-12-31", RotatedAt: timestamp,
	}

	path, err := WriteEscrow(dir, e)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadEscrow(path)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.RotatedAt != timestamp {
		t.Errorf("RotatedAt = %q, want %q", loaded.RotatedAt, timestamp)
	}
}

func TestWriteEscrow_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	e := &Escrow{
		Version: 1, Host: "test.com", State: StateRotated,
		OldTokenID: 1, NewTokenID: 2, NewToken: "token",
		ExpiresAt: "2026-12-31", Scopes: []string{"api"},
		ConfigPath: "/etc/config", ConfigSHA256: "abc123",
	}

	path, err := WriteEscrow(dir, e)
	if err != nil {
		t.Fatal(err)
	}

	// Read raw JSON and verify it's valid and indented
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("JSON should be valid: %v", err)
	}

	// Verify it's indented (contains newlines)
	if string(b)[0:2] != "{\n" {
		t.Errorf("JSON should be indented with 2-space indent")
	}
}
