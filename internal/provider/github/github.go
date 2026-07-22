// Package github adapts the GitHub REST client to the host-neutral
// provider.Provider interface. Activated only when
// GLVALET_GITHUB_ENABLED=true. Pull requests are exposed via the
// MergeRequest surface; issues via the Issue surface. Epics and
// work items return provider.ErrUnsupported.
package github

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	gh "github.com/google/go-github/v66/github"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/provider"
)

// ErrFeatureDisabled is returned when the GitHub provider is constructed
// without the GLVALET_GITHUB_ENABLED=true environment variable.
var ErrFeatureDisabled = errors.New("github: feature disabled; set GLVALET_GITHUB_ENABLED=true to enable")

// GitHub is the provider.Provider implementation for GitHub and GitHub Enterprise.
type GitHub struct {
	client *gh.Client
	host   string
	token  string
}

// New constructs a GitHub provider bound to cfg. Returns ErrFeatureDisabled
// when the GLVALET_GITHUB_ENABLED flag is not "true".
func New(cfg *config.Config) (*GitHub, error) {
	if os.Getenv("GLVALET_GITHUB_ENABLED") != "true" {
		return nil, ErrFeatureDisabled
	}

	httpClient := &http.Client{}
	if cfg.SkipTLS {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec
			},
		}
	}

	// GitHubToken takes precedence; fall back to the shared Token field.
	token := cfg.GitHubToken
	if token == "" {
		token = cfg.Token
	}

	client := gh.NewClient(httpClient).WithAuthToken(token)

	host := "github.com"
	if cfg.GitHubURL != "" && !isDefaultGitHubAPI(cfg.GitHubURL) {
		var err error
		client, err = client.WithEnterpriseURLs(cfg.GitHubURL, cfg.GitHubURL)
		if err != nil {
			return nil, fmt.Errorf("github provider init enterprise URLs: %w", err)
		}
		if u, parseErr := url.Parse(cfg.GitHubURL); parseErr == nil && u.Host != "" {
			host = u.Host
		}
	}

	return &GitHub{client: client, host: host, token: token}, nil
}

// NewWithClient is used in tests to inject a preconfigured gh.Client.
// It bypasses the feature-flag check and the HTTP client construction.
func NewWithClient(client *gh.Client, host string) *GitHub {
	return &GitHub{client: client, host: host, token: ""}
}

// isDefaultGitHubAPI reports whether rawURL points to the default GitHub API.
func isDefaultGitHubAPI(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Host)
	return h == "github.com" || h == "api.github.com" || rawURL == ""
}

// Kind returns provider.KindGitHub.
func (g *GitHub) Kind() provider.Kind { return provider.KindGitHub }

// Host returns the bare host the provider talks to (e.g. "github.com").
func (g *GitHub) Host() string { return g.host }

// splitProject splits "owner/repo" into (owner, repo). Returns an error
// if the format is not exactly two slash-separated segments.
func splitProject(project string) (owner, repo string, err error) {
	parts := strings.SplitN(project, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("github: project must be 'owner/repo': %q", project)
	}
	return parts[0], parts[1], nil
}

// ListIssues lists issues for the given owner/repo project.
// Pull requests returned by the GitHub issues API are filtered out.
func (g *GitHub) ListIssues(ctx context.Context, project string, opts provider.ListIssuesOptions) ([]provider.Issue, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return nil, err
	}

	// GitHub filters issues by milestone number, not title; resolving a title
	// is unsupported here (consistent with UpdateIssue's milestone handling).
	if opts.Milestone != "" {
		return nil, provider.ErrUnsupported
	}

	ghOpts := &gh.IssueListByRepoOptions{
		ListOptions: gh.ListOptions{
			Page:    opts.Page,
			PerPage: opts.PerPage,
		},
	}
	if opts.State != "" {
		ghOpts.State = opts.State
	}
	if opts.Assignee != "" {
		ghOpts.Assignee = opts.Assignee
	}
	if opts.Author != "" {
		ghOpts.Creator = opts.Author
	}
	if len(opts.Labels) > 0 {
		ghOpts.Labels = opts.Labels
	}

	items, _, err := g.client.Issues.ListByRepo(ctx, owner, repo, ghOpts)
	if err != nil {
		return nil, fmt.Errorf("github list issues: %w", err)
	}

	result := make([]provider.Issue, 0, len(items))
	for _, i := range items {
		// GitHub issues API returns both issues and pull requests;
		// pull requests must be excluded from the issue surface.
		if i.IsPullRequest() {
			continue
		}
		result = append(result, toIssue(project, i))
	}
	return result, nil
}

