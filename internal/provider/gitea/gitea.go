// Package gitea adapts the Gitea REST client (gitea.dev/sdk) to the
// host-neutral provider.Provider interface. It targets Gitea instances
// configured via the tea CLI (~/.config/tea/config.yml) and is [S]
// experimental: group milestones, epics, and cross-repo "my merge requests"
// return provider.ErrUnsupported.
package gitea

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	giteasdk "gitea.dev/sdk"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/provider"
)

// Gitea is the provider.Provider implementation for Gitea instances.
type Gitea struct {
	client *giteasdk.Client
	host   string
}

// New returns a provider bound to cfg.GiteaURL and cfg.Token.
func New(cfg *config.Config) (*Gitea, error) {
	httpClient := &http.Client{}
	if cfg.SkipTLS {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec
			},
		}
	}

	baseURL := cfg.GiteaURL
	if baseURL == "" {
		return nil, fmt.Errorf("gitea provider init: no GiteaURL in config")
	}

	c, err := giteasdk.NewClient(baseURL,
		giteasdk.SetToken(cfg.Token),
		giteasdk.SetHTTPClient(httpClient),
	)
	if err != nil {
		return nil, fmt.Errorf("gitea provider init: %w", err)
	}

	host := cfg.Host
	if u, parseErr := url.Parse(baseURL); parseErr == nil && u.Host != "" {
		host = u.Host
	}

	return &Gitea{client: c, host: host}, nil
}

// Kind returns provider.KindGitea.
func (g *Gitea) Kind() provider.Kind { return provider.KindGitea }

// Host returns the bare host (no scheme) the provider talks to.
func (g *Gitea) Host() string { return g.host }

// splitProject splits "owner/repo" into (owner, repo). Returns an error
// if the format is not exactly two slash-separated segments.
func splitProject(project string) (owner, repo string, err error) {
	parts := strings.SplitN(project, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("gitea: project must be 'owner/repo': %q", project)
	}
	return parts[0], parts[1], nil
}

// ─── Issue surface ────────────────────────────────────────────────────────────

// ListIssues lists issues (not pull requests) in the given repository.
func (g *Gitea) ListIssues(ctx context.Context, project string, opts provider.ListIssuesOptions) ([]provider.Issue, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return nil, err
	}

	giteaOpts := giteasdk.ListIssueOption{
		ListOptions: giteasdk.ListOptions{Page: opts.Page, PageSize: opts.PerPage},
		Type:        giteasdk.IssueTypeIssue,
	}
	if opts.State != "" {
		giteaOpts.State = giteaState(opts.State)
	}
	if len(opts.Labels) > 0 {
		giteaOpts.Labels = opts.Labels
	}
	if opts.Author != "" {
		giteaOpts.CreatedBy = opts.Author
	}
	if opts.Assignee != "" {
		giteaOpts.AssignedBy = opts.Assignee
	}
	if opts.Milestone != "" {
		giteaOpts.Milestones = []string{opts.Milestone}
	}
	if opts.Search != "" {
		giteaOpts.KeyWord = opts.Search
	}

	issues, _, err := g.client.Issues.ListRepoIssues(ctx, owner, repo, giteaOpts)
	if err != nil {
		return nil, fmt.Errorf("gitea list issues: %w", err)
	}

	result := make([]provider.Issue, 0, len(issues))
	for _, iss := range issues {
		result = append(result, toIssue(project, iss))
	}
	return result, nil
}

// ListMyIssues lists issues assigned to the authenticated user across all
// repositories visible to the token.
func (g *Gitea) ListMyIssues(ctx context.Context, opts provider.ListMyIssuesOptions) ([]provider.Issue, error) {
	giteaOpts := giteasdk.ListIssueOption{
		ListOptions: giteasdk.ListOptions{Page: opts.Page, PageSize: opts.PerPage},
		Type:        giteasdk.IssueTypeIssue,
	}
	if opts.State != "" {
		giteaOpts.State = giteaState(opts.State)
	}

	issues, _, err := g.client.Issues.ListIssues(ctx, giteaOpts)
	if err != nil {
		return nil, fmt.Errorf("gitea list my issues: %w", err)
	}

	result := make([]provider.Issue, 0, len(issues))
	for _, iss := range issues {
		project := projectFromIssue(iss)
		result = append(result, toIssue(project, iss))
	}
	return result, nil
}

