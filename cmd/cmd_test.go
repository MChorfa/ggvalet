package cmd

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ckodex/gitlabvalet/internal/client"
	"github.com/ckodex/gitlabvalet/internal/config"
	"github.com/ckodex/gitlabvalet/internal/journal"
	"github.com/ckodex/gitlabvalet/internal/observed"
	"github.com/ckodex/gitlabvalet/internal/provider"
)

// cmdTestProvider is a permissive stub that returns sensible defaults for all
// host-neutral command paths. It lets command-level tests exercise the happy
// path without a real GitLab instance.
type cmdTestProvider struct{}

var _ provider.Provider = (*cmdTestProvider)(nil)
var _ provider.EpicLinker = (*cmdTestProvider)(nil)

func (p *cmdTestProvider) Kind() provider.Kind { return provider.KindGitLab }
func (p *cmdTestProvider) Host() string        { return "gitlab.example.com" }

func (p *cmdTestProvider) ListIssues(_ context.Context, project string, opts provider.ListIssuesOptions) ([]provider.Issue, error) {
	if opts.Page > 1 {
		return nil, nil
	}
	return []provider.Issue{defaultIssue(project)}, nil
}

func (p *cmdTestProvider) ListMyIssues(_ context.Context, _ provider.ListMyIssuesOptions) ([]provider.Issue, error) {
	return []provider.Issue{defaultIssue("group/project")}, nil
}

func (p *cmdTestProvider) GetIssue(_ context.Context, project string, iid int) (provider.Issue, error) {
	iss := defaultIssue(project)
	iss.IID = iid
	return iss, nil
}

func (p *cmdTestProvider) CreateIssue(_ context.Context, project string, opts provider.CreateIssueOptions) (provider.Issue, error) {
	iss := defaultIssue(project)
	iss.Title = opts.Title
	return iss, nil
}

func (p *cmdTestProvider) UpdateIssue(_ context.Context, project string, iid int, opts provider.UpdateIssueOptions) (provider.Issue, error) {
	iss := defaultIssue(project)
	iss.IID = iid
	if opts.Title != nil {
		iss.Title = *opts.Title
	}
	if opts.State != nil {
		iss.State = *opts.State
	}
	return iss, nil
}

func (p *cmdTestProvider) CreateIssueNote(_ context.Context, project string, iid int, body string) (provider.Note, error) {
	return provider.Note{ID: 1, Body: body, Author: provider.User{Username: "alice"}}, nil
}

func (p *cmdTestProvider) ListMergeRequests(_ context.Context, project string, _ provider.ListMergeRequestsOptions) ([]provider.MergeRequest, error) {
	return []provider.MergeRequest{defaultMR(project)}, nil
}

func (p *cmdTestProvider) ListMyMergeRequests(_ context.Context, _ provider.ListMyMergeRequestsOptions) ([]provider.MergeRequest, error) {
	return []provider.MergeRequest{defaultMR("group/project")}, nil
}

func (p *cmdTestProvider) GetMergeRequest(_ context.Context, project string, iid int) (provider.MergeRequest, error) {
	mr := defaultMR(project)
	mr.IID = iid
	return mr, nil
}

func (p *cmdTestProvider) CreateMergeRequest(_ context.Context, project string, opts provider.CreateMergeRequestOptions) (provider.MergeRequest, error) {
	mr := defaultMR(project)
	mr.Title = opts.Title
	mr.SourceBranch = opts.SourceBranch
	mr.TargetBranch = opts.TargetBranch
	return mr, nil
}

func (p *cmdTestProvider) UpdateMergeRequest(_ context.Context, project string, iid int, opts provider.UpdateMergeRequestOptions) (provider.MergeRequest, error) {
	mr := defaultMR(project)
	mr.IID = iid
	if opts.Title != nil {
		mr.Title = *opts.Title
	}
	if opts.State != nil {
		mr.State = *opts.State
	}
	return mr, nil
}

func (p *cmdTestProvider) ApproveMergeRequest(_ context.Context, _ string, _ int) error { return nil }

func (p *cmdTestProvider) MergeMergeRequest(_ context.Context, project string, iid int, _ provider.MergeOptions) (provider.MergeRequest, error) {
	mr := defaultMR(project)
	mr.IID = iid
	mr.State = "merged"
	return mr, nil
}

func (p *cmdTestProvider) GetMergeRequestDiff(_ context.Context, _ string, _ int) ([]provider.FileDiff, error) {
	return []provider.FileDiff{{OldPath: "a.go", NewPath: "b.go", Diff: "diff"}}, nil
}

func (p *cmdTestProvider) ListLabels(_ context.Context, _ string) ([]provider.Label, error) {
	return []provider.Label{{ID: 1, Name: "bug", Color: "#FF0000"}}, nil
}

func (p *cmdTestProvider) CreateLabel(_ context.Context, _ string, opts provider.CreateLabelOptions) (provider.Label, error) {
	return provider.Label{ID: 1, Name: opts.Name, Color: opts.Color, Description: opts.Description}, nil
}

