package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	body := readBody(r)
	isOther := strings.Contains(path, "/other")
	page := r.URL.Query().Get("page")
	pageNotFirst := page != "" && page != "1"

	write := func(v any) {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	writeEmpty := func(v any) {
		if isOther && method == http.MethodGet {
			write([]map[string]any{})
			return
		}
		write(v)
	}

	// Users (required by issue/mr create to resolve assignees/reviewers).
	if strings.Contains(path, "/users") {
		if pageNotFirst {
			write([]map[string]any{})
			return
		}
		write([]map[string]any{userJSON(1, "alice", "Alice", "alice@example.com")})
		return
	}

	// Work items (raw HTTP helpers in workitem.go).
	if strings.Contains(path, "/work_items") {
		switch method {
		case http.MethodGet:
			if pageNotFirst {
				write([]map[string]any{})
				return
			}
			write([]map[string]any{workItemJSON(1, "Test work item", "opened")})
		case http.MethodPost, http.MethodPatch:
			title := stringField(body, "title", "Created work item")
			write(workItemJSON(42, title, "opened"))
		}
		return
	}

	// Labels: GET list, POST/PATCH single.
	if strings.Contains(path, "/labels") {
		if method == http.MethodPost || method == http.MethodPatch {
			write(labelJSON(1,
				stringField(body, "name", "bug"),
				stringField(body, "color", "#FF0000"),
				stringField(body, "description", "")))
			return
		}
		if pageNotFirst {
			write([]map[string]any{})
			return
		}
		write([]map[string]any{
			labelJSON(1, "bug", "#FF0000", ""),
			labelJSON(2, "blocked", "#FFA500", ""),
		})
		return
	}

	// Pipelines.
	if strings.Contains(path, "/pipelines") {
		if pageNotFirst {
			write([]map[string]any{})
			return
		}
		write([]map[string]any{{"id": 1, "status": "success", "web_url": "https://gitlab.example.com/p/1"}})
		return
	}

	// Merge requests.
	if strings.Contains(path, "/merge_requests/") {
		iid := pathIID(path)
		if strings.HasSuffix(path, "/approve") && method == http.MethodPost {
			write(map[string]any{"approved": true})
			return
		}
		if strings.HasSuffix(path, "/merge") && method == http.MethodPut {
			write(mergeRequestJSON(iid, "Merged MR", "merged", "renovate/patch-foo", "patch"))
			return
		}
		if strings.HasSuffix(path, "/changes") && method == http.MethodGet {
			write(mergeRequestWithChangesJSON(iid))
			return
		}
		title := stringField(body, "title", "MR")
		state := stateFromStateEvent(stringField(body, "state_event", ""), "opened")
		source := stringField(body, "source_branch", "feature")
		desc := stringField(body, "description", "")
		write(mergeRequestJSON(iid, title, state, source, desc))
		return
	}
	if strings.Contains(path, "/merge_requests") {
		if method == http.MethodPost {
			title := stringField(body, "title", "Created MR")
			source := stringField(body, "source_branch", "feature")
			desc := stringField(body, "description", "")
			write(mergeRequestJSON(42, title, "opened", source, desc))
			return
		}
		if pageNotFirst {
			write([]map[string]any{})
			return
		}
		write([]map[string]any{
			mergeRequestJSON(1, "chore(deps): update patch", "opened", "renovate/patch-foo", "patch"),
			mergeRequestJSON(2, "chore(deps): update minor", "opened", "renovate/minor-foo", "minor"),
			mergeRequestJSON(3, "chore(deps): update major", "opened", "renovate/major-foo", "major"),
		})
		return
	}

	// Epics.
	if strings.Contains(path, "/epics/") {
		// Assign issue to epic: /groups/{id}/epics/{iid}/issues/{issue_id}.
		if strings.Contains(path, "/issues/") && (method == http.MethodPost || method == http.MethodPut) {
			issueID := pathIID(path)
			write(issueJSON(issueID, "Epic issue", "opened", "description", []string{"bug"}))
			return
		}
		if strings.Contains(path, "/issues") && method == http.MethodGet {
			if pageNotFirst {
				write([]map[string]any{})
				return
			}
			write([]map[string]any{issueJSON(10, "Epic issue", "opened", "description", []string{"bug"})})
			return
		}
		iid := pathIID(path)
		title := stringField(body, "title", "Epic one")
		state := stateFromStateEvent(stringField(body, "state_event", ""), "opened")
		desc := stringField(body, "description", fmt.Sprintf("description for %s", title))
		labels := stringSliceField(body, "labels")
		if len(labels) == 0 {
			labels = []string{"epic"}
		}
		write(epicJSON(iid, title, state, desc, labels))
		return
	}
	if strings.Contains(path, "/epics") {
		if method == http.MethodPost || method == http.MethodPut {
			title := stringField(body, "title", "Created epic")
			desc := stringField(body, "description", fmt.Sprintf("description for %s", title))
			labels := stringSliceField(body, "labels")
			write(epicJSON(42, title, "opened", desc, labels))
			return
		}
		if pageNotFirst {
			write([]map[string]any{})
			return
		}
		writeEmpty([]map[string]any{epicJSON(1, "Epic one", "opened", "description for Epic one", []string{"epic"})})
		return
	}

	// Milestones.
	if strings.Contains(path, "/milestones/") {
		iid := pathIID(path)
		title := stringField(body, "title", "v1.0")
		state := stateFromStateEvent(stringField(body, "state_event", ""), "active")
		desc := stringField(body, "description", fmt.Sprintf("description for %s", title))
		startDate := stringField(body, "start_date", "2026-01-01")
		dueDate := stringField(body, "due_date", "2026-12-31")
		write(milestoneJSON(iid, title, state, desc, startDate, dueDate))
		return
	}
	if strings.Contains(path, "/milestones") {
		if method == http.MethodPost || method == http.MethodPut {
			title := stringField(body, "title", "Created milestone")
			desc := stringField(body, "description", fmt.Sprintf("description for %s", title))
			startDate := stringField(body, "start_date", "2026-01-01")
			dueDate := stringField(body, "due_date", "2026-12-31")
			write(milestoneJSON(42, title, "active", desc, startDate, dueDate))
			return
		}
		if pageNotFirst {
			write([]map[string]any{})
			return
		}
		writeEmpty([]map[string]any{milestoneJSON(1, "v1.0", "active", "description for v1.0", "2026-01-01", "2026-12-31")})
		return
	}

	// Issues (project-specific: get / update / notes / create).
	if strings.Contains(path, "/issues/") {
		iid := pathIID(path)
		if strings.Contains(path, "/notes") {
			if method == http.MethodPost {
				write(noteJSON(1, stringField(body, "body", "note")))
				return
			}
			write([]map[string]any{noteJSON(1, "note")})
			return
		}
		title := stringField(body, "title", "Issue one")
		state := stateFromStateEvent(stringField(body, "state_event", ""), "opened")
		desc := stringField(body, "description", fmt.Sprintf("description for %s", title))
		labels := stringSliceField(body, "labels")
		if len(labels) == 0 {
			labels = []string{"bug"}
		}
		write(issueJSON(iid, title, state, desc, labels))
		return
	}
	if strings.Contains(path, "/issues") {
		if method == http.MethodPost {
			title := stringField(body, "title", "Created issue")
			desc := stringField(body, "description", fmt.Sprintf("description for %s", title))
			labels := stringSliceField(body, "labels")
			if len(labels) == 0 {
				labels = []string{"bug"}
			}
			write(issueJSON(42, title, "opened", desc, labels))
			return
		}
		if pageNotFirst {
			write([]map[string]any{})
			return
		}
		writeEmpty([]map[string]any{
			issueJSON(1, "Issue one", "opened", "description for Issue one", []string{"bug"}),
			issueJSON(2, "Blocked issue", "opened", "description for Blocked issue", []string{"blocked"}),
		})
		return
	}

	// Project.
	if strings.Contains(path, "/projects/") {
		write(projectJSON("group/project", 12))
		return
	}

	// Fallback for unknown endpoints.
	write(map[string]any{"message": "ok"})
}

