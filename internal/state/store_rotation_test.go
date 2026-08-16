package state

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLastRotated_RoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.LastRotated("gitlab.example.com"); err != nil || ok {
		t.Fatalf("expected no record; ok=%v err=%v", ok, err)
	}
	want := time.Now().UTC().Truncate(time.Second)
	if err := s.SetLastRotated("gitlab.example.com", want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.LastRotated("gitlab.example.com")
	if err != nil || !ok {
		t.Fatalf("expected record; ok=%v err=%v", ok, err)
	}
	if !got.Equal(want) {
		t.Errorf("got %v want %v", got, want)
	}
}
