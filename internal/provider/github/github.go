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
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
// A milestone title is resolved to its number via ListMilestones first.
func (g *GitHub) ListIssues(ctx context.Context, project string, opts provider.ListIssuesOptions) ([]provider.Issue, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return nil, err
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
	if opts.Milestone != "" {
		num, err := g.milestoneNumberByTitle(ctx, owner, repo, opts.Milestone)
		if err != nil {
			return nil, err
		}
		ghOpts.Milestone = fmt.Sprintf("%d", num)
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
// State is translated to GitHub's state noun (open/closed). A milestone given
// by title (without MilestoneID) is resolved to its number via ListMilestones.
func (g *GitHub) UpdateIssue(ctx context.Context, project string, iid int, opts provider.UpdateIssueOptions) (provider.Issue, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Issue{}, err
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
	} else if opts.Milestone != nil {
		num, err := g.milestoneNumberByTitle(ctx, owner, repo, *opts.Milestone)
		if err != nil {
			return provider.Issue{}, err
		}
		req.Milestone = &num
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

// GetProject fetches repository metadata via the GitHub repos API.
func (g *GitHub) GetProject(ctx context.Context, project string) (provider.Project, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Project{}, err
	}
	r, _, err := g.client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return provider.Project{}, fmt.Errorf("github get project: %w", err)
	}
	openIssues := 0
	if r.OpenIssues != nil {
		openIssues = int(*r.OpenIssues)
	}
	name := ""
	if r.Name != nil {
		name = *r.Name
	}
	fullName := ""
	if r.FullName != nil {
		fullName = *r.FullName
	}
	desc := ""
	if r.Description != nil {
		desc = *r.Description
	}
	webURL := ""
	if r.HTMLURL != nil {
		webURL = *r.HTMLURL
	}
	defaultBranch := ""
	if r.DefaultBranch != nil {
		defaultBranch = *r.DefaultBranch
	}
	archived := false
	if r.Archived != nil {
		archived = *r.Archived
	}
	return provider.Project{
		ID:              int(r.GetID()),
		Name:            name,
		Path:            fullName,
		FullName:        fullName,
		Description:     desc,
		WebURL:          webURL,
		DefaultBranch:   defaultBranch,
		OpenIssuesCount: openIssues,
		Archived:        archived,
	}, nil
}

// ListWorkItems returns ErrUnsupported on GitHub (no first-class work items).
func (g *GitHub) ListWorkItems(context.Context, string, provider.ListWorkItemsOptions) ([]provider.WorkItem, error) {
	return nil, fmt.Errorf("github work items: %w", provider.ErrUnsupported)
}

// CreateWorkItem returns ErrUnsupported on GitHub.
func (g *GitHub) CreateWorkItem(context.Context, string, provider.CreateWorkItemOptions) (provider.WorkItem, error) {
	return provider.WorkItem{}, fmt.Errorf("github work items: %w", provider.ErrUnsupported)
}

// CloseWorkItem returns ErrUnsupported on GitHub.
func (g *GitHub) CloseWorkItem(context.Context, string, int) error {
	return fmt.Errorf("github work items: %w", provider.ErrUnsupported)
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

// ListMyMergeRequests lists pull requests assigned to the authenticated user
// across all repositories, using the GitHub Search API (issues endpoint with
// is:pr). Each result's Project is recovered from the issue's Repository field.
func (g *GitHub) ListMyMergeRequests(ctx context.Context, opts provider.ListMyMergeRequestsOptions) ([]provider.MergeRequest, error) {
	q := "is:pr assignee:@me"
	switch opts.State {
	case "opened", "open":
		q += " is:open"
	case "closed":
		q += " is:closed"
	case "merged":
		q += " is:merged"
	case "all", "":
		// no state filter
	default:
		q += " is:" + opts.State
	}

	res, _, err := g.client.Search.Issues(ctx, q, &gh.SearchOptions{
		ListOptions: gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage},
	})
	if err != nil {
		return nil, fmt.Errorf("github list my merge requests: %w", err)
	}

	result := make([]provider.MergeRequest, 0, len(res.Issues))
	for _, iss := range res.Issues {
		if iss == nil || !iss.IsPullRequest() {
			continue
		}
		// Search results carry a PullRequestLinks blob but not the full PR;
		// the host-neutral MergeRequest is built from the issue-shaped fields.
		project := iss.GetRepository().GetFullName()
		result = append(result, issueToPR(project, iss))
	}
	return result, nil
}

// ApproveMergeRequest submits an APPROVE review on the pull request. GitHub
// models approvals as PR reviews, so this creates a review with Event=APPROVE.
func (g *GitHub) ApproveMergeRequest(ctx context.Context, project string, iid int) error {
	owner, repo, err := splitProject(project)
	if err != nil {
		return err
	}

	_, _, err = g.client.PullRequests.CreateReview(ctx, owner, repo, iid,
		&gh.PullRequestReviewRequest{Event: gh.String("APPROVE")})
	if err != nil {
		return fmt.Errorf("github approve pull request: %w", err)
	}
	return nil
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

// ListMilestones returns repo-scoped milestones for the given owner/repo project.
func (g *GitHub) ListMilestones(ctx context.Context, project string, opts provider.ListMilestonesOptions) ([]provider.Milestone, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return nil, err
	}
	state := opts.State
	if state == "" {
		state = "all"
	}
	ghOpts := &gh.MilestoneListOptions{
		State:       state,
		ListOptions: gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage},
	}
	ms, _, err := g.client.Issues.ListMilestones(ctx, owner, repo, ghOpts)
	if err != nil {
		return nil, fmt.Errorf("github list milestones for %s: %w", project, err)
	}
	result := make([]provider.Milestone, len(ms))
	for i, m := range ms {
		result[i] = provider.Milestone{
			ID:          int(m.GetID()),
			IID:         m.GetNumber(),
			Title:       m.GetTitle(),
			Description: m.GetDescription(),
			State:       m.GetState(),
			WebURL:      m.GetHTMLURL(),
		}
		if m.DueOn != nil {
			result[i].DueDate = m.GetDueOn().Format("2006-01-02")
		}
	}
	return result, nil
}

