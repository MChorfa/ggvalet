package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newGitLabTestServer starts a permissive httptest server that returns generic
// GitLab-flavoured JSON for every API endpoint exercised by the cmd tests.
func newGitLabTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(gitlabTestHandler))
	t.Cleanup(srv.Close)
	return srv
}

func gitlabTestHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Path
	method := r.Method

	write := func(v any) {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	// Treat any project/group path containing "other" as an empty destination
	// for sync tests; create endpoints still return created resources below.
	isOther := strings.Contains(path, "/other")

	// Helper used by list endpoints to return empty for the "other" namespace.
	writeEmpty := func(v any) {
		if isOther && method == http.MethodGet {
			write([]map[string]any{})
			return
		}
		write(v)
	}

	// Work items (raw HTTP helpers in workitem.go)
	if strings.Contains(path, "/work_items") {
		switch method {
		case http.MethodGet:
			write([]map[string]any{workItemJSON(1, "Test work item", "opened")})
		case http.MethodPost, http.MethodPatch:
			write(workItemJSON(42, "Created work item", "opened"))
		}
		return
	}

	// Labels
	if strings.Contains(path, "/labels") {
		write([]map[string]any{
			{"name": "bug", "color": "#FF0000", "open_issues_count": 3, "closed_issues_count": 1},
			{"name": "blocked", "color": "#FFA500", "open_issues_count": 1, "closed_issues_count": 0},
		})
		return
	}

	// Pipelines
	if strings.Contains(path, "/pipelines") {
		write([]map[string]any{{"id": 1, "status": "success", "web_url": "https://gitlab.example.com/p/1"}})
		return
	}

	// Merge requests (approve / merge / list)
	if strings.Contains(path, "/merge_requests/") {
		if strings.HasSuffix(path, "/approve") && method == http.MethodPost {
			write(map[string]any{"approved": true})
			return
		}
		if strings.HasSuffix(path, "/merge") && method == http.MethodPut {
			write(mergeRequestJSON(1, "Merged MR", "merged", "renovate/patch-foo", "patch"))
			return
		}
		write(mergeRequestJSON(1, "MR", "opened", "feature", ""))
		return
	}
	if strings.Contains(path, "/merge_requests") {
		write([]map[string]any{
			mergeRequestJSON(1, "chore(deps): update patch", "opened", "renovate/patch-foo", "patch"),
			mergeRequestJSON(2, "chore(deps): update minor", "opened", "renovate/minor-foo", "minor"),
			mergeRequestJSON(3, "chore(deps): update major", "opened", "renovate/major-foo", "major"),
		})
		return
	}

	// Epics
	if strings.Contains(path, "/epics/") {
		if strings.Contains(path, "/issues") && method == http.MethodGet {
			write([]map[string]any{issueJSON(10, "Epic issue", "opened", []string{"bug"})})
			return
		}
		write(epicJSON(1, "Epic one", "opened"))
		return
	}
	if strings.Contains(path, "/epics") {
		if method == http.MethodPost || method == http.MethodPut {
			write(epicJSON(42, "Created epic", "opened"))
			return
		}
		writeEmpty([]map[string]any{epicJSON(1, "Epic one", "opened")})
		return
	}

	// Milestones
	if strings.Contains(path, "/milestones/") {
		write(milestoneJSON(1, "v1.0", "active"))
		return
	}
	if strings.Contains(path, "/milestones") {
		if method == http.MethodPost || method == http.MethodPut {
			write(milestoneJSON(42, "Created milestone", "active"))
			return
		}
		writeEmpty([]map[string]any{milestoneJSON(1, "v1.0", "active")})
		return
	}

	// Issues (project-specific: get / update / list / create)
	if strings.Contains(path, "/issues/") {
		if method == http.MethodPut {
			write(issueJSON(1, "Updated issue", "closed", []string{"bug"}))
			return
		}
		write(issueJSON(1, "Issue one", "opened", []string{"bug"}))
		return
	}
	if strings.Contains(path, "/issues") {
		if method == http.MethodPost {
			write(issueJSON(42, "Created issue", "opened", []string{"bug"}))
			return
		}
		writeEmpty([]map[string]any{
			issueJSON(1, "Issue one", "opened", []string{"bug"}),
			issueJSON(2, "Blocked issue", "opened", []string{"blocked"}),
		})
		return
	}

	// Project
	if strings.Contains(path, "/projects/") {
		write(projectJSON("group/project", 12))
		return
	}

	// Fallback for unknown endpoints
	write(map[string]any{"message": "ok"})
}

func issueJSON(iid int, title, state string, labels []string) map[string]any {
	return map[string]any{
		"id":          iid,
		"iid":         iid,
		"title":       title,
		"state":       state,
		"description": fmt.Sprintf("description for %s", title),
		"labels":      labels,
		"web_url":     fmt.Sprintf("https://gitlab.example.com/group/project/-/issues/%d", iid),
		"created_at":  "2026-07-01T12:00:00Z",
		"author":      map[string]any{"username": "alice"},
		"assignee":    map[string]any{"username": "bob"},
		"milestone":   map[string]any{"title": "v1.0"},
		"references":  map[string]any{"full": "group/project#42"},
	}
}

func mergeRequestJSON(iid int, title, state, branch, bump string) map[string]any {
	desc := ""
	switch bump {
	case "patch":
		desc = "patch"
	case "minor":
		desc = "minor"
	case "major":
		desc = "major"
	}
	return map[string]any{
		"id":            iid,
		"iid":           iid,
		"title":         title,
		"state":         state,
		"source_branch": branch,
		"target_branch": "main",
		"description":   desc,
		"labels":        []string{},
		"web_url":       fmt.Sprintf("https://gitlab.example.com/group/project/-/merge_requests/%d", iid),
		"author":        map[string]any{"username": "renovate-bot"},
		"head_pipeline": map[string]any{"status": "success"},
	}
}

func epicJSON(iid int, title, state string) map[string]any {
	return map[string]any{
		"id":          iid,
		"iid":         iid,
		"title":       title,
		"state":       state,
		"description": fmt.Sprintf("description for %s", title),
		"web_url":     fmt.Sprintf("https://gitlab.example.com/groups/group/-/epics/%d", iid),
		"author":      map[string]any{"username": "alice"},
		"start_date":  "2026-01-01",
		"due_date":    "2026-12-31",
		"labels":      []string{"epic"},
	}
}

func milestoneJSON(iid int, title, state string) map[string]any {
	return map[string]any{
		"id":          iid,
		"iid":         iid,
		"title":       title,
		"state":       state,
		"description": fmt.Sprintf("description for %s", title),
		"web_url":     fmt.Sprintf("https://gitlab.example.com/group/project/-/milestones/%d", iid),
		"start_date":  "2026-01-01",
		"due_date":    "2026-12-31",
	}
}

func projectJSON(path string, openIssues int) map[string]any {
	return map[string]any{
		"id":                  1,
		"name":                "project",
		"path":                "project",
		"path_with_namespace": path,
		"open_issues_count":   openIssues,
		"web_url":             fmt.Sprintf("https://gitlab.example.com/%s", path),
	}
}

func workItemJSON(iid int, title, state string) map[string]any {
	return map[string]any{
		"id":             iid,
		"iid":            iid,
		"title":          title,
		"state":          state,
		"web_url":        fmt.Sprintf("https://gitlab.example.com/group/project/-/work_items/%d", iid),
		"work_item_type": map[string]any{"name": "Task"},
		"assignees":      []map[string]any{{"username": "alice"}},
	}
}
