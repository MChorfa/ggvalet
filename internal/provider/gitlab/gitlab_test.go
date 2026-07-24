package gitlab_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
				"author": map[string]any{"id": 9, "username": "alice", "name": "Alice", "web_url": "https://gitlab.example.com/u/alice"},
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