// GetMilestone returns a single repo-scoped milestone by number.
func (g *GitHub) GetMilestone(ctx context.Context, project string, id int) (provider.Milestone, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Milestone{}, err
	}
	m, _, err := g.client.Issues.GetMilestone(ctx, owner, repo, id)
	if err != nil {
		return provider.Milestone{}, fmt.Errorf("github get milestone: %w", err)
	}
	return ghMilestone(m), nil
}

// ResolveGroup is unsupported on GitHub — orgs have numeric IDs but the
// GitHub adapter does not implement group-epic/group-milestone surfaces.
func (g *GitHub) ResolveGroup(_ context.Context, _ string) (int, error) {
	return 0, provider.ErrUnsupported
}

// ListGroupEpics is unsupported on GitHub (no first-class epic concept).
func (g *GitHub) ListGroupEpics(_ context.Context, _ int, _ provider.ListGroupEpicsOptions) ([]provider.Epic, error) {
	return nil, provider.ErrUnsupported
}

// LinkIssueToEpic is unsupported because GitHub has no first-class epic relation.
func (g *GitHub) LinkIssueToEpic(_ context.Context, _, _, _ int) error {
	return provider.ErrUnsupported
}

// UpdateGroupEpic is unsupported on GitHub (no first-class epic concept).
func (g *GitHub) UpdateGroupEpic(_ context.Context, _ int, _ int, _ provider.UpdateEpicOptions) (provider.Epic, error) {
	return provider.Epic{}, provider.ErrUnsupported
}

// ListEpicIssues is unsupported on GitHub (no first-class epic relation).
func (g *GitHub) ListEpicIssues(_ context.Context, _ int, _ int) ([]provider.Issue, error) {
	return nil, provider.ErrUnsupported
}

// CreateMilestone creates a new repo-scoped milestone on GitHub.
func (g *GitHub) CreateMilestone(ctx context.Context, project string, opts provider.CreateMilestoneOptions) (provider.Milestone, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Milestone{}, err
	}
	ghReq := &gh.Milestone{
		Title:       gh.String(opts.Title),
		Description: gh.String(opts.Description),
	}
	if opts.DueDate != "" {
		t, err := time.Parse("2006-01-02", opts.DueDate)
		if err != nil {
			return provider.Milestone{}, fmt.Errorf("github create milestone: invalid due date: %w", err)
		}
		ghReq.DueOn = &gh.Timestamp{Time: t}
	}
	// GitHub milestones only have a due date, not a start date; StartDate is ignored.

	m, _, err := g.client.Issues.CreateMilestone(ctx, owner, repo, ghReq)
	if err != nil {
		return provider.Milestone{}, fmt.Errorf("github create milestone: %w", err)
	}
	return ghMilestone(m), nil
}

