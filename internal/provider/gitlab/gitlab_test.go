package gitlab_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/provider"
	gitlabprov "github.com/MChorfa/ggvalet/internal/provider/gitlab"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// newTestProvider spins up handler behind an httptest.Server and returns a
// *GitLab provider pointed at it. The server is closed via t.Cleanup.
func newTestProvider(t *testing.T, handler http.Handler) *gitlabprov.GitLab {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	cfg := &config.Config{
		Host:        "test.gitlab.local",
		GitLabURL:   srv.URL,
		Token:       "glpat-test-token",
		SkipTLS:     false,
		JournalPath: filepath.Join(dir, "journal.jsonl"),
		CachePath:   filepath.Join(dir, "cache"),
	}

	prov, err := gitlabprov.New(cfg)
	if err != nil {
		t.Fatalf("gitlab.New: %v", err)
	}
	return prov
}

// writeJSON writes v as JSON with HTTP 200.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}

// containsString reports whether target appears in ss.
func containsString(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}

// ─── Kind ─────────────────────────────────────────────────────────────────────

func TestGitLab_Kind(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	if got := prov.Kind(); got != provider.KindGitLab {
		t.Errorf("Kind() = %q; want %q", got, provider.KindGitLab)
	}
}

// ─── Host ─────────────────────────────────────────────────────────────────────

// TestGitLab_Host_ParsesFromURL verifies that Host() returns the bare hostname
// when the config URL includes a scheme and path.
func TestGitLab_Host_ParsesFromURL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := &config.Config{
		Host:        "gitlab.example.com",
		GitLabURL:   "https://gitlab.example.com/api/v4",
		Token:       "tok",
		JournalPath: filepath.Join(dir, "j.jsonl"),
		CachePath:   filepath.Join(dir, "cache"),
	}

	prov, err := gitlabprov.New(cfg)
	if err != nil {
		t.Fatalf("gitlab.New: %v", err)
	}

	if got := prov.Host(); got != "gitlab.example.com" {
		t.Errorf("Host() = %q; want %q", got, "gitlab.example.com")
	}
}

// ─── ListIssues ───────────────────────────────────────────────────────────────

func TestGitLab_ListIssues_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []map[string]any{
			{
				"id":      1,
				"iid":     42,
				"title":   "X",
				"state":   "opened",
				"web_url": "https://gitlab.example.com/proj/-/issues/42",
				"labels":  []string{"bug"},
			},
		})
	}))

	issues, err := prov.ListIssues(context.Background(), "mygroup/myproject", provider.ListIssuesOptions{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d; want 1", len(issues))
	}
	if issues[0].IID != 42 {
		t.Errorf("issues[0].IID = %d; want 42", issues[0].IID)
	}
	if !containsString(issues[0].Labels, "bug") {
		t.Errorf("issues[0].Labels = %v; want to contain %q", issues[0].Labels, "bug")
	}
}

func TestGitLab_ListIssues_SendsPrivateToken(t *testing.T) {
	t.Parallel()

	const wantToken = "glpat-test-token"
	var capturedToken string

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedToken = r.Header.Get("PRIVATE-TOKEN")
		writeJSON(w, []map[string]any{})
	}))

	_, err := prov.ListIssues(context.Background(), "proj", provider.ListIssuesOptions{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if capturedToken != wantToken {
		t.Errorf("PRIVATE-TOKEN = %q; want %q", capturedToken, wantToken)
	}
}

func TestGitLab_ListIssues_ReturnsErrorOn403(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"403 Forbidden"}`, http.StatusForbidden)
	}))

	_, err := prov.ListIssues(context.Background(), "proj", provider.ListIssuesOptions{})
	if err == nil {
		t.Fatal("expected error from ListIssues on HTTP 403; got nil")
	}
}

// ─── GetIssue ─────────────────────────────────────────────────────────────────

func TestGitLab_GetIssue_DecodesSingle(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"id":      10,
			"iid":     7,
			"title":   "Single issue",
			"state":   "opened",
			"web_url": "https://gitlab.example.com/proj/-/issues/7",
			"labels":  []string{},
		})
	}))

	iss, err := prov.GetIssue(context.Background(), "mygroup/myproject", 7)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if iss.IID != 7 {
		t.Errorf("iss.IID = %d; want 7", iss.IID)
	}
	if iss.Title != "Single issue" {
		t.Errorf("iss.Title = %q; want %q", iss.Title, "Single issue")
	}
}

// ─── CreateIssue ──────────────────────────────────────────────────────────────

// TestGitLab_CreateIssue_SendsTitleAndLabels verifies the POST body includes
// the title field and encodes labels as a comma-separated string (go-gitlab
// LabelOptions.MarshalJSON behaviour in v0.115.0).
func TestGitLab_CreateIssue_SendsTitleAndLabels(t *testing.T) {
	t.Parallel()

	var capturedBody string

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		writeJSON(w, map[string]any{
			"id":      20,
			"iid":     1,
			"title":   "New issue",
			"state":   "opened",
			"web_url": "https://gitlab.example.com/proj/-/issues/1",
			"labels":  []string{"bug", "critical"},
		})
	}))

	_, err := prov.CreateIssue(context.Background(), "mygroup/myproject", provider.CreateIssueOptions{
		Title:  "New issue",
		Labels: []string{"bug", "critical"},
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if !strings.Contains(capturedBody, `"title"`) {
		t.Errorf("request body missing %q; got: %s", "title", capturedBody)
	}
	// LabelOptions.MarshalJSON encodes as a comma-separated JSON string.
	if !strings.Contains(capturedBody, "bug,critical") {
		t.Errorf("request body missing comma-separated labels; got: %s", capturedBody)
	}
}

// ─── ListMergeRequests ────────────────────────────────────────────────────────

// TestGitLab_ListMergeRequests_DecodesResponse verifies that source_branch and
// target_branch round-trip through the provider adapter.
func TestGitLab_ListMergeRequests_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []map[string]any{
			{
				"id":            30,
				"iid":           5,
				"title":         "Add feature",
				"state":         "opened",
				"source_branch": "feature/foo",
				"target_branch": "main",
				"web_url":       "https://gitlab.example.com/proj/-/merge_requests/5",
				"labels":        []string{},
			},
		})
	}))

	mrs, err := prov.ListMergeRequests(context.Background(), "mygroup/myproject", provider.ListMergeRequestsOptions{})
	if err != nil {
		t.Fatalf("ListMergeRequests: %v", err)
	}
	if len(mrs) != 1 {
		t.Fatalf("len(mrs) = %d; want 1", len(mrs))
	}
	if mrs[0].IID != 5 {
		t.Errorf("mrs[0].IID = %d; want 5", mrs[0].IID)
	}
	if mrs[0].SourceBranch != "feature/foo" {
		t.Errorf("SourceBranch = %q; want %q", mrs[0].SourceBranch, "feature/foo")
	}
	if mrs[0].TargetBranch != "main" {
		t.Errorf("TargetBranch = %q; want %q", mrs[0].TargetBranch, "main")
	}
}

// ─── GetMergeRequest ──────────────────────────────────────────────────────────

// TestGitLab_GetMergeRequest_DecodesSingle verifies GetMergeRequest decodes the
// response including assignees and reviewers (exercises toUser).
func TestGitLab_GetMergeRequest_DecodesSingle(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"id":            40,
			"iid":           3,
			"title":         "Fix the bug",
			"state":         "opened",
			"source_branch": "fix/bug",
			"target_branch": "main",
			"web_url":       "https://gitlab.example.com/proj/-/merge_requests/3",
			"labels":        []string{"bug"},
			"author":        map[string]any{"id": 1, "username": "alice", "name": "Alice", "web_url": "https://gitlab.example.com/alice"},
			"assignees":     []map[string]any{{"id": 2, "username": "bob", "name": "Bob", "web_url": "https://gitlab.example.com/bob"}},
			"reviewers":     []map[string]any{{"id": 3, "username": "carol", "name": "Carol", "web_url": "https://gitlab.example.com/carol"}},
		})
	}))

	mr, err := prov.GetMergeRequest(context.Background(), "mygroup/myproject", 3)
	if err != nil {
		t.Fatalf("GetMergeRequest: %v", err)
	}
	if mr.IID != 3 {
		t.Errorf("mr.IID = %d; want 3", mr.IID)
	}
	if mr.Title != "Fix the bug" {
		t.Errorf("mr.Title = %q; want %q", mr.Title, "Fix the bug")
	}
	if mr.Author.Username != "alice" {
		t.Errorf("mr.Author.Username = %q; want %q", mr.Author.Username, "alice")
	}
	if len(mr.Assignees) != 1 || mr.Assignees[0].Username != "bob" {
		t.Errorf("mr.Assignees = %v; want [{bob}]", mr.Assignees)
	}
	if len(mr.Reviewers) != 1 || mr.Reviewers[0].Username != "carol" {
		t.Errorf("mr.Reviewers = %v; want [{carol}]", mr.Reviewers)
	}
	if !containsString(mr.Labels, "bug") {
		t.Errorf("mr.Labels = %v; want to contain %q", mr.Labels, "bug")
	}
}

// TestGitLab_GetMergeRequest_ReturnsErrorOn403 verifies error propagation.
func TestGitLab_GetMergeRequest_ReturnsErrorOn403(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"403 Forbidden"}`, http.StatusForbidden)
	}))

	_, err := prov.GetMergeRequest(context.Background(), "proj", 1)
	if err == nil {
		t.Fatal("expected error from GetMergeRequest on HTTP 403; got nil")
	}
}

