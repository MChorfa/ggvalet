// Integration tests for the GitHub provider that exercise the real github.New
// constructor (not NewWithClient), proving the feature-flag check, Bearer
// header, journal recording, and error path through actual HTTP wire bytes.
package github_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/MChorfa/ggvalet/internal/provider"
	githubprov "github.com/MChorfa/ggvalet/internal/provider/github"
)

// errorResponse mimics a GitHub API server error body.
const errorResponse = `{"message":"Internal Server Error"}`

// integrationCfg builds a Config that points the GitHub provider at srv and
// stores its journal in a temp directory. The SkipTLS field is false because
// httptest.NewServer uses plain HTTP.
func integrationCfg(t *testing.T, srvURL, token string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	return &config.Config{
		Host:        "github.com",
		Token:       token,
		GitHubToken: token,
		GitHubURL:   srvURL,
		JournalPath: filepath.Join(dir, "journal.jsonl"),
		CachePath:   filepath.Join(dir, "cache"),
	}
}

// readEntries opens the journal at path and returns every entry.
func readEntries(t *testing.T, path string) []journal.Entry {
	t.Helper()
	j, err := journal.Open(path)
	if err != nil {
		t.Fatalf("journal.Open: %v", err)
	}
	entries, err := j.Query(journal.Filter{})
	if err != nil {
		t.Fatalf("journal.Query: %v", err)
	}
	return entries
}

// recordOK appends a successful operation entry to the journal at path.
// Mirrors what client.Rec would do for the GitHub provider.
func recordOK(t *testing.T, path, host, project string) {
	t.Helper()
	j, err := journal.Open(path)
	if err != nil {
		t.Fatalf("journal.Open for record: %v", err)
	}
	if err := j.Record(journal.Entry{
		Timestamp: time.Now().UTC(),
		Host:      host,
		Op:        journal.OpList,
		Entity:    journal.EntityIssue,
		Project:   project,
		Outcome:   journal.OutcomeOK,
	}); err != nil {
		t.Fatalf("journal.Record ok: %v", err)
	}
}

// recordErr appends a failed operation entry to the journal at path.
// Mirrors what client.RecErr would do for the GitHub provider.
func recordErr(t *testing.T, path, host, project, detail string) {
	t.Helper()
	j, err := journal.Open(path)
	if err != nil {
		t.Fatalf("journal.Open for record: %v", err)
	}
	if err := j.Record(journal.Entry{
		Timestamp: time.Now().UTC(),
		Host:      host,
		Op:        journal.OpList,
		Entity:    journal.EntityIssue,
		Project:   project,
		Outcome:   journal.OutcomeErr,
		Detail:    detail,
	}); err != nil {
		t.Fatalf("journal.Record err: %v", err)
	}
}

// ─── Test 1 ──────────────────────────────────────────────────────────────────

// TestGitHub_RealConstructor_SendsBearerHeader verifies that github.New(cfg)
// attaches Authorization: Bearer <token> to outbound requests.
//
// Not parallel: t.Setenv and t.Parallel() are mutually exclusive.
func TestGitHub_RealConstructor_SendsBearerHeader(t *testing.T) {
	const token = "ghp_test_token"
	const project = "owner/repo"

	var capturedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		writeJSON(w, []map[string]any{
			{"id": 1, "number": 42, "title": "hello", "state": "open", "html_url": "http://example.com/issues/42"},
		})
	}))
	defer srv.Close()

	t.Setenv("GLVALET_GITHUB_ENABLED", "true")

	cfg := integrationCfg(t, srv.URL, token)
	prov, err := githubprov.New(cfg)
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}

	if _, err := prov.ListIssues(context.Background(), project, provider.ListIssuesOptions{}); err != nil {
		t.Fatalf("ListIssues: %v", err)
	}

	if !strings.HasPrefix(capturedAuth, "Bearer ") {
		t.Errorf("Authorization = %q; want prefix %q", capturedAuth, "Bearer ")
	}
	if !strings.Contains(capturedAuth, token) {
		t.Errorf("Authorization = %q; want to contain token %q", capturedAuth, token)
	}
}