// UpdateMilestone updates an existing repo-scoped milestone on GitHub.
func (g *GitHub) UpdateMilestone(ctx context.Context, project string, id int, opts provider.UpdateMilestoneOptions) (provider.Milestone, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Milestone{}, err
	}
	ghReq := &gh.Milestone{}
	if opts.Title != nil {
		ghReq.Title = gh.String(*opts.Title)
	}
	if opts.Description != nil {
		ghReq.Description = gh.String(*opts.Description)
	}
	if opts.State != nil {
		// GitHub uses "open" | "closed"; neutral is "active" | "closed"
		state := *opts.State
		if state == "active" {
			state = "open"
		}
		if state == "close" || state == "closed" {
			state = "closed"
		}
		ghReq.State = gh.String(state)
	}

	m, _, err := g.client.Issues.EditMilestone(ctx, owner, repo, id, ghReq)
	if err != nil {
		return provider.Milestone{}, fmt.Errorf("github update milestone: %w", err)
	}
	return ghMilestone(m), nil
}

// ─── CI/CD pipeline surface (GitHub Actions) ──────────────────────────────────

// ListPipelines returns GitHub Actions workflow runs for the given repo.
// On GitHub, a "pipeline" maps to a workflow run.
func (g *GitHub) ListPipelines(ctx context.Context, project string, opts provider.ListPipelinesOptions) ([]provider.Pipeline, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return nil, err
	}
	listOpts := &gh.ListWorkflowRunsOptions{
		ListOptions: gh.ListOptions{Page: opts.Page, PerPage: opts.PerPage},
	}
	if opts.Status != "" && opts.Status != "all" {
		// Map neutral statuses to GitHub statuses
		statusMap := map[string]string{
			"running":  "in_progress",
			"pending":  "queued",
			"success":  "success",
			"failed":   "failure",
			"canceled": "cancelled",
		}
		if ghStatus, ok := statusMap[opts.Status]; ok {
			listOpts.Status = ghStatus
		}
	}
	if opts.Ref != "" {
		listOpts.Branch = opts.Ref
	}

	runs, _, err := g.client.Actions.ListRepositoryWorkflowRuns(ctx, owner, repo, listOpts)
	if err != nil {
		return nil, fmt.Errorf("github list workflow runs: %w", err)
	}

	result := make([]provider.Pipeline, len(runs.WorkflowRuns))
	for i, r := range runs.WorkflowRuns {
		result[i] = ghPipeline(project, r)
	}
	return result, nil
}

// GetPipeline returns a single GitHub Actions workflow run.
func (g *GitHub) GetPipeline(ctx context.Context, project string, id int) (provider.Pipeline, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Pipeline{}, err
	}
	run, _, err := g.client.Actions.GetWorkflowRunByID(ctx, owner, repo, int64(id))
	if err != nil {
		return provider.Pipeline{}, fmt.Errorf("github get workflow run: %w", err)
	}
	return ghPipeline(project, run), nil
}

