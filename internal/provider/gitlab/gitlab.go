// Package gitlab adapts the GitLab REST client to the host-neutral
// provider.Provider interface.
package gitlab

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/provider"
	gl "github.com/xanzy/go-gitlab"
)

// GitLab is the provider.Provider implementation for GitLab instances.
type GitLab struct {
	client *gl.Client
	host   string
}

// New returns a provider bound to cfg.GitLabURL/cfg.Token.
func New(cfg *config.Config) (*GitLab, error) {
	httpClient := &http.Client{}
	if cfg.SkipTLS {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec
			},
		}
	}

	glc, err := gl.NewClient(cfg.Token,
		gl.WithBaseURL(cfg.GitLabURL),
		gl.WithHTTPClient(httpClient),
	)
	if err != nil {
		return nil, fmt.Errorf("gitlab provider init: %w", err)
	}

	host := cfg.GitLabURL
	if u, parseErr := url.Parse(cfg.GitLabURL); parseErr == nil && u.Host != "" {
		host = u.Host
	}

	return &GitLab{client: glc, host: host}, nil
}

// Kind returns provider.KindGitLab.
func (g *GitLab) Kind() provider.Kind { return provider.KindGitLab }

// Host returns the bare host parsed from cfg.GitLabURL.
func (g *GitLab) Host() string { return g.host }

// ListIssues calls the GitLab list project issues API.
func (g *GitLab) ListIssues(ctx context.Context, project string, opts provider.ListIssuesOptions) ([]provider.Issue, error) {
	glOpts := &gl.ListProjectIssuesOptions{
		ListOptions: gl.ListOptions{Page: opts.Page, PerPage: opts.PerPage},
	}
	if opts.State != "" {
		glOpts.State = gl.Ptr(opts.State)
	}
	if opts.Author != "" {
		glOpts.AuthorUsername = gl.Ptr(opts.Author)
	}
	if opts.Assignee != "" {
		glOpts.AssigneeUsername = gl.Ptr(opts.Assignee)
	}
	if opts.Milestone != "" {
		glOpts.Milestone = gl.Ptr(opts.Milestone)
	}
	if len(opts.Labels) > 0 {
		lv := gl.LabelOptions(opts.Labels)
		glOpts.Labels = &lv
	}

	issues, _, err := g.client.Issues.ListProjectIssues(project, glOpts, gl.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("gitlab list issues: %w", err)
	}

	result := make([]provider.Issue, len(issues))
	for i, iss := range issues {
		result[i] = toIssue(project, iss)
	}
	return result, nil
}

// ListMyIssues lists issues assigned to the authenticated user across all
// projects, using the GitLab cross-project "assigned_to_me" scope. Each
// issue's project path is recovered from its full reference.
func (g *GitLab) ListMyIssues(ctx context.Context, opts provider.ListMyIssuesOptions) ([]provider.Issue, error) {
	glOpts := &gl.ListIssuesOptions{
		Scope:       gl.Ptr("assigned_to_me"),
		ListOptions: gl.ListOptions{Page: opts.Page, PerPage: opts.PerPage},
	}
	if opts.State != "" {
		glOpts.State = gl.Ptr(opts.State)
	}

	issues, _, err := g.client.Issues.ListIssues(glOpts, gl.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("gitlab list my issues: %w", err)
	}

	result := make([]provider.Issue, len(issues))
	for i, iss := range issues {
		result[i] = toIssue(projectFromRef(iss.References), iss)
	}
	return result, nil
}

// GetIssue retrieves a single issue by IID.
func (g *GitLab) GetIssue(ctx context.Context, project string, iid int) (provider.Issue, error) {
	iss, _, err := g.client.Issues.GetIssue(project, iid, gl.WithContext(ctx))
	if err != nil {
		return provider.Issue{}, fmt.Errorf("gitlab get issue: %w", err)
	}
	return toIssue(project, iss), nil
}

