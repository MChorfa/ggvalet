// Package client_test provides httptest-based integration tests that verify
// client.Client wires correctly against the real go-gitlab code path.
// No mocks are used for the Client itself; every test drives a real client.New(cfg)
// pointed at an httptest.Server.
package client_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ckodex/gitlabvalet/internal/cache"
	"github.com/ckodex/gitlabvalet/internal/client"
	"github.com/ckodex/gitlabvalet/internal/config"
	"github.com/ckodex/gitlabvalet/internal/journal"
	"github.com/ckodex/gitlabvalet/internal/provider"
	gl "github.com/xanzy/go-gitlab"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// testCfg builds a minimal Config directed at the given base URL.
func testCfg(t *testing.T, baseURL, token string, skipTLS bool) *config.Config {
	t.Helper()
	dir := t.TempDir()
	return &config.Config{
		Host:        "test.gitlab.local",
		GitLabURL:   baseURL,
		Token:       token,
		SkipTLS:     skipTLS,
		JournalPath: filepath.Join(dir, "journal.jsonl"),
		CachePath:   filepath.Join(dir, "cache"),
	}
}

// issueJSON returns a minimal JSON array of issues suitable for go-gitlab parsing.
func issueJSON(ids ...int) []byte {
	type minimal struct {
		ID    int    `json:"id"`
		IID   int    `json:"iid"`
		Title string `json:"title"`
		State string `json:"state"`
	}
	items := make([]minimal, len(ids))
	for i, id := range ids {
		items[i] = minimal{ID: id, IID: id, Title: fmt.Sprintf("Issue %d", id), State: "opened"}
	}
	b, _ := json.Marshal(items)
	return b
}

// readJournalAll reads every entry from the journal at path.
func readJournalAll(t *testing.T, path string) []journal.Entry {
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

// ─── TestClient_New_GitHubOnly_NoGitLabClient ─────────────────────────────────

// TestClient_New_GitHubOnly_NoGitLabClient verifies that when the GitHub
// provider is selected, client.New skips the go-gitlab client construction
// entirely and returns a nil GL field, while still producing a working
// provider and journal/state stores.
func TestClient_New_GitHubOnly_NoGitLabClient(t *testing.T) {
	t.Setenv("GLVALET_PROVIDER", "github")
	t.Setenv("GLVALET_GITHUB_ENABLED", "true")

	dir := t.TempDir()
	cfg := &config.Config{
		Host:        "github.com",
		Token:       "ghp-test-token",
		Provider:    "github",
		GitHubToken: "ghp-test-token",
		JournalPath: filepath.Join(dir, "journal.jsonl"),
		CachePath:   filepath.Join(dir, "cache"),
		// Intentionally no GitLabURL — this is the GitHub-only startup case.
	}

	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	defer c.Close()

	if c.GL != nil {
		t.Errorf("GL = %v, expected nil for GitHub-only client", c.GL)
	}
	if c.Provider == nil {
		t.Fatal("Provider is nil; expected a live provider")
	}
	if got := c.Provider.Kind(); got != provider.KindGitHub {
		t.Errorf("Provider.Kind() = %q, expected %q", got, provider.KindGitHub)
	}
	if c.State == nil {
		t.Error("State is nil; expected a live state store")
	}
}

// ─── TestClient_ListIssues_RecordsJournalEntry ────────────────────────────────

// TestClient_ListIssues_RecordsJournalEntry verifies that:
//  1. The outbound request carries the PRIVATE-TOKEN header with the expected value.
//  2. A successful ListProjectIssues call followed by Rec writes one journal
//     entry with Outcome=ok, matching Op/Entity/Host/Project.
func TestClient_ListIssues_RecordsJournalEntry(t *testing.T) {
	t.Parallel()

	const token = "glpat-test-token-abc123"
	const project = "mygroup/myproject"

	var capturedToken string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedToken = r.Header.Get("PRIVATE-TOKEN")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(issueJSON(1, 2))
	}))
	defer srv.Close()

	cfg := testCfg(t, srv.URL, token, false)
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	issues, _, err := c.GL.Issues.ListProjectIssues(project, &gl.ListProjectIssuesOptions{})
	if err != nil {
		t.Fatalf("ListProjectIssues: %v", err)
	}

	// Assert PRIVATE-TOKEN header was received by the server.
	if capturedToken != token {
		t.Errorf("PRIVATE-TOKEN header = %q; want %q", capturedToken, token)
	}

	// Assert two issues were returned.
	if len(issues) != 2 {
		t.Errorf("len(issues) = %d; want 2", len(issues))
	}

	// Record a journal entry for the successful operation.
	c.Rec(journal.OpList, journal.EntityIssue, project, "", 0, 0, "list 2 issues", "")

	// Read the journal back and verify the entry.
	entries := readJournalAll(t, cfg.JournalPath)
	if len(entries) != 1 {
		t.Fatalf("journal entry count = %d; want 1", len(entries))
	}

	e := entries[0]
	if e.Host != cfg.Host {
		t.Errorf("entry.Host = %q; want %q", e.Host, cfg.Host)
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
	if e.Outcome != journal.OutcomeOK {
		t.Errorf("entry.Outcome = %q; want %q", e.Outcome, journal.OutcomeOK)
	}
}