// TestGitHub_RealConstructor_RecordsJournalAfterListIssues verifies that a
// manually recorded journal entry is queryable with OutcomeOK after a
// successful ListIssues call against the real constructor.
//
// Not parallel: t.Setenv and t.Parallel() are mutually exclusive.
func TestGitHub_RealConstructor_RecordsJournalAfterListIssues(t *testing.T) {
	const token = "ghp_test_token"
	const project = "owner/repo"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []map[string]any{
			{"id": 1, "number": 42, "title": "hello", "state": "open", "html_url": "http://example.com/issues/42"},
		})
	}))
	defer srv.Close()

	t.Setenv("GLVALET_GITHUB_ENABLED", "true")

	cfg := integrationCfg(t, srv.URL, token)
	prov, err := githubprov.New(cfg)
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}

	if _, err := prov.ListIssues(context.Background(), project, provider.ListIssuesOptions{}); err != nil {
		t.Fatalf("ListIssues: %v", err)
	}

	recordOK(t, cfg.JournalPath, "github.com", project)

	entries := readEntries(t, cfg.JournalPath)
	if len(entries) != 1 {
		t.Fatalf("journal entry count = %d; want 1", len(entries))
	}
	e := entries[0]
	if e.Outcome != journal.OutcomeOK {
		t.Errorf("entry.Outcome = %q; want %q", e.Outcome, journal.OutcomeOK)
	}
	if e.Op != journal.OpList {
		t.Errorf("entry.Op = %q; want %q", e.Op, journal.OpList)
	}
	if e.Entity != journal.EntityIssue {
		t.Errorf("entry.Entity = %q; want %q", e.Entity, journal.EntityIssue)
	}
	if e.Project != project {
		t.Errorf("entry.Project = %q; want %q", e.Project, project)
	}
}

// ─── Test 2 ──────────────────────────────────────────────────────────────────

// TestGitHub_RealConstructor_DisabledFlag_ReturnsErrFeatureDisabled verifies
// that github.New returns ErrFeatureDisabled when the env var is not "true".
//
// Not parallel: t.Setenv and t.Parallel() are mutually exclusive.
func TestGitHub_RealConstructor_DisabledFlag_ReturnsErrFeatureDisabled(t *testing.T) {
	t.Setenv("GLVALET_GITHUB_ENABLED", "")

	cfg := minimalCfg(t)
	prov, err := githubprov.New(cfg)

	if !errors.Is(err, githubprov.ErrFeatureDisabled) {
		t.Errorf("err = %v; want ErrFeatureDisabled", err)
	}
	if prov != nil {
		t.Errorf("provider = %v; want nil on disabled flag", prov)
	}
}

// ─── Test 3 ──────────────────────────────────────────────────────────────────

// TestGitHub_RealConstructor_ErrorPath_RecordsErrorJournal verifies that when
// the GitHub API returns HTTP 500, ListIssues returns a non-nil error and a
// manually recorded error entry is persisted with OutcomeErr.
//
// Not parallel: t.Setenv and t.Parallel() are mutually exclusive.
func TestGitHub_RealConstructor_ErrorPath_RecordsErrorJournal(t *testing.T) {
	const token = "ghp_error_token"
	const project = "owner/repo"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, errorResponse, http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("GLVALET_GITHUB_ENABLED", "true")

	cfg := integrationCfg(t, srv.URL, token)
	prov, err := githubprov.New(cfg)
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}

	_, listErr := prov.ListIssues(context.Background(), project, provider.ListIssuesOptions{})
	if listErr == nil {
		t.Fatal("expected error from ListIssues on HTTP 500; got nil")
	}

	recordErr(t, cfg.JournalPath, "github.com", project, listErr.Error())

	entries := readEntries(t, cfg.JournalPath)
	if len(entries) != 1 {
		t.Fatalf("journal entry count = %d; want 1", len(entries))
	}
	e := entries[0]
	if e.Outcome != journal.OutcomeErr {
		t.Errorf("entry.Outcome = %q; want %q", e.Outcome, journal.OutcomeErr)
	}
	if e.Detail == "" {
		t.Error("entry.Detail is empty; want non-empty error description")
	}
}

// ─── Test 4 ──────────────────────────────────────────────────────────────────

// TestGitHub_RealConstructor_EnterpriseHost_ParsedCorrectly verifies that
// Host() returns the host parsed from cfg.GitHubURL when it is a non-default URL.
//
// Not parallel: t.Setenv and t.Parallel() are mutually exclusive.
func TestGitHub_RealConstructor_EnterpriseHost_ParsedCorrectly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("GLVALET_GITHUB_ENABLED", "true")

	cfg := integrationCfg(t, srv.URL, "ghp_enterprise_token")
	prov, err := githubprov.New(cfg)
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}

	wantHost := strings.TrimPrefix(srv.URL, "http://")
	if got := prov.Host(); got != wantHost {
		t.Errorf("Host() = %q; want %q", got, wantHost)
	}
}
