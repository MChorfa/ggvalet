package cmd

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/MChorfa/ggvalet/internal/client"
	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/journal"
)

// setupTestClient builds a real Client wired to an httptest GitLab server and
// installs the real GitLab provider on the global cmd variables. This replaces
// the previous cmdTestProvider stub with an actual provider.Provider call path.
func setupTestClient(t *testing.T) *client.Client {
	t.Helper()
	dir := t.TempDir()
	srv := newGitLabTestServer(t)

	testCfg := &config.Config{
		Host:           "gitlab.example.com",
		GitLabURL:      srv.URL,
		Token:          "test-token",
		User:           "testuser",
		JournalPath:    filepath.Join(dir, "journal.jsonl"),
		CachePath:      filepath.Join(dir, "cache"),
		StatePath:      filepath.Join(dir, "state.db"),
		DefaultProject: "group/project",
		DefaultGroup:   "group",
		Hosts: map[string]*config.HostConfig{
			"gitlab.example.com": {
				Token:       "test-token",
				APIProtocol: "http",
				APIHost:     srv.URL,
			},
		},
	}
	t.Setenv("GLVALET_PROVIDER", "gitlab")
	t.Setenv("GLVALET_CACHE", testCfg.CachePath)
	t.Setenv("GLVALET_STATE", testCfg.StatePath)

	origClient := glClient
	origCfg := cfg

	c, err := client.New(testCfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	glClient = c
	cfg = testCfg

	t.Cleanup(func() {
		if glClient != nil {
			glClient.Close()
		}
		glClient = origClient
		cfg = origCfg
	})

	return c
}

// setupTestClientWithServer is an alias for setupTestClient; every cmd test now
// runs against an httptest-backed provider.
func setupTestClientWithServer(t *testing.T) *client.Client {
	t.Helper()
	return setupTestClient(t)
}

func addJournalEntry(t *testing.T, op journal.Op, entity journal.Entity, title string) {
	t.Helper()
	if err := glClient.Rec(op, entity, "group/project", "", 0, 0, title, "https://example.com/t"); err != nil {
		t.Fatalf("record journal: %v", err)
	}
}

func addOldJournalEntry(t *testing.T) {
	t.Helper()
	if err := glClient.Journal.Record(journal.Entry{
		ID:        "old",
		Timestamp: time.Now().UTC().Add(-30 * 24 * time.Hour),
		Host:      "gitlab.example.com",
		Op:        journal.OpList,
		Entity:    journal.EntityIssue,
		Project:   "group/project",
		Title:     "old entry",
		URL:       "https://example.com/old",
		Outcome:   journal.OutcomeOK,
	}); err != nil {
		t.Fatalf("record old journal: %v", err)
	}
}