// ─── TestClient_ListIssues_FailureRecordsError ────────────────────────────────

// TestClient_ListIssues_FailureRecordsError verifies that when the GitLab API
// returns HTTP 500, a RecErr call writes a journal entry with Outcome=err and a
// non-empty Detail field.
func TestClient_ListIssues_FailureRecordsError(t *testing.T) {
	t.Parallel()

	const token = "glpat-failure-token"
	const project = "failgroup/failproject"

	// go-gitlab wraps retryablehttp which retries 500 responses.
	// 403 Forbidden is not retried and produces an error immediately.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"403 Forbidden"}`, http.StatusForbidden)
	}))
	defer srv.Close()

	cfg := testCfg(t, srv.URL, token, false)
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	_, _, apiErr := c.GL.Issues.ListProjectIssues(project, &gl.ListProjectIssuesOptions{})
	if apiErr == nil {
		t.Fatal("expected an error from ListProjectIssues on HTTP 500; got nil")
	}

	// Mirror what production code does on failure.
	c.RecErr(journal.OpList, journal.EntityIssue, project, "", apiErr.Error())

	entries := readJournalAll(t, cfg.JournalPath)
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
	if e.Op != journal.OpList {
		t.Errorf("entry.Op = %q; want %q", e.Op, journal.OpList)
	}
	if e.Project != project {
		t.Errorf("entry.Project = %q; want %q", e.Project, project)
	}
}

// ─── TestClient_New_SkipTLS_VerifiesCert ─────────────────────────────────────

// TestClient_New_SkipTLS_VerifiesCert exercises the TLS path:
//   - SkipTLS=true  → request to a TLS server with self-signed cert succeeds.
//   - SkipTLS=false → request to the same server fails with a TLS error.
//
// Sub-tests are NOT marked parallel because they share the httptest.TLSServer
// and the server must remain open until both sub-tests complete.
func TestClient_New_SkipTLS_VerifiesCert(t *testing.T) {
	t.Parallel()

	const token = "glpat-tls-token"
	const project = "tlsgroup/tlsproject"

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "[]")
	}))
	t.Cleanup(srv.Close)

	t.Run("SkipTLS=true succeeds", func(t *testing.T) {
		// Not parallel — must run before parent defers close.
		cfg := testCfg(t, srv.URL, token, true /* skipTLS */)
		c, err := client.New(cfg)
		if err != nil {
			t.Fatalf("client.New: %v", err)
		}
		_, _, err = c.GL.Issues.ListProjectIssues(project, &gl.ListProjectIssuesOptions{})
		if err != nil {
			t.Errorf("expected success with SkipTLS=true; got %v", err)
		}
	})

	t.Run("SkipTLS=false fails", func(t *testing.T) {
		// Not parallel — must run before parent defers close.
		cfg := testCfg(t, srv.URL, token, false /* skipTLS */)
		c, err := client.New(cfg)
		if err != nil {
			t.Fatalf("client.New: %v", err)
		}
		_, _, err = c.GL.Issues.ListProjectIssues(project, &gl.ListProjectIssuesOptions{})
		if err == nil {
			t.Error("expected TLS error with SkipTLS=false against self-signed cert; got nil")
		}
	})
}