// ─── ListIssues with assignees (exercises toUser path in toIssue) ─────────────

// TestGitLab_ListIssues_DecodesAssignees verifies assignee fields round-trip.
func TestGitLab_ListIssues_DecodesAssignees(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []map[string]any{
			{
				"id":        2,
				"iid":       10,
				"title":     "Assigned issue",
				"state":     "opened",
				"web_url":   "https://gitlab.example.com/proj/-/issues/10",
				"labels":    []string{},
				"author":    map[string]any{"id": 1, "username": "alice", "name": "Alice", "web_url": "https://gitlab.example.com/alice"},
				"assignees": []map[string]any{{"id": 5, "username": "dave", "name": "Dave", "web_url": "https://gitlab.example.com/dave"}},
			},
		})
	}))

	issues, err := prov.ListIssues(context.Background(), "mygroup/myproject", provider.ListIssuesOptions{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d; want 1", len(issues))
	}
	if issues[0].Author.Username != "alice" {
		t.Errorf("Author.Username = %q; want %q", issues[0].Author.Username, "alice")
	}
	if len(issues[0].Assignees) != 1 || issues[0].Assignees[0].Username != "dave" {
		t.Errorf("Assignees = %v; want [{dave}]", issues[0].Assignees)
	}
}

// ─── ListIssues with filters (exercises remaining option branches) ────────────

// TestGitLab_ListIssues_WithLabelFilter exercises the label-option branch in
// ListIssues and the milestone branch in toIssue.
func TestGitLab_ListIssues_WithFiltersAndMilestone(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []map[string]any{
			{
				"id":        3,
				"iid":       20,
				"title":     "Milestone issue",
				"state":     "opened",
				"web_url":   "https://gitlab.example.com/proj/-/issues/20",
				"labels":    []string{"infra"},
				"milestone": map[string]any{"id": 1, "title": "v1.0"},
			},
		})
	}))

	issues, err := prov.ListIssues(context.Background(), "mygroup/myproject", provider.ListIssuesOptions{
		State:    "opened",
		Labels:   []string{"infra"},
		Author:   "alice",
		Assignee: "bob",
	})
	if err != nil {
		t.Fatalf("ListIssues with filters: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d; want 1", len(issues))
	}
	if issues[0].Milestone != "v1.0" {
		t.Errorf("Milestone = %q; want %q", issues[0].Milestone, "v1.0")
	}
}

// ─── ListMergeRequests with filters (exercises label/branch option paths) ─────

func TestGitLab_ListMergeRequests_WithFilters(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []map[string]any{
			{
				"id":            50,
				"iid":           8,
				"title":         "Filtered MR",
				"state":         "opened",
				"source_branch": "fix/thing",
				"target_branch": "main",
				"web_url":       "https://gitlab.example.com/proj/-/merge_requests/8",
				"labels":        []string{"ci"},
				"merged_at":     "2026-01-01T00:00:00Z",
			},
		})
	}))

	mrs, err := prov.ListMergeRequests(context.Background(), "mygroup/myproject", provider.ListMergeRequestsOptions{
		State:        "opened",
		Labels:       []string{"ci"},
		Author:       "alice",
		SourceBranch: "fix/thing",
		TargetBranch: "main",
	})
	if err != nil {
		t.Fatalf("ListMergeRequests with filters: %v", err)
	}
	if len(mrs) != 1 {
		t.Fatalf("len(mrs) = %d; want 1", len(mrs))
	}
	if mrs[0].MergedAt == nil {
		t.Error("MergedAt is nil; want non-nil for merged MR")
	}
}

// ─── ListLabels ───────────────────────────────────────────────────────────────

func TestGitLab_ListLabels_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []map[string]any{
			{"id": 1, "name": "bug", "color": "#d9534f", "description": "a bug"},
			{"id": 2, "name": "enhancement", "color": "#5cb85c", "description": ""},
		})
	}))

	labels, err := prov.ListLabels(context.Background(), "mygroup/myproject")
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(labels) != 2 {
		t.Fatalf("len(labels) = %d; want 2", len(labels))
	}

	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = l.Name
	}
	if !containsString(names, "bug") {
		t.Errorf("label names = %v; want to contain %q", names, "bug")
	}
	if !containsString(names, "enhancement") {
		t.Errorf("label names = %v; want to contain %q", names, "enhancement")
	}
}

// ─── CreateMergeRequest / UpdateMergeRequest ───────────────────────────────────

// TestGitLab_CreateMergeRequest_SendsBranches verifies the source/target branches
// are sent and the response maps to provider.MergeRequest.
func TestGitLab_CreateMergeRequest_SendsBranches(t *testing.T) {
	t.Parallel()

	var capturedBody string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		writeJSON(w, map[string]any{
			"id": 9, "iid": 4, "title": "Add feature", "state": "opened",
			"source_branch": "feat", "target_branch": "main",
		})
	}))

	mr, err := prov.CreateMergeRequest(context.Background(), "mygroup/myproject", provider.CreateMergeRequestOptions{
		Title:        "Add feature",
		SourceBranch: "feat",
		TargetBranch: "main",
	})
	if err != nil {
		t.Fatalf("CreateMergeRequest: %v", err)
	}
	if mr.IID != 4 {
		t.Errorf("IID = %d; want 4", mr.IID)
	}
	if !strings.Contains(capturedBody, "feat") || !strings.Contains(capturedBody, "main") {
		t.Errorf("request body missing branches; got: %s", capturedBody)
	}
}