// ListMyIssues lists issues assigned to the authenticated user across all
// repositories visible to the token. Each issue's Project is recovered from
// its repository. GitHub uses "open" where the neutral surface uses "opened".
func (g *GitHub) ListMyIssues(ctx context.Context, opts provider.ListMyIssuesOptions) ([]provider.Issue, error) {
	ghOpts := &gh.IssueListOptions{
		Filter: "assigned",
		ListOptions: gh.ListOptions{
			Page:    opts.Page,
			PerPage: opts.PerPage,
		},
	}
	switch opts.State {
	case "opened", "open":
		ghOpts.State = "open"
	case "closed":
		ghOpts.State = "closed"
	case "all", "":
		ghOpts.State = "all"
	default:
		ghOpts.State = opts.State
	}

	// all=false scopes to issues assigned to the authenticated user.
	items, _, err := g.client.Issues.List(ctx, false, ghOpts)
	if err != nil {
		return nil, fmt.Errorf("github list my issues: %w", err)
	}

	result := make([]provider.Issue, 0, len(items))
	for _, i := range items {
		if i.IsPullRequest() {
			continue
		}
		result = append(result, toIssue(i.GetRepository().GetFullName(), i))
	}
	return result, nil
}

// GetIssue retrieves a single issue by number.
func (g *GitHub) GetIssue(ctx context.Context, project string, iid int) (provider.Issue, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Issue{}, err
	}

	iss, _, err := g.client.Issues.Get(ctx, owner, repo, iid)
	if err != nil {
		return provider.Issue{}, fmt.Errorf("github get issue: %w", err)
	}
	return toIssue(project, iss), nil
}

// CreateIssue creates a new issue in the given project.
func (g *GitHub) CreateIssue(ctx context.Context, project string, opts provider.CreateIssueOptions) (provider.Issue, error) {
	if opts.Weight != 0 {
		return provider.Issue{}, provider.ErrUnsupported
	}
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Issue{}, err
	}

	req := &gh.IssueRequest{
		Title: gh.String(opts.Title),
		Body:  gh.String(opts.Description),
	}
	if len(opts.Labels) > 0 {
		req.Labels = &opts.Labels
	}

	iss, _, err := g.client.Issues.Create(ctx, owner, repo, req)
	if err != nil {
		return provider.Issue{}, fmt.Errorf("github create issue: %w", err)
	}
	return toIssue(project, iss), nil
}

// UpdateIssue applies a partial update to an existing issue. The neutral
// State is translated to GitHub's state noun (open/closed). A milestone
// given by title (without MilestoneID) is unsupported because the GitHub
// API addresses milestones by number.
func (g *GitHub) UpdateIssue(ctx context.Context, project string, iid int, opts provider.UpdateIssueOptions) (provider.Issue, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Issue{}, err
	}
	if opts.Milestone != nil && opts.MilestoneID == nil {
		return provider.Issue{}, fmt.Errorf("github update issue: milestone by title: %w", provider.ErrUnsupported)
	}

	req := &gh.IssueRequest{}
	if opts.Title != nil {
		req.Title = gh.String(*opts.Title)
	}
	if opts.Description != nil {
		req.Body = gh.String(*opts.Description)
	}
	if opts.State != nil {
		req.State = gh.String(githubState(*opts.State))
	}
	if opts.Labels != nil {
		req.Labels = opts.Labels
	}
	if opts.MilestoneID != nil {
		req.Milestone = opts.MilestoneID
	}

	iss, _, err := g.client.Issues.Edit(ctx, owner, repo, iid, req)
	if err != nil {
		return provider.Issue{}, fmt.Errorf("github update issue: %w", err)
	}
	return toIssue(project, iss), nil
}

// githubState maps a neutral issue state to GitHub's state noun. Anything
// other than a close request opens the issue.
func githubState(neutral string) string {
	switch neutral {
	case "closed", "close":
		return "closed"
	default:
		return "open"
	}
}

// CreateIssueNote adds a comment to an issue (GitHub issue comment).
func (g *GitHub) CreateIssueNote(ctx context.Context, project string, iid int, body string) (provider.Note, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Note{}, err
	}

	c, _, err := g.client.Issues.CreateComment(ctx, owner, repo, iid, &gh.IssueComment{Body: gh.String(body)})
	if err != nil {
		return provider.Note{}, fmt.Errorf("github create issue note: %w", err)
	}
	return toNote(c), nil
}