// CreateIssue creates a new issue in the given project.
func (g *GitLab) CreateIssue(ctx context.Context, project string, opts provider.CreateIssueOptions) (provider.Issue, error) {
	glOpts := &gl.CreateIssueOptions{
		Title:       gl.Ptr(opts.Title),
		Description: gl.Ptr(opts.Description),
	}
	if len(opts.Labels) > 0 {
		lv := gl.LabelOptions(opts.Labels)
		glOpts.Labels = &lv
	}
	if len(opts.AssigneeIDs) > 0 {
		glOpts.AssigneeIDs = &opts.AssigneeIDs
	}
	if opts.MilestoneID != nil {
		glOpts.MilestoneID = opts.MilestoneID
	}
	if opts.Weight != 0 {
		glOpts.Weight = gl.Ptr(opts.Weight)
	}

	iss, _, err := g.client.Issues.CreateIssue(project, glOpts, gl.WithContext(ctx))
	if err != nil {
		return provider.Issue{}, fmt.Errorf("gitlab create issue: %w", err)
	}
	return toIssue(project, iss), nil
}

// UpdateIssue applies a partial update to an existing issue. The neutral
// State is translated to GitLab's state_event verb (close/reopen).
func (g *GitLab) UpdateIssue(ctx context.Context, project string, iid int, opts provider.UpdateIssueOptions) (provider.Issue, error) {
	glOpts := &gl.UpdateIssueOptions{}
	if opts.Title != nil {
		glOpts.Title = gl.Ptr(*opts.Title)
	}
	if opts.Description != nil {
		glOpts.Description = gl.Ptr(*opts.Description)
	}
	if opts.State != nil {
		glOpts.StateEvent = gl.Ptr(gitlabStateEvent(*opts.State))
	}
	if opts.Labels != nil {
		lv := gl.LabelOptions(*opts.Labels)
		glOpts.Labels = &lv
	}
	if opts.MilestoneID != nil {
		glOpts.MilestoneID = opts.MilestoneID
	}

	iss, _, err := g.client.Issues.UpdateIssue(project, iid, glOpts, gl.WithContext(ctx))
	if err != nil {
		return provider.Issue{}, fmt.Errorf("gitlab update issue: %w", err)
	}
	return toIssue(project, iss), nil
}

// gitlabStateEvent maps a neutral issue state to GitLab's state_event verb.
// Anything other than a close request reopens the issue.
func gitlabStateEvent(neutral string) string {
	switch neutral {
	case "closed", "close":
		return "close"
	default:
		return "reopen"
	}
}

// CreateIssueNote adds a note (comment) to an issue.
func (g *GitLab) CreateIssueNote(ctx context.Context, project string, iid int, body string) (provider.Note, error) {
	note, _, err := g.client.Notes.CreateIssueNote(project, iid,
		&gl.CreateIssueNoteOptions{Body: gl.Ptr(body)},
		gl.WithContext(ctx))
	if err != nil {
		return provider.Note{}, fmt.Errorf("gitlab create issue note: %w", err)
	}
	return toNote(note), nil
}

// ListUsers searches users via the GitLab list users API.
func (g *GitLab) ListUsers(ctx context.Context, opts provider.ListUsersOptions) ([]provider.User, error) {
	glOpts := &gl.ListUsersOptions{
		ListOptions: gl.ListOptions{Page: opts.Page, PerPage: opts.PerPage},
	}
	if opts.Search != "" {
		glOpts.Search = gl.Ptr(opts.Search)
	}

	users, _, err := g.client.Users.ListUsers(glOpts, gl.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("gitlab list users: %w", err)
	}

	result := make([]provider.User, len(users))
	for i, u := range users {
		result[i] = provider.User{
			ID:       u.ID,
			Username: u.Username,
			Name:     u.Name,
			Email:    u.Email,
			WebURL:   u.WebURL,
		}
	}
	return result, nil
}

// ListMergeRequests calls the GitLab list project merge requests API.
func (g *GitLab) ListMergeRequests(ctx context.Context, project string, opts provider.ListMergeRequestsOptions) ([]provider.MergeRequest, error) {
	glOpts := &gl.ListProjectMergeRequestsOptions{
		ListOptions: gl.ListOptions{Page: opts.Page, PerPage: opts.PerPage},
	}
	if opts.State != "" {
		glOpts.State = gl.Ptr(opts.State)
	}
	if opts.Author != "" {
		glOpts.AuthorUsername = gl.Ptr(opts.Author)
	}
	if opts.SourceBranch != "" {
		glOpts.SourceBranch = gl.Ptr(opts.SourceBranch)
	}
	if opts.TargetBranch != "" {
		glOpts.TargetBranch = gl.Ptr(opts.TargetBranch)
	}
	if len(opts.Labels) > 0 {
		lv := gl.LabelOptions(opts.Labels)
		glOpts.Labels = &lv
	}

	mrs, _, err := g.client.MergeRequests.ListProjectMergeRequests(project, glOpts, gl.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("gitlab list merge requests: %w", err)
	}

	result := make([]provider.MergeRequest, len(mrs))
	for i, mr := range mrs {
		result[i] = toMR(project, mr)
	}
	return result, nil
}