// TestGitLab_UpdateMergeRequest_MapsCloseState verifies neutral "closed" maps to
// GitLab's state_event "close".
func TestGitLab_UpdateMergeRequest_MapsCloseState(t *testing.T) {
	t.Parallel()

	var capturedBody string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		writeJSON(w, map[string]any{"id": 9, "iid": 4, "state": "closed"})
	}))

	state := "closed"
	mr, err := prov.UpdateMergeRequest(context.Background(), "proj", 4, provider.UpdateMergeRequestOptions{State: &state})
	if err != nil {
		t.Fatalf("UpdateMergeRequest: %v", err)
	}
	if mr.IID != 4 {
		t.Errorf("IID = %d; want 4", mr.IID)
	}
	if !strings.Contains(capturedBody, "close") {
		t.Errorf("request body missing state_event close; got: %s", capturedBody)
	}
}

// ─── CreateLabel ──────────────────────────────────────────────────────────────

// TestGitLab_CreateLabel_SendsNameColorDescription verifies CreateLabel sends the
// name/color and maps the response (including open_issues_count) to provider.Label.
func TestGitLab_CreateLabel_SendsNameColorDescription(t *testing.T) {
	t.Parallel()

	var capturedBody string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		writeJSON(w, map[string]any{
			"id": 7, "name": "bug", "color": "#d9534f", "description": "a bug", "open_issues_count": 3,
		})
	}))

	lbl, err := prov.CreateLabel(context.Background(), "mygroup/myproject", provider.CreateLabelOptions{
		Name:        "bug",
		Color:       "#d9534f",
		Description: "a bug",
	})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	if lbl.ID != 7 {
		t.Errorf("lbl.ID = %d; want 7", lbl.ID)
	}
	if lbl.OpenIssuesCount != 3 {
		t.Errorf("lbl.OpenIssuesCount = %d; want 3", lbl.OpenIssuesCount)
	}
	if !strings.Contains(capturedBody, "bug") || !strings.Contains(capturedBody, "d9534f") {
		t.Errorf("request body missing name/color; got: %s", capturedBody)
	}
}

// TestGitLab_CreateLabel_ReturnsErrorOn400 verifies error propagation.
func TestGitLab_CreateLabel_ReturnsErrorOn400(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"400 Bad Request"}`, http.StatusBadRequest)
	}))

	_, err := prov.CreateLabel(context.Background(), "proj", provider.CreateLabelOptions{Name: "x", Color: "#fff"})
	if err == nil {
		t.Fatal("expected error from CreateLabel on HTTP 400; got nil")
	}
}

// ─── UpdateIssue ──────────────────────────────────────────────────────────────

// TestGitLab_UpdateIssue_SendsStateEventAndTitle verifies that UpdateIssue maps
// state "closed" to StateEvent "close" and sends the title field.
func TestGitLab_UpdateIssue_SendsStateEventAndTitle(t *testing.T) {
	t.Parallel()

	var capturedBody string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		writeJSON(w, map[string]any{
			"id":    50,
			"iid":   7,
			"title": "Updated title",
			"state": "closed",
		})
	}))

	title := "Updated title"
	state := "closed"
	iss, err := prov.UpdateIssue(context.Background(), "mygroup/myproject", 7, provider.UpdateIssueOptions{
		Title: &title,
		State: &state,
	})
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if iss.IID != 7 {
		t.Errorf("IID = %d; want 7", iss.IID)
	}
	if !strings.Contains(capturedBody, `"title"`) {
		t.Errorf("request body missing %q; got: %s", "title", capturedBody)
	}
	if !strings.Contains(capturedBody, `"close"`) {
		t.Errorf("request body missing state_event %q; got: %s", "close", capturedBody)
	}
}

// TestGitLab_UpdateIssue_ReturnsErrorOn404 verifies error propagation.
func TestGitLab_UpdateIssue_ReturnsErrorOn404(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"404 Not Found"}`, http.StatusNotFound)
	}))

	title := "x"
	_, err := prov.UpdateIssue(context.Background(), "proj", 99, provider.UpdateIssueOptions{Title: &title})
	if err == nil {
		t.Fatal("expected error from UpdateIssue on HTTP 404; got nil")
	}
}

// ─── CreateIssueNote ──────────────────────────────────────────────────────────

// TestGitLab_CreateIssueNote_SendsBody verifies that CreateIssueNote sends the
// note body and maps the response to provider.Note.
func TestGitLab_CreateIssueNote_SendsBody(t *testing.T) {
	t.Parallel()

	var capturedBody string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		writeJSON(w, map[string]any{
			"id":   101,
			"body": "Great fix!",
			"author": map[string]any{
				"id": 3, "username": "carol", "name": "Carol", "web_url": "https://gitlab.example.com/carol",
			},
		})
	}))

	note, err := prov.CreateIssueNote(context.Background(), "mygroup/myproject", 7, "Great fix!")
	if err != nil {
		t.Fatalf("CreateIssueNote: %v", err)
	}
	if note.ID != 101 {
		t.Errorf("note.ID = %d; want 101", note.ID)
	}
	if note.Body != "Great fix!" {
		t.Errorf("note.Body = %q; want %q", note.Body, "Great fix!")
	}
	if note.Author.Username != "carol" {
		t.Errorf("note.Author.Username = %q; want %q", note.Author.Username, "carol")
	}
	if !strings.Contains(capturedBody, "Great fix!") {
		t.Errorf("request body missing note body; got: %s", capturedBody)
	}
}

// TestGitLab_CreateIssueNote_ReturnsErrorOn403 verifies error propagation.
func TestGitLab_CreateIssueNote_ReturnsErrorOn403(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"403 Forbidden"}`, http.StatusForbidden)
	}))

	_, err := prov.CreateIssueNote(context.Background(), "proj", 1, "hello")
	if err == nil {
		t.Fatal("expected error from CreateIssueNote on HTTP 403; got nil")
	}
}

// ─── ListUsers ────────────────────────────────────────────────────────────────

// TestGitLab_ListUsers_DecodesResponse verifies that ListUsers maps GitLab
// users to provider.User correctly.
func TestGitLab_ListUsers_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []map[string]any{
			{
				"id":       10,
				"username": "alice",
				"name":     "Alice",
				"email":    "alice@example.com",
				"web_url":  "https://gitlab.example.com/alice",
			},
			{
				"id":       11,
				"username": "bob",
				"name":     "Bob",
				"email":    "bob@example.com",
				"web_url":  "https://gitlab.example.com/bob",
			},
		})
	}))

	users, err := prov.ListUsers(context.Background(), provider.ListUsersOptions{Search: "ali", Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len(users) = %d; want 2", len(users))
	}
	if users[0].Username != "alice" {
		t.Errorf("users[0].Username = %q; want %q", users[0].Username, "alice")
	}
	if users[0].Email != "alice@example.com" {
		t.Errorf("users[0].Email = %q; want %q", users[0].Email, "alice@example.com")
	}
}

// TestGitLab_ListUsers_ReturnsErrorOn500 verifies error propagation.
func TestGitLab_ListUsers_ReturnsErrorOn500(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"500 Internal Server Error"}`, http.StatusInternalServerError)
	}))

	_, err := prov.ListUsers(context.Background(), provider.ListUsersOptions{Search: "x"})
	if err == nil {
		t.Fatal("expected error from ListUsers on HTTP 500; got nil")
	}
}