// readBody decodes a JSON request body when present. It is safe to call on
// requests with no body.
func readBody(r *http.Request) map[string]any {
	if r.Body == nil {
		return nil
	}
	data, err := io.ReadAll(r.Body)
	if err != nil || len(data) == 0 {
		return nil
	}
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}

func stringField(m map[string]any, key, fallback string) string {
	if m == nil {
		return fallback
	}
	v, ok := m[key]
	if !ok {
		return fallback
	}
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	return s
}

func stringSliceField(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	slice, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(slice))
	for _, item := range slice {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func pathIID(path string) int {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if n, err := strconv.Atoi(parts[i]); err == nil {
			return n
		}
	}
	return 1
}

func stateFromStateEvent(event, defaultState string) string {
	switch event {
	case "close":
		return "closed"
	case "reopen":
		return "opened"
	default:
		return defaultState
	}
}

func issueJSON(iid int, title, state, description string, labels []string) map[string]any {
	return map[string]any{
		"id":          iid,
		"iid":         iid,
		"title":       title,
		"state":       state,
		"description": description,
		"labels":      labels,
		"web_url":     fmt.Sprintf("https://gitlab.example.com/group/project/-/issues/%d", iid),
		"created_at":  "2026-07-01T12:00:00Z",
		"author":      map[string]any{"username": "alice"},
		"assignee":    map[string]any{"username": "bob"},
		"milestone":   map[string]any{"title": "v1.0"},
		"references":  map[string]any{"full": "group/project#42"},
	}
}

