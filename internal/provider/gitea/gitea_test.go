package gitea

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	giteasdk "gitea.dev/sdk"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/provider"
)

// diffRaw is a two-file unified diff used by GetMergeRequestDiff tests.
// File 1 is an addition (main.go); file 2 is a rename (renamed.go → old.go).
const diffRaw = `diff --git a/main.go b/main.go
index 0000000..1111111 100644
--- /dev/null
+++ b/main.go
@@ -0,0 +1,2 @@
+package main
+
func main() {}
diff --git a/renamed.go b/old.go
index 2222222..3333333 100644
--- a/renamed.go
+++ b/old.go
@@ -1,3 +1,3 @@
 package old
-func old() {}
+func new() {}
`

func TestSplitProject(t *testing.T) {
	tests := []struct {
		input      string
		wantOwner  string
		wantRepo   string
		wantErr    bool
		errContain string
	}{
		{"owner/repo", "owner", "repo", false, ""},
		{"/repo", "", "", true, "owner/repo"},
		{"owner/", "", "", true, "owner/repo"},
		{"owner/repo/extra", "owner", "repo/extra", false, ""},
		{"norepo", "", "", true, "owner/repo"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			owner, repo, err := splitProject(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("splitProject(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				if !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContain)
				}
				return
			}
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("splitProject(%q) = (%q, %q), want (%q, %q)",
					tt.input, owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

func TestGiteaState(t *testing.T) {
	tests := []struct {
		input string
		want  giteasdk.StateType
	}{
		{"opened", giteasdk.StateOpen},
		{"open", giteasdk.StateOpen},
		{"closed", giteasdk.StateClosed},
		{"close", giteasdk.StateClosed},
		{"all", giteasdk.StateAll},
		{"", giteasdk.StateOpen},
		{"merged", giteasdk.StateOpen},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := giteaState(tt.input)
			if got != tt.want {
				t.Errorf("giteaState(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToIssue(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	closed := now.Add(2 * time.Hour)
	iss := &giteasdk.Issue{
		ID:      12345,
		Index:   7,
		Title:   "Fix bug",
		Body:    "description",
		State:   giteasdk.StateOpen,
		HTMLURL: "https://gitea.example.com/owner/repo/issues/7",
		Poster: &giteasdk.User{
			ID:       1,
			UserName: "alice",
			FullName: "Alice",
		},
		Assignees: []*giteasdk.User{
			{ID: 2, UserName: "bob"},
		},
		Labels: []*giteasdk.Label{
			{ID: 10, Name: "bug", Color: "ff0000"},
		},
		Milestone: &giteasdk.Milestone{
			ID:    5,
			Title: "v1.0",
		},
		Created: now,
		Updated: now,
		Closed:  &closed,
	}

	got := toIssue("owner/repo", iss)
	if got.IID != 7 {
		t.Errorf("IID = %d, want 7", got.IID)
	}
	if got.Project != "owner/repo" {
		t.Errorf("Project = %q, want owner/repo", got.Project)
	}
	if got.Author.Username != "alice" {
		t.Errorf("Author.Username = %q, want alice", got.Author.Username)
	}
	if len(got.Assignees) != 1 || got.Assignees[0].Username != "bob" {
		t.Errorf("Assignees = %v, want [bob]", got.Assignees)
	}
	if len(got.Labels) != 1 || got.Labels[0] != "bug" {
		t.Errorf("Labels = %v, want [bug]", got.Labels)
	}
	if got.Milestone != "v1.0" {
		t.Errorf("Milestone = %q, want v1.0", got.Milestone)
	}
	if got.ClosedAt == nil || !got.ClosedAt.Equal(closed) {
		t.Errorf("ClosedAt = %v, want %v", got.ClosedAt, closed)
	}
}

func TestToIssue_ProjectFromRepo(t *testing.T) {
	iss := &giteasdk.Issue{
		Repository: &giteasdk.RepositoryMeta{
			FullName: "other/repo",
		},
	}
	got := toIssue("", iss)
	if got.Project != "other/repo" {
		t.Errorf("Project = %q, want other/repo", got.Project)
	}
}

func TestToPR(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	pr := &giteasdk.PullRequest{
		ID:     42,
		Index:  3,
		Title:  "Feature",
		Body:   "pr body",
		State:  giteasdk.StateOpen,
		HTMLURL: "https://gitea.example.com/owner/repo/pulls/3",
		Poster: &giteasdk.User{ID: 1, UserName: "alice"},
		Head:   &giteasdk.PRBranchInfo{Ref: "feature"},
		Base:   &giteasdk.PRBranchInfo{Ref: "main"},
		Labels: []*giteasdk.Label{{Name: "enhancement"}},
		Created: &now,
		Updated: &now,
	}

	got := toPR("owner/repo", pr)
	if got.IID != 3 {
		t.Errorf("IID = %d, want 3", got.IID)
	}
	if got.SourceBranch != "feature" || got.TargetBranch != "main" {
		t.Errorf("SourceBranch=%q TargetBranch=%q, want feature/main", got.SourceBranch, got.TargetBranch)
	}
	if len(got.Labels) != 1 || got.Labels[0] != "enhancement" {
		t.Errorf("Labels = %v, want [enhancement]", got.Labels)
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, now)
	}
}

func TestToLabel(t *testing.T) {
	tests := []struct {
		color string
		want  string
	}{
		{"00aabb", "#00aabb"},
		{"#00aabb", "#00aabb"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.color, func(t *testing.T) {
			got := toLabel(&giteasdk.Label{ID: 1, Name: "bug", Color: tt.color, Description: "defect"})
			if got.Color != tt.want {
				t.Errorf("Color = %q, want %q", got.Color, tt.want)
			}
		})
	}
}

func TestToUser(t *testing.T) {
	u := &giteasdk.User{
		ID:       99,
		UserName: "alice",
		FullName: "Alice Smith",
		Email:    "alice@example.com",
		HTMLURL:  "https://gitea.example.com/alice",
	}
	got := toUser(u)
	if got.ID != 99 || got.Username != "alice" || got.Name != "Alice Smith" || got.Email != "alice@example.com" {
		t.Errorf("toUser mismatch: %+v", got)
	}
}

func TestToNote(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c := &giteasdk.Comment{
		ID:      5,
		Body:    "note",
		Created: now,
		Updated: now,
		Poster:  &giteasdk.User{UserName: "alice"},
	}
	got := toNote(c)
	if got.ID != 5 || got.Body != "note" || got.Author.Username != "alice" {
		t.Errorf("toNote mismatch: %+v", got)
	}
}

func TestHasAllLabelNames(t *testing.T) {
	labels := []*giteasdk.Label{
		{Name: "bug"},
		{Name: "help wanted"},
	}
	if !hasAllLabelNames(labels, []string{"bug"}) {
		t.Error("expected to find bug")
	}
	if !hasAllLabelNames(labels, []string{"bug", "help wanted"}) {
		t.Error("expected to find both labels")
	}
	if hasAllLabelNames(labels, []string{"bug", "feature"}) {
		t.Error("did not expect to find feature")
	}
}

func TestDerefTime(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if got := derefTime(&now); !got.Equal(now) {
		t.Errorf("derefTime(&t) = %v, want %v", got, now)
	}
	if got := derefTime(nil); !got.IsZero() {
		t.Errorf("derefTime(nil) = %v, want zero", got)
	}
}

// Compile-time interface satisfaction is already checked in the provider package,
// but an additional local check keeps the gitea package self-contained.
var _ provider.Provider = (*Gitea)(nil)

// ─── HTTP-test-backed provider surface tests ─────────────────────────────────

func newTestGitea(t *testing.T) (*Gitea, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		testGiteaHandler(t, w, r)
	})
	srv := httptest.NewServer(mux)
	client, err := giteasdk.NewClient(srv.URL,
		giteasdk.SetToken("test-token"),
		giteasdk.SetGiteaVersion("1.26.0"),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return &Gitea{client: client, host: "gitea.example.com"}, srv
}

func testGiteaHandler(t *testing.T, w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1")
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	issueJSON := func(id int64) string {
		return fmt.Sprintf(`{
			"id": %d,
			"number": %d,
			"title": "issue %d",
			"body": "body",
			"state": "open",
			"user": {"id": 1, "login": "alice", "full_name": "Alice", "email": "alice@example.com"},
			"assignees": [{"id": 2, "login": "bob"}],
			"labels": [{"id": 10, "name": "bug", "color": "ff0000"}],
			"milestone": {"id": 5, "title": "v1.0"},
			"created_at": "%s",
			"updated_at": "%s"
		}`, id, id, id, now.Format(time.RFC3339), now.Format(time.RFC3339))
	}

	prJSON := func(id int64) string {
		return fmt.Sprintf(`{
			"id": %d,
			"number": %d,
			"title": "pr %d",
			"body": "pr body",
			"state": "open",
			"user": {"id": 1, "login": "alice"},
			"head": {"ref": "feature", "sha": "abc123"},
			"base": {"ref": "main", "sha": "def456"},
			"labels": [{"id": 11, "name": "enhancement", "color": "00ff00"}],
			"created_at": "%s",
			"updated_at": "%s"
		}`, id, id, id, now.Format(time.RFC3339), now.Format(time.RFC3339))
	}

	labelJSON := `[{"id": 10, "name": "bug", "color": "ff0000"}, {"id": 11, "name": "enhancement", "color": "00ff00"}]`
	userJSON := `{"data": [{"id": 1, "login": "alice", "full_name": "Alice", "email": "alice@example.com"}]}`
	milestoneJSON := `{"id": 5, "title": "v1.0"}`
	commentJSON := fmt.Sprintf(`{"id": 8, "body": "note", "user": {"id": 1, "login": "alice"}, "created_at": "%s", "updated_at": "%s"}`, now.Format(time.RFC3339), now.Format(time.RFC3339))

	switch {
	case path == "/version":
		fmt.Fprint(w, `{"version": "1.26.0"}`)
	case path == "/repos/issues/search":
		// ListMyIssues sends type=issues; ListMyMergeRequests sends type=pulls.
		if r.URL.Query().Get("type") == "pulls" {
			// Return a PR-shaped issue with a repository field so
			// ListMyMergeRequests can extract owner/repo and fetch the full PR.
			fmt.Fprintf(w, `[%s]`, fmt.Sprintf(`{
				"id": 50, "number": 3, "title": "pr 3", "body": "pr body", "state": "open",
				"user": {"id": 1, "login": "alice"},
				"pull_request": {"merged": false},
				"repository": {"id": 99, "name": "repo", "owner": "owner", "full_name": "owner/repo"},
				"created_at": "%s", "updated_at": "%s"
			}`, now.Format(time.RFC3339), now.Format(time.RFC3339)))
		} else {
			fmt.Fprintf(w, "[%s]", issueJSON(1))
		}
	case path == "/repos/owner/repo/issues" && r.Method == "GET":
		fmt.Fprintf(w, "[%s]", issueJSON(1))
	case path == "/repos/owner/repo/issues" && r.Method == "POST":
		fmt.Fprint(w, issueJSON(2))
	case strings.HasPrefix(path, "/repos/owner/repo/issues/") && (r.Method == "GET" || r.Method == "PATCH"):
		fmt.Fprint(w, issueJSON(7))
	case path == "/repos/owner/repo/labels" && r.Method == "GET":
		fmt.Fprint(w, labelJSON)
	case path == "/repos/owner/repo/labels" && r.Method == "POST":
		fmt.Fprint(w, `{"id": 12, "name": "new-label", "color": "0000ff"}`)
	case strings.HasSuffix(path, "/labels") && (r.Method == "PUT" || r.Method == "POST"):
		fmt.Fprint(w, labelJSON)
	case strings.HasSuffix(path, "/comments") && r.Method == "POST":
		fmt.Fprint(w, commentJSON)
	case path == "/repos/owner/repo/pulls" && r.Method == "GET":
		fmt.Fprintf(w, "[%s]", prJSON(3))
	case path == "/repos/owner/repo/pulls" && r.Method == "POST":
		fmt.Fprint(w, prJSON(4))
	case strings.HasSuffix(path, "/pulls/3/files") && r.Method == "GET":
		fmt.Fprint(w, `[{"filename":"main.go","previous_filename":"","status":"added","additions":2,"deletions":0,"changes":2},{"filename":"old.go","previous_filename":"renamed.go","status":"renamed","additions":1,"deletions":1,"changes":2}]`)
	case strings.HasSuffix(path, "/pulls/3.diff") && r.Method == "GET":
		fmt.Fprint(w, diffRaw)
	case strings.HasPrefix(path, "/repos/owner/repo/pulls/") && r.Method == "GET":
		fmt.Fprint(w, prJSON(3))
	case strings.HasPrefix(path, "/repos/owner/repo/pulls/") && r.Method == "PATCH":
		fmt.Fprint(w, prJSON(3))
	case strings.HasSuffix(path, "/reviews") && r.Method == "POST":
		fmt.Fprint(w, `{"id": 9, "state": "APPROVED"}`)
	case strings.HasSuffix(path, "/merge") && r.Method == "POST":
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"merged": true}`)
	case path == "/users/search":
		fmt.Fprint(w, userJSON)
	case path == "/repos/owner/repo/milestones" && r.Method == "GET":
		fmt.Fprint(w, `[{"id": 5, "title": "v1.0", "description": "First release", "state": "open", "due_on": "2026-01-14T00:00:00Z"}]`)
	case path == "/repos/owner/repo/milestones" && r.Method == "POST":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		title, _ := body["title"].(string)
		fmt.Fprintf(w, `{"id": 10, "title": %q, "state": "open"}`, title)
	case strings.HasPrefix(path, "/repos/owner/repo/milestones/") && r.Method == "PATCH":
		fmt.Fprint(w, `{"id": 5, "title": "Renamed", "state": "closed"}`)
	case strings.HasPrefix(path, "/repos/owner/repo/milestones/") && r.Method == "GET":
		fmt.Fprint(w, milestoneJSON)
	case path == "/repos/owner/repo/actions/runs" && r.Method == "GET":
		fmt.Fprint(w, `{"workflow_runs": [{"id": 100, "status": "success", "conclusion": "success", "head_branch": "main", "head_sha": "abc123", "html_url": "https://gitea.example.com/owner/repo/actions/runs/100", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:05:00Z"}]}`)
	case strings.HasPrefix(path, "/repos/owner/repo/actions/runs/") && strings.HasSuffix(path, "/rerun") && r.Method == "POST":
		fmt.Fprint(w, `{"id": 300, "status": "queued", "conclusion": "", "head_branch": "main", "head_sha": "abc123", "html_url": "https://gitea.example.com/owner/repo/actions/runs/300"}`)
	case strings.HasPrefix(path, "/repos/owner/repo/actions/runs/") && strings.HasSuffix(path, "/jobs") && r.Method == "GET":
		fmt.Fprint(w, `{"jobs": [{"id": 500, "name": "build", "status": "success", "conclusion": "success", "head_branch": "main", "html_url": "https://gitea.example.com/owner/repo/actions/runs/100/jobs/500", "started_at": "2026-01-01T00:00:00Z", "completed_at": "2026-01-01T00:02:00Z"}]}`)
	case strings.HasPrefix(path, "/repos/owner/repo/actions/runs/") && strings.HasSuffix(path, "/artifacts") && r.Method == "GET":
		fmt.Fprint(w, `{"artifacts": [{"id": 600, "name": "build-output", "size_in_bytes": 1024, "expired": false}]}`)
	case strings.HasPrefix(path, "/repos/owner/repo/actions/artifacts/") && strings.HasSuffix(path, "/zip") && r.Method == "GET":
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write([]byte("PK\x03\x04fake-zip"))
	case strings.HasPrefix(path, "/repos/owner/repo/actions/jobs/") && strings.HasSuffix(path, "/logs") && r.Method == "GET":
		fmt.Fprint(w, "log line 1\nlog line 2\n")
	case strings.HasPrefix(path, "/repos/owner/repo/actions/runs/") && r.Method == "GET":
		// Single run by ID
		fmt.Fprint(w, `{"id": 200, "status": "in_progress", "conclusion": "", "head_branch": "feature", "head_sha": "def456", "html_url": "https://gitea.example.com/owner/repo/actions/runs/200", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:05:00Z"}`)
	case path == "/repos/owner/repo" && r.Method == "GET":
		fmt.Fprint(w, `{"id": 99, "name": "repo", "full_name": "owner/repo", "description": "demo", "html_url": "https://gitea.example.com/owner/repo", "default_branch": "main", "open_issues_count": 3, "archived": false}`)
	default:
		t.Logf("unhandled %s %s", r.Method, path)
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func TestGitea_ListIssues(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	issues, err := g.ListIssues(context.Background(), "owner/repo", provider.ListIssuesOptions{State: "opened"})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 1 || issues[0].IID != 1 {
		t.Errorf("ListIssues = %+v", issues)
	}
}

func TestGitea_ListMyIssues(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	issues, err := g.ListMyIssues(context.Background(), provider.ListMyIssuesOptions{State: "opened"})
	if err != nil {
		t.Fatalf("ListMyIssues: %v", err)
	}
	if len(issues) != 1 {
		t.Errorf("ListMyIssues = %d issues, want 1", len(issues))
	}
}

func TestGitea_GetIssue(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	issue, err := g.GetIssue(context.Background(), "owner/repo", 7)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.IID != 7 {
		t.Errorf("GetIssue IID = %d, want 7", issue.IID)
	}
}

func TestGitea_CreateIssue(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	issue, err := g.CreateIssue(context.Background(), "owner/repo", provider.CreateIssueOptions{
		Title:       "New issue",
		Description: "desc",
		Labels:      []string{"bug"},
		Milestone:   "v1.0",
		AssigneeIDs: []int{2},
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if issue.IID != 2 {
		t.Errorf("CreateIssue IID = %d, want 2", issue.IID)
	}
}

func TestGitea_UpdateIssue(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	closed := "closed"
	issue, err := g.UpdateIssue(context.Background(), "owner/repo", 7, provider.UpdateIssueOptions{
		Title:       stringPtr("updated"),
		Description: stringPtr("new body"),
		State:       &closed,
		Labels:      &[]string{"bug"},
		Milestone:   stringPtr("v1.0"),
	})
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if issue.IID != 7 {
		t.Errorf("UpdateIssue IID = %d, want 7", issue.IID)
	}
}

func TestGitea_CreateIssueNote(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	note, err := g.CreateIssueNote(context.Background(), "owner/repo", 7, "note")
	if err != nil {
		t.Fatalf("CreateIssueNote: %v", err)
	}
	if note.Body != "note" {
		t.Errorf("CreateIssueNote Body = %q, want note", note.Body)
	}
}

func TestGitea_ListMergeRequests(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	prs, err := g.ListMergeRequests(context.Background(), "owner/repo", provider.ListMergeRequestsOptions{State: "opened"})
	if err != nil {
		t.Fatalf("ListMergeRequests: %v", err)
	}
	if len(prs) != 1 || prs[0].IID != 3 {
		t.Errorf("ListMergeRequests = %+v", prs)
	}
}

func TestGitea_ListMyMergeRequests(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	mrs, err := g.ListMyMergeRequests(context.Background(), provider.ListMyMergeRequestsOptions{State: "opened"})
	if err != nil {
		t.Fatalf("ListMyMergeRequests: %v", err)
	}
	if len(mrs) != 1 {
		t.Fatalf("len(mrs) = %d; want 1", len(mrs))
	}
	// The search returns issue #3 in owner/repo; the handler then serves
	// the full PR (prJSON(3)) for the GetPullRequest call.
	if mrs[0].IID != 3 || mrs[0].Project != "owner/repo" {
		t.Errorf("mr = %+v; want IID=3 Project=owner/repo", mrs[0])
	}
	if mrs[0].SourceBranch != "feature" {
		t.Errorf("SourceBranch = %q; want feature", mrs[0].SourceBranch)
	}
}

func TestGitea_GetMergeRequest(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	pr, err := g.GetMergeRequest(context.Background(), "owner/repo", 3)
	if err != nil {
		t.Fatalf("GetMergeRequest: %v", err)
	}
	if pr.IID != 3 {
		t.Errorf("GetMergeRequest IID = %d, want 3", pr.IID)
	}
}

func TestGitea_CreateMergeRequest(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	assignee := 1
	pr, err := g.CreateMergeRequest(context.Background(), "owner/repo", provider.CreateMergeRequestOptions{
		Title:        "New PR",
		SourceBranch: "feature",
		TargetBranch: "main",
		AssigneeID:   &assignee,
	})
	if err != nil {
		t.Fatalf("CreateMergeRequest: %v", err)
	}
	if pr.IID != 4 {
		t.Errorf("CreateMergeRequest IID = %d, want 4", pr.IID)
	}
}

func TestGitea_UpdateMergeRequest(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	state := "closed"
	pr, err := g.UpdateMergeRequest(context.Background(), "owner/repo", 3, provider.UpdateMergeRequestOptions{
		Title:       stringPtr("updated"),
		Description: stringPtr("new body"),
		State:       &state,
	})
	if err != nil {
		t.Fatalf("UpdateMergeRequest: %v", err)
	}
	if pr.IID != 3 {
		t.Errorf("UpdateMergeRequest IID = %d, want 3", pr.IID)
	}
}

func TestGitea_ApproveMergeRequest(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	if err := g.ApproveMergeRequest(context.Background(), "owner/repo", 3); err != nil {
		t.Fatalf("ApproveMergeRequest: %v", err)
	}
}

func TestGitea_MergeMergeRequest(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	pr, err := g.MergeMergeRequest(context.Background(), "owner/repo", 3, provider.MergeOptions{})
	if err != nil {
		t.Fatalf("MergeMergeRequest: %v", err)
	}
	if pr.IID != 3 {
		t.Errorf("MergeMergeRequest IID = %d, want 3", pr.IID)
	}
}

func TestGitea_ListLabels(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	labels, err := g.ListLabels(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(labels) != 2 {
		t.Errorf("ListLabels = %d labels, want 2", len(labels))
	}
}

func TestGitea_CreateLabel(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	label, err := g.CreateLabel(context.Background(), "owner/repo", provider.CreateLabelOptions{
		Name:        "new-label",
		Color:       "#0000ff",
		Description: "description",
	})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	if label.Name != "new-label" {
		t.Errorf("CreateLabel Name = %q, want new-label", label.Name)
	}
}

func TestGitea_ListUsers(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	users, err := g.ListUsers(context.Background(), provider.ListUsersOptions{Search: "alice"})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 || users[0].Username != "alice" {
		t.Errorf("ListUsers = %+v", users)
	}
}

func stringPtr(s string) *string { return &s }

func TestGitea_New(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		testGiteaHandler(t, w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	g, err := New(&config.Config{
		GiteaURL: srv.URL,
		Token:    "token",
		Host:     "gitea.example.com",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if g.Kind() != provider.KindGitea {
		t.Errorf("Kind = %q, want %q", g.Kind(), provider.KindGitea)
	}
	if g.Host() != srv.URL[7:] { // strip http://
		t.Errorf("Host = %q, want %q", g.Host(), srv.URL[7:])
	}
}

func TestGitea_Unsupported(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	if _, err := g.CreateGroupMilestone(context.Background(), 1, provider.CreateMilestoneOptions{}); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("CreateGroupMilestone error = %v, want ErrUnsupported", err)
	}
	if _, err := g.ListGroupMilestones(context.Background(), 1, provider.ListGroupMilestonesOptions{}); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("ListGroupMilestones error = %v, want ErrUnsupported", err)
	}
	if _, err := g.CreateGroupEpic(context.Background(), 1, provider.CreateEpicOptions{}); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("CreateGroupEpic error = %v, want ErrUnsupported", err)
	}
	if _, err := g.ListGroupEpics(context.Background(), 1, provider.ListGroupEpicsOptions{}); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("ListGroupEpics error = %v, want ErrUnsupported", err)
	}
	if err := g.LinkIssueToEpic(context.Background(), 1, 1, 1); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("LinkIssueToEpic error = %v, want ErrUnsupported", err)
	}
}

func TestGitea_GetMergeRequestDiff(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	diffs, err := g.GetMergeRequestDiff(context.Background(), "owner/repo", 3)
	if err != nil {
		t.Fatalf("GetMergeRequestDiff: %v", err)
	}
	if len(diffs) != 2 {
		t.Fatalf("got %d file diffs, want 2", len(diffs))
	}

	// File 1: addition.
	if diffs[0].NewPath != "main.go" {
		t.Errorf("diffs[0].NewPath = %q, want main.go", diffs[0].NewPath)
	}
	if diffs[0].OldPath != "main.go" {
		t.Errorf("diffs[0].OldPath = %q, want main.go", diffs[0].OldPath)
	}
	if !diffs[0].NewFile {
		t.Errorf("diffs[0].NewFile = false, want true")
	}
	if !strings.Contains(diffs[0].Diff, "+package main") {
		t.Errorf("diffs[0].Diff missing patch text; got %q", diffs[0].Diff)
	}

	// File 2: rename (renamed.go → old.go).
	if diffs[1].NewPath != "old.go" {
		t.Errorf("diffs[1].NewPath = %q, want old.go", diffs[1].NewPath)
	}
	if diffs[1].OldPath != "renamed.go" {
		t.Errorf("diffs[1].OldPath = %q, want renamed.go", diffs[1].OldPath)
	}
	if !diffs[1].RenamedFile {
		t.Errorf("diffs[1].RenamedFile = false, want true")
	}
	if !strings.Contains(diffs[1].Diff, "+func new() {}") {
		t.Errorf("diffs[1].Diff missing patch text; got %q", diffs[1].Diff)
	}
}

func TestGitea_GetMergeRequestDiff_BadProject(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	if _, err := g.GetMergeRequestDiff(context.Background(), "bad", 3); err == nil {
		t.Error("expected error for malformed project, got nil")
	}
}

func TestParseUnifiedDiff(t *testing.T) {
	patches := parseUnifiedDiff(diffRaw)
	if len(patches) != 2 {
		t.Fatalf("got %d patches, want 2", len(patches))
	}
	if _, ok := patches["main.go"]; !ok {
		t.Errorf("missing patch for main.go; keys: %v", mapKeys(patches))
	}
	if _, ok := patches["old.go"]; !ok {
		t.Errorf("missing patch for old.go; keys: %v", mapKeys(patches))
	}
	if !strings.Contains(patches["main.go"], "diff --git a/main.go b/main.go") {
		t.Errorf("main.go patch missing diff header: %q", patches["main.go"])
	}
}

func TestParseUnifiedDiff_Deletion(t *testing.T) {
	raw := "diff --git a/gone.go b/gone.go\nindex 111..000 100644\n--- a/gone.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-func gone() {}\n"
	patches := parseUnifiedDiff(raw)
	if _, ok := patches["gone.go"]; !ok {
		t.Fatalf("deletion keyed by /dev/null instead of gone.go; keys: %v", mapKeys(patches))
	}
}

func TestParseUnifiedDiff_Empty(t *testing.T) {
	if got := parseUnifiedDiff(""); len(got) != 0 {
		t.Errorf("empty input produced %d patches, want 0", len(got))
	}
}

func TestPathFromDiffHeader(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"+++ b/main.go", "main.go"},
		{"--- a/old.go", "old.go"},
		{"+++ /dev/null", "/dev/null"},
		{`+++ "b/sp ace.go"`, "sp ace.go"},
	}
	for _, tt := range tests {
		if got := pathFromDiffHeader(tt.in); got != tt.want {
			t.Errorf("pathFromDiffHeader(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func mapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestGitea_CreateIssue_LabelNotFound(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	_, err := g.CreateIssue(context.Background(), "owner/repo", provider.CreateIssueOptions{
		Title:       "x",
		Description: "y",
		Labels:      []string{"missing"},
	})
	if err == nil {
		t.Fatal("CreateIssue: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error = %q, expected to contain 'missing'", err.Error())
	}
}

func TestGitea_ListMilestones_DecodesResponse(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	ms, err := g.ListMilestones(context.Background(), "owner/repo", provider.ListMilestonesOptions{State: "open", PerPage: 20})
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	if len(ms) != 1 {
		t.Fatalf("len(ms) = %d; want 1", len(ms))
	}
	if ms[0].Title != "v1.0" || ms[0].State != "open" {
		t.Errorf("ms[0] = %+v", ms[0])
	}
	if ms[0].DueDate != "2026-01-14" {
		t.Errorf("ms[0].DueDate = %q; want 2026-01-14", ms[0].DueDate)
	}
	if ms[0].Description != "First release" {
		t.Errorf("ms[0].Description = %q", ms[0].Description)
	}
}

func TestGitea_ListMilestones_RejectsBadProject(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	if _, err := g.ListMilestones(context.Background(), "no-slash", provider.ListMilestonesOptions{}); err == nil {
		t.Fatal("expected error for project without slash")
	}
}

func TestGitea_ResolveGroup_Unsupported(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	if _, err := g.ResolveGroup(context.Background(), "owner"); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("ResolveGroup err = %v; want ErrUnsupported", err)
	}
}

func TestGitea_GetProject_DecodesResponse(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	p, err := g.GetProject(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if p.ID != 99 || p.Name != "repo" || p.Path != "owner/repo" ||
		p.FullName != "owner/repo" || p.Description != "demo" ||
		p.WebURL != "https://gitea.example.com/owner/repo" ||
		p.DefaultBranch != "main" || p.OpenIssuesCount != 3 || p.Archived {
		t.Errorf("project = %+v", p)
	}
}

func TestGitea_GetProject_RejectsBadProject(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	if _, err := g.GetProject(context.Background(), "no-slash"); err == nil {
		t.Fatal("GetProject: expected error for malformed project, got nil")
	}
}

func TestGitea_WorkItems_Unsupported(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	if _, err := g.ListWorkItems(context.Background(), "owner/repo", provider.ListWorkItemsOptions{}); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("ListWorkItems err = %v; want ErrUnsupported", err)
	}
	if _, err := g.CreateWorkItem(context.Background(), "owner/repo", provider.CreateWorkItemOptions{Title: "x"}); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("CreateWorkItem err = %v; want ErrUnsupported", err)
	}
	if err := g.CloseWorkItem(context.Background(), "owner/repo", 1); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("CloseWorkItem err = %v; want ErrUnsupported", err)
	}
}

// ─── GetMilestone ─────────────────────────────────────────────────────────────

func TestGitea_GetMilestone_DecodesResponse(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	m, err := g.GetMilestone(context.Background(), "owner/repo", 5)
	if err != nil {
		t.Fatalf("GetMilestone: %v", err)
	}
	if m.ID != 5 || m.Title != "v1.0" {
		t.Errorf("milestone = %+v", m)
	}
}

func TestGitea_GetMilestone_RejectsBadProject(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	if _, err := g.GetMilestone(context.Background(), "no-slash", 1); err == nil {
		t.Fatal("GetMilestone: expected error for malformed project, got nil")
	}
}

// ─── CreateMilestone ──────────────────────────────────────────────────────────

func TestGitea_CreateMilestone_SendsTitle(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	m, err := g.CreateMilestone(context.Background(), "owner/repo", provider.CreateMilestoneOptions{
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
}

func TestGitea_CreateMilestone_RejectsBadDueDate(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	if _, err := g.CreateMilestone(context.Background(), "owner/repo", provider.CreateMilestoneOptions{
		Title:   "x",
		DueDate: "not-a-date",
	}); err == nil {
		t.Fatal("CreateMilestone: expected error for bad due date, got nil")
	}
}

// ─── UpdateMilestone ──────────────────────────────────────────────────────────

func TestGitea_UpdateMilestone_SendsTitleAndState(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	title := "Renamed"
	state := "close"
	m, err := g.UpdateMilestone(context.Background(), "owner/repo", 5, provider.UpdateMilestoneOptions{
		Title: &title,
		State: &state,
	})
	if err != nil {
		t.Fatalf("UpdateMilestone: %v", err)
	}
	if m.ID != 5 || m.Title != "Renamed" || m.State != "closed" {
		t.Errorf("milestone = %+v", m)
	}
}

// ─── Epic stubs (unsupported) ─────────────────────────────────────────────────

func TestGitea_LinkIssueToEpic_Unsupported(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	if err := g.LinkIssueToEpic(context.Background(), 1, 2, 3); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("LinkIssueToEpic err = %v; want ErrUnsupported", err)
	}
}

func TestGitea_UpdateGroupEpic_Unsupported(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	if _, err := g.UpdateGroupEpic(context.Background(), 1, 2, provider.UpdateEpicOptions{}); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("UpdateGroupEpic err = %v; want ErrUnsupported", err)
	}
}

func TestGitea_ListEpicIssues_Unsupported(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	if _, err := g.ListEpicIssues(context.Background(), 1, 2); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("ListEpicIssues err = %v; want ErrUnsupported", err)
	}
}

// ─── ListPipelines ────────────────────────────────────────────────────────────

func TestGitea_ListPipelines_DecodesResponse(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	pipes, err := g.ListPipelines(context.Background(), "owner/repo", provider.ListPipelinesOptions{
		Page: 1, PerPage: 20,
	})
	if err != nil {
		t.Fatalf("ListPipelines: %v", err)
	}
	if len(pipes) != 1 {
		t.Fatalf("pipelines len = %d; want 1", len(pipes))
	}
	if pipes[0].ID != 100 || pipes[0].Status != "success" || pipes[0].Ref != "main" {
		t.Errorf("pipeline = %+v", pipes[0])
	}
}

func TestGitea_ListPipelines_RejectsBadProject(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	if _, err := g.ListPipelines(context.Background(), "no-slash", provider.ListPipelinesOptions{}); err == nil {
		t.Fatal("ListPipelines: expected error for malformed project, got nil")
	}
}

// ─── GetPipeline ──────────────────────────────────────────────────────────────

func TestGitea_GetPipeline_DecodesResponse(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	p, err := g.GetPipeline(context.Background(), "owner/repo", 200)
	if err != nil {
		t.Fatalf("GetPipeline: %v", err)
	}
	if p.ID != 200 || p.Status != "running" || p.Ref != "feature" {
		t.Errorf("pipeline = %+v", p)
	}
}

// ─── RunPipeline (unsupported) ────────────────────────────────────────────────

func TestGitea_RunPipeline_Unsupported(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	if _, err := g.RunPipeline(context.Background(), "owner/repo", provider.RunPipelineOptions{}); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("RunPipeline err = %v; want ErrUnsupported", err)
	}
}

// ─── RetryPipeline ────────────────────────────────────────────────────────────

func TestGitea_RetryPipeline_RerunsAndFetches(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	p, err := g.RetryPipeline(context.Background(), "owner/repo", 300)
	if err != nil {
		t.Fatalf("RetryPipeline: %v", err)
	}
	if p.ID != 300 || p.Status != "pending" {
		t.Errorf("pipeline = %+v", p)
	}
}

// ─── CancelPipeline (unsupported) ─────────────────────────────────────────────

func TestGitea_CancelPipeline_Unsupported(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	if err := g.CancelPipeline(context.Background(), "owner/repo", 400); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("CancelPipeline err = %v; want ErrUnsupported", err)
	}
}

// ─── ListPipelineJobs ─────────────────────────────────────────────────────────

func TestGitea_ListPipelineJobs_DecodesResponse(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	jobs, err := g.ListPipelineJobs(context.Background(), "owner/repo", 100)
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

// ─── GetJobLogs ───────────────────────────────────────────────────────────────

func TestGitea_GetJobLogs_ReturnsContent(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	logs, err := g.GetJobLogs(context.Background(), "owner/repo", 500)
	if err != nil {
		t.Fatalf("GetJobLogs: %v", err)
	}
	if !strings.Contains(logs, "log line 1") {
		t.Errorf("logs = %q; want to contain 'log line 1'", logs)
	}
}

// ─── ListArtifacts ────────────────────────────────────────────────────────────

func TestGitea_ListArtifacts_DecodesResponse(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	arts, err := g.ListArtifacts(context.Background(), "owner/repo", 100)
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

// ─── DownloadArtifact ─────────────────────────────────────────────────────────

func TestGitea_DownloadArtifact_WritesZip(t *testing.T) {
	g, srv := newTestGitea(t)
	defer srv.Close()

	dest := t.TempDir()
	if err := g.DownloadArtifact(context.Background(), "owner/repo", 600, dest); err != nil {
		t.Fatalf("DownloadArtifact: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "artifact.zip"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.HasPrefix(string(data), "PK") {
		t.Errorf("file content = %q; want zip starting with PK", string(data))
	}
}