func TestGitLab_ListMyIssues_UsesAssignedScopeAndRecoversProject(t *testing.T) {
	t.Parallel()

	var gotScope string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Cross-project endpoint is /api/v4/issues (no project segment), and the
		// "assigned to me" semantics come from the scope query param.
		gotScope = r.URL.Query().Get("scope")
		writeJSON(w, []map[string]any{
			{
				"id": 1, "iid": 7, "title": "Mine A", "state": "opened",
				"references": map[string]any{"full": "group/projA#7"},
			},
			{
				"id": 2, "iid": 9, "title": "Mine B", "state": "opened",
				"references": map[string]any{"full": "group/projB#9"},
			},
		})
	}))

	issues, err := prov.ListMyIssues(context.Background(),
		provider.ListMyIssuesOptions{State: "opened", PerPage: 50})
	if err != nil {
		t.Fatalf("ListMyIssues: %v", err)
	}
	if gotScope != "assigned_to_me" {
		t.Errorf("scope = %q; want assigned_to_me", gotScope)
	}
	if len(issues) != 2 {
		t.Fatalf("len(issues) = %d; want 2", len(issues))
	}
	// Project must be recovered per-issue from references.full (sans #iid).
	if issues[0].Project != "group/projA" {
		t.Errorf("issues[0].Project = %q; want group/projA", issues[0].Project)
	}
	if issues[1].Project != "group/projB" {
		t.Errorf("issues[1].Project = %q; want group/projB", issues[1].Project)
	}
}

func TestGitLab_ListMyMergeRequests_ScopeProjectAndPipeline(t *testing.T) {
	t.Parallel()

	var gotScope string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotScope = r.URL.Query().Get("scope")
		writeJSON(w, []map[string]any{
			{
				"id": 1, "iid": 3, "title": "MR A", "state": "opened",
				"source_branch": "feat", "target_branch": "main",
				"references":    map[string]any{"full": "group/projA!3"},
				"head_pipeline": map[string]any{"status": "success"},
			},
		})
	}))

	mrs, err := prov.ListMyMergeRequests(context.Background(),
		provider.ListMyMergeRequestsOptions{State: "opened", PerPage: 50})
	if err != nil {
		t.Fatalf("ListMyMergeRequests: %v", err)
	}
	if gotScope != "assigned_to_me" {
		t.Errorf("scope = %q; want assigned_to_me", gotScope)
	}
	if len(mrs) != 1 {
		t.Fatalf("len(mrs) = %d; want 1", len(mrs))
	}
	if mrs[0].Project != "group/projA" {
		t.Errorf("Project = %q; want group/projA (recovered from references.full)", mrs[0].Project)
	}
	if mrs[0].Pipeline != "success" {
		t.Errorf("Pipeline = %q; want success (from head_pipeline.status)", mrs[0].Pipeline)
	}
}

func TestGitLab_ApproveMergeRequest_HitsApproveEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(w, map[string]any{"id": 1, "iid": 3, "state": "opened"})
	}))

	if err := prov.ApproveMergeRequest(context.Background(), "group/projA", 3); err != nil {
		t.Fatalf("ApproveMergeRequest: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/merge_requests/3/approve") {
		t.Errorf("path = %q; want suffix /merge_requests/3/approve", gotPath)
	}
}

func TestGitLab_MergeMergeRequest_ReturnsMergedMR(t *testing.T) {
	t.Parallel()

	var gotMethod string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		writeJSON(w, map[string]any{
			"id": 1, "iid": 3, "title": "MR A", "state": "merged",
			"web_url": "https://gitlab.example.com/group/projA/-/merge_requests/3",
		})
	}))

	mr, err := prov.MergeMergeRequest(context.Background(), "group/projA", 3,
		provider.MergeOptions{RemoveSourceBranch: true, Squash: true})
	if err != nil {
		t.Fatalf("MergeMergeRequest: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q; want PUT (accept merge request)", gotMethod)
	}
	if mr.State != "merged" {
		t.Errorf("mr.State = %q; want merged", mr.State)
	}
}

func TestGitLab_ListIssues_SendsMilestoneTitleFilter(t *testing.T) {
	t.Parallel()

	var gotMilestone string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMilestone = r.URL.Query().Get("milestone")
		writeJSON(w, []map[string]any{})
	}))

	_, err := prov.ListIssues(context.Background(), "group/proj",
		provider.ListIssuesOptions{State: "opened", Milestone: "v1.0", PerPage: 50})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if gotMilestone != "v1.0" {
		t.Errorf("milestone query = %q; want v1.0", gotMilestone)
	}
}

func TestGitLab_GetMergeRequestDiff_MapsChanges(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"id": 1, "iid": 3,
			"changes": []map[string]any{
				{"old_path": "a.go", "new_path": "a.go", "diff": "@@ -1 +1 @@", "new_file": false},
				{"old_path": "old.go", "new_path": "new.go", "diff": "", "renamed_file": true},
			},
		})
	}))

	diffs, err := prov.GetMergeRequestDiff(context.Background(), "group/projA", 3)
	if err != nil {
		t.Fatalf("GetMergeRequestDiff: %v", err)
	}
	if len(diffs) != 2 {
		t.Fatalf("len(diffs) = %d; want 2", len(diffs))
	}
	if diffs[0].NewPath != "a.go" || diffs[0].Diff != "@@ -1 +1 @@" {
		t.Errorf("diffs[0] = %+v; want a.go with patch text", diffs[0])
	}
	if !diffs[1].RenamedFile || diffs[1].OldPath != "old.go" || diffs[1].NewPath != "new.go" {
		t.Errorf("diffs[1] = %+v; want rename old.go→new.go", diffs[1])
	}
}

func TestGitLab_ListMilestones_DecodesResponse(t *testing.T) {
	t.Parallel()

	var gotState, gotSearch, gotPath string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotState = r.URL.Query().Get("state")
		gotSearch = r.URL.Query().Get("search")
		writeJSON(w, []map[string]any{
			{"id": 11, "iid": 1, "title": "Sprint 1", "state": "active",
				"start_date": "2026-01-01", "due_date": "2026-01-14",
				"description": "First sprint", "web_url": "https://gitlab.example.com/group/proj/-/milestones/1"},
		})
	}))

	ms, err := prov.ListMilestones(context.Background(), "group/proj",
		provider.ListMilestonesOptions{State: "active", Search: "Sprint", PerPage: 20})
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	if len(ms) != 1 {
		t.Fatalf("len(ms) = %d; want 1", len(ms))
	}
	if ms[0].Title != "Sprint 1" || ms[0].State != "active" {
		t.Errorf("ms[0] = %+v", ms[0])
	}
	if ms[0].StartDate != "2026-01-01" || ms[0].DueDate != "2026-01-14" {
		t.Errorf("ms[0] dates = %q / %q", ms[0].StartDate, ms[0].DueDate)
	}
	if ms[0].WebURL == "" {
		t.Errorf("ms[0].WebURL should be populated for project milestones")
	}
	if gotState != "active" || gotSearch != "Sprint" {
		t.Errorf("query state=%q search=%q", gotState, gotSearch)
	}
	if !strings.Contains(gotPath, "/milestones") {
		t.Errorf("path = %q; want milestones endpoint", gotPath)
	}
}

