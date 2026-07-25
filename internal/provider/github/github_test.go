package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	gh "github.com/google/go-github/v66/github"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/provider"
	githubprov "github.com/MChorfa/ggvalet/internal/provider/github"
)

// ─── static interface check ───────────────────────────────────────────────────

// TestGitHub_SatisfiesProviderInterface is a compile-time guarantee that
// GitHub implements provider.Provider completely.
func TestGitHub_SatisfiesProviderInterface(t *testing.T) {
	var _ provider.Provider = (*githubprov.GitHub)(nil)
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// minimalCfg returns a *config.Config suitable for tests that call New().
func minimalCfg(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	return &config.Config{
		Host:        "github.com",
		Token:       "test-token",
		JournalPath: filepath.Join(dir, "journal.jsonl"),
		CachePath:   filepath.Join(dir, "cache"),
	}
}

// newTestProvider wires handler into an httptest.Server and returns a *GitHub
// provider whose gh.Client points at that server. The server is closed via
// t.Cleanup. It bypasses the feature-flag check intentionally — flag tests
// call New() directly.
func newTestProvider(t *testing.T, handler http.Handler) *githubprov.GitHub {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client := gh.NewClient(srv.Client()).WithAuthToken("test-token")
	baseURL, _ := url.Parse(srv.URL + "/")
	client.BaseURL = baseURL
	client.UploadURL = baseURL

	return githubprov.NewWithClient(client, "github.com")
}

// writeJSON writes v as JSON with HTTP 200.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}

// ─── Feature flag ─────────────────────────────────────────────────────────────

// TestGitHub_New_DisabledByDefault asserts New returns ErrFeatureDisabled
// when GLVALET_GITHUB_ENABLED is not set to "true".
func TestGitHub_New_DisabledByDefault(t *testing.T) {
	t.Setenv("GLVALET_GITHUB_ENABLED", "")

	prov, err := githubprov.New(minimalCfg(t))
	if !errors.Is(err, githubprov.ErrFeatureDisabled) {
		t.Errorf("err = %v; want ErrFeatureDisabled", err)
	}
	if prov != nil {
		t.Errorf("provider = %v; want nil", prov)
	}
}

// TestGitHub_New_EnabledByFlag asserts New returns a non-nil provider when
// GLVALET_GITHUB_ENABLED is exactly "true".
func TestGitHub_New_EnabledByFlag(t *testing.T) {
	t.Setenv("GLVALET_GITHUB_ENABLED", "true")

	prov, err := githubprov.New(minimalCfg(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if prov == nil {
		t.Fatal("provider is nil; want non-nil")
	}
}

// ─── Kind & Host ──────────────────────────────────────────────────────────────

// TestGitHub_Kind verifies Kind returns provider.KindGitHub.
func TestGitHub_Kind(t *testing.T) {
	t.Setenv("GLVALET_GITHUB_ENABLED", "true")

	prov, err := githubprov.New(minimalCfg(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := prov.Kind(); got != provider.KindGitHub {
		t.Errorf("Kind() = %q; want %q", got, provider.KindGitHub)
	}
}

// TestGitHub_Host_DefaultsToGitHubDotCom verifies that Host returns
// "github.com" when cfg.GitHubURL is empty.
func TestGitHub_Host_DefaultsToGitHubDotCom(t *testing.T) {
	t.Setenv("GLVALET_GITHUB_ENABLED", "true")

	cfg := minimalCfg(t)
	cfg.GitHubURL = ""

	prov, err := githubprov.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := prov.Host(); got != "github.com" {
		t.Errorf("Host() = %q; want %q", got, "github.com")
	}
}

// TestGitHub_Host_ParsesEnterprise verifies that Host returns the bare
// hostname when cfg.GitHubURL points to a GitHub Enterprise Server.
func TestGitHub_Host_ParsesEnterprise(t *testing.T) {
	t.Setenv("GLVALET_GITHUB_ENABLED", "true")

	cfg := minimalCfg(t)
	cfg.GitHubURL = "https://github.example.com/api/v3"

	prov, err := githubprov.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := prov.Host(); got != "github.example.com" {
		t.Errorf("Host() = %q; want %q", got, "github.example.com")
	}
}

// ─── ListIssues ───────────────────────────────────────────────────────────────

// TestGitHub_ListIssues_FiltersPullRequests is the critical correctness test:
// the GitHub issues API returns both issues and pull requests. The provider
// MUST exclude items that have a pull_request links block.
func TestGitHub_ListIssues_FiltersPullRequests(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{
			{
				"id": 1, "number": 10, "title": "Real issue A", "state": "open",
			},
			{
				"id": 2, "number": 11, "title": "A pull request", "state": "open",
				"pull_request": map[string]any{
					"url": "https://api.github.com/repos/owner/repo/pulls/11",
				},
			},
			{
				"id": 3, "number": 12, "title": "Real issue B", "state": "open",
			},
		})
	}))

	issues, err := prov.ListIssues(context.Background(), "owner/repo", provider.ListIssuesOptions{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("len(issues) = %d; want 2 (PR must be filtered out)", len(issues))
	}
	for _, iss := range issues {
		if strings.Contains(iss.Title, "pull request") {
			t.Errorf("issues list contains a PR: %q", iss.Title)
		}
	}
}

// TestGitHub_ListIssues_SendsAuthorizationHeader asserts the Authorization
// Bearer header is present on every request.
func TestGitHub_ListIssues_SendsAuthorizationHeader(t *testing.T) {
	t.Parallel()

	var capturedAuth string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		writeJSON(w, []map[string]any{})
	}))

	_, err := prov.ListIssues(context.Background(), "owner/repo", provider.ListIssuesOptions{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if !strings.HasPrefix(capturedAuth, "Bearer ") {
		t.Errorf("Authorization = %q; want prefix %q", capturedAuth, "Bearer ")
	}
}

// TestGitHub_GetIssue_DecodesResponse verifies that the issue fields are
// mapped correctly from the GitHub JSON response.
func TestGitHub_GetIssue_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"id": 100, "number": 42, "title": "X", "state": "open",
			"html_url": "https://github.com/owner/repo/issues/42",
		})
	}))

	iss, err := prov.GetIssue(context.Background(), "owner/repo", 42)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if iss.IID != 42 {
		t.Errorf("IID = %d; want 42", iss.IID)
	}
	if iss.Title != "X" {
		t.Errorf("Title = %q; want %q", iss.Title, "X")
	}
	if iss.State != "open" {
		t.Errorf("State = %q; want %q", iss.State, "open")
	}
}