// RunPipeline triggers a GitHub Actions workflow via workflow_dispatch.
// On GitHub, this requires the workflow file name (opts.Workflow) and the
// workflow must support workflow_dispatch triggers.
func (g *GitHub) RunPipeline(ctx context.Context, project string, opts provider.RunPipelineOptions) (provider.Pipeline, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Pipeline{}, err
	}
	if opts.Workflow == "" {
		return provider.Pipeline{}, fmt.Errorf("github run pipeline: --workflow required (workflow file name, e.g. ci.yml): %w", provider.ErrUnsupported)
	}

	event := gh.CreateWorkflowDispatchEventRequest{
		Ref: opts.Ref,
	}
	if len(opts.Variables) > 0 {
		inputs := make(map[string]interface{}, len(opts.Variables))
		for k, v := range opts.Variables {
			inputs[k] = v
		}
		event.Inputs = inputs
	}

	_, err = g.client.Actions.CreateWorkflowDispatchEventByFileName(ctx, owner, repo, opts.Workflow, event)
	if err != nil {
		return provider.Pipeline{}, fmt.Errorf("github run pipeline: %w", err)
	}

	// GitHub's workflow_dispatch endpoint returns 204 No Content — no run
	// object. We return a minimal Pipeline with the known ref and a pending
	// status; the caller can poll ListPipelines to find the actual run.
	return provider.Pipeline{
		Project:   project,
		Status:    "pending",
		Ref:       opts.Ref,
		CommitMsg: fmt.Sprintf("workflow_dispatch: %s on %s", opts.Workflow, opts.Ref),
		Author:    provider.User{},
	}, nil
}

// RetryPipeline reruns a failed GitHub Actions workflow run.
func (g *GitHub) RetryPipeline(ctx context.Context, project string, id int) (provider.Pipeline, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Pipeline{}, err
	}
	_, err = g.client.Actions.RerunWorkflowByID(ctx, owner, repo, int64(id))
	if err != nil {
		return provider.Pipeline{}, fmt.Errorf("github retry workflow run: %w", err)
	}
	// Return the updated run
	return g.GetPipeline(ctx, project, id)
}

// CancelPipeline cancels a running GitHub Actions workflow run.
func (g *GitHub) CancelPipeline(ctx context.Context, project string, id int) error {
	owner, repo, err := splitProject(project)
	if err != nil {
		return err
	}
	_, err = g.client.Actions.CancelWorkflowRunByID(ctx, owner, repo, int64(id))
	if err != nil {
		return fmt.Errorf("github cancel workflow run: %w", err)
	}
	return nil
}

// ListPipelineJobs returns jobs (steps) for a GitHub Actions workflow run.
func (g *GitHub) ListPipelineJobs(ctx context.Context, project string, pipelineID int) ([]provider.Job, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return nil, err
	}
	jobs, _, err := g.client.Actions.ListWorkflowJobs(ctx, owner, repo, int64(pipelineID), nil)
	if err != nil {
		return nil, fmt.Errorf("github list workflow jobs: %w", err)
	}

	result := make([]provider.Job, len(jobs.Jobs))
	for i, j := range jobs.Jobs {
		result[i] = ghJob(j)
	}
	return result, nil
}

// GetJobLogs returns the log output for a single GitHub Actions job.
func (g *GitHub) GetJobLogs(ctx context.Context, project string, jobID int) (string, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return "", err
	}
	// GetWorkflowJobLogs returns a redirect URL to the logs archive.
	logURL, _, err := g.client.Actions.GetWorkflowJobLogs(ctx, owner, repo, int64(jobID), 3)
	if err != nil {
		return "", fmt.Errorf("github get job logs: %w", err)
	}
	resp, err := http.Get(logURL.String())
	if err != nil {
		return "", fmt.Errorf("github get job logs: fetch: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("github read job logs: %w", err)
	}
	return string(b), nil
}

// ListArtifacts returns artifacts for a GitHub Actions workflow run.
func (g *GitHub) ListArtifacts(ctx context.Context, project string, pipelineID int) ([]provider.Artifact, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return nil, err
	}
	artifacts, _, err := g.client.Actions.ListWorkflowRunArtifacts(ctx, owner, repo, int64(pipelineID), nil)
	if err != nil {
		return nil, fmt.Errorf("github list artifacts: %w", err)
	}

	result := make([]provider.Artifact, len(artifacts.Artifacts))
	for i, a := range artifacts.Artifacts {
		result[i] = provider.Artifact{
			Name:    a.GetName(),
			Size:    a.GetSizeInBytes(),
			Expired: a.GetExpired(),
		}
	}
	return result, nil
}