// ListMyMergeRequests lists MRs assigned to the authenticated user across all
// projects via the cross-project "assigned_to_me" scope.
func (g *GitLab) ListMyMergeRequests(ctx context.Context, opts provider.ListMyMergeRequestsOptions) ([]provider.MergeRequest, error) {
	glOpts := &gl.ListMergeRequestsOptions{
		Scope:       gl.Ptr("assigned_to_me"),
		ListOptions: gl.ListOptions{Page: opts.Page, PerPage: opts.PerPage},
	}
	if opts.State != "" {
		glOpts.State = gl.Ptr(opts.State)
	}

	mrs, _, err := g.client.MergeRequests.ListMergeRequests(glOpts, gl.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("gitlab list my merge requests: %w", err)
	}

	result := make([]provider.MergeRequest, len(mrs))
	for i, mr := range mrs {
		result[i] = toMR(projectFromRef(mr.References), mr)
	}
	return result, nil
}

// ApproveMergeRequest records an approval via the GitLab approvals API.
func (g *GitLab) ApproveMergeRequest(ctx context.Context, project string, iid int) error {
	_, _, err := g.client.MergeRequestApprovals.ApproveMergeRequest(
		project, iid, &gl.ApproveMergeRequestOptions{}, gl.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("gitlab approve merge request: %w", err)
	}
	return nil
}

// MergeMergeRequest accepts (merges) a merge request.
func (g *GitLab) MergeMergeRequest(ctx context.Context, project string, iid int, opts provider.MergeOptions) (provider.MergeRequest, error) {
	mr, _, err := g.client.MergeRequests.AcceptMergeRequest(project, iid,
		&gl.AcceptMergeRequestOptions{
			ShouldRemoveSourceBranch: gl.Ptr(opts.RemoveSourceBranch),
			Squash:                   gl.Ptr(opts.Squash),
		}, gl.WithContext(ctx))
	if err != nil {
		return provider.MergeRequest{}, fmt.Errorf("gitlab merge merge request: %w", err)
	}
	return toMR(project, mr), nil
}

// GetMergeRequestDiff returns the per-file changes of a merge request.
func (g *GitLab) GetMergeRequestDiff(ctx context.Context, project string, iid int) ([]provider.FileDiff, error) {
	changes, _, err := g.client.MergeRequests.GetMergeRequestChanges(project, iid,
		&gl.GetMergeRequestChangesOptions{}, gl.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("gitlab get merge request diff: %w", err)
	}

	out := make([]provider.FileDiff, len(changes.Changes))
	for i, ch := range changes.Changes {
		out[i] = provider.FileDiff{
			OldPath:     ch.OldPath,
			NewPath:     ch.NewPath,
			Diff:        ch.Diff,
			NewFile:     ch.NewFile,
			DeletedFile: ch.DeletedFile,
			RenamedFile: ch.RenamedFile,
		}
	}
	return out, nil
}

// CreateMergeRequest creates a new merge request in the given project.
func (g *GitLab) CreateMergeRequest(ctx context.Context, project string, opts provider.CreateMergeRequestOptions) (provider.MergeRequest, error) {
	glOpts := &gl.CreateMergeRequestOptions{
		Title:        gl.Ptr(opts.Title),
		Description:  gl.Ptr(opts.Description),
		SourceBranch: gl.Ptr(opts.SourceBranch),
		TargetBranch: gl.Ptr(opts.TargetBranch),
	}
	if opts.AssigneeID != nil {
		glOpts.AssigneeID = opts.AssigneeID
	}

	mr, _, err := g.client.MergeRequests.CreateMergeRequest(project, glOpts, gl.WithContext(ctx))
	if err != nil {
		return provider.MergeRequest{}, fmt.Errorf("gitlab create merge request: %w", err)
	}
	return toMR(project, mr), nil
}