// ListUsers searches users via the GitHub user search API. GitHub has no
// unfiltered user-listing endpoint, so an empty Search returns ErrUnsupported.
func (g *GitHub) ListUsers(ctx context.Context, opts provider.ListUsersOptions) ([]provider.User, error) {
	if opts.Search == "" {
		return nil, fmt.Errorf("github list users: empty search: %w", provider.ErrUnsupported)
	}

	res, _, err := g.client.Search.Users(ctx, opts.Search, &gh.SearchOptions{
		ListOptions: gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage},
	})
	if err != nil {
		return nil, fmt.Errorf("github list users: %w", err)
	}

	result := make([]provider.User, 0, len(res.Users))
	for _, u := range res.Users {
		if u != nil {
			result = append(result, toUser(u))
		}
	}
	return result, nil
}

// ListMergeRequests lists pull requests for the given owner/repo project.
func (g *GitHub) ListMergeRequests(ctx context.Context, project string, opts provider.ListMergeRequestsOptions) ([]provider.MergeRequest, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return nil, err
	}

	ghOpts := &gh.PullRequestListOptions{
		ListOptions: gh.ListOptions{
			Page:    opts.Page,
			PerPage: opts.PerPage,
		},
	}
	if opts.State != "" {
		ghOpts.State = opts.State
	}
	if opts.SourceBranch != "" {
		ghOpts.Head = opts.SourceBranch
	}
	if opts.TargetBranch != "" {
		ghOpts.Base = opts.TargetBranch
	}

	prs, _, err := g.client.PullRequests.List(ctx, owner, repo, ghOpts)
	if err != nil {
		return nil, fmt.Errorf("github list pull requests: %w", err)
	}

	result := make([]provider.MergeRequest, len(prs))
	for i, pr := range prs {
		result[i] = toPR(project, pr)
	}
	return result, nil
}

// GetMergeRequest retrieves a single pull request by number.
func (g *GitHub) GetMergeRequest(ctx context.Context, project string, iid int) (provider.MergeRequest, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.MergeRequest{}, err
	}

	pr, _, err := g.client.PullRequests.Get(ctx, owner, repo, iid)
	if err != nil {
		return provider.MergeRequest{}, fmt.Errorf("github get pull request: %w", err)
	}
	return toPR(project, pr), nil
}

// CreateMergeRequest opens a new pull request. AssigneeID is ignored because
// GitHub addresses assignees by login, not numeric ID.
func (g *GitHub) CreateMergeRequest(ctx context.Context, project string, opts provider.CreateMergeRequestOptions) (provider.MergeRequest, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.MergeRequest{}, err
	}

	pr, _, err := g.client.PullRequests.Create(ctx, owner, repo, &gh.NewPullRequest{
		Title: gh.String(opts.Title),
		Head:  gh.String(opts.SourceBranch),
		Base:  gh.String(opts.TargetBranch),
		Body:  gh.String(opts.Description),
	})
	if err != nil {
		return provider.MergeRequest{}, fmt.Errorf("github create pull request: %w", err)
	}
	return toPR(project, pr), nil
}

// UpdateMergeRequest applies a partial update to a pull request. The neutral
// State is translated to GitHub's state noun (open/closed).
func (g *GitHub) UpdateMergeRequest(ctx context.Context, project string, iid int, opts provider.UpdateMergeRequestOptions) (provider.MergeRequest, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.MergeRequest{}, err
	}

	patch := &gh.PullRequest{}
	if opts.Title != nil {
		patch.Title = gh.String(*opts.Title)
	}
	if opts.Description != nil {
		patch.Body = gh.String(*opts.Description)
	}
	if opts.State != nil {
		patch.State = gh.String(githubState(*opts.State))
	}
	if opts.TargetBranch != nil {
		patch.Base = &gh.PullRequestBranch{Ref: gh.String(*opts.TargetBranch)}
	}

	pr, _, err := g.client.PullRequests.Edit(ctx, owner, repo, iid, patch)
	if err != nil {
		return provider.MergeRequest{}, fmt.Errorf("github update pull request: %w", err)
	}
	return toPR(project, pr), nil
}