// DownloadArtifact downloads a single GitHub Actions artifact to destDir.
func (g *GitHub) DownloadArtifact(ctx context.Context, project string, artifactID int, destDir string) error {
	owner, repo, err := splitProject(project)
	if err != nil {
		return err
	}
	// GitHub returns a redirect URL for artifact download
	url, _, err := g.client.Actions.DownloadArtifact(ctx, owner, repo, int64(artifactID), 3)
	if err != nil {
		return fmt.Errorf("github download artifact: %w", err)
	}

	resp, err := http.Get(url.String())
	if err != nil {
		return fmt.Errorf("github download artifact: fetch: %w", err)
	}
	defer resp.Body.Close()
	dest := filepath.Join(destDir, "artifact.zip")
	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("github download artifact: create file: %w", err)
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("github download artifact: write: %w", err)
	}
	return nil
}

// ─── GitHub CI/CD conversion helpers ──────────────────────────────────────────

func ghPipeline(project string, r *gh.WorkflowRun) provider.Pipeline {
	out := provider.Pipeline{
		ID:        int(r.GetID()),
		IID:       int(r.GetID()),
		Project:   project,
		Status:    ghRunStatus(r.GetStatus(), r.GetConclusion()),
		Ref:       r.GetHeadBranch(),
		SHA:       r.GetHeadSHA(),
		WebURL:    r.GetHTMLURL(),
		CreatedAt: r.GetCreatedAt().Time,
		UpdatedAt: r.GetUpdatedAt().Time,
	}
	return out
}

func ghJob(j *gh.WorkflowJob) provider.Job {
	out := provider.Job{
		ID:     int(j.GetID()),
		Name:   j.GetName(),
		Status: ghRunStatus(j.GetStatus(), j.GetConclusion()),
		Ref:    j.GetHeadBranch(),
		WebURL: j.GetHTMLURL(),
	}
	if j.StartedAt != nil {
		out.StartedAt = &j.StartedAt.Time
	}
	if j.CompletedAt != nil {
		t := j.CompletedAt.Time
		out.FinishedAt = &t
	}
	return out
}

// ghRunStatus maps GitHub Actions status + conclusion to a neutral status.
func ghRunStatus(status, conclusion string) string {
	if status == "completed" {
		switch conclusion {
		case "success":
			return "success"
		case "failure":
			return "failed"
		case "cancelled":
			return "canceled"
		case "skipped":
			return "skipped"
		case "neutral":
			return "success"
		default:
			return conclusion
		}
	}
	switch status {
	case "in_progress":
		return "running"
	case "queued":
		return "pending"
	default:
		return status
	}
}

func ghMilestone(m *gh.Milestone) provider.Milestone {
	out := provider.Milestone{
		ID:          int(m.GetID()),
		IID:         m.GetNumber(),
		Title:       m.GetTitle(),
		Description: m.GetDescription(),
		State:       m.GetState(),
		WebURL:      m.GetHTMLURL(),
	}
	if m.DueOn != nil {
		out.DueDate = m.GetDueOn().Format("2006-01-02")
	}
	return out
}

// ─── conversion helpers ───────────────────────────────────────────────────────

// milestoneNumberByTitle resolves a milestone title to its GitHub number.
// GitHub's issues API filters by milestone number, not title, so callers
// that receive a neutral title must resolve it first.
func (g *GitHub) milestoneNumberByTitle(ctx context.Context, owner, repo, title string) (int, error) {
	milestones, _, err := g.client.Issues.ListMilestones(ctx, owner, repo, &gh.MilestoneListOptions{
		State:       "all",
		ListOptions: gh.ListOptions{PerPage: 100},
	})
	if err != nil {
		return 0, fmt.Errorf("github resolve milestone %q: %w", title, err)
	}
	for _, m := range milestones {
		if m.GetTitle() == title {
			return m.GetNumber(), nil
		}
	}
	return 0, fmt.Errorf("github milestone %q not found in %s/%s", title, owner, repo)
}

// issueToPR builds a host-neutral MergeRequest from a GitHub Issue returned
// by the Search API. Search results carry issue-shaped fields plus a
// PullRequestLinks blob; the branch names are not available in search results
// and are left empty.
func issueToPR(project string, iss *gh.Issue) provider.MergeRequest {
	out := provider.MergeRequest{
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

	out.CreatedAt = derefTime(iss.CreatedAt)
	out.UpdatedAt = derefTime(iss.UpdatedAt)
	if iss.ClosedAt != nil {
		t := iss.ClosedAt.Time
		out.ClosedAt = &t
	}
	return out
}

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