func (p *cmdTestProvider) ListUsers(_ context.Context, _ provider.ListUsersOptions) ([]provider.User, error) {
	return []provider.User{{ID: 1, Username: "alice"}}, nil
}

func (p *cmdTestProvider) CreateGroupMilestone(_ context.Context, _ int, opts provider.CreateMilestoneOptions) (provider.Milestone, error) {
	return provider.Milestone{IID: 1, Title: opts.Title, WebURL: "https://gitlab.example.com/groups/group/-/milestones/1"}, nil
}

func (p *cmdTestProvider) ListGroupMilestones(_ context.Context, _ int, opts provider.ListGroupMilestonesOptions) ([]provider.Milestone, error) {
	if opts.Page > 1 {
		return nil, nil
	}
	return []provider.Milestone{{IID: 1, Title: "v1.0"}}, nil
}

func (p *cmdTestProvider) CreateGroupEpic(_ context.Context, _ int, opts provider.CreateEpicOptions) (provider.Epic, error) {
	return provider.Epic{IID: 1, Title: opts.Title, WebURL: "https://gitlab.example.com/groups/group/-/epics/1"}, nil
}

func (p *cmdTestProvider) ListGroupEpics(_ context.Context, _ int, opts provider.ListGroupEpicsOptions) ([]provider.Epic, error) {
	if opts.Page > 1 {
		return nil, nil
	}
	return []provider.Epic{{IID: 1, Title: "epic"}}, nil
}

func (p *cmdTestProvider) LinkIssueToEpic(_ context.Context, _, _, _ int) error { return nil }

func defaultIssue(project string) provider.Issue {
	return provider.Issue{
		ID:      1,
		IID:     1,
		Project: project,
		Title:   "Test issue",
		State:   "opened",
		Author:  provider.User{Username: "alice"},
		Labels:  []string{"bug"},
		WebURL:  "https://gitlab.example.com/" + project + "/-/issues/1",
	}
}

func defaultMR(project string) provider.MergeRequest {
	return provider.MergeRequest{
		ID:           1,
		IID:          1,
		Project:      project,
		Title:        "Test MR",
		State:        "opened",
		SourceBranch: "feature",
		TargetBranch: "main",
		Author:       provider.User{Username: "alice"},
		WebURL:       "https://gitlab.example.com/" + project + "/-/merge_requests/1",
	}
}

// setupTestClient builds a real Client in a temporary directory and installs a
// stub provider on the global glClient/cfg variables used by cmd commands.
func setupTestClient(t *testing.T) *cmdTestProvider {
	t.Helper()
	dir := t.TempDir()
	testCfg := &config.Config{
		Host:           "gitlab.example.com",
		GitLabURL:      "https://gitlab.example.com",
		Token:          "test-token",
		JournalPath:    filepath.Join(dir, "journal.jsonl"),
		CachePath:      filepath.Join(dir, "cache"),
		StatePath:      filepath.Join(dir, "state.db"),
		DefaultProject: "group/project",
		DefaultGroup:   "group",
		Hosts: map[string]*config.HostConfig{
			"gitlab.example.com": {Token: "test-token", APIProtocol: "https"},
		},
	}
	t.Setenv("GLVALET_PROVIDER", "gitlab")

	origClient := glClient
	origCfg := cfg

	c, err := client.New(testCfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	prov := &cmdTestProvider{}
	c.Provider = observed.NewProvider(prov, c.State, testCfg.Host)
	glClient = c
	cfg = testCfg

	t.Cleanup(func() {
		if glClient != nil {
			glClient.Close()
		}
		glClient = origClient
		cfg = origCfg
	})

	return prov
}

// addJournalEntry appends one record to the test client's journal.
// setupTestClientWithServer creates a real Client wired to an httptest GitLab
// server and installs the stub provider on the global cmd variables.
func setupTestClientWithServer(t *testing.T) *cmdTestProvider {
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
				APIProtocol: "https",
				APIHost:     srv.URL,
			},
		},
	}
	t.Setenv("GLVALET_PROVIDER", "gitlab")
	t.Setenv("GLVALET_CACHE", dir)
	t.Setenv("GLVALET_STATE", filepath.Join(dir, "state.db"))
	t.Setenv("GLVALET_JOURNAL", filepath.Join(dir, "journal.jsonl"))

	origClient := glClient
	origCfg := cfg

	c, err := client.New(testCfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	prov := &cmdTestProvider{}
	c.Provider = observed.NewProvider(prov, c.State, testCfg.Host)
	glClient = c
	cfg = testCfg

	t.Cleanup(func() {
		if glClient != nil {
			glClient.Close()
		}
		glClient = origClient
		cfg = origCfg
	})

	return prov
}

func addJournalEntry(t *testing.T, op journal.Op, entity journal.Entity, title string) {
	t.Helper()
	if err := glClient.Rec(op, entity, "group/project", "", 0, 0, title, "https://example.com/t"); err != nil {
		t.Fatalf("record journal: %v", err)
	}
}

// addOldJournalEntry writes a record older than the report window.
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