// ─── TestClient_CacheKey_StableAcrossInputs ───────────────────────────────────

// TestClient_CacheKey_StableAcrossInputs verifies the CacheKey determinism
// contract: same inputs → same key; different inputs → different key.
// This tests the client.CacheKey wrapper around cache.Key directly.
func TestClient_CacheKey_StableAcrossInputs(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "[]")
	}))
	defer srv.Close()

	cfg := testCfg(t, srv.URL, "glpat-cache-token", false)
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	type tc struct {
		name        string
		path1       string
		params1     string
		path2       string
		params2     string
		expectEqual bool
	}

	cases := []tc{
		{
			name:        "identical inputs produce identical key",
			path1:       "projects/foo/issues",
			params1:     "state=opened",
			path2:       "projects/foo/issues",
			params2:     "state=opened",
			expectEqual: true,
		},
		{
			name:        "different params produce different key",
			path1:       "projects/foo/issues",
			params1:     "state=opened",
			path2:       "projects/foo/issues",
			params2:     "state=closed",
			expectEqual: false,
		},
		{
			name:        "different paths produce different key",
			path1:       "projects/foo/issues",
			params1:     "",
			path2:       "projects/bar/issues",
			params2:     "",
			expectEqual: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			k1 := c.CacheKey(tc.path1, tc.params1)
			k2 := c.CacheKey(tc.path2, tc.params2)
			if tc.expectEqual && k1 != k2 {
				t.Errorf("expected equal keys; got %q and %q", k1, k2)
			}
			if !tc.expectEqual && k1 == k2 {
				t.Errorf("expected different keys; both returned %q", k1)
			}
		})
	}
}

// ─── TestClient_CacheKey_IncludesHost ─────────────────────────────────────────

// TestClient_CacheKey_IncludesHost verifies that the host component is baked
// into the cache key, so two clients pointed at different hosts produce
// different keys for the same path+params.
func TestClient_CacheKey_IncludesHost(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "[]")
	}))
	defer srv.Close()

	dirA := t.TempDir()
	cfgA := &config.Config{
		Host:        "host-alpha.local",
		GitLabURL:   srv.URL,
		Token:       "tok",
		JournalPath: filepath.Join(dirA, "j.jsonl"),
		CachePath:   filepath.Join(dirA, "cache"),
	}
	dirB := t.TempDir()
	cfgB := &config.Config{
		Host:        "host-beta.local",
		GitLabURL:   srv.URL,
		Token:       "tok",
		JournalPath: filepath.Join(dirB, "j.jsonl"),
		CachePath:   filepath.Join(dirB, "cache"),
	}

	cA, err := client.New(cfgA)
	if err != nil {
		t.Fatalf("client.New(cfgA): %v", err)
	}
	cB, err := client.New(cfgB)
	if err != nil {
		t.Fatalf("client.New(cfgB): %v", err)
	}

	const path, params = "projects/x/issues", "state=opened"
	kA := cA.CacheKey(path, params)
	kB := cB.CacheKey(path, params)

	if kA == kB {
		t.Errorf("expected different cache keys for different hosts; both = %q", kA)
	}
}

// ─── TestClient_Host_ReturnsConfigHost ───────────────────────────────────────

// TestClient_Host_ReturnsConfigHost verifies that Host() surfaces cfg.Host.
func TestClient_Host_ReturnsConfigHost(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testCfg(t, srv.URL, "tok", false)
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	if got := c.Host(); got != cfg.Host {
		t.Errorf("Host() = %q; want %q", got, cfg.Host)
	}
}

// ─── TestClient_CacheSetAndGet ────────────────────────────────────────────────