// UpdateMergeRequest applies a partial update to an existing merge request.
// The neutral State is translated to GitLab's state_event verb (close/reopen).
func (g *GitLab) UpdateMergeRequest(ctx context.Context, project string, iid int, opts provider.UpdateMergeRequestOptions) (provider.MergeRequest, error) {
	glOpts := &gl.UpdateMergeRequestOptions{}
	if opts.Title != nil {
		glOpts.Title = gl.Ptr(*opts.Title)
	}
	if opts.Description != nil {
		glOpts.Description = gl.Ptr(*opts.Description)
	}
	if opts.State != nil {
		glOpts.StateEvent = gl.Ptr(gitlabStateEvent(*opts.State))
	}
	if opts.TargetBranch != nil {
		glOpts.TargetBranch = gl.Ptr(*opts.TargetBranch)
	}

	mr, _, err := g.client.MergeRequests.UpdateMergeRequest(project, iid, glOpts, gl.WithContext(ctx))
	if err != nil {
		return provider.MergeRequest{}, fmt.Errorf("gitlab update merge request: %w", err)
	}
	return toMR(project, mr), nil
}

// GetMergeRequest retrieves a single merge request by IID.
func (g *GitLab) GetMergeRequest(ctx context.Context, project string, iid int) (provider.MergeRequest, error) {
	mr, _, err := g.client.MergeRequests.GetMergeRequest(project, iid, nil, gl.WithContext(ctx))
	if err != nil {
		return provider.MergeRequest{}, fmt.Errorf("gitlab get merge request: %w", err)
	}
	return toMR(project, mr), nil
}

// ListLabels retrieves all labels for the given project.
func (g *GitLab) ListLabels(ctx context.Context, project string) ([]provider.Label, error) {
	labels, _, err := g.client.Labels.ListLabels(project,
		&gl.ListLabelsOptions{ListOptions: gl.ListOptions{PerPage: 100}},
		gl.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("gitlab list labels: %w", err)
	}

	result := make([]provider.Label, len(labels))
	for i, lbl := range labels {
		result[i] = toLabel(lbl)
	}
	return result, nil
}

// CreateLabel creates a new label in the given project.
func (g *GitLab) CreateLabel(ctx context.Context, project string, opts provider.CreateLabelOptions) (provider.Label, error) {
	lbl, _, err := g.client.Labels.CreateLabel(project, &gl.CreateLabelOptions{
		Name:        gl.Ptr(opts.Name),
		Color:       gl.Ptr(opts.Color),
		Description: gl.Ptr(opts.Description),
	}, gl.WithContext(ctx))
	if err != nil {
		return provider.Label{}, fmt.Errorf("gitlab create label: %w", err)
	}
	return toLabel(lbl), nil
}

// CreateGroupMilestone creates a new milestone in the given group.
func (g *GitLab) CreateGroupMilestone(ctx context.Context, groupID int, opts provider.CreateMilestoneOptions) (provider.Milestone, error) {
	glOpts := &gl.CreateGroupMilestoneOptions{
		Title:       gl.Ptr(opts.Title),
		Description: gl.Ptr(opts.Description),
	}
	if opts.StartDate != "" {
		t, err := time.Parse("2006-01-02", opts.StartDate)
		if err != nil {
			return provider.Milestone{}, fmt.Errorf("gitlab create group milestone: invalid start date: %w", err)
		}
		glOpts.StartDate = gl.Ptr(gl.ISOTime(t))
	}
	if opts.DueDate != "" {
		t, err := time.Parse("2006-01-02", opts.DueDate)
		if err != nil {
			return provider.Milestone{}, fmt.Errorf("gitlab create group milestone: invalid due date: %w", err)
		}
		glOpts.DueDate = gl.Ptr(gl.ISOTime(t))
	}

	m, _, err := g.client.GroupMilestones.CreateGroupMilestone(groupID, glOpts, gl.WithContext(ctx))
	if err != nil {
		return provider.Milestone{}, fmt.Errorf("gitlab create group milestone: %w", err)
	}
	return toMilestone(groupID, m), nil
}