// GetIssue retrieves a single issue by index.
func (g *Gitea) GetIssue(ctx context.Context, project string, iid int) (provider.Issue, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Issue{}, err
	}

	iss, _, err := g.client.Issues.GetIssue(ctx, owner, repo, int64(iid))
	if err != nil {
		return provider.Issue{}, fmt.Errorf("gitea get issue: %w", err)
	}
	return toIssue(project, iss), nil
}

// CreateIssue creates a new issue in the given repository.
func (g *Gitea) CreateIssue(ctx context.Context, project string, opts provider.CreateIssueOptions) (provider.Issue, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Issue{}, err
	}
	if opts.Weight != 0 {
		return provider.Issue{}, provider.ErrUnsupported
	}

	createOpts := giteasdk.CreateIssueOption{
		Title: opts.Title,
		Body:  opts.Description,
	}

	if len(opts.AssigneeIDs) > 0 {
		names, err := g.usernamesByIDs(ctx, opts.AssigneeIDs)
		if err != nil {
			return provider.Issue{}, err
		}
		createOpts.Assignees = names
	}

	if opts.MilestoneID != nil {
		createOpts.Milestone = int64(*opts.MilestoneID)
	} else if opts.Milestone != "" {
		mid, err := g.milestoneIDByName(ctx, owner, repo, opts.Milestone)
		if err != nil {
			return provider.Issue{}, err
		}
		createOpts.Milestone = mid
	}

	if len(opts.Labels) > 0 {
		labelIDs, err := g.labelIDsByNames(ctx, owner, repo, opts.Labels)
		if err != nil {
			return provider.Issue{}, err
		}
		createOpts.Labels = labelIDs
	}

	iss, _, err := g.client.Issues.CreateIssue(ctx, owner, repo, createOpts)
	if err != nil {
		return provider.Issue{}, fmt.Errorf("gitea create issue: %w", err)
	}
	return toIssue(project, iss), nil
}

// UpdateIssue applies a partial update to an existing issue, including a
// full label replacement when opts.Labels is non-nil.
func (g *Gitea) UpdateIssue(ctx context.Context, project string, iid int, opts provider.UpdateIssueOptions) (provider.Issue, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Issue{}, err
	}

	// Fetch the current issue so we can send the existing title/body when
	// the caller only wants to mutate state, labels, or milestone.
	current, _, err := g.client.Issues.GetIssue(ctx, owner, repo, int64(iid))
	if err != nil {
		return provider.Issue{}, fmt.Errorf("gitea get issue before update: %w", err)
	}

	title := current.Title
	if opts.Title != nil {
		title = *opts.Title
	}

	editOpts := giteasdk.EditIssueOption{
		Title: title,
		Body:  &current.Body,
	}
	if opts.Description != nil {
		editOpts.Body = opts.Description
	}
	if opts.State != nil {
		state := giteaState(*opts.State)
		editOpts.State = &state
	}

	if opts.MilestoneID != nil {
		editOpts.Milestone = (*int64)(&[]int64{int64(*opts.MilestoneID)}[0])
	} else if opts.Milestone != nil {
		mid, err := g.milestoneIDByName(ctx, owner, repo, *opts.Milestone)
		if err != nil {
			return provider.Issue{}, err
		}
		editOpts.Milestone = &mid
	}

	if opts.Labels != nil {
		labelIDs, err := g.labelIDsByNames(ctx, owner, repo, *opts.Labels)
		if err != nil {
			return provider.Issue{}, err
		}
		if _, _, err := g.client.Issues.ReplaceIssueLabels(ctx, owner, repo, int64(iid),
			giteasdk.IssueLabelsOption{Labels: labelIDs}); err != nil {
			return provider.Issue{}, fmt.Errorf("gitea replace issue labels: %w", err)
		}
	}

	iss, _, err := g.client.Issues.EditIssue(ctx, owner, repo, int64(iid), editOpts)
	if err != nil {
		return provider.Issue{}, fmt.Errorf("gitea update issue: %w", err)
	}
	return toIssue(project, iss), nil
}