// TestGitHub_CreateIssue_SendsTitleAndLabels verifies the POST body includes
// title and labels as a JSON array (GitHub uses an array, not a comma-separated string).
func TestGitHub_CreateIssue_SendsTitleAndLabels(t *testing.T) {
	t.Parallel()

	var capturedBody string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		writeJSON(w, map[string]any{
			"id": 200, "number": 1, "title": "Bug", "state": "open",
			"labels": []map[string]any{
				{"id": 1, "name": "bug", "color": "d73a4a"},
				{"id": 2, "name": "critical", "color": "e4e669"},
			},
		})
	}))

	_, err := prov.CreateIssue(context.Background(), "owner/repo", provider.CreateIssueOptions{
		Title:  "Bug",
		Labels: []string{"bug", "critical"},
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if !strings.Contains(capturedBody, `"title"`) {
		t.Errorf("request body missing %q; got: %s", "title", capturedBody)
	}
	// GitHub encodes labels as a JSON array of strings, not a CSV string.
	if !strings.Contains(capturedBody, `"bug"`) || !strings.Contains(capturedBody, `"critical"`) {
		t.Errorf("request body missing label entries; got: %s", capturedBody)
	}
	if strings.Contains(capturedBody, "bug,critical") {
		t.Errorf("request body must not use comma-separated labels (GitLab format); got: %s", capturedBody)
	}
}

// TestGitHub_ListIssues_DecodesAssigneesAndAuthor verifies that user fields
// in an issue response round-trip through toUser correctly.
func TestGitHub_ListIssues_DecodesAssigneesAndAuthor(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{
			{
				"id": 5, "number": 20, "title": "Assigned", "state": "open",
				"user":      map[string]any{"id": 1, "login": "alice", "html_url": "https://github.com/alice"},
				"assignees": []map[string]any{{"id": 2, "login": "dave", "html_url": "https://github.com/dave"}},
				"labels":    []map[string]any{{"id": 9, "name": "infra", "color": "0075ca"}},
				"milestone": map[string]any{"id": 1, "title": "v2.0"},
			},
		})
	}))

	issues, err := prov.ListIssues(context.Background(), "owner/repo", provider.ListIssuesOptions{
		State:    "open",
		Labels:   []string{"infra"},
		Author:   "alice",
		Assignee: "dave",
	})
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
	if issues[0].Milestone != "v2.0" {
		t.Errorf("Milestone = %q; want %q", issues[0].Milestone, "v2.0")
	}
	if len(issues[0].Labels) != 1 || issues[0].Labels[0] != "infra" {
		t.Errorf("Labels = %v; want [infra]", issues[0].Labels)
	}
}