func TestGitLab_ListMilestones_ReturnsErrorOn403(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))

	if _, err := prov.ListMilestones(context.Background(), "group/proj", provider.ListMilestonesOptions{}); err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestGitLab_ResolveGroup_ReturnsNumericID(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"id": 42, "full_path": "my-org/sub"})
	}))

	id, err := prov.ResolveGroup(context.Background(), "my-org/sub")
	if err != nil {
		t.Fatalf("ResolveGroup: %v", err)
	}
	if id != 42 {
		t.Errorf("id = %d; want 42", id)
	}
}

func TestGitLab_ResolveGroup_ReturnsErrorOn404(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	if _, err := prov.ResolveGroup(context.Background(), "missing"); err == nil {
		t.Fatal("expected error on 404")
	}
}

func TestGitLab_ListGroupEpics_DecodesAuthorAndDates(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []map[string]any{
			{"id": 7, "iid": 3, "title": "Epic A", "state": "opened",
				"description": "desc", "labels": []string{"red"},
				"author":     map[string]any{"id": 9, "username": "alice", "name": "Alice", "web_url": "https://gitlab.example.com/u/alice"},
				"start_date": "2026-02-01", "due_date": "2026-03-01",
				"web_url": "https://gitlab.example.com/groups/g/-/epics/3"},
		})
	}))

	epics, err := prov.ListGroupEpics(context.Background(), 5, provider.ListGroupEpicsOptions{State: "opened", PerPage: 10})
	if err != nil {
		t.Fatalf("ListGroupEpics: %v", err)
	}
	if len(epics) != 1 {
		t.Fatalf("len(epics) = %d; want 1", len(epics))
	}
	ep := epics[0]
	if ep.IID != 3 || ep.Title != "Epic A" || ep.State != "opened" {
		t.Errorf("epic = %+v", ep)
	}
	if ep.Author.Username != "alice" || ep.Author.ID != 9 {
		t.Errorf("author = %+v", ep.Author)
	}
	if ep.StartDate != "2026-02-01" || ep.DueDate != "2026-03-01" {
		t.Errorf("dates = %q / %q", ep.StartDate, ep.DueDate)
	}
	if ep.GroupID != 5 {
		t.Errorf("GroupID = %d; want 5", ep.GroupID)
	}
	if !containsString(ep.Labels, "red") {
		t.Errorf("labels = %v", ep.Labels)
	}
}

func TestGitLab_ListGroupMilestones_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []map[string]any{
			{"id": 21, "iid": 2, "title": "Q1", "state": "active",
				"start_date": "2026-01-01", "due_date": "2026-03-31", "description": "Q1 plan"},
		})
	}))

	ms, err := prov.ListGroupMilestones(context.Background(), 7, provider.ListGroupMilestonesOptions{State: "active", PerPage: 10})
	if err != nil {
		t.Fatalf("ListGroupMilestones: %v", err)
	}
	if len(ms) != 1 {
		t.Fatalf("len(ms) = %d; want 1", len(ms))
	}
	if ms[0].Title != "Q1" || ms[0].GroupID != 7 {
		t.Errorf("ms[0] = %+v", ms[0])
	}
	if ms[0].StartDate != "2026-01-01" || ms[0].DueDate != "2026-03-31" {
		t.Errorf("ms[0] dates = %q / %q", ms[0].StartDate, ms[0].DueDate)
	}
}

func TestGitLab_CreateGroupMilestone_SendsTitleAndDates(t *testing.T) {
	t.Parallel()

	var gotTitle, gotStart, gotDue string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTitle, _ = body["title"].(string)
		gotStart, _ = body["start_date"].(string)
		gotDue, _ = body["due_date"].(string)
		writeJSON(w, map[string]any{"id": 31, "iid": 1, "title": gotTitle, "state": "active",
			"start_date": gotStart, "due_date": gotDue})
	}))

	ms, err := prov.CreateGroupMilestone(context.Background(), 8, provider.CreateMilestoneOptions{
		Title:     "Sprint 2",
		StartDate: "2026-04-01",
		DueDate:   "2026-04-14",
	})
	if err != nil {
		t.Fatalf("CreateGroupMilestone: %v", err)
	}
	if ms.Title != "Sprint 2" || ms.GroupID != 8 {
		t.Errorf("ms = %+v", ms)
	}
	if gotTitle != "Sprint 2" || gotStart != "2026-04-01" || gotDue != "2026-04-14" {
		t.Errorf("body = title=%q start=%q due=%q", gotTitle, gotStart, gotDue)
	}
}

func TestGitLab_CreateGroupEpic_SendsTitleAndLabels(t *testing.T) {
	t.Parallel()

	var gotTitle, gotLabels string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTitle, _ = body["title"].(string)
		// GitLab's LabelOptions marshals labels as a comma-separated string.
		if l, ok := body["labels"].(string); ok {
			gotLabels = l
		}
		writeJSON(w, map[string]any{"id": 41, "iid": 5, "title": gotTitle, "state": "opened",
			"author": map[string]any{"id": 1, "username": "bot", "name": "Bot", "web_url": "u"}})
	}))

	ep, err := prov.CreateGroupEpic(context.Background(), 9, provider.CreateEpicOptions{
		Title:       "New Epic",
		Description: "desc",
		Labels:      []string{"red", "blue"},
	})
	if err != nil {
		t.Fatalf("CreateGroupEpic: %v", err)
	}
	if ep.Title != "New Epic" || ep.IID != 5 || ep.GroupID != 9 {
		t.Errorf("ep = %+v", ep)
	}
	if gotTitle != "New Epic" {
		t.Errorf("body title = %q", gotTitle)
	}
	if !strings.Contains(gotLabels, "red") || !strings.Contains(gotLabels, "blue") {
		t.Errorf("body labels = %q; want red and blue", gotLabels)
	}
}

// ─── GetProject ───────────────────────────────────────────────────────────────

func TestGitLab_GetProject_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q; want GET", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/projects/group/proj") {
			t.Errorf("path = %q; want .../projects/group/proj", r.URL.Path)
		}
		writeJSON(w, map[string]any{
			"id":                  42,
			"name":                "proj",
			"path_with_namespace": "group/proj",
			"name_with_namespace": "Group / Proj",
			"description":         "demo",
			"web_url":             "https://gitlab.example/group/proj",
			"default_branch":      "main",
			"open_issues_count":   7,
			"archived":            false,
		})
	}))

	p, err := prov.GetProject(context.Background(), "group/proj")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if p.ID != 42 || p.Name != "proj" || p.Path != "group/proj" ||
		p.FullName != "Group / Proj" || p.Description != "demo" ||
		p.WebURL != "https://gitlab.example/group/proj" ||
		p.DefaultBranch != "main" || p.OpenIssuesCount != 7 || p.Archived {
		t.Errorf("project = %+v", p)
	}
}