// CreateIssueNote adds a comment to an issue.
func (g *Gitea) CreateIssueNote(ctx context.Context, project string, iid int, body string) (provider.Note, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Note{}, err
	}

	c, _, err := g.client.Issues.CreateIssueComment(ctx, owner, repo, int64(iid),
		giteasdk.CreateIssueCommentOption{Body: body})
	if err != nil {
		return provider.Note{}, fmt.Errorf("gitea create issue note: %w", err)
	}
	return toNote(c), nil
}

// ─── Merge request (pull request) surface ─────────────────────────────────────

// ListMergeRequests lists pull requests for the given repository, filtering
// client-side for fields Gitea's list endpoint does not expose.
func (g *Gitea) ListMergeRequests(ctx context.Context, project string, opts provider.ListMergeRequestsOptions) ([]provider.MergeRequest, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return nil, err
	}

	giteaOpts := giteasdk.ListPullRequestsOptions{
		ListOptions: giteasdk.ListOptions{Page: opts.Page, PageSize: opts.PerPage},
	}
	if opts.State != "" {
		giteaOpts.State = giteaState(opts.State)
	}

	prs, _, err := g.client.PullRequests.ListRepoPullRequests(ctx, owner, repo, giteaOpts)
	if err != nil {
		return nil, fmt.Errorf("gitea list pull requests: %w", err)
	}

	result := make([]provider.MergeRequest, 0, len(prs))
	for _, pr := range prs {
		if opts.Author != "" && (pr.Poster == nil || pr.Poster.UserName != opts.Author) {
			continue
		}
		if opts.SourceBranch != "" && (pr.Head == nil || pr.Head.Ref != opts.SourceBranch) {
			continue
		}
		if opts.TargetBranch != "" && (pr.Base == nil || pr.Base.Ref != opts.TargetBranch) {
			continue
		}
		if len(opts.Labels) > 0 && !hasAllLabelNames(pr.Labels, opts.Labels) {
			continue
		}
		result = append(result, toPR(project, pr))
	}
	return result, nil
}

// ListMyMergeRequests is unsupported: Gitea has no cross-repo "assigned to me"
// pull-request endpoint.
func (g *Gitea) ListMyMergeRequests(_ context.Context, _ provider.ListMyMergeRequestsOptions) ([]provider.MergeRequest, error) {
	return nil, provider.ErrUnsupported
}

// GetMergeRequest retrieves a single pull request by index.
func (g *Gitea) GetMergeRequest(ctx context.Context, project string, iid int) (provider.MergeRequest, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.MergeRequest{}, err
	}

	pr, _, err := g.client.PullRequests.GetPullRequest(ctx, owner, repo, int64(iid))
	if err != nil {
		return provider.MergeRequest{}, fmt.Errorf("gitea get pull request: %w", err)
	}
	return toPR(project, pr), nil
}

// CreateMergeRequest opens a new pull request.
func (g *Gitea) CreateMergeRequest(ctx context.Context, project string, opts provider.CreateMergeRequestOptions) (provider.MergeRequest, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.MergeRequest{}, err
	}

	createOpts := giteasdk.CreatePullRequestOption{
		Title: opts.Title,
		Head:  opts.SourceBranch,
		Base:  opts.TargetBranch,
		Body:  opts.Description,
	}

	if opts.AssigneeID != nil {
		u, _, err := g.client.Users.GetUserByID(ctx, int64(*opts.AssigneeID))
		if err != nil {
			return provider.MergeRequest{}, fmt.Errorf("gitea resolve assignee: %w", err)
		}
		createOpts.Assignees = []string{u.UserName}
	}

	pr, _, err := g.client.PullRequests.CreatePullRequest(ctx, owner, repo, createOpts)
	if err != nil {
		return provider.MergeRequest{}, fmt.Errorf("gitea create pull request: %w", err)
	}
	return toPR(project, pr), nil
}