// CreateGroupEpic creates a new epic in the given group.
func (g *GitLab) CreateGroupEpic(ctx context.Context, groupID int, opts provider.CreateEpicOptions) (provider.Epic, error) {
	glOpts := &gl.CreateEpicOptions{
		Title:       gl.Ptr(opts.Title),
		Description: gl.Ptr(opts.Description),
	}
	if len(opts.Labels) > 0 {
		lv := gl.LabelOptions(opts.Labels)
		glOpts.Labels = &lv
	}

	e, _, err := g.client.Epics.CreateEpic(groupID, glOpts, gl.WithContext(ctx))
	if err != nil {
		return provider.Epic{}, fmt.Errorf("gitlab create group epic: %w", err)
	}
	return toEpic(groupID, e), nil
}

// ListGroupMilestones returns milestones for the given group.
func (g *GitLab) ListGroupMilestones(ctx context.Context, groupID int, opts provider.ListGroupMilestonesOptions) ([]provider.Milestone, error) {
	glOpts := &gl.ListGroupMilestonesOptions{
		ListOptions: gl.ListOptions{Page: opts.Page, PerPage: opts.PerPage},
	}
	if opts.State != "" {
		glOpts.State = gl.Ptr(opts.State)
	}
	if opts.Search != "" {
		glOpts.Search = gl.Ptr(opts.Search)
	}

	ms, _, err := g.client.GroupMilestones.ListGroupMilestones(groupID, glOpts, gl.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("gitlab list group milestones: %w", err)
	}

	result := make([]provider.Milestone, len(ms))
	for i, m := range ms {
		result[i] = toMilestone(groupID, m)
	}
	return result, nil
}

// ListGroupEpics returns epics for the given group.
func (g *GitLab) ListGroupEpics(ctx context.Context, groupID int, opts provider.ListGroupEpicsOptions) ([]provider.Epic, error) {
	glOpts := &gl.ListGroupEpicsOptions{
		ListOptions: gl.ListOptions{Page: opts.Page, PerPage: opts.PerPage},
	}
	if opts.State != "" {
		glOpts.State = gl.Ptr(opts.State)
	}
	if opts.Search != "" {
		glOpts.Search = gl.Ptr(opts.Search)
	}
	if len(opts.Labels) > 0 {
		lv := gl.LabelOptions(opts.Labels)
		glOpts.Labels = &lv
	}

	es, _, err := g.client.Epics.ListGroupEpics(groupID, glOpts, gl.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("gitlab list group epics: %w", err)
	}

	result := make([]provider.Epic, len(es))
	for i, e := range es {
		result[i] = toEpic(groupID, e)
	}
	return result, nil
}

// LinkIssueToEpic assigns a project issue, addressed by global issue ID, to a
// group epic addressed by IID.
func (g *GitLab) LinkIssueToEpic(ctx context.Context, groupID, epicIID, issueID int) error {
	page := 1
	for {
		issues, resp, err := g.client.EpicIssues.ListEpicIssues(groupID, epicIID,
			&gl.ListOptions{Page: page, PerPage: 100}, gl.WithContext(ctx))
		if err != nil {
			return fmt.Errorf("gitlab list epic issues: %w", err)
		}
		for _, issue := range issues {
			if issue.ID == issueID {
				return nil
			}
		}
		if resp.CurrentPage >= resp.TotalPages {
			break
		}
		page++
	}
	_, _, err := g.client.EpicIssues.AssignEpicIssue(groupID, epicIID, issueID, gl.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("gitlab link issue to epic: %w", err)
	}
	return nil
}

// ─── conversion helpers ───────────────────────────────────────────────────────

// projectFromRef recovers the "namespace/project" path from a full reference,
// used when listing cross-project. GitLab sigils the IID with '#' for issues
// and '!' for merge requests (e.g. "group/proj#7", "group/proj!3"); strip at
// whichever appears first.
func projectFromRef(ref *gl.IssueReferences) string {
	if ref == nil {
		return ""
	}
	if i := strings.IndexAny(ref.Full, "#!"); i >= 0 {
		return ref.Full[:i]
	}
	return ref.Full
}

