package github_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/MChorfa/ggvalet/internal/provider"
)

// ─── ListMergeRequests ────────────────────────────────────────────────────────

// TestGitHub_ListMergeRequests_DecodesResponse verifies SourceBranch and
// TargetBranch are populated from head.ref and base.ref respectively.
func TestGitHub_ListMergeRequests_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{
			{
				"id": 300, "number": 7, "title": "Add feature", "state": "open",
				"html_url": "https://github.com/owner/repo/pull/7",
				"head":     map[string]any{"ref": "feature", "sha": "abc"},
				"base":     map[string]any{"ref": "main", "sha": "def"},
			},
		})
	}))

	mrs, err := prov.ListMergeRequests(context.Background(), "owner/repo", provider.ListMergeRequestsOptions{})
	if err != nil {
		t.Fatalf("ListMergeRequests: %v", err)
	}
	if len(mrs) != 1 {
		t.Fatalf("len(mrs) = %d; want 1", len(mrs))
	}
	if mrs[0].SourceBranch != "feature" {
		t.Errorf("SourceBranch = %q; want %q", mrs[0].SourceBranch, "feature")
	}
	if mrs[0].TargetBranch != "main" {
		t.Errorf("TargetBranch = %q; want %q", mrs[0].TargetBranch, "main")
	}
}

// TestGitHub_ListMergeRequests_WithFilters exercises the source/target branch
// and state option branches in ListMergeRequests.
func TestGitHub_ListMergeRequests_WithFilters(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{
			{
				"id": 500, "number": 9, "title": "Filtered PR", "state": "closed",
				"html_url":  "https://github.com/owner/repo/pull/9",
				"merged_at": "2026-02-01T00:00:00Z",
				"head":      map[string]any{"ref": "fix/thing", "sha": "ccc"},
				"base":      map[string]any{"ref": "main", "sha": "ddd"},
			},
		})
	}))

	mrs, err := prov.ListMergeRequests(context.Background(), "owner/repo", provider.ListMergeRequestsOptions{
		State:        "closed",
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
		t.Error("MergedAt is nil; want non-nil for merged PR")
	}
}

// ─── GetMergeRequest ──────────────────────────────────────────────────────────

// TestGitHub_GetMergeRequest_DecodesResponse verifies GetMergeRequest decodes
// fields including assignees, reviewers, labels, and MergedAt.
func TestGitHub_GetMergeRequest_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"id": 400, "number": 3, "title": "Fix the bug", "state": "closed",
			"html_url":  "https://github.com/owner/repo/pull/3",
			"merged_at": "2026-01-02T00:00:00Z",
			"head":      map[string]any{"ref": "fix/bug", "sha": "aaa"},
			"base":      map[string]any{"ref": "main", "sha": "bbb"},
			"user":      map[string]any{"id": 1, "login": "alice", "html_url": "https://github.com/alice"},
			"assignees": []map[string]any{
				{"id": 2, "login": "bob", "html_url": "https://github.com/bob"},
			},
			"requested_reviewers": []map[string]any{
				{"id": 3, "login": "carol", "html_url": "https://github.com/carol"},
			},
			"labels": []map[string]any{
				{"id": 10, "name": "bug", "color": "d73a4a"},
			},
		})
	}))

	mr, err := prov.GetMergeRequest(context.Background(), "owner/repo", 3)
	if err != nil {
		t.Fatalf("GetMergeRequest: %v", err)
	}
	if mr.IID != 3 {
		t.Errorf("IID = %d; want 3", mr.IID)
	}
	if mr.SourceBranch != "fix/bug" {
		t.Errorf("SourceBranch = %q; want %q", mr.SourceBranch, "fix/bug")
	}
	if mr.TargetBranch != "main" {
		t.Errorf("TargetBranch = %q; want %q", mr.TargetBranch, "main")
	}
	if mr.Author.Username != "alice" {
		t.Errorf("Author.Username = %q; want %q", mr.Author.Username, "alice")
	}
	if len(mr.Assignees) != 1 || mr.Assignees[0].Username != "bob" {
		t.Errorf("Assignees = %v; want [{bob}]", mr.Assignees)
	}
	if len(mr.Reviewers) != 1 || mr.Reviewers[0].Username != "carol" {
		t.Errorf("Reviewers = %v; want [{carol}]", mr.Reviewers)
	}
	if len(mr.Labels) != 1 || mr.Labels[0] != "bug" {
		t.Errorf("Labels = %v; want [bug]", mr.Labels)
	}
	if mr.MergedAt == nil {
		t.Error("MergedAt is nil; want non-nil for merged PR")
	}
}

// TestGitHub_GetMergeRequest_ReturnsErrorOn404 verifies error propagation
// when the server returns a non-2xx status.
func TestGitHub_GetMergeRequest_ReturnsErrorOn404(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))

	_, err := prov.GetMergeRequest(context.Background(), "owner/repo", 999)
	if err == nil {
		t.Fatal("expected error from GetMergeRequest on HTTP 404; got nil")
	}
}

// ─── ListLabels ───────────────────────────────────────────────────────────────

// TestGitHub_ListLabels_DecodesResponse verifies label fields round-trip.
func TestGitHub_ListLabels_DecodesResponse(t *testing.T) {
	t.Parallel()

	prov := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{
			{"id": 1, "name": "bug", "color": "d73a4a", "description": "a bug"},
			{"id": 2, "name": "enhancement", "color": "84b6eb", "description": ""},
		})
	}))

	labels, err := prov.ListLabels(context.Background(), "owner/repo")
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
	found := func(target string) bool {
		for _, n := range names {
			if n == target {
				return true
			}
		}
		return false
	}
	if !found("bug") {
		t.Errorf("label names = %v; want to contain %q", names, "bug")
	}
	if !found("enhancement") {
		t.Errorf("label names = %v; want to contain %q", names, "enhancement")
	}
}