func TestGitLab_GetProject_ReturnsErrorOn404(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"404 Not Found"}`))
	}))

	if _, err := prov.GetProject(context.Background(), "missing/proj"); err == nil {
		t.Fatal("GetProject: expected error for 404, got nil")
	}
}

// ─── ListWorkItems ────────────────────────────────────────────────────────────

func TestGitLab_ListWorkItems_DecodesResponse(t *testing.T) {
	t.Parallel()

	var gotState, gotType string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotState = r.URL.Query().Get("state")
		gotType = r.URL.Query().Get("work_item_type_name")
		writeJSON(w, []map[string]any{
			{
				"id":    101,
				"iid":   5,
				"title": "Define API",
				"state": "opened",
				"work_item_type": map[string]any{
					"name": "EPIC",
				},
				"web_url": "https://gitlab.example/group/proj/-/work_items/5",
			},
			{
				"id":    102,
				"iid":   6,
				"title": "Write tests",
				"state": "closed",
				"work_item_type": map[string]any{
					"name": "TASK",
				},
				"web_url": "https://gitlab.example/group/proj/-/work_items/6",
			},
		})
	}))

	items, err := prov.ListWorkItems(context.Background(), "group/proj", provider.ListWorkItemsOptions{
		State: "all",
		Type:  "TASK",
	})
	if err != nil {
		t.Fatalf("ListWorkItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items len = %d; want 2", len(items))
	}
	if items[0].ID != 101 || items[0].IID != 5 || items[0].Title != "Define API" ||
		items[0].State != "opened" || items[0].Type != "EPIC" ||
		items[0].WebURL != "https://gitlab.example/group/proj/-/work_items/5" {
		t.Errorf("items[0] = %+v", items[0])
	}
	if items[1].Type != "TASK" || items[1].State != "closed" {
		t.Errorf("items[1] = %+v", items[1])
	}
	if gotState != "all" {
		t.Errorf("query state = %q; want all", gotState)
	}
	if gotType != "TASK" {
		t.Errorf("query type = %q; want TASK", gotType)
	}
}

func TestGitLab_ListWorkItems_ReturnsErrorOn403(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))

	if _, err := prov.ListWorkItems(context.Background(), "group/proj", provider.ListWorkItemsOptions{}); err == nil {
		t.Fatal("ListWorkItems: expected error for 403, got nil")
	}
}

func TestGitLab_ListWorkItems_SendsPrivateToken(t *testing.T) {
	t.Parallel()

	var tok string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok = r.Header.Get("PRIVATE-TOKEN")
		writeJSON(w, []any{})
	}))

	if _, err := prov.ListWorkItems(context.Background(), "group/proj", provider.ListWorkItemsOptions{}); err != nil {
		t.Fatalf("ListWorkItems: %v", err)
	}
	if tok != "glpat-test-token" {
		t.Errorf("PRIVATE-TOKEN = %q; want glpat-test-token", tok)
	}
}

// ─── CreateWorkItem ───────────────────────────────────────────────────────────

func TestGitLab_CreateWorkItem_SendsTitleAndType(t *testing.T) {
	t.Parallel()

	var gotTitle, gotType string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q; want POST", r.Method)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTitle, _ = body["title"].(string)
		gotType, _ = body["work_item_type_name"].(string)
		writeJSON(w, map[string]any{
			"iid":     7,
			"title":   gotTitle,
			"web_url": "https://gitlab.example/group/proj/-/work_items/7",
		})
	}))

	wi, err := prov.CreateWorkItem(context.Background(), "group/proj", provider.CreateWorkItemOptions{
		Title: "New Task",
		Type:  "TASK",
	})
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}
	if wi.IID != 7 || wi.Title != "New Task" || wi.Type != "TASK" ||
		wi.WebURL != "https://gitlab.example/group/proj/-/work_items/7" {
		t.Errorf("work item = %+v", wi)
	}
	if gotTitle != "New Task" {
		t.Errorf("body title = %q", gotTitle)
	}
	if gotType != "TASK" {
		t.Errorf("body type = %q", gotType)
	}
}

func TestGitLab_CreateWorkItem_DefaultsToTask(t *testing.T) {
	t.Parallel()

	var gotType string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotType, _ = body["work_item_type_name"].(string)
		writeJSON(w, map[string]any{"iid": 8, "title": "x"})
	}))

	if _, err := prov.CreateWorkItem(context.Background(), "group/proj", provider.CreateWorkItemOptions{
		Title: "x",
	}); err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}
	if gotType != "TASK" {
		t.Errorf("body type = %q; want TASK default", gotType)
	}
}

func TestGitLab_CreateWorkItem_ReturnsErrorOn400(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"bad"}`))
	}))

	if _, err := prov.CreateWorkItem(context.Background(), "group/proj", provider.CreateWorkItemOptions{
		Title: "x",
	}); err == nil {
		t.Fatal("CreateWorkItem: expected error for 400, got nil")
	}
}

// ─── CloseWorkItem ────────────────────────────────────────────────────────────

func TestGitLab_CloseWorkItem_SendsCloseStateEvent(t *testing.T) {
	t.Parallel()

	var gotStateEvent string
	var gotPath string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %q; want PATCH", r.Method)
		}
		gotPath = r.URL.Path
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotStateEvent, _ = body["state_event"].(string)
		w.WriteHeader(http.StatusOK)
	}))

	if err := prov.CloseWorkItem(context.Background(), "group/proj", 42); err != nil {
		t.Fatalf("CloseWorkItem: %v", err)
	}
	if gotStateEvent != "close" {
		t.Errorf("body state_event = %q; want close", gotStateEvent)
	}
	if !strings.HasSuffix(gotPath, "/projects/group/proj/work_items/42") {
		t.Errorf("path = %q; want .../projects/group/proj/work_items/42", gotPath)
	}
}

func TestGitLab_CloseWorkItem_ReturnsErrorOn404(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	}))

	if err := prov.CloseWorkItem(context.Background(), "group/proj", 999); err == nil {
		t.Fatal("CloseWorkItem: expected error for 404, got nil")
	}
}

// ─── ListPipelines ────────────────────────────────────────────────────────────

func TestGitLab_ListPipelines_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/projects/group/proj/pipelines") {
			t.Errorf("path = %q; want pipelines", r.URL.Path)
		}
		writeJSON(w, []map[string]any{
			{"id": 1, "iid": 2, "status": "success", "ref": "main", "sha": "abc", "web_url": "u1", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:05:00Z"},
			{"id": 3, "iid": 4, "status": "failed", "ref": "dev", "sha": "def", "web_url": "u2"},
		})
	}))

	pipes, err := prov.ListPipelines(context.Background(), "group/proj", provider.ListPipelinesOptions{PerPage: 10})
	if err != nil {
		t.Fatalf("ListPipelines: %v", err)
	}
	if len(pipes) != 2 {
		t.Fatalf("pipelines len = %d; want 2", len(pipes))
	}
	if pipes[0].ID != 1 || pipes[0].IID != 2 || pipes[0].Status != "success" ||
		pipes[0].Ref != "main" || pipes[0].SHA != "abc" || pipes[0].WebURL != "u1" ||
		pipes[0].Project != "group/proj" {
		t.Errorf("pipes[0] = %+v", pipes[0])
	}
	if pipes[1].Status != "failed" || pipes[1].Ref != "dev" {
		t.Errorf("pipes[1] = %+v", pipes[1])
	}
}

func TestGitLab_ListPipelines_ReturnsErrorOn403(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))

	if _, err := prov.ListPipelines(context.Background(), "group/proj", provider.ListPipelinesOptions{}); err == nil {
		t.Fatal("ListPipelines: expected error for 403, got nil")
	}
}

// ─── GetPipeline ──────────────────────────────────────────────────────────────