// UpdateMergeRequest applies a partial update to a pull request.
func (g *Gitea) UpdateMergeRequest(ctx context.Context, project string, iid int, opts provider.UpdateMergeRequestOptions) (provider.MergeRequest, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.MergeRequest{}, err
	}

	current, _, err := g.client.PullRequests.GetPullRequest(ctx, owner, repo, int64(iid))
	if err != nil {
		return provider.MergeRequest{}, fmt.Errorf("gitea get pull request before update: %w", err)
	}

	title := current.Title
	if opts.Title != nil {
		title = *opts.Title
	}
	body := current.Body
	if opts.Description != nil {
		body = *opts.Description
	}
	base := ""
	if current.Base != nil {
		base = current.Base.Ref
	}
	if opts.TargetBranch != nil {
		base = *opts.TargetBranch
	}

	editOpts := giteasdk.EditPullRequestOption{
		Title: title,
		Body:  &body,
		Base:  base,
	}
	if opts.State != nil {
		state := giteaState(*opts.State)
		editOpts.State = &state
	}

	pr, _, err := g.client.PullRequests.EditPullRequest(ctx, owner, repo, int64(iid), editOpts)
	if err != nil {
		return provider.MergeRequest{}, fmt.Errorf("gitea update pull request: %w", err)
	}
	return toPR(project, pr), nil
}

// ApproveMergeRequest records an approval by submitting an APPROVED review.
func (g *Gitea) ApproveMergeRequest(ctx context.Context, project string, iid int) error {
	owner, repo, err := splitProject(project)
	if err != nil {
		return err
	}

	pr, _, err := g.client.PullRequests.GetPullRequest(ctx, owner, repo, int64(iid))
	if err != nil {
		return fmt.Errorf("gitea get pull request before approve: %w", err)
	}
	if pr.Head == nil || pr.Head.Sha == "" {
		return fmt.Errorf("gitea approve: pull request head sha unavailable")
	}

	_, _, err = g.client.PullRequests.CreatePullReview(ctx, owner, repo, int64(iid),
		giteasdk.CreatePullReviewOptions{
			State:    giteasdk.ReviewStateApproved,
			CommitID: pr.Head.Sha,
		})
	if err != nil {
		return fmt.Errorf("gitea approve pull request: %w", err)
	}
	return nil
}

// MergeMergeRequest merges a pull request.
func (g *Gitea) MergeMergeRequest(ctx context.Context, project string, iid int, opts provider.MergeOptions) (provider.MergeRequest, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.MergeRequest{}, err
	}

	style := giteasdk.MergeStyleMerge
	if opts.Squash {
		style = giteasdk.MergeStyleSquash
	}
	deleteBranch := opts.RemoveSourceBranch

	_, _, err = g.client.PullRequests.MergePullRequest(ctx, owner, repo, int64(iid),
		giteasdk.MergePullRequestOption{
			Style:                  style,
			DeleteBranchAfterMerge: &deleteBranch,
		})
	if err != nil {
		return provider.MergeRequest{}, fmt.Errorf("gitea merge pull request: %w", err)
	}

	pr, _, err := g.client.PullRequests.GetPullRequest(ctx, owner, repo, int64(iid))
	if err != nil {
		return provider.MergeRequest{}, fmt.Errorf("gitea get merged pull request: %w", err)
	}
	return toPR(project, pr), nil
}

// GetMergeRequestDiff returns the per-file changes of a pull request. Gitea's
// changed-file endpoint exposes paths and status but not the patch text, so
// the raw unified diff is fetched separately and split into per-file sections
// that are attributed to each ChangedFile by path.
func (g *Gitea) GetMergeRequestDiff(ctx context.Context, project string, iid int) ([]provider.FileDiff, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return nil, err
	}

	files, _, err := g.client.PullRequests.ListPullRequestFiles(ctx, owner, repo, int64(iid),
		giteasdk.ListPullRequestFilesOptions{ListOptions: giteasdk.ListOptions{Page: 1, PageSize: 100}})
	if err != nil {
		return nil, fmt.Errorf("gitea list pull request files: %w", err)
	}

	raw, _, err := g.client.PullRequests.GetPullRequestDiff(ctx, owner, repo, int64(iid),
		giteasdk.PullRequestDiffOptions{Binary: false})
	if err != nil {
		return nil, fmt.Errorf("gitea get pull request diff: %w", err)
	}
	patches := parseUnifiedDiff(string(raw))

	out := make([]provider.FileDiff, 0, len(files))
	for _, f := range files {
		if f == nil {
			continue
		}
		oldPath := f.PreviousFilename
		newPath := f.Filename
		if oldPath == "" {
			oldPath = newPath
		}
		fd := provider.FileDiff{
			OldPath:     oldPath,
			NewPath:     newPath,
			Diff:        patches[newPath],
			NewFile:     f.Status == "added",
			DeletedFile: f.Status == "removed",
			RenamedFile: f.Status == "renamed",
		}
		out = append(out, fd)
	}
	return out, nil
}

