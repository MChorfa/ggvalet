package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestLastRotated_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.LastRotated(ctx, "gitlab.example.com"); err != nil || ok {
		t.Fatalf("expected no record; ok=%v err=%v", ok, err)
	}
	want := time.Now().UTC().Truncate(time.Second)
	if err := s.SetLastRotated(ctx, "gitlab.example.com", want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.LastRotated(ctx, "gitlab.example.com")
	if err != nil || !ok {
		t.Fatalf("expected record; ok=%v err=%v", ok, err)
	}
	if !got.Equal(want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestLastRotated_CorruptTimestamp(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO rotation_state (host, last_rotated_at) VALUES (?, ?)`,
		"gitlab.example.com", "not-a-timestamp"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.LastRotated(ctx, "gitlab.example.com"); err == nil || ok {
		t.Fatalf("expected parse error; ok=%v err=%v", ok, err)
	}
}