// TestGitHub_ListIssues_ReturnsErrorOnHTTPError verifies error propagation.
func TestGitHub_ListIssues_ReturnsErrorOnHTTPError(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"403 Forbidden"}`, http.StatusForbidden)
	}))

	_, err := prov.ListIssues(context.Background(), "owner/repo", provider.ListIssuesOptions{})
	if err == nil {
		t.Fatal("expected error from ListIssues on HTTP 403; got nil")
	}
}

// ─── Project format validation ────────────────────────────────────────────────

// TestGitHub_ProjectFormat_RejectsBadFormat asserts that any method rejects
// a project string not in "owner/repo" format with a descriptive error.
func TestGitHub_ProjectFormat_RejectsBadFormat(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	_, err := prov.ListIssues(context.Background(), "invalid", provider.ListIssuesOptions{})
	if err == nil {
		t.Fatal("expected error for invalid project format; got nil")
	}
	if !strings.Contains(err.Error(), "owner/repo") {
		t.Errorf("error = %q; want to mention %q", err.Error(), "owner/repo")
	}
}

// ─── Unsupported operations ───────────────────────────────────────────────────

// TestGitHub_CreateGroupMilestone_ReturnsErrUnsupported verifies that
// CreateGroupMilestone returns provider.ErrUnsupported on GitHub.
func TestGitHub_CreateGroupMilestone_ReturnsErrUnsupported(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	_, err := prov.CreateGroupMilestone(context.Background(), 1, provider.CreateMilestoneOptions{
		Title: "v1.0",
	})
	if !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("err = %v; want ErrUnsupported", err)
	}
}

// TestGitHub_CreateGroupEpic_ReturnsErrUnsupported verifies that
// CreateGroupEpic returns provider.ErrUnsupported on GitHub.
func TestGitHub_CreateGroupEpic_ReturnsErrUnsupported(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	_, err := prov.CreateGroupEpic(context.Background(), 1, provider.CreateEpicOptions{
		Title: "Sprint 1",
	})
	if !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("err = %v; want ErrUnsupported", err)
	}
}

// TestGitHub_ListGroupMilestones_ReturnsErrUnsupported verifies that
// ListGroupMilestones returns provider.ErrUnsupported on GitHub because
// milestones are repo-scoped; groupID has no equivalent in the GitHub API.
func TestGitHub_ListGroupMilestones_ReturnsErrUnsupported(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	_, err := prov.ListGroupMilestones(context.Background(), 1, provider.ListGroupMilestonesOptions{})
	if !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("err = %v; want ErrUnsupported", err)
	}
}

// TestGitHub_ListGroupEpics_ReturnsErrUnsupported verifies that
// ListGroupEpics returns provider.ErrUnsupported on GitHub because
// GitHub has no first-class epic concept.
func TestGitHub_ListGroupEpics_ReturnsErrUnsupported(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	_, err := prov.ListGroupEpics(context.Background(), 1, provider.ListGroupEpicsOptions{})
	if !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("err = %v; want ErrUnsupported", err)
	}
}

// ─── CreateMergeRequest / UpdateMergeRequest ───────────────────────────────────

// TestGitHub_CreateMergeRequest_SendsHeadBase verifies head/base are sent.
func TestGitHub_CreateMergeRequest_SendsHeadBase(t *testing.T) {
	t.Parallel()

	var capturedBody string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		writeJSON(w, map[string]any{"id": 50, "number": 7, "title": "PR", "state": "open"})
	}))

	mr, err := prov.CreateMergeRequest(context.Background(), "owner/repo", provider.CreateMergeRequestOptions{
		Title:        "PR",
		SourceBranch: "feat",
		TargetBranch: "main",
		Description:  "body",
	})
	if err != nil {
		t.Fatalf("CreateMergeRequest: %v", err)
	}
	if mr.IID != 7 {
		t.Errorf("IID = %d; want 7", mr.IID)
	}
	if !strings.Contains(capturedBody, `"head"`) || !strings.Contains(capturedBody, `"base"`) {
		t.Errorf("request body missing head/base; got: %s", capturedBody)
	}
}

// TestGitHub_UpdateMergeRequest_MapsClosedState verifies neutral "closed" maps to
// GitHub's state noun "closed".
func TestGitHub_UpdateMergeRequest_MapsClosedState(t *testing.T) {
	t.Parallel()

	var capturedBody string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		writeJSON(w, map[string]any{"id": 50, "number": 7, "state": "closed"})
	}))

	state := "closed"
	mr, err := prov.UpdateMergeRequest(context.Background(), "owner/repo", 7, provider.UpdateMergeRequestOptions{State: &state})
	if err != nil {
		t.Fatalf("UpdateMergeRequest: %v", err)
	}
	if mr.IID != 7 {
		t.Errorf("IID = %d; want 7", mr.IID)
	}
	if !strings.Contains(capturedBody, `"closed"`) {
		t.Errorf("request body must contain closed; got: %s", capturedBody)
	}
}

// ─── CreateLabel ──────────────────────────────────────────────────────────────

// TestGitHub_CreateLabel_StripsHashFromColor verifies that CreateLabel removes a
// leading '#' from the color before sending (GitHub stores bare hex).
func TestGitHub_CreateLabel_StripsHashFromColor(t *testing.T) {
	t.Parallel()

	var capturedBody string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		writeJSON(w, map[string]any{
			"id": 12, "name": "bug", "color": "d73a4a", "description": "a bug",
		})
	}))

	lbl, err := prov.CreateLabel(context.Background(), "owner/repo", provider.CreateLabelOptions{
		Name:        "bug",
		Color:       "#d73a4a",
		Description: "a bug",
	})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	if lbl.Name != "bug" {
		t.Errorf("lbl.Name = %q; want %q", lbl.Name, "bug")
	}
	// The adapter must strip '#'; the wire payload must carry bare hex.
	if strings.Contains(capturedBody, "#d73a4a") {
		t.Errorf("request body must not contain '#'-prefixed color; got: %s", capturedBody)
	}
	if !strings.Contains(capturedBody, "d73a4a") {
		t.Errorf("request body missing color; got: %s", capturedBody)
	}
}

// TestGitHub_CreateLabel_ReturnsErrorOn422 verifies error propagation.
func TestGitHub_CreateLabel_ReturnsErrorOn422(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"422 Unprocessable Entity"}`, http.StatusUnprocessableEntity)
	}))

	_, err := prov.CreateLabel(context.Background(), "owner/repo", provider.CreateLabelOptions{Name: "x", Color: "#fff"})
	if err == nil {
		t.Fatal("expected error from CreateLabel on HTTP 422; got nil")
	}
}

// ─── UpdateIssue ──────────────────────────────────────────────────────────────