// GetMergeRequestDiff returns the per-file changes of a pull request via the
// ListFiles API, mapping GitHub's status string to the neutral flags.
func (g *GitHub) GetMergeRequestDiff(ctx context.Context, project string, iid int) ([]provider.FileDiff, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return nil, err
	}

	files, _, err := g.client.PullRequests.ListFiles(ctx, owner, repo, iid, &gh.ListOptions{PerPage: 100})
	if err != nil {
		return nil, fmt.Errorf("github get pull request diff: %w", err)
	}

	out := make([]provider.FileDiff, len(files))
	for i, f := range files {
		newPath := f.GetFilename()
		oldPath := f.GetPreviousFilename()
		if oldPath == "" {
			oldPath = newPath
		}
		out[i] = provider.FileDiff{
			OldPath:     oldPath,
			NewPath:     newPath,
			Diff:        f.GetPatch(),
			NewFile:     f.GetStatus() == "added",
			DeletedFile: f.GetStatus() == "removed",
			RenamedFile: f.GetStatus() == "renamed",
		}
	}
	return out, nil
}

// ListMyMergeRequests is unsupported: GitHub has no cross-repo "assigned to me"
// PR scope on the PullRequests API (it requires the Search API). Use a
// repo-scoped ListMergeRequests instead.
func (g *GitHub) ListMyMergeRequests(_ context.Context, _ provider.ListMyMergeRequestsOptions) ([]provider.MergeRequest, error) {
	return nil, provider.ErrUnsupported
}

// ApproveMergeRequest is unsupported: GitHub approvals are pull-request reviews
// submitted by another user, not a first-class self-service approve action.
func (g *GitHub) ApproveMergeRequest(_ context.Context, _ string, _ int) error {
	return provider.ErrUnsupported
}

// MergeMergeRequest merges a pull request. Squash maps to the GitHub "squash"
// merge method; branch deletion is a separate Git ref operation and is not
// performed here.
func (g *GitHub) MergeMergeRequest(ctx context.Context, project string, iid int, opts provider.MergeOptions) (provider.MergeRequest, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.MergeRequest{}, err
	}

	mopts := &gh.PullRequestOptions{}
	if opts.Squash {
		mopts.MergeMethod = "squash"
	}
	if _, _, err := g.client.PullRequests.Merge(ctx, owner, repo, iid, "", mopts); err != nil {
		return provider.MergeRequest{}, fmt.Errorf("github merge pull request: %w", err)
	}

	// Re-fetch the merged PR for an accurate post-merge view.
	pr, _, err := g.client.PullRequests.Get(ctx, owner, repo, iid)
	if err != nil {
		return provider.MergeRequest{}, fmt.Errorf("github get merged pull request: %w", err)
	}
	return toPR(project, pr), nil
}

// ListLabels retrieves all labels for the given project.
func (g *GitHub) ListLabels(ctx context.Context, project string) ([]provider.Label, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return nil, err
	}

	labels, _, err := g.client.Issues.ListLabels(ctx, owner, repo, &gh.ListOptions{PerPage: 100})
	if err != nil {
		return nil, fmt.Errorf("github list labels: %w", err)
	}

	result := make([]provider.Label, len(labels))
	for i, lbl := range labels {
		result[i] = toLabel(lbl)
	}
	return result, nil
}

// CreateLabel creates a new label in the given project. GitHub stores colors
// without a leading '#', so it is stripped before the call.
func (g *GitHub) CreateLabel(ctx context.Context, project string, opts provider.CreateLabelOptions) (provider.Label, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Label{}, err
	}

	color := strings.TrimPrefix(opts.Color, "#")
	lbl, _, err := g.client.Issues.CreateLabel(ctx, owner, repo, &gh.Label{
		Name:        gh.String(opts.Name),
		Color:       gh.String(color),
		Description: gh.String(opts.Description),
	})
	if err != nil {
		return provider.Label{}, fmt.Errorf("github create label: %w", err)
	}
	return toLabel(lbl), nil
}

// CreateGroupMilestone is unsupported on GitHub because milestones are
// repo-scoped and the neutral interface carries a groupID with no repo mapping.
func (g *GitHub) CreateGroupMilestone(ctx context.Context, groupID int, opts provider.CreateMilestoneOptions) (provider.Milestone, error) {
	return provider.Milestone{}, provider.ErrUnsupported
}

// CreateGroupEpic is unsupported on GitHub (no first-class epic concept).
func (g *GitHub) CreateGroupEpic(ctx context.Context, groupID int, opts provider.CreateEpicOptions) (provider.Epic, error) {
	return provider.Epic{}, provider.ErrUnsupported
}

// ListGroupMilestones is unsupported on GitHub because milestones are repo-scoped,
// not org/group-scoped; groupID has no equivalent in the GitHub API.
func (g *GitHub) ListGroupMilestones(_ context.Context, _ int, _ provider.ListGroupMilestonesOptions) ([]provider.Milestone, error) {
	return nil, provider.ErrUnsupported
}