// ─── Label surface ────────────────────────────────────────────────────────────

// ListLabels retrieves all labels for the given repository.
func (g *Gitea) ListLabels(ctx context.Context, project string) ([]provider.Label, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return nil, err
	}

	labels, _, err := g.client.Repositories.ListRepoLabels(ctx, owner, repo,
		giteasdk.ListLabelsOptions{ListOptions: giteasdk.ListOptions{Page: 1, PageSize: 100}})
	if err != nil {
		return nil, fmt.Errorf("gitea list labels: %w", err)
	}

	result := make([]provider.Label, 0, len(labels))
	for _, l := range labels {
		result = append(result, toLabel(l))
	}
	return result, nil
}

// CreateLabel creates a new label in the given repository.
func (g *Gitea) CreateLabel(ctx context.Context, project string, opts provider.CreateLabelOptions) (provider.Label, error) {
	owner, repo, err := splitProject(project)
	if err != nil {
		return provider.Label{}, err
	}

	color := strings.TrimPrefix(opts.Color, "#")
	lbl, _, err := g.client.Repositories.CreateLabel(ctx, owner, repo,
		giteasdk.CreateLabelOption{
			Name:        opts.Name,
			Color:       color,
			Description: opts.Description,
		})
	if err != nil {
		return provider.Label{}, fmt.Errorf("gitea create label: %w", err)
	}
	return toLabel(lbl), nil
}

// ─── User surface ─────────────────────────────────────────────────────────────

// ListUsers searches users by username. An empty search returns ErrUnsupported
// because Gitea has no unfiltered user-listing endpoint.
func (g *Gitea) ListUsers(ctx context.Context, opts provider.ListUsersOptions) ([]provider.User, error) {
	if opts.Search == "" {
		return nil, fmt.Errorf("gitea list users: empty search: %w", provider.ErrUnsupported)
	}

	res, _, err := g.client.Users.SearchUsers(ctx, giteasdk.SearchUsersOption{
		ListOptions: giteasdk.ListOptions{Page: opts.Page, PageSize: opts.PerPage},
		KeyWord:     opts.Search,
	})
	if err != nil {
		return nil, fmt.Errorf("gitea search users: %w", err)
	}

	result := make([]provider.User, 0, len(res))
	for _, u := range res {
		if u != nil {
			result = append(result, toUser(u))
		}
	}
	return result, nil
}

// ─── Group milestone / epic surface (unsupported on Gitea) ────────────────────

// CreateGroupMilestone is unsupported: Gitea milestones are repo-scoped.
func (g *Gitea) CreateGroupMilestone(_ context.Context, _ int, _ provider.CreateMilestoneOptions) (provider.Milestone, error) {
	return provider.Milestone{}, provider.ErrUnsupported
}

// ListGroupMilestones is unsupported: Gitea milestones are repo-scoped.
func (g *Gitea) ListGroupMilestones(_ context.Context, _ int, _ provider.ListGroupMilestonesOptions) ([]provider.Milestone, error) {
	return nil, provider.ErrUnsupported
}

// CreateGroupEpic is unsupported: Gitea has no first-class epic concept.
func (g *Gitea) CreateGroupEpic(_ context.Context, _ int, _ provider.CreateEpicOptions) (provider.Epic, error) {
	return provider.Epic{}, provider.ErrUnsupported
}

// ListGroupEpics is unsupported: Gitea has no first-class epic concept.
func (g *Gitea) ListGroupEpics(_ context.Context, _ int, _ provider.ListGroupEpicsOptions) ([]provider.Epic, error) {
	return nil, provider.ErrUnsupported
}