func TestGitLab_GetPipeline_DecodesSingle(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/pipelines/42") {
			t.Errorf("path = %q; want .../pipelines/42", r.URL.Path)
		}
		writeJSON(w, map[string]any{
			"id": 42, "iid": 7, "status": "running", "ref": "main", "sha": "abc",
			"web_url":     "https://gitlab.example/p/42",
			"created_at":  "2026-01-01T00:00:00Z",
			"updated_at":  "2026-01-01T00:05:00Z",
			"finished_at": "2026-01-01T00:10:00Z",
			"user":        map[string]any{"id": 1, "username": "alice", "name": "Alice", "web_url": "u"},
		})
	}))

	p, err := prov.GetPipeline(context.Background(), "group/proj", 42)
	if err != nil {
		t.Fatalf("GetPipeline: %v", err)
	}
	if p.ID != 42 || p.IID != 7 || p.Status != "running" || p.Ref != "main" ||
		p.SHA != "abc" || p.WebURL != "https://gitlab.example/p/42" ||
		p.Author.ID != 1 || p.Author.Username != "alice" || p.FinishedAt == nil {
		t.Errorf("pipeline = %+v", p)
	}
}

func TestGitLab_GetPipeline_ReturnsErrorOn404(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	if _, err := prov.GetPipeline(context.Background(), "group/proj", 999); err == nil {
		t.Fatal("GetPipeline: expected error for 404, got nil")
	}
}

// ─── RunPipeline ──────────────────────────────────────────────────────────────

func TestGitLab_RunPipeline_SendsRefAndVariables(t *testing.T) {
	t.Parallel()

	var gotRef string
	var gotVars []any
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q; want POST", r.Method)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotRef, _ = body["ref"].(string)
		gotVars, _ = body["variables"].([]any)
		writeJSON(w, map[string]any{
			"id": 100, "iid": 1, "status": "created", "ref": gotRef, "sha": "xyz",
			"web_url": "u",
		})
	}))

	p, err := prov.RunPipeline(context.Background(), "group/proj", provider.RunPipelineOptions{
		Ref:       "main",
		Variables: map[string]string{"FOO": "bar"},
	})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if p.ID != 100 || p.Status != "created" || p.Ref != "main" {
		t.Errorf("pipeline = %+v", p)
	}
	if gotRef != "main" {
		t.Errorf("body ref = %q; want main", gotRef)
	}
	if len(gotVars) == 0 {
		t.Error("body variables = empty; want array with FOO=bar")
	}
}

// ─── RetryPipeline ────────────────────────────────────────────────────────────

func TestGitLab_RetryPipeline_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q; want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/pipelines/42/retry") {
			t.Errorf("path = %q; want .../pipelines/42/retry", r.URL.Path)
		}
		writeJSON(w, map[string]any{
			"id": 42, "iid": 7, "status": "pending", "ref": "main", "sha": "abc", "web_url": "u",
		})
	}))

	p, err := prov.RetryPipeline(context.Background(), "group/proj", 42)
	if err != nil {
		t.Fatalf("RetryPipeline: %v", err)
	}
	if p.ID != 42 || p.Status != "pending" {
		t.Errorf("pipeline = %+v", p)
	}
}

// ─── CancelPipeline ───────────────────────────────────────────────────────────

func TestGitLab_CancelPipeline_SendsCancel(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q; want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/pipelines/42/cancel") {
			t.Errorf("path = %q; want .../pipelines/42/cancel", r.URL.Path)
		}
		writeJSON(w, map[string]any{"id": 42, "status": "canceled"})
	}))

	if err := prov.CancelPipeline(context.Background(), "group/proj", 42); err != nil {
		t.Fatalf("CancelPipeline: %v", err)
	}
}

func TestGitLab_CancelPipeline_ReturnsErrorOn404(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	if err := prov.CancelPipeline(context.Background(), "group/proj", 999); err == nil {
		t.Fatal("CancelPipeline: expected error for 404, got nil")
	}
}

// ─── ListPipelineJobs ─────────────────────────────────────────────────────────

func TestGitLab_ListPipelineJobs_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{
			{"id": 1, "name": "build", "status": "success", "stage": "build", "ref": "main", "web_url": "j1",
				"started_at": "2026-01-01T00:00:00Z"},
			{"id": 2, "name": "test", "status": "failed", "stage": "test", "ref": "main", "web_url": "j2"},
		})
	}))

	jobs, err := prov.ListPipelineJobs(context.Background(), "group/proj", 42)
	if err != nil {
		t.Fatalf("ListPipelineJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("jobs len = %d; want 2", len(jobs))
	}
	if jobs[0].ID != 1 || jobs[0].Name != "build" || jobs[0].Status != "success" ||
		jobs[0].Stage != "build" || jobs[0].Ref != "main" || jobs[0].WebURL != "j1" {
		t.Errorf("jobs[0] = %+v", jobs[0])
	}
	if jobs[1].Name != "test" || jobs[1].Status != "failed" {
		t.Errorf("jobs[1] = %+v", jobs[1])
	}
}

// ─── GetJobLogs ───────────────────────────────────────────────────────────────

func TestGitLab_GetJobLogs_ReturnsTrace(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("line 1\nline 2\n"))
	}))

	logs, err := prov.GetJobLogs(context.Background(), "group/proj", 99)
	if err != nil {
		t.Fatalf("GetJobLogs: %v", err)
	}
	if logs != "line 1\nline 2\n" {
		t.Errorf("logs = %q; want 'line 1\\nline 2\\n'", logs)
	}
}

// ─── ListArtifacts ────────────────────────────────────────────────────────────

func TestGitLab_ListArtifacts_DecodesFromJobs(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []map[string]any{
			{
				"id": 1, "name": "build", "status": "success", "stage": "build", "ref": "main",
				"web_url": "j1",
				"artifacts": []map[string]any{
					{"filename": "report.html", "size": 1024},
				},
				"artifacts_expire_at": "2026-01-01T00:00:00Z",
			},
		})
	}))

	arts, err := prov.ListArtifacts(context.Background(), "group/proj", 42)
	if err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	if len(arts) != 1 {
		t.Fatalf("artifacts len = %d; want 1", len(arts))
	}
	if arts[0].Name != "report.html" || arts[0].Size != 1024 {
		t.Errorf("artifact = %+v", arts[0])
	}
}

// ─── DownloadArtifact ─────────────────────────────────────────────────────────

func TestGitLab_DownloadArtifact_WritesZip(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("PK\x03\x04fake-zip-content"))
	}))

	dest := t.TempDir()
	if err := prov.DownloadArtifact(context.Background(), "group/proj", 1, dest); err != nil {
		t.Fatalf("DownloadArtifact: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "artifacts.zip"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "PK\x03\x04fake-zip-content" {
		t.Errorf("file content = %q; want fake-zip-content", string(data))
	}
}

// ─── GetMilestone ─────────────────────────────────────────────────────────────

func TestGitLab_GetMilestone_DecodesSingle(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/milestones/5") {
			t.Errorf("path = %q; want .../milestones/5", r.URL.Path)
		}
		writeJSON(w, map[string]any{
			"id": 5, "iid": 1, "title": "Sprint 1", "state": "active",
			"description": "first sprint", "due_date": "2026-02-01",
			"start_date": "2026-01-15",
		})
	}))

	m, err := prov.GetMilestone(context.Background(), "group/proj", 5)
	if err != nil {
		t.Fatalf("GetMilestone: %v", err)
	}
	if m.ID != 5 || m.Title != "Sprint 1" || m.State != "active" ||
		m.Description != "first sprint" || m.DueDate != "2026-02-01" ||
		m.StartDate != "2026-01-15" {
		t.Errorf("milestone = %+v", m)
	}
}