// TestGitHub_UpdateIssue_MapsStateAndMilestone verifies that UpdateIssue sends
// the correct PATCH body: "opened" maps to "open", MilestoneID is forwarded.
func TestGitHub_UpdateIssue_MapsStateAndMilestone(t *testing.T) {
	t.Parallel()

	var capturedBody string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		writeJSON(w, map[string]any{
			"id": 300, "number": 5, "title": "Fixed", "state": "open",
		})
	}))

	title := "Fixed"
	state := "opened"
	milestoneID := 7
	iss, err := prov.UpdateIssue(context.Background(), "owner/repo", 5, provider.UpdateIssueOptions{
		Title:       &title,
		State:       &state,
		MilestoneID: &milestoneID,
	})
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if iss.IID != 5 {
		t.Errorf("IID = %d; want 5", iss.IID)
	}
	// "opened" must be translated to "open" for the GitHub API.
	if !strings.Contains(capturedBody, `"open"`) {
		t.Errorf("request body state must be %q; got: %s", "open", capturedBody)
	}
}

// TestGitHub_UpdateIssue_MilestoneByTitleResolvesNumber verifies that
// providing a Milestone title (without MilestoneID) resolves the title to
// a number via ListMilestones and sends it in the PATCH body.
func TestGitHub_UpdateIssue_MilestoneByTitleResolvesNumber(t *testing.T) {
	t.Parallel()

	var capturedMilestone any
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/milestones") && r.Method == "GET":
			writeJSON(w, []map[string]any{
				{"id": 1, "number": 1, "title": "Sprint 1"},
				{"id": 7, "number": 7, "title": "Sprint 2"},
			})
		case r.Method == "PATCH":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			capturedMilestone = body["milestone"]
			writeJSON(w, map[string]any{"id": 300, "number": 1, "title": "Fixed", "state": "open"})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))

	ms := "Sprint 2"
	_, err := prov.UpdateIssue(context.Background(), "owner/repo", 1, provider.UpdateIssueOptions{
		Milestone: &ms,
	})
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	// Sprint 2 → number 7
	if capturedMilestone != float64(7) {
		t.Errorf("milestone = %v; want 7", capturedMilestone)
	}
}

// ─── CreateIssueNote ──────────────────────────────────────────────────────────

// TestGitHub_CreateIssueNote_SendsBody verifies that CreateIssueNote posts the
// comment body and maps the response to provider.Note.
func TestGitHub_CreateIssueNote_SendsBody(t *testing.T) {
	t.Parallel()

	var capturedBody string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		writeJSON(w, map[string]any{
			"id":   999,
			"body": "LGTM",
			"user": map[string]any{"id": 1, "login": "alice", "html_url": "https://github.com/alice"},
		})
	}))

	note, err := prov.CreateIssueNote(context.Background(), "owner/repo", 5, "LGTM")
	if err != nil {
		t.Fatalf("CreateIssueNote: %v", err)
	}
	if note.ID != 999 {
		t.Errorf("note.ID = %d; want 999", note.ID)
	}
	if note.Body != "LGTM" {
		t.Errorf("note.Body = %q; want %q", note.Body, "LGTM")
	}
	if note.Author.Username != "alice" {
		t.Errorf("note.Author.Username = %q; want %q", note.Author.Username, "alice")
	}
	if !strings.Contains(capturedBody, "LGTM") {
		t.Errorf("request body missing comment body; got: %s", capturedBody)
	}
}