// LinkIssueToEpic is unsupported because Gitea has no first-class epic relation.
func (g *Gitea) LinkIssueToEpic(_ context.Context, _, _, _ int) error {
	return provider.ErrUnsupported
}

// ─── internal helpers ─────────────────────────────────────────────────────────

// labelIDsByNames resolves label names to Gitea label IDs. It is not
// paginated; it fetches the first 100 labels and builds a name-to-ID map.
func (g *Gitea) labelIDsByNames(ctx context.Context, owner, repo string, names []string) ([]int64, error) {
	labels, _, err := g.client.Repositories.ListRepoLabels(ctx, owner, repo,
		giteasdk.ListLabelsOptions{ListOptions: giteasdk.ListOptions{Page: 1, PageSize: 100}})
	if err != nil {
		return nil, fmt.Errorf("gitea list labels for resolve: %w", err)
	}

	byName := make(map[string]int64, len(labels))
	for _, l := range labels {
		if l != nil {
			byName[l.Name] = l.ID
		}
	}

	var ids []int64
	for _, name := range names {
		id, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("gitea: label %q not found in %s/%s", name, owner, repo)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// milestoneIDByName resolves a milestone title to its Gitea ID.
func (g *Gitea) milestoneIDByName(ctx context.Context, owner, repo, name string) (int64, error) {
	m, _, err := g.client.Repositories.GetMilestoneByName(ctx, owner, repo, name)
	if err != nil {
		return 0, fmt.Errorf("gitea resolve milestone %q: %w", name, err)
	}
	return m.ID, nil
}

// usernamesByIDs resolves user IDs to Gitea usernames.
func (g *Gitea) usernamesByIDs(ctx context.Context, ids []int) ([]string, error) {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		u, _, err := g.client.Users.GetUserByID(ctx, int64(id))
		if err != nil {
			return nil, fmt.Errorf("gitea resolve user id %d: %w", id, err)
		}
		names = append(names, u.UserName)
	}
	return names, nil
}

// ─── conversion helpers ───────────────────────────────────────────────────────

func toIssue(project string, iss *giteasdk.Issue) provider.Issue {
	if iss == nil {
		return provider.Issue{}
	}
	if project == "" && iss.Repository != nil {
		project = iss.Repository.FullName
	}

	out := provider.Issue{
		ID:        int(iss.ID),
		IID:       int(iss.Index),
		Project:   project,
		Title:     iss.Title,
		Body:      iss.Body,
		State:     string(iss.State),
		WebURL:    iss.HTMLURL,
		CreatedAt: iss.Created,
		UpdatedAt: iss.Updated,
	}

	if iss.Poster != nil {
		out.Author = toUser(iss.Poster)
	}
	for _, a := range iss.Assignees {
		if a != nil {
			out.Assignees = append(out.Assignees, toUser(a))
		}
	}
	for _, l := range iss.Labels {
		if l != nil {
			out.Labels = append(out.Labels, l.Name)
		}
	}
	if iss.Milestone != nil {
		out.Milestone = iss.Milestone.Title
	}
	if iss.Closed != nil {
		t := *iss.Closed
		out.ClosedAt = &t
	}

	return out
}

func projectFromIssue(iss *giteasdk.Issue) string {
	if iss != nil && iss.Repository != nil {
		return iss.Repository.FullName
	}
	return ""
}

func toPR(project string, pr *giteasdk.PullRequest) provider.MergeRequest {
	if pr == nil {
		return provider.MergeRequest{}
	}

	out := provider.MergeRequest{
		ID:     int(pr.ID),
		IID:    int(pr.Index),
		Project: project,
		Title:   pr.Title,
		Body:    pr.Body,
		State:   string(pr.State),
		WebURL:  pr.HTMLURL,
	}

	if pr.Poster != nil {
		out.Author = toUser(pr.Poster)
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
			out.Labels = append(out.Labels, l.Name)
		}
	}
	if pr.Head != nil {
		out.SourceBranch = pr.Head.Ref
	}
	if pr.Base != nil {
		out.TargetBranch = pr.Base.Ref
	}
	out.CreatedAt = derefTime(pr.Created)
	out.UpdatedAt = derefTime(pr.Updated)
	if pr.Closed != nil {
		t := *pr.Closed
		out.ClosedAt = &t
	}
	if pr.Merged != nil {
		t := *pr.Merged
		out.MergedAt = &t
	}

	return out
}