// ─── CreateMilestone ──────────────────────────────────────────────────────────

func TestGitLab_CreateMilestone_SendsTitleAndDates(t *testing.T) {
	t.Parallel()

	var gotTitle, gotDesc, gotStart, gotDue string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q; want POST", r.Method)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTitle, _ = body["title"].(string)
		gotDesc, _ = body["description"].(string)
		gotStart, _ = body["start_date"].(string)
		gotDue, _ = body["due_date"].(string)
		writeJSON(w, map[string]any{
			"id": 10, "title": gotTitle, "state": "active",
			"description": gotDesc, "start_date": gotStart, "due_date": gotDue,
		})
	}))

	m, err := prov.CreateMilestone(context.Background(), "group/proj", provider.CreateMilestoneOptions{
		Title:       "Sprint 2",
		Description: "second sprint",
		StartDate:   "2026-04-01",
		DueDate:     "2026-04-14",
	})
	if err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}
	if m.ID != 10 || m.Title != "Sprint 2" || m.State != "active" {
		t.Errorf("milestone = %+v", m)
	}
	if gotTitle != "Sprint 2" || gotDesc != "second sprint" ||
		gotStart != "2026-04-01" || gotDue != "2026-04-14" {
		t.Errorf("body = title=%q desc=%q start=%q due=%q", gotTitle, gotDesc, gotStart, gotDue)
	}
}

func TestGitLab_CreateMilestone_RejectsBadStartDate(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if _, err := prov.CreateMilestone(context.Background(), "group/proj", provider.CreateMilestoneOptions{
		Title:     "x",
		StartDate: "not-a-date",
	}); err == nil {
		t.Fatal("CreateMilestone: expected error for bad start date, got nil")
	}
}

func TestGitLab_CreateMilestone_RejectsBadDueDate(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if _, err := prov.CreateMilestone(context.Background(), "group/proj", provider.CreateMilestoneOptions{
		Title:   "x",
		DueDate: "not-a-date",
	}); err == nil {
		t.Fatal("CreateMilestone: expected error for bad due date, got nil")
	}
}

// ─── UpdateMilestone ──────────────────────────────────────────────────────────

func TestGitLab_UpdateMilestone_SendsTitleAndState(t *testing.T) {
	t.Parallel()

	var gotTitle, gotState string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %q; want PUT", r.Method)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTitle, _ = body["title"].(string)
		gotState, _ = body["state_event"].(string)
		writeJSON(w, map[string]any{
			"id": 5, "title": gotTitle, "state": "closed", "state_event": gotState,
		})
	}))

	title := "Renamed"
	state := "close"
	m, err := prov.UpdateMilestone(context.Background(), "group/proj", 5, provider.UpdateMilestoneOptions{
		Title: &title,
		State: &state,
	})
	if err != nil {
		t.Fatalf("UpdateMilestone: %v", err)
	}
	if m.ID != 5 || m.Title != "Renamed" {
		t.Errorf("milestone = %+v", m)
	}
	if gotTitle != "Renamed" {
		t.Errorf("body title = %q; want Renamed", gotTitle)
	}
	if gotState != "close" {
		t.Errorf("body state_event = %q; want close", gotState)
	}
}

// ─── UpdateGroupEpic ──────────────────────────────────────────────────────────

func TestGitLab_UpdateGroupEpic_SendsTitleAndState(t *testing.T) {
	t.Parallel()

	var gotTitle, gotState string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %q; want PUT", r.Method)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTitle, _ = body["title"].(string)
		gotState, _ = body["state_event"].(string)
		writeJSON(w, map[string]any{
			"id": 41, "iid": 5, "title": gotTitle, "state": "closed",
			"author": map[string]any{"id": 1, "username": "bot", "name": "Bot", "web_url": "u"},
		})
	}))

	title := "Updated Epic"
	state := "close"
	ep, err := prov.UpdateGroupEpic(context.Background(), 9, 5, provider.UpdateEpicOptions{
		Title: &title,
		State: &state,
	})
	if err != nil {
		t.Fatalf("UpdateGroupEpic: %v", err)
	}
	if ep.Title != "Updated Epic" || ep.IID != 5 || ep.GroupID != 9 || ep.State != "closed" {
		t.Errorf("epic = %+v", ep)
	}
	if gotTitle != "Updated Epic" {
		t.Errorf("body title = %q", gotTitle)
	}
	if gotState != "close" {
		t.Errorf("body state_event = %q; want close", gotState)
	}
}

// ─── ListEpicIssues ───────────────────────────────────────────────────────────

func TestGitLab_ListEpicIssues_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a response with X-Total-Pages = 1 so the pagination loop breaks.
		w.Header().Set("X-Total-Pages", "1")
		w.Header().Set("X-Page", "1")
		writeJSON(w, []map[string]any{
			{
				"id": 100, "iid": 7, "title": "Epic issue",
				"state": "opened", "description": "desc",
				"author":     map[string]any{"id": 1, "username": "alice", "name": "Alice", "web_url": "u"},
				"references": map[string]any{"full": "group/proj#7"},
				"created_at": "2026-01-01T00:00:00Z",
				"updated_at": "2026-01-02T00:00:00Z",
				"web_url":    "https://gitlab.example/group/proj/issues/7",
			},
		})
	}))

	issues, err := prov.ListEpicIssues(context.Background(), 9, 5)
	if err != nil {
		t.Fatalf("ListEpicIssues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("issues len = %d; want 1", len(issues))
	}
	if issues[0].ID != 100 || issues[0].IID != 7 || issues[0].Title != "Epic issue" {
		t.Errorf("issues[0] = %+v", issues[0])
	}
}

// ─── LinkIssueToEpic ──────────────────────────────────────────────────────────

func TestGitLab_LinkIssueToEpic_AssignsWhenNotLinked(t *testing.T) {
	t.Parallel()

	var assignCalled bool
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// POST to groups/:group/epics/:epic/issues/:issue is the assign call.
		// GET to groups/:group/epics/:epic/issues is the list call.
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/issues/100") {
			assignCalled = true
			writeJSON(w, map[string]any{"id": 100, "iid": 7, "epic_issue_id": 999})
			return
		}
		// GET: list epic issues — return empty so assign is called.
		w.Header().Set("X-Total-Pages", "1")
		w.Header().Set("X-Page", "1")
		writeJSON(w, []any{})
	}))

	if err := prov.LinkIssueToEpic(context.Background(), 9, 5, 100); err != nil {
		t.Fatalf("LinkIssueToEpic: %v", err)
	}
	if !assignCalled {
		t.Error("AssignEpicIssue was not called")
	}
}

func TestGitLab_LinkIssueToEpic_SkipsWhenAlreadyLinked(t *testing.T) {
	t.Parallel()

	var assignCalled bool
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/issues/100") {
			assignCalled = true
			writeJSON(w, map[string]any{"id": 100, "iid": 7})
			return
		}
		// GET: list epic issues — return the issue already linked.
		w.Header().Set("X-Total-Pages", "1")
		w.Header().Set("X-Page", "1")
		writeJSON(w, []map[string]any{
			{"id": 100, "iid": 7, "title": "already linked"},
		})
	}))

	if err := prov.LinkIssueToEpic(context.Background(), 9, 5, 100); err != nil {
		t.Fatalf("LinkIssueToEpic: %v", err)
	}
	if assignCalled {
		t.Error("AssignEpicIssue was called but issue already linked")
	}
}