func mergeRequestJSON(iid int, title, state, sourceBranch, description string) map[string]any {
	bump := ""
	switch description {
	case "patch", "minor", "major":
		bump = description
	}
	return map[string]any{
		"id":            iid,
		"iid":           iid,
		"title":         title,
		"state":         state,
		"source_branch": sourceBranch,
		"target_branch": "main",
		"description":   bump,
		"labels":        []string{},
		"web_url":       fmt.Sprintf("https://gitlab.example.com/group/project/-/merge_requests/%d", iid),
		"author":        map[string]any{"username": "renovate-bot"},
		"head_pipeline": map[string]any{"status": "success"},
	}
}

func mergeRequestWithChangesJSON(iid int) map[string]any {
	mr := mergeRequestJSON(iid, "MR", "opened", "feature", "")
	mr["changes"] = []map[string]any{{
		"old_path":     "a.go",
		"new_path":     "b.go",
		"diff":         "diff content",
		"new_file":     false,
		"deleted_file": false,
		"renamed_file": true,
	}}
	return mr
}

func epicJSON(iid int, title, state, description string, labels []string) map[string]any {
	return map[string]any{
		"id":          iid,
		"iid":         iid,
		"title":       title,
		"state":       state,
		"description": description,
		"web_url":     fmt.Sprintf("https://gitlab.example.com/groups/group/-/epics/%d", iid),
		"author":      map[string]any{"username": "alice"},
		"start_date":  "2026-01-01",
		"due_date":    "2026-12-31",
		"labels":      labels,
	}
}

func milestoneJSON(iid int, title, state, description, startDate, dueDate string) map[string]any {
	return map[string]any{
		"id":          iid,
		"iid":         iid,
		"title":       title,
		"state":       state,
		"description": description,
		"web_url":     fmt.Sprintf("https://gitlab.example.com/group/project/-/milestones/%d", iid),
		"start_date":  startDate,
		"due_date":    dueDate,
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

func noteJSON(id int, body string) map[string]any {
	return map[string]any{
		"id":   id,
		"body": body,
		"author": map[string]any{
			"id":       1,
			"username": "alice",
			"name":     "Alice",
		},
	}
}

func labelJSON(id int, name, color, description string) map[string]any {
	return map[string]any{
		"id":          id,
		"name":        name,
		"color":       color,
		"description": description,
	}
}

func userJSON(id int, username, name, email string) map[string]any {
	return map[string]any{
		"id":       id,
		"username": username,
		"name":     name,
		"email":    email,
		"web_url":  fmt.Sprintf("https://gitlab.example.com/%s", username),
	}
}