func toIssue(project string, iss *gl.Issue) provider.Issue {
	out := provider.Issue{
		ID:      iss.ID,
		IID:     iss.IID,
		Project: project,
		Title:   iss.Title,
		Body:    iss.Description,
		State:   iss.State,
		WebURL:  iss.WebURL,
		Labels:  []string(iss.Labels),
	}
	if iss.Author != nil {
		out.Author = provider.User{
			ID:       iss.Author.ID,
			Username: iss.Author.Username,
			Name:     iss.Author.Name,
			WebURL:   iss.Author.WebURL,
		}
	}
	for _, a := range iss.Assignees {
		if a != nil {
			out.Assignees = append(out.Assignees, provider.User{
				ID: a.ID, Username: a.Username, Name: a.Name, WebURL: a.WebURL,
			})
		}
	}
	if iss.Milestone != nil {
		out.Milestone = iss.Milestone.Title
	}
	out.CreatedAt = derefTime(iss.CreatedAt)
	out.UpdatedAt = derefTime(iss.UpdatedAt)
	if iss.ClosedAt != nil {
		t := *iss.ClosedAt
		out.ClosedAt = &t
	}
	return out
}

func toMR(project string, mr *gl.MergeRequest) provider.MergeRequest {
	out := provider.MergeRequest{
		ID:           mr.ID,
		IID:          mr.IID,
		Project:      project,
		Title:        mr.Title,
		Body:         mr.Description,
		State:        mr.State,
		SourceBranch: mr.SourceBranch,
		TargetBranch: mr.TargetBranch,
		WebURL:       mr.WebURL,
		Labels:       []string(mr.Labels),
	}
	if mr.HeadPipeline != nil {
		out.Pipeline = mr.HeadPipeline.Status
	}
	if mr.Author != nil {
		out.Author = toUser(mr.Author)
	}
	for _, a := range mr.Assignees {
		if a != nil {
			out.Assignees = append(out.Assignees, toUser(a))
		}
	}
	for _, r := range mr.Reviewers {
		if r != nil {
			out.Reviewers = append(out.Reviewers, toUser(r))
		}
	}
	out.CreatedAt = derefTime(mr.CreatedAt)
	out.UpdatedAt = derefTime(mr.UpdatedAt)
	if mr.MergedAt != nil {
		t := *mr.MergedAt
		out.MergedAt = &t
	}
	if mr.ClosedAt != nil {
		t := *mr.ClosedAt
		out.ClosedAt = &t
	}
	return out
}

func toNote(n *gl.Note) provider.Note {
	return provider.Note{
		ID:   n.ID,
		Body: n.Body,
		Author: provider.User{
			ID:       n.Author.ID,
			Username: n.Author.Username,
			Name:     n.Author.Name,
			Email:    n.Author.Email,
			WebURL:   n.Author.WebURL,
		},
		CreatedAt: derefTime(n.CreatedAt),
		UpdatedAt: derefTime(n.UpdatedAt),
	}
}

func toLabel(lbl *gl.Label) provider.Label {
	return provider.Label{
		ID:              lbl.ID,
		Name:            lbl.Name,
		Color:           lbl.Color,
		Description:     lbl.Description,
		OpenIssuesCount: lbl.OpenIssuesCount,
	}
}

func toUser(u *gl.BasicUser) provider.User {
	return provider.User{
		ID:       u.ID,
		Username: u.Username,
		Name:     u.Name,
		WebURL:   u.WebURL,
	}
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func isoTimeString(t *gl.ISOTime) string {
	if t == nil {
		return ""
	}
	return t.String()
}

func toMilestone(groupID int, m *gl.GroupMilestone) provider.Milestone {
	return provider.Milestone{
		ID:          m.ID,
		IID:         m.IID,
		Title:       m.Title,
		Description: m.Description,
		State:       m.State,
		StartDate:   isoTimeString(m.StartDate),
		DueDate:     isoTimeString(m.DueDate),
		WebURL:      "",
		GroupID:     groupID,
	}
}

func toEpic(groupID int, e *gl.Epic) provider.Epic {
	return provider.Epic{
		ID:          e.ID,
		IID:         e.IID,
		Title:       e.Title,
		Description: e.Description,
		State:       e.State,
		Labels:      []string(e.Labels),
		WebURL:      e.WebURL,
		GroupID:     groupID,
	}
}