// TestGitHub_CreateIssueNote_ReturnsErrorOn404 verifies error propagation.
func TestGitHub_CreateIssueNote_ReturnsErrorOn404(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"404 Not Found"}`, http.StatusNotFound)
	}))

	_, err := prov.CreateIssueNote(context.Background(), "owner/repo", 1, "hi")
	if err == nil {
		t.Fatal("expected error from CreateIssueNote on HTTP 404; got nil")
	}
}

// ─── ListUsers ────────────────────────────────────────────────────────────────

// TestGitHub_ListUsers_EmptySearchReturnsErrUnsupported verifies that
// ListUsers requires a non-empty Search query on GitHub.
func TestGitHub_ListUsers_EmptySearchReturnsErrUnsupported(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	_, err := prov.ListUsers(context.Background(), provider.ListUsersOptions{Search: ""})
	if !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("err = %v; want ErrUnsupported", err)
	}
}

// TestGitHub_ListUsers_DecodesSearchResult verifies that a successful search
// maps GitHub users to provider.User correctly.
func TestGitHub_ListUsers_DecodesSearchResult(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"total_count": 1,
			"items": []map[string]any{
				{
					"id":       42,
					"login":    "alice",
					"html_url": "https://github.com/alice",
				},
			},
		})
	}))

	users, err := prov.ListUsers(context.Background(), provider.ListUsersOptions{Search: "alice", Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("len(users) = %d; want 1", len(users))
	}
	if users[0].Username != "alice" {
		t.Errorf("users[0].Username = %q; want %q", users[0].Username, "alice")
	}
	if users[0].ID != 42 {
		t.Errorf("users[0].ID = %d; want 42", users[0].ID)
	}
}

func TestGitHub_ListMyIssues_AssignedFilterStateAndRepoProject(t *testing.T) {
	t.Parallel()

	var gotFilter, gotState string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFilter = r.URL.Query().Get("filter")
		gotState = r.URL.Query().Get("state")
		writeJSON(w, []map[string]any{
			{
				"id": 1, "number": 5, "title": "Mine A", "state": "open",
				"repository": map[string]any{"full_name": "owner/repoA"},
			},
			{ // a PR must be filtered out of the issue surface
				"id": 2, "number": 6, "title": "Mine PR", "state": "open",
				"repository":   map[string]any{"full_name": "owner/repoA"},
				"pull_request": map[string]any{"url": "https://api.github.com/repos/owner/repoA/pulls/6"},
			},
			{
				"id": 3, "number": 8, "title": "Mine B", "state": "open",
				"repository": map[string]any{"full_name": "owner/repoB"},
			},
		})
	}))

	issues, err := prov.ListMyIssues(context.Background(),
		provider.ListMyIssuesOptions{State: "opened", PerPage: 50})
	if err != nil {
		t.Fatalf("ListMyIssues: %v", err)
	}
	if gotFilter != "assigned" {
		t.Errorf("filter = %q; want assigned", gotFilter)
	}
	if gotState != "open" { // neutral "opened" must map to GitHub "open"
		t.Errorf("state = %q; want open (translated from opened)", gotState)
	}
	if len(issues) != 2 {
		t.Fatalf("len(issues) = %d; want 2 (PR filtered)", len(issues))
	}
	if issues[0].Project != "owner/repoA" || issues[1].Project != "owner/repoB" {
		t.Errorf("projects = [%q,%q]; want [owner/repoA, owner/repoB]",
			issues[0].Project, issues[1].Project)
	}
}

// TestGitHub_ListIssues_MilestoneTitleResolvesNumber verifies that a
// milestone title in ListIssuesOptions is resolved to a number via
// ListMilestones and used as the milestone query parameter.
func TestGitHub_ListIssues_MilestoneTitleResolvesNumber(t *testing.T) {
	t.Parallel()

	var milestoneParam string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/milestones") && r.Method == "GET":
			writeJSON(w, []map[string]any{
				{"id": 1, "number": 5, "title": "v1.0"},
			})
		case strings.Contains(r.URL.Path, "/issues") && r.Method == "GET":
			milestoneParam = r.URL.Query().Get("milestone")
			writeJSON(w, []map[string]any{})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))

	_, err := prov.ListIssues(context.Background(), "owner/repo",
		provider.ListIssuesOptions{Milestone: "v1.0"})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if milestoneParam != "5" {
		t.Errorf("milestone query param = %q; want 5", milestoneParam)
	}
}

// TestGitHub_ListMyMergeRequests_UsesSearchAPI verifies that ListMyMergeRequests
// queries the Search API with is:pr assignee:@me and maps results to MergeRequest.
func TestGitHub_ListMyMergeRequests_UsesSearchAPI(t *testing.T) {
	t.Parallel()

	var query string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/search/issues") {
			query = r.URL.Query().Get("q")
			writeJSON(w, map[string]any{
				"total_count": 1,
				"items": []map[string]any{
					{
						"id":     100,
						"number": 42,
						"title":  "Fix bug",
						"state":  "open",
						"html_url": "https://github.com/owner/repo/pull/42",
						"repository": map[string]any{
							"full_name": "owner/repo",
						},
						"pull_request": map[string]any{"url": "https://api.github.com/repos/owner/repo/pulls/42"},
						"user": map[string]any{"id": 1, "login": "alice"},
					},
				},
			})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))

	mrs, err := prov.ListMyMergeRequests(context.Background(), provider.ListMyMergeRequestsOptions{State: "opened"})
	if err != nil {
		t.Fatalf("ListMyMergeRequests: %v", err)
	}
	if !strings.Contains(query, "is:pr") || !strings.Contains(query, "assignee:@me") {
		t.Errorf("query = %q; must contain is:pr and assignee:@me", query)
	}
	if !strings.Contains(query, "is:open") {
		t.Errorf("query = %q; opened state must map to is:open", query)
	}
	if len(mrs) != 1 {
		t.Fatalf("len(mrs) = %d; want 1", len(mrs))
	}
	if mrs[0].IID != 42 || mrs[0].Project != "owner/repo" {
		t.Errorf("mr = %+v; want IID=42 Project=owner/repo", mrs[0])
	}
}

// TestGitHub_ApproveMergeRequest_CreatesReview verifies that ApproveMergeRequest
// POSTs a review with event=APPROVE to the pulls reviews endpoint.
func TestGitHub_ApproveMergeRequest_CreatesReview(t *testing.T) {
	t.Parallel()

	var event string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// POST /repos/owner/repo/pulls/3/reviews
		if strings.HasSuffix(r.URL.Path, "/pulls/3/reviews") && r.Method == "POST" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			event, _ = body["event"].(string)
			writeJSON(w, map[string]any{"id": 1, "state": "APPROVED"})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))

	err := prov.ApproveMergeRequest(context.Background(), "owner/repo", 3)
	if err != nil {
		t.Fatalf("ApproveMergeRequest: %v", err)
	}
	if event != "APPROVE" {
		t.Errorf("review event = %q; want APPROVE", event)
	}
}

func TestGitHub_MergeMergeRequest_MergesAndReturnsPR(t *testing.T) {
	t.Parallel()

	var mergeMethod, mergeHTTP string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut: // PullRequests.Merge
			mergeHTTP = r.Method
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if m, ok := body["merge_method"].(string); ok {
				mergeMethod = m
			}
			writeJSON(w, map[string]any{"merged": true, "sha": "abc123"})
		default: // PullRequests.Get (post-merge re-fetch)
			writeJSON(w, map[string]any{
				"id": 1, "number": 3, "title": "PR A", "state": "closed",
				"merged": true,
			})
		}
	}))

	mr, err := prov.MergeMergeRequest(context.Background(), "owner/repo", 3,
		provider.MergeOptions{Squash: true})
	if err != nil {
		t.Fatalf("MergeMergeRequest: %v", err)
	}
	if mergeHTTP != http.MethodPut {
		t.Errorf("merge HTTP method = %q; want PUT", mergeHTTP)
	}
	if mergeMethod != "squash" {
		t.Errorf("merge_method = %q; want squash", mergeMethod)
	}
	if mr.IID != 3 {
		t.Errorf("mr.IID = %d; want 3", mr.IID)
	}
}

func TestGitHub_GetMergeRequestDiff_MapsStatusToFlags(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []map[string]any{
			{"filename": "added.go", "status": "added", "patch": "@@ +1 @@"},
			{"filename": "new.go", "previous_filename": "old.go", "status": "renamed", "patch": ""},
			{"filename": "gone.go", "status": "removed", "patch": "@@ -1 @@"},
		})
	}))

	diffs, err := prov.GetMergeRequestDiff(context.Background(), "owner/repo", 3)
	if err != nil {
		t.Fatalf("GetMergeRequestDiff: %v", err)
	}
	if len(diffs) != 3 {
		t.Fatalf("len(diffs) = %d; want 3", len(diffs))
	}
	if !diffs[0].NewFile || diffs[0].NewPath != "added.go" {
		t.Errorf("diffs[0] = %+v; want NewFile added.go", diffs[0])
	}
	if !diffs[1].RenamedFile || diffs[1].OldPath != "old.go" || diffs[1].NewPath != "new.go" {
		t.Errorf("diffs[1] = %+v; want rename old.go→new.go", diffs[1])
	}
	if !diffs[2].DeletedFile {
		t.Errorf("diffs[2] = %+v; want DeletedFile", diffs[2])
	}
}

func TestGitHub_ListMilestones_DecodesResponse(t *testing.T) {
	t.Parallel()

	var gotState, gotPath string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotState = r.URL.Query().Get("state")
		writeJSON(w, []map[string]any{
			{"id": 11, "number": 1, "title": "Sprint 1", "state": "open",
				"description": "First sprint",
				"html_url":    "https://github.com/owner/repo/milestone/1",
				"due_on":      "2026-01-14T00:00:00Z"},
		})
	}))

	ms, err := prov.ListMilestones(context.Background(), "owner/repo",
		provider.ListMilestonesOptions{State: "open", PerPage: 20})
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	if len(ms) != 1 {
		t.Fatalf("len(ms) = %d; want 1", len(ms))
	}
	if ms[0].Title != "Sprint 1" || ms[0].State != "open" {
		t.Errorf("ms[0] = %+v", ms[0])
	}
	if ms[0].DueDate != "2026-01-14" {
		t.Errorf("ms[0].DueDate = %q; want 2026-01-14", ms[0].DueDate)
	}
	if ms[0].WebURL == "" {
		t.Errorf("ms[0].WebURL should be populated")
	}
	if gotState != "open" {
		t.Errorf("state query = %q; want open", gotState)
	}
	if !strings.Contains(gotPath, "/milestones") {
		t.Errorf("path = %q; want milestones endpoint", gotPath)
	}
}

func TestGitHub_ListMilestones_DefaultsStateToAll(t *testing.T) {
	t.Parallel()

	var gotState string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotState = r.URL.Query().Get("state")
		writeJSON(w, []map[string]any{})
	}))

	if _, err := prov.ListMilestones(context.Background(), "owner/repo", provider.ListMilestonesOptions{}); err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	if gotState != "all" {
		t.Errorf("state = %q; want all (default)", gotState)
	}
}

func TestGitHub_ListMilestones_RejectsBadProject(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler should not be called for bad project")
	}))

	if _, err := prov.ListMilestones(context.Background(), "no-slash", provider.ListMilestonesOptions{}); err == nil {
		t.Fatal("expected error for project without slash")
	}
}

func TestGitHub_ResolveGroup_Unsupported(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if _, err := prov.ResolveGroup(context.Background(), "owner"); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("ResolveGroup err = %v; want ErrUnsupported", err)
	}
}

func TestGitHub_GetProject_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/repos/owner/repo") {
			t.Errorf("path = %q; want /repos/owner/repo", r.URL.Path)
		}
		writeJSON(w, map[string]any{
			"id":             1234,
			"name":           "repo",
			"full_name":      "owner/repo",
			"description":    "demo repo",
			"html_url":       "https://github.com/owner/repo",
			"default_branch": "main",
			"open_issues":    5,
			"archived":       false,
		})
	}))

	p, err := prov.GetProject(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if p.ID != 1234 || p.Name != "repo" || p.Path != "owner/repo" ||
		p.FullName != "owner/repo" || p.Description != "demo repo" ||
		p.WebURL != "https://github.com/owner/repo" ||
		p.DefaultBranch != "main" || p.OpenIssuesCount != 5 || p.Archived {
		t.Errorf("project = %+v", p)
	}
}

func TestGitHub_GetProject_RejectsBadProject(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if _, err := prov.GetProject(context.Background(), "no-slash"); err == nil {
		t.Fatal("GetProject: expected error for malformed project, got nil")
	}
}

func TestGitHub_GetProject_ReturnsErrorOn404(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))

	if _, err := prov.GetProject(context.Background(), "owner/missing"); err == nil {
		t.Fatal("GetProject: expected error for 404, got nil")
	}
}

func TestGitHub_ListWorkItems_Unsupported(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if _, err := prov.ListWorkItems(context.Background(), "owner/repo", provider.ListWorkItemsOptions{}); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("ListWorkItems err = %v; want ErrUnsupported", err)
	}
}

func TestGitHub_CreateWorkItem_Unsupported(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if _, err := prov.CreateWorkItem(context.Background(), "owner/repo", provider.CreateWorkItemOptions{Title: "x"}); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("CreateWorkItem err = %v; want ErrUnsupported", err)
	}
}

func TestGitHub_CloseWorkItem_Unsupported(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if err := prov.CloseWorkItem(context.Background(), "owner/repo", 1); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("CloseWorkItem err = %v; want ErrUnsupported", err)
	}
}

// ─── GetMilestone ─────────────────────────────────────────────────────────────

func TestGitHub_GetMilestone_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/milestones/5") {
			t.Errorf("path = %q; want .../milestones/5", r.URL.Path)
		}
		writeJSON(w, map[string]any{
			"id": 5, "number": 5, "title": "Sprint 1", "state": "open",
			"description": "first sprint",
			"due_on":      "2026-02-01T00:00:00Z",
		})
	}))

	m, err := prov.GetMilestone(context.Background(), "owner/repo", 5)
	if err != nil {
		t.Fatalf("GetMilestone: %v", err)
	}
	if m.ID != 5 || m.Title != "Sprint 1" || m.State != "open" ||
		m.Description != "first sprint" || m.DueDate != "2026-02-01" {
		t.Errorf("milestone = %+v", m)
	}
}

func TestGitHub_GetMilestone_RejectsBadProject(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if _, err := prov.GetMilestone(context.Background(), "no-slash", 1); err == nil {
		t.Fatal("GetMilestone: expected error for malformed project, got nil")
	}
}

// ─── CreateMilestone ──────────────────────────────────────────────────────────

func TestGitHub_CreateMilestone_SendsTitleAndDueDate(t *testing.T) {
	t.Parallel()

	var gotTitle string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q; want POST", r.Method)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTitle, _ = body["title"].(string)
		writeJSON(w, map[string]any{
			"id": 10, "number": 10, "title": gotTitle, "state": "open",
		})
	}))

	m, err := prov.CreateMilestone(context.Background(), "owner/repo", provider.CreateMilestoneOptions{
		Title:       "Sprint 2",
		Description: "second sprint",
		DueDate:     "2026-04-14",
	})
	if err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}
	if m.ID != 10 || m.Title != "Sprint 2" {
		t.Errorf("milestone = %+v", m)
	}
	if gotTitle != "Sprint 2" {
		t.Errorf("body title = %q; want Sprint 2", gotTitle)
	}
}

func TestGitHub_CreateMilestone_RejectsBadDueDate(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if _, err := prov.CreateMilestone(context.Background(), "owner/repo", provider.CreateMilestoneOptions{
		Title:   "x",
		DueDate: "not-a-date",
	}); err == nil {
		t.Fatal("CreateMilestone: expected error for bad due date, got nil")
	}
}

// ─── UpdateMilestone ──────────────────────────────────────────────────────────

func TestGitHub_UpdateMilestone_SendsTitleAndState(t *testing.T) {
	t.Parallel()

	var gotTitle, gotState string
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %q; want PATCH", r.Method)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTitle, _ = body["title"].(string)
		gotState, _ = body["state"].(string)
		writeJSON(w, map[string]any{
			"id": 5, "number": 5, "title": gotTitle, "state": gotState,
		})
	}))

	title := "Renamed"
	state := "close"
	m, err := prov.UpdateMilestone(context.Background(), "owner/repo", 5, provider.UpdateMilestoneOptions{
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
	if gotState != "closed" {
		t.Errorf("body state = %q; want closed", gotState)
	}
}

// ─── Epic stubs (unsupported) ─────────────────────────────────────────────────

func TestGitHub_LinkIssueToEpic_Unsupported(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if err := prov.LinkIssueToEpic(context.Background(), 1, 2, 3); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("LinkIssueToEpic err = %v; want ErrUnsupported", err)
	}
}

func TestGitHub_UpdateGroupEpic_Unsupported(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if _, err := prov.UpdateGroupEpic(context.Background(), 1, 2, provider.UpdateEpicOptions{}); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("UpdateGroupEpic err = %v; want ErrUnsupported", err)
	}
}

func TestGitHub_ListEpicIssues_Unsupported(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if _, err := prov.ListEpicIssues(context.Background(), 1, 2); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("ListEpicIssues err = %v; want ErrUnsupported", err)
	}
}

// ─── ListPipelines ────────────────────────────────────────────────────────────

func TestGitHub_ListPipelines_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/actions/runs") {
			t.Errorf("path = %q; want .../actions/runs", r.URL.Path)
		}
		writeJSON(w, map[string]any{
			"total_count": 1,
			"workflow_runs": []map[string]any{
				{
					"id":           100,
					"status":       "completed",
					"conclusion":   "success",
					"head_branch":  "main",
					"head_sha":     "abc123",
					"html_url":     "https://github.com/owner/repo/actions/runs/100",
					"created_at":   "2026-01-01T00:00:00Z",
					"updated_at":   "2026-01-01T00:05:00Z",
					"run_number":   1,
					"event":        "push",
					"display_title": "CI",
				},
			},
		})
	}))

	pipes, err := prov.ListPipelines(context.Background(), "owner/repo", provider.ListPipelinesOptions{
		Page: 1, PerPage: 20,
	})
	if err != nil {
		t.Fatalf("ListPipelines: %v", err)
	}
	if len(pipes) != 1 {
		t.Fatalf("pipelines len = %d; want 1", len(pipes))
	}
	if pipes[0].ID != 100 || pipes[0].Status != "success" || pipes[0].Ref != "main" ||
		pipes[0].SHA != "abc123" {
		t.Errorf("pipeline = %+v", pipes[0])
	}
}

func TestGitHub_ListPipelines_RejectsBadProject(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if _, err := prov.ListPipelines(context.Background(), "no-slash", provider.ListPipelinesOptions{}); err == nil {
		t.Fatal("ListPipelines: expected error for malformed project, got nil")
	}
}

// ─── GetPipeline ──────────────────────────────────────────────────────────────

func TestGitHub_GetPipeline_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"id":           200,
			"status":       "in_progress",
			"conclusion":   nil,
			"head_branch":  "feature",
			"head_sha":     "def456",
			"html_url":     "https://github.com/owner/repo/actions/runs/200",
			"created_at":   "2026-01-01T00:00:00Z",
			"updated_at":   "2026-01-01T00:05:00Z",
			"run_number":   2,
			"event":        "pull_request",
			"display_title": "PR CI",
		})
	}))

	p, err := prov.GetPipeline(context.Background(), "owner/repo", 200)
	if err != nil {
		t.Fatalf("GetPipeline: %v", err)
	}
	if p.ID != 200 || p.Status != "running" || p.Ref != "feature" {
		t.Errorf("pipeline = %+v", p)
	}
}

// ─── RunPipeline (unsupported) ────────────────────────────────────────────────

func TestGitHub_RunPipeline_Unsupported(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if _, err := prov.RunPipeline(context.Background(), "owner/repo", provider.RunPipelineOptions{}); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("RunPipeline err = %v; want ErrUnsupported", err)
	}
}

// ─── RetryPipeline ────────────────────────────────────────────────────────────

func TestGitHub_RetryPipeline_RerunsAndFetches(t *testing.T) {
	t.Parallel()

	var rerunCalled bool
	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/rerun") {
			rerunCalled = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		writeJSON(w, map[string]any{
			"id":           300,
			"status":       "queued",
			"conclusion":   nil,
			"head_branch":  "main",
			"head_sha":     "abc123",
			"html_url":     "https://github.com/owner/repo/actions/runs/300",
			"created_at":   "2026-01-01T00:00:00Z",
			"updated_at":   "2026-01-01T00:05:00Z",
			"run_number":   3,
			"event":        "push",
			"display_title": "CI",
		})
	}))

	p, err := prov.RetryPipeline(context.Background(), "owner/repo", 300)
	if err != nil {
		t.Fatalf("RetryPipeline: %v", err)
	}
	if !rerunCalled {
		t.Error("rerun endpoint was not called")
	}
	if p.ID != 300 || p.Status != "pending" {
		t.Errorf("pipeline = %+v", p)
	}
}

// ─── CancelPipeline ───────────────────────────────────────────────────────────

func TestGitHub_CancelPipeline_SendsCancel(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q; want POST", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))

	if err := prov.CancelPipeline(context.Background(), "owner/repo", 400); err != nil {
		t.Fatalf("CancelPipeline: %v", err)
	}
}

// ─── ListPipelineJobs ─────────────────────────────────────────────────────────

func TestGitHub_ListPipelineJobs_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"total_count": 1,
			"jobs": []map[string]any{
				{
					"id":          500,
					"status":      "completed",
					"conclusion":  "success",
					"name":        "build",
					"started_at":  "2026-01-01T00:00:00Z",
					"completed_at": "2026-01-01T00:02:00Z",
					"html_url":    "https://github.com/owner/repo/actions/runs/100/job/500",
				},
			},
		})
	}))

	jobs, err := prov.ListPipelineJobs(context.Background(), "owner/repo", 100)
	if err != nil {
		t.Fatalf("ListPipelineJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs len = %d; want 1", len(jobs))
	}
	if jobs[0].ID != 500 || jobs[0].Name != "build" || jobs[0].Status != "success" {
		t.Errorf("job = %+v", jobs[0])
	}
}

// ─── ListArtifacts ────────────────────────────────────────────────────────────

func TestGitHub_ListArtifacts_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"total_count": 1,
			"artifacts": []map[string]any{
				{
					"id": 600, "name": "build-output", "size_in_bytes": 1024,
					"expired": false,
				},
			},
		})
	}))

	arts, err := prov.ListArtifacts(context.Background(), "owner/repo", 100)
	if err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	if len(arts) != 1 {
		t.Fatalf("artifacts len = %d; want 1", len(arts))
	}
	if arts[0].Name != "build-output" || arts[0].Size != 1024 || arts[0].Expired {
		t.Errorf("artifact = %+v", arts[0])
	}
}