// ListGroupEpics is unsupported on GitHub (no first-class epic concept).
func (g *GitHub) ListGroupEpics(_ context.Context, _ int, _ provider.ListGroupEpicsOptions) ([]provider.Epic, error) {
	return nil, provider.ErrUnsupported
}

// LinkIssueToEpic is unsupported because GitHub has no first-class epic relation.
func (g *GitHub) LinkIssueToEpic(_ context.Context, _, _, _ int) error {
	return provider.ErrUnsupported
}

// ─── conversion helpers ───────────────────────────────────────────────────────

// toIssue converts a GitHub issue to the host-neutral provider.Issue.
func toIssue(project string, iss *gh.Issue) provider.Issue {
	out := provider.Issue{
		ID:      int(iss.GetID()),
		IID:     iss.GetNumber(),
		Project: project,
		Title:   iss.GetTitle(),
		Body:    iss.GetBody(),
		State:   iss.GetState(),
		WebURL:  iss.GetHTMLURL(),
	}

	if iss.User != nil {
		out.Author = toUser(iss.User)
	}

	for _, a := range iss.Assignees {
		if a != nil {
			out.Assignees = append(out.Assignees, toUser(a))
		}
	}

	for _, l := range iss.Labels {
		if l != nil {
			out.Labels = append(out.Labels, l.GetName())
		}
	}

	if iss.Milestone != nil {
		out.Milestone = iss.Milestone.GetTitle()
	}

	out.CreatedAt = derefTime(iss.CreatedAt)
	out.UpdatedAt = derefTime(iss.UpdatedAt)

	if iss.ClosedAt != nil {
		t := iss.ClosedAt.Time
		out.ClosedAt = &t
	}

	return out
}

// toPR converts a GitHub pull request to the host-neutral provider.MergeRequest.
func toPR(project string, pr *gh.PullRequest) provider.MergeRequest {
	out := provider.MergeRequest{
		ID:           int(pr.GetID()),
		IID:          pr.GetNumber(),
		Project:      project,
		Title:        pr.GetTitle(),
		Body:         pr.GetBody(),
		State:        pr.GetState(),
		WebURL:       pr.GetHTMLURL(),
		SourceBranch: pr.GetHead().GetRef(),
		TargetBranch: pr.GetBase().GetRef(),
	}

	if pr.User != nil {
		out.Author = toUser(pr.User)
	}

	for _, a := range pr.Assignees {
		if a != nil {
			out.Assignees = append(out.Assignees, toUser(a))
		}
	}

	for _, r := range pr.RequestedReviewers {
		if r != nil {
			out.Reviewers = append(out.Reviewers, toUser(r))
		}
	}

	for _, l := range pr.Labels {
		if l != nil {
			out.Labels = append(out.Labels, l.GetName())
		}
	}

	out.CreatedAt = derefTime(pr.CreatedAt)
	out.UpdatedAt = derefTime(pr.UpdatedAt)

	if pr.MergedAt != nil {
		t := pr.MergedAt.Time
		out.MergedAt = &t
	}
	if pr.ClosedAt != nil {
		t := pr.ClosedAt.Time
		out.ClosedAt = &t
	}

	return out
}

// toNote converts a GitHub issue comment to the host-neutral provider.Note.
func toNote(c *gh.IssueComment) provider.Note {
	out := provider.Note{
		ID:        int(c.GetID()),
		Body:      c.GetBody(),
		CreatedAt: derefTime(c.CreatedAt),
		UpdatedAt: derefTime(c.UpdatedAt),
	}
	if c.User != nil {
		out.Author = toUser(c.User)
	}
	return out
}

// toLabel converts a GitHub label to the host-neutral provider.Label.
func toLabel(lbl *gh.Label) provider.Label {
	return provider.Label{
		ID:          int(lbl.GetID()),
		Name:        lbl.GetName(),
		Color:       "#" + lbl.GetColor(),
		Description: lbl.GetDescription(),
	}
}

// toUser converts a GitHub user to the host-neutral provider.User.
func toUser(u *gh.User) provider.User {
	return provider.User{
		ID:       int(u.GetID()),
		Username: u.GetLogin(),
		Name:     u.GetName(),
		Email:    u.GetEmail(),
		WebURL:   u.GetHTMLURL(),
	}
}

// derefTime dereferences a *gh.Timestamp, returning time.Time{} on nil.
func derefTime(t *gh.Timestamp) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.Time
}