// TestClient_CacheSetAndGet verifies that the Cache field on a successfully
// constructed Client can store and retrieve values, confirming the cache
// initialisation path in client.New succeeded.
func TestClient_CacheSetAndGet(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testCfg(t, srv.URL, "tok", false)
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	if c.Cache == nil {
		t.Fatal("Cache is nil; expected a live *cache.Cache")
	}

	key := cache.Key(cfg.Host, "projects/x/issues", "state=opened")
	type payload struct{ Count int }

	// Miss before set.
	var dst payload
	if c.Cache.Unmarshal(key, &dst) {
		t.Error("Unmarshal returned true on an empty cache; expected false (miss)")
	}

	// Set then get.
	c.Cache.Set(key, payload{Count: 7}, 30_000_000_000 /* 30s */)
	if !c.Cache.Unmarshal(key, &dst) {
		t.Error("Unmarshal returned false after Set; expected true (hit)")
	}
	if dst.Count != 7 {
		t.Errorf("cached Count = %d; want 7", dst.Count)
	}
}

// ─── TestClient_New_InvalidURL_ReturnsError ───────────────────────────────────

// TestClient_New_InvalidURL_ReturnsError verifies client.New returns an error
// for a URL that go-gitlab cannot parse.
func TestClient_New_InvalidURL_ReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := &config.Config{
		Host:        "bad",
		GitLabURL:   "://not-a-url",
		Token:       "tok",
		JournalPath: filepath.Join(dir, "j.jsonl"),
		CachePath:   filepath.Join(dir, "cache"),
	}

	_, err := client.New(cfg)
	if err == nil {
		t.Error("expected error from client.New with invalid URL; got nil")
	}
}

// ─── TestClient_JournalQuery_FilterByHost ─────────────────────────────────────

// TestClient_JournalQuery_FilterByHost verifies that journal entries are
// filterable by host — records from two distinct clients sharing a journal
// file are correctly separated by the Filter.Host predicate.
func TestClient_JournalQuery_FilterByHost(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(issueJSON(10))
	}))
	defer srv.Close()

	dir := t.TempDir()
	sharedJournal := filepath.Join(dir, "journal.jsonl")

	cfgA := &config.Config{
		Host:        "host-a.local",
		GitLabURL:   srv.URL,
		Token:       "tokA",
		JournalPath: sharedJournal,
		CachePath:   filepath.Join(dir, "cacheA"),
	}
	cfgB := &config.Config{
		Host:        "host-b.local",
		GitLabURL:   srv.URL,
		Token:       "tokB",
		JournalPath: sharedJournal,
		CachePath:   filepath.Join(dir, "cacheB"),
	}

	cA, err := client.New(cfgA)
	if err != nil {
		t.Fatalf("client.New(cfgA): %v", err)
	}
	cB, err := client.New(cfgB)
	if err != nil {
		t.Fatalf("client.New(cfgB): %v", err)
	}

	cA.Rec(journal.OpList, journal.EntityIssue, "proj-a", "", 0, 0, "list 1 issue", "")
	cB.Rec(journal.OpList, journal.EntityMR, "proj-b", "", 0, 0, "list 1 mr", "")

	j, err := journal.Open(sharedJournal)
	if err != nil {
		t.Fatalf("journal.Open: %v", err)
	}

	aEntries, err := j.Query(journal.Filter{Host: "host-a.local"})
	if err != nil {
		t.Fatalf("Query host-a: %v", err)
	}
	bEntries, err := j.Query(journal.Filter{Host: "host-b.local"})
	if err != nil {
		t.Fatalf("Query host-b: %v", err)
	}

	if len(aEntries) != 1 {
		t.Errorf("host-a entries = %d; want 1", len(aEntries))
	}
	if len(bEntries) != 1 {
		t.Errorf("host-b entries = %d; want 1", len(bEntries))
	}
	if len(aEntries) > 0 && aEntries[0].Entity != journal.EntityIssue {
		t.Errorf("host-a entity = %q; want %q", aEntries[0].Entity, journal.EntityIssue)
	}
	if len(bEntries) > 0 && bEntries[0].Entity != journal.EntityMR {
		t.Errorf("host-b entity = %q; want %q", bEntries[0].Entity, journal.EntityMR)
	}
}
