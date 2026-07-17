// Package provider defines a host-agnostic interface for upstream code
// hosts (GitLab, GitHub) so the rest of the codebase can address either
// without importing provider-specific SDKs.
package provider

import (
	"context"
	"errors"
	"time"
)

// ErrUnsupported signals that the underlying provider does not implement
// the requested operation. Callers should branch on errors.Is.
var ErrUnsupported = errors.New("provider: operation not supported")

// Kind identifies a concrete provider implementation.
type Kind string

const (
	KindGitLab Kind = "gitlab"
	KindGitHub Kind = "github"
)

// User is a host-neutral identity reference.
type User struct {
	ID       int
	Username string
	Name     string
	Email    string
	WebURL   string
}

// Label is a host-neutral label/tag.
type Label struct {
	ID              int
	Name            string
	Color           string
	Description     string
	OpenIssuesCount int // GitLab only; 0 on hosts without an equivalent
}

// CreateLabelOptions captures the fields accepted on label creation.
type CreateLabelOptions struct {
	Name        string
	Color       string // hex, e.g. "#FF0000"; adapters normalize per host
	Description string
}

// Note is a host-neutral comment on an issue or merge request.
type Note struct {
	ID        int
	Body      string
	Author    User
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Issue is a host-neutral issue/ticket. On GitHub, pull requests are
// modelled as MergeRequest, not Issue.
type Issue struct {
	ID        int
	IID       int
	Project   string
	Title     string
	Body      string
	State     string
	Author    User
	Assignees []User
	Labels    []string
	Milestone string
	WebURL    string
	CreatedAt time.Time
	UpdatedAt time.Time
	ClosedAt  *time.Time
}

// MergeRequest is a host-neutral merge request / pull request.
type MergeRequest struct {
	ID           int
	IID          int
	Project      string
	Title        string
	Body         string
	State        string
	SourceBranch string
	TargetBranch string
	Author       User
	Assignees    []User
	Reviewers    []User
	Labels       []string
	Pipeline     string // head pipeline / CI status; empty if the host has none
	WebURL       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	MergedAt     *time.Time
	ClosedAt     *time.Time
}

// ListIssuesOptions filters an issue list query.
type ListIssuesOptions struct {
	State     string   // open|closed|all
	Labels    []string // intersection
	Author    string
	Assignee  string
	Milestone string // milestone title; GitHub filters by number → ErrUnsupported when set
	Search    string
	Page      int
	PerPage   int
}

// ListMyIssuesOptions filters a cross-project "assigned to me" issue query.
// State is neutral ("opened" | "closed" | "all"); adapters translate per host
// (GitHub uses "open" for "opened").
type ListMyIssuesOptions struct {
	State   string
	Page    int
	PerPage int
}

// CreateIssueOptions captures the fields accepted on issue creation.
type CreateIssueOptions struct {
	Title       string
	Description string
	Labels      []string
	AssigneeIDs []int
	Milestone   string
	MilestoneID *int // optional: remote milestone ID to associate (GitLab milestone_id)
	Weight      int
}

// UpdateIssueOptions captures the mutable fields on an issue update. Each
// field is a pointer so a nil value means "leave unchanged" and a non-nil
// value means "set to this", which lets callers clear a field explicitly.
type UpdateIssueOptions struct {
	Title       *string
	Description *string
	State       *string   // neutral state: "opened" | "closed"; adapters translate
	Labels      *[]string // replaces the full label set when non-nil
	Milestone   *string   // milestone title; GitHub requires MilestoneID instead (ErrUnsupported)
	MilestoneID *int      // remote milestone ID (GitLab milestone_id / GitHub milestone number)
}

// Milestone is a host-neutral milestone (group-scoped on GitLab, repo-scoped on GitHub).
type Milestone struct {
	ID          int
	IID         int // group milestone IID on GitLab
	Title       string
	Description string
	State       string
	StartDate   string // YYYY-MM-DD (empty if unset)
	DueDate     string // YYYY-MM-DD (empty if unset)
	WebURL      string
	GroupID     int // 0 if not group-scoped
}

// Epic is a host-neutral epic (GitLab group epic; GitHub has no first-class equivalent).
type Epic struct {
	ID          int
	IID         int
	Title       string
	Description string
	State       string
	Labels      []string
	WebURL      string
	GroupID     int
}

// CreateMilestoneOptions captures the fields accepted on milestone creation.
type CreateMilestoneOptions struct {
	Title       string
	Description string
	StartDate   string // YYYY-MM-DD
	DueDate     string // YYYY-MM-DD
}

// CreateEpicOptions captures the fields accepted on epic creation.
type CreateEpicOptions struct {
	Title       string
	Description string
	Labels      []string
}

// ListGroupMilestonesOptions filters a group milestone list query.
type ListGroupMilestonesOptions struct {
	State   string // GitLab Group Milestones API: active|closed (empty = both)
	Search  string
	Page    int
	PerPage int
}

// ListGroupEpicsOptions filters a group epic list query.
type ListGroupEpicsOptions struct {
	State   string // GitLab Group Epics API: opened|closed|all
	Search  string
	Labels  []string
	Page    int
	PerPage int
}

// ListUsersOptions filters a user list/search query. On GitHub, Search is
// required (an empty Search returns ErrUnsupported).
type ListUsersOptions struct {
	Search  string
	Page    int
	PerPage int
}

// ListMergeRequestsOptions filters an MR/PR list query.
type ListMergeRequestsOptions struct {
	State        string
	Labels       []string
	Author       string
	SourceBranch string
	TargetBranch string
	Search       string
	Page         int
	PerPage      int
}

// FileDiff is a host-neutral per-file change in an MR/PR. Diff holds the
// unified-diff (patch) text; it is empty for binary files.
type FileDiff struct {
	OldPath     string
	NewPath     string
	Diff        string
	NewFile     bool
	DeletedFile bool
	RenamedFile bool
}

// ListMyMergeRequestsOptions filters a cross-project "assigned to me" MR query.
// State is neutral ("opened" | "closed" | "merged" | "all"); adapters translate.
type ListMyMergeRequestsOptions struct {
	State   string
	Page    int
	PerPage int
}

// MergeOptions captures the choices accepted when merging an MR/PR.
type MergeOptions struct {
	RemoveSourceBranch bool
	Squash             bool
}

// CreateMergeRequestOptions captures the fields accepted on MR/PR creation.
type CreateMergeRequestOptions struct {
	Title        string
	Description  string
	SourceBranch string
	TargetBranch string
	AssigneeID   *int // GitLab user ID; ignored on GitHub (logins, not IDs)
}

// UpdateMergeRequestOptions captures the mutable fields on an MR/PR update.
// Pointer fields distinguish "leave unchanged" (nil) from "set to value".
type UpdateMergeRequestOptions struct {
	Title        *string
	Description  *string
	State        *string // neutral: "opened" | "closed"; adapters translate
	TargetBranch *string
}

// Provider is the host-neutral surface the rest of the codebase
// addresses. Implementations live under internal/provider/<kind>/.
// Unsupported operations return ErrUnsupported.
type Provider interface {
	// Kind identifies the concrete provider.
	Kind() Kind
	// Host returns the bare host (no scheme) the provider talks to.
	Host() string

	// Issue surface.
	ListIssues(ctx context.Context, project string, opts ListIssuesOptions) ([]Issue, error)
	// ListMyIssues lists issues assigned to the authenticated user across all
	// projects/repositories visible to the token. Each Issue.Project is
	// populated from its source project.
	ListMyIssues(ctx context.Context, opts ListMyIssuesOptions) ([]Issue, error)
	GetIssue(ctx context.Context, project string, iid int) (Issue, error)
	CreateIssue(ctx context.Context, project string, opts CreateIssueOptions) (Issue, error)
	UpdateIssue(ctx context.Context, project string, iid int, opts UpdateIssueOptions) (Issue, error)
	CreateIssueNote(ctx context.Context, project string, iid int, body string) (Note, error)

	// Merge request surface.
	ListMergeRequests(ctx context.Context, project string, opts ListMergeRequestsOptions) ([]MergeRequest, error)
	// ListMyMergeRequests lists MRs/PRs assigned to the authenticated user
	// across all projects. ErrUnsupported on hosts without a cross-project
	// "assigned to me" scope.
	ListMyMergeRequests(ctx context.Context, opts ListMyMergeRequestsOptions) ([]MergeRequest, error)
	GetMergeRequest(ctx context.Context, project string, iid int) (MergeRequest, error)
	CreateMergeRequest(ctx context.Context, project string, opts CreateMergeRequestOptions) (MergeRequest, error)
	UpdateMergeRequest(ctx context.Context, project string, iid int, opts UpdateMergeRequestOptions) (MergeRequest, error)
	// ApproveMergeRequest records an approval. ErrUnsupported on hosts where
	// approval is not a first-class self-service action (e.g. GitHub reviews).
	ApproveMergeRequest(ctx context.Context, project string, iid int) error
	// MergeMergeRequest merges/accepts an MR/PR.
	MergeMergeRequest(ctx context.Context, project string, iid int, opts MergeOptions) (MergeRequest, error)
	// GetMergeRequestDiff returns the per-file changes of an MR/PR.
	GetMergeRequestDiff(ctx context.Context, project string, iid int) ([]FileDiff, error)

	// Label surface.
	ListLabels(ctx context.Context, project string) ([]Label, error)
	CreateLabel(ctx context.Context, project string, opts CreateLabelOptions) (Label, error)

	// User surface.
	ListUsers(ctx context.Context, opts ListUsersOptions) ([]User, error)

	// Milestone surface (group-scoped on GitLab).
	CreateGroupMilestone(ctx context.Context, groupID int, opts CreateMilestoneOptions) (Milestone, error)
	ListGroupMilestones(ctx context.Context, groupID int, opts ListGroupMilestonesOptions) ([]Milestone, error)

	// Epic surface (group-scoped on GitLab; ErrUnsupported on GitHub).
	CreateGroupEpic(ctx context.Context, groupID int, opts CreateEpicOptions) (Epic, error)
	ListGroupEpics(ctx context.Context, groupID int, opts ListGroupEpicsOptions) ([]Epic, error)
}

// EpicLinker is an optional capability implemented by providers with a
// first-class epic-to-issue relationship.
type EpicLinker interface {
	LinkIssueToEpic(ctx context.Context, groupID, epicIID, issueID int) error
}