func toLabel(l *giteasdk.Label) provider.Label {
	if l == nil {
		return provider.Label{}
	}
	color := l.Color
	if !strings.HasPrefix(color, "#") && color != "" {
		color = "#" + color
	}
	return provider.Label{
		ID:          int(l.ID),
		Name:        l.Name,
		Color:       color,
		Description: l.Description,
	}
}

func toUser(u *giteasdk.User) provider.User {
	if u == nil {
		return provider.User{}
	}
	return provider.User{
		ID:       int(u.ID),
		Username: u.UserName,
		Name:     u.FullName,
		Email:    u.Email,
		WebURL:   u.HTMLURL,
	}
}

func toNote(c *giteasdk.Comment) provider.Note {
	if c == nil {
		return provider.Note{}
	}
	out := provider.Note{
		ID:        int(c.ID),
		Body:      c.Body,
		CreatedAt: c.Created,
		UpdatedAt: c.Updated,
	}
	if c.Poster != nil {
		out.Author = toUser(c.Poster)
	}
	return out
}

func giteaState(neutral string) giteasdk.StateType {
	switch strings.ToLower(neutral) {
	case "closed", "close":
		return giteasdk.StateClosed
	case "all":
		return giteasdk.StateAll
	case "opened", "open":
		return giteasdk.StateOpen
	default:
		return giteasdk.StateOpen
	}
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func hasAllLabelNames(labels []*giteasdk.Label, want []string) bool {
	if len(want) == 0 {
		return true
	}
	have := make(map[string]bool, len(labels))
	for _, l := range labels {
		if l != nil {
			have[l.Name] = true
		}
	}
	for _, name := range want {
		if !have[name] {
			return false
		}
	}
	return true
}

// parseUnifiedDiff splits a raw unified diff into per-file patch sections,
// keyed by the new file path. Each section starts at a "diff --git " header
// and runs until the next one (or EOF). The key is extracted from the
// "+++ b/<path>" line; when the path is "/dev/null" (deletions) the
// "--- a/<path>" line is used instead. Returns an empty map for empty input.
func parseUnifiedDiff(raw string) map[string]string {
	patches := make(map[string]string)
	if raw == "" {
		return patches
	}

	const marker = "diff --git "
	lines := strings.Split(raw, "\n")
	var (
		current []string
		key     string
	)
	flush := func() {
		if key == "" || len(current) == 0 {
			return
		}
		patches[key] = strings.Join(current, "\n")
	}

	for _, line := range lines {
		if strings.HasPrefix(line, marker) {
			flush()
			current = []string{line}
			key = ""
			continue
		}
		if len(current) == 0 {
			continue
		}
		current = append(current, line)
		switch {
		case strings.HasPrefix(line, "+++ "):
			// +++ comes after ---, so prefer it unless the file was
			// deleted (+++ /dev/null), in which case keep the --- key.
			if p := pathFromDiffHeader(line); p != "/dev/null" {
				key = p
			}
		case key == "" && strings.HasPrefix(line, "--- "):
			key = pathFromDiffHeader(line)
		}
	}
	flush()
	return patches
}

// pathFromDiffHeader extracts the path from a "--- a/path" or "+++ b/path"
// line, stripping the leading prefix and any surrounding quotes. Returns
// "/dev/null" unchanged so callers can distinguish deletions.
func pathFromDiffHeader(line string) string {
	rest := line
	if i := strings.IndexByte(rest, ' '); i >= 0 {
		rest = rest[i+1:]
	}
	rest = strings.Trim(rest, `"`)
	for _, prefix := range []string{"a/", "b/"} {
		if strings.HasPrefix(rest, prefix) {
			rest = strings.TrimPrefix(rest, prefix)
			break
		}
	}
	return rest
}

// compile-time interface check.
var _ provider.Provider = (*Gitea)(nil)
