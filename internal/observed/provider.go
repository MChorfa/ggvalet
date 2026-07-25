// Package observed enforces durable receipts around remote provider operations.
package observed

import (
	"context"
	"errors"
	"fmt"

	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/MChorfa/ggvalet/internal/state"
)

type ReceiptUncertainError struct {
	OperationID string
	Cause       error
}

func (e *ReceiptUncertainError) Error() string {
	return fmt.Sprintf("remote outcome receipt uncertain for operation %s: %v", e.OperationID, e.Cause)
}

func (e *ReceiptUncertainError) Unwrap() error { return e.Cause }

type Provider struct {
	inner provider.Provider
	store *state.Store
	host  string
}

func NewProvider(inner provider.Provider, store *state.Store, host string) *Provider {
	return &Provider{inner: inner, store: store, host: host}
}

func (p *Provider) Kind() provider.Kind { return p.inner.Kind() }
func (p *Provider) Host() string        { return p.inner.Host() }

func call[T any](ctx context.Context, p *Provider, action, resource, target string, input any, fn func() (T, error)) (T, error) {
	var zero T
	op := state.Operation{Provider: string(p.inner.Kind()), Host: p.host, Action: action,
		Resource: resource, Target: target, InputDigest: state.Digest(input)}
	id, err := p.store.Begin(ctx, op)
	if err != nil {
		return zero, fmt.Errorf("persist operation intent: %w", err)
	}
	result, callErr := fn()
	status, detail := state.StatusSucceeded, ""
	if callErr != nil {
		status, detail = state.StatusFailed, callErr.Error()
	}
	if receiptErr := p.store.Complete(ctx, id, status, detail); receiptErr != nil {
		return zero, &ReceiptUncertainError{OperationID: id, Cause: receiptErr}
	}
	return result, callErr
}

func callErr(ctx context.Context, p *Provider, action, resource, target string, input any, fn func() error) error {
	_, err := call(ctx, p, action, resource, target, input, func() (struct{}, error) {
		return struct{}{}, fn()
	})
	return err
}

func (p *Provider) ListIssues(ctx context.Context, project string, opts provider.ListIssuesOptions) ([]provider.Issue, error) {
	return call(ctx, p, "list", "issue", project, opts, func() ([]provider.Issue, error) {
		return p.inner.ListIssues(ctx, project, opts)
	})
}

func (p *Provider) ListMyIssues(ctx context.Context, opts provider.ListMyIssuesOptions) ([]provider.Issue, error) {
	return call(ctx, p, "list", "issue", "all", opts, func() ([]provider.Issue, error) {
		return p.inner.ListMyIssues(ctx, opts)
	})
}

func (p *Provider) GetIssue(ctx context.Context, project string, iid int) (provider.Issue, error) {
	return call(ctx, p, "get", "issue", project, iid, func() (provider.Issue, error) {
		return p.inner.GetIssue(ctx, project, iid)
	})
}

func (p *Provider) CreateIssue(ctx context.Context, project string, opts provider.CreateIssueOptions) (provider.Issue, error) {
	return call(ctx, p, "create", "issue", project, opts, func() (provider.Issue, error) {
		return p.inner.CreateIssue(ctx, project, opts)
	})
}

func (p *Provider) UpdateIssue(ctx context.Context, project string, iid int, opts provider.UpdateIssueOptions) (provider.Issue, error) {
	return call(ctx, p, "update", "issue", project, struct {
		IID  int
		Opts provider.UpdateIssueOptions
	}{iid, opts}, func() (provider.Issue, error) {
		return p.inner.UpdateIssue(ctx, project, iid, opts)
	})
}

func (p *Provider) CreateIssueNote(ctx context.Context, project string, iid int, body string) (provider.Note, error) {
	return call(ctx, p, "comment", "issue", project, struct {
		IID  int
		Body string
	}{iid, body}, func() (provider.Note, error) {
		return p.inner.CreateIssueNote(ctx, project, iid, body)
	})
}

func (p *Provider) ListMergeRequests(ctx context.Context, project string, opts provider.ListMergeRequestsOptions) ([]provider.MergeRequest, error) {
	return call(ctx, p, "list", "mr", project, opts, func() ([]provider.MergeRequest, error) {
		return p.inner.ListMergeRequests(ctx, project, opts)
	})
}

func (p *Provider) ListMyMergeRequests(ctx context.Context, opts provider.ListMyMergeRequestsOptions) ([]provider.MergeRequest, error) {
	return call(ctx, p, "list", "mr", "all", opts, func() ([]provider.MergeRequest, error) {
		return p.inner.ListMyMergeRequests(ctx, opts)
	})
}

func (p *Provider) GetMergeRequest(ctx context.Context, project string, iid int) (provider.MergeRequest, error) {
	return call(ctx, p, "get", "mr", project, iid, func() (provider.MergeRequest, error) {
		return p.inner.GetMergeRequest(ctx, project, iid)
	})
}

func (p *Provider) CreateMergeRequest(ctx context.Context, project string, opts provider.CreateMergeRequestOptions) (provider.MergeRequest, error) {
	return call(ctx, p, "create", "mr", project, opts, func() (provider.MergeRequest, error) {
		return p.inner.CreateMergeRequest(ctx, project, opts)
	})
}

func (p *Provider) UpdateMergeRequest(ctx context.Context, project string, iid int, opts provider.UpdateMergeRequestOptions) (provider.MergeRequest, error) {
	return call(ctx, p, "update", "mr", project, struct {
		IID  int
		Opts provider.UpdateMergeRequestOptions
	}{iid, opts}, func() (provider.MergeRequest, error) {
		return p.inner.UpdateMergeRequest(ctx, project, iid, opts)
	})
}

func (p *Provider) ApproveMergeRequest(ctx context.Context, project string, iid int) error {
	return callErr(ctx, p, "approve", "mr", project, iid, func() error {
		return p.inner.ApproveMergeRequest(ctx, project, iid)
	})
}

func (p *Provider) MergeMergeRequest(ctx context.Context, project string, iid int, opts provider.MergeOptions) (provider.MergeRequest, error) {
	return call(ctx, p, "merge", "mr", project, struct {
		IID  int
		Opts provider.MergeOptions
	}{iid, opts}, func() (provider.MergeRequest, error) {
		return p.inner.MergeMergeRequest(ctx, project, iid, opts)
	})
}

func (p *Provider) GetMergeRequestDiff(ctx context.Context, project string, iid int) ([]provider.FileDiff, error) {
	return call(ctx, p, "diff", "mr", project, iid, func() ([]provider.FileDiff, error) {
		return p.inner.GetMergeRequestDiff(ctx, project, iid)
	})
}

func (p *Provider) ListLabels(ctx context.Context, project string) ([]provider.Label, error) {
	return call(ctx, p, "list", "label", project, nil, func() ([]provider.Label, error) {
		return p.inner.ListLabels(ctx, project)
	})
}

func (p *Provider) CreateLabel(ctx context.Context, project string, opts provider.CreateLabelOptions) (provider.Label, error) {
	return call(ctx, p, "create", "label", project, opts, func() (provider.Label, error) {
		return p.inner.CreateLabel(ctx, project, opts)
	})
}

func (p *Provider) ListUsers(ctx context.Context, opts provider.ListUsersOptions) ([]provider.User, error) {
	return call(ctx, p, "list", "user", "all", opts, func() ([]provider.User, error) {
		return p.inner.ListUsers(ctx, opts)
	})
}

func (p *Provider) GetProject(ctx context.Context, project string) (provider.Project, error) {
	return call(ctx, p, "get", "project", project, project, func() (provider.Project, error) {
		return p.inner.GetProject(ctx, project)
	})
}

func (p *Provider) ListWorkItems(ctx context.Context, project string, opts provider.ListWorkItemsOptions) ([]provider.WorkItem, error) {
	return call(ctx, p, "list", "work-item", project, opts, func() ([]provider.WorkItem, error) {
		return p.inner.ListWorkItems(ctx, project, opts)
	})
}

func (p *Provider) CreateWorkItem(ctx context.Context, project string, opts provider.CreateWorkItemOptions) (provider.WorkItem, error) {
	return call(ctx, p, "create", "work-item", project, opts, func() (provider.WorkItem, error) {
		return p.inner.CreateWorkItem(ctx, project, opts)
	})
}

func (p *Provider) CloseWorkItem(ctx context.Context, project string, id int) error {
	return callErr(ctx, p, "close", "work-item", project, id, func() error {
		return p.inner.CloseWorkItem(ctx, project, id)
	})
}

func (p *Provider) ListMilestones(ctx context.Context, project string, opts provider.ListMilestonesOptions) ([]provider.Milestone, error) {
	return call(ctx, p, "list", "milestone", project, opts, func() ([]provider.Milestone, error) {
		return p.inner.ListMilestones(ctx, project, opts)
	})
}

func (p *Provider) GetMilestone(ctx context.Context, project string, id int) (provider.Milestone, error) {
	return call(ctx, p, "get", "milestone", project, id, func() (provider.Milestone, error) {
		return p.inner.GetMilestone(ctx, project, id)
	})
}

func (p *Provider) ResolveGroup(ctx context.Context, groupPath string) (int, error) {
	return call(ctx, p, "resolve", "group", groupPath, groupPath, func() (int, error) {
		return p.inner.ResolveGroup(ctx, groupPath)
	})
}

func (p *Provider) CreateGroupMilestone(ctx context.Context, groupID int, opts provider.CreateMilestoneOptions) (provider.Milestone, error) {
	return call(ctx, p, "create", "milestone", fmt.Sprint(groupID), opts, func() (provider.Milestone, error) {
		return p.inner.CreateGroupMilestone(ctx, groupID, opts)
	})
}

func (p *Provider) ListGroupMilestones(ctx context.Context, groupID int, opts provider.ListGroupMilestonesOptions) ([]provider.Milestone, error) {
	return call(ctx, p, "list", "milestone", fmt.Sprint(groupID), opts, func() ([]provider.Milestone, error) {
		return p.inner.ListGroupMilestones(ctx, groupID, opts)
	})
}

func (p *Provider) CreateGroupEpic(ctx context.Context, groupID int, opts provider.CreateEpicOptions) (provider.Epic, error) {
	return call(ctx, p, "create", "epic", fmt.Sprint(groupID), opts, func() (provider.Epic, error) {
		return p.inner.CreateGroupEpic(ctx, groupID, opts)
	})
}

func (p *Provider) ListGroupEpics(ctx context.Context, groupID int, opts provider.ListGroupEpicsOptions) ([]provider.Epic, error) {
	return call(ctx, p, "list", "epic", fmt.Sprint(groupID), opts, func() ([]provider.Epic, error) {
		return p.inner.ListGroupEpics(ctx, groupID, opts)
	})
}

func (p *Provider) LinkIssueToEpic(ctx context.Context, groupID, epicIID, issueID int) error {
	linker, ok := p.inner.(provider.EpicLinker)
	if !ok {
		return provider.ErrUnsupported
	}
	input := struct{ GroupID, EpicIID, IssueID int }{groupID, epicIID, issueID}
	return callErr(ctx, p, "link", "epic-issue", fmt.Sprint(groupID), input, func() error {
		return linker.LinkIssueToEpic(ctx, groupID, epicIID, issueID)
	})
}

func (p *Provider) UpdateGroupEpic(ctx context.Context, groupID int, epicIID int, opts provider.UpdateEpicOptions) (provider.Epic, error) {
	return call(ctx, p, "update", "epic", fmt.Sprint(groupID), struct {
		EpicIID int
		Opts    provider.UpdateEpicOptions
	}{epicIID, opts}, func() (provider.Epic, error) {
		return p.inner.UpdateGroupEpic(ctx, groupID, epicIID, opts)
	})
}

func (p *Provider) ListEpicIssues(ctx context.Context, groupID int, epicIID int) ([]provider.Issue, error) {
	return call(ctx, p, "list", "epic-issue", fmt.Sprint(groupID), epicIID, func() ([]provider.Issue, error) {
		return p.inner.ListEpicIssues(ctx, groupID, epicIID)
	})
}

func (p *Provider) CreateMilestone(ctx context.Context, project string, opts provider.CreateMilestoneOptions) (provider.Milestone, error) {
	return call(ctx, p, "create", "milestone", project, opts, func() (provider.Milestone, error) {
		return p.inner.CreateMilestone(ctx, project, opts)
	})
}

func (p *Provider) UpdateMilestone(ctx context.Context, project string, id int, opts provider.UpdateMilestoneOptions) (provider.Milestone, error) {
	return call(ctx, p, "update", "milestone", project, struct {
		ID   int
		Opts provider.UpdateMilestoneOptions
	}{id, opts}, func() (provider.Milestone, error) {
		return p.inner.UpdateMilestone(ctx, project, id, opts)
	})
}

// ─── CI/CD pipeline surface ───────────────────────────────────────────────────

func (p *Provider) ListPipelines(ctx context.Context, project string, opts provider.ListPipelinesOptions) ([]provider.Pipeline, error) {
	return call(ctx, p, "list", "pipeline", project, opts, func() ([]provider.Pipeline, error) {
		return p.inner.ListPipelines(ctx, project, opts)
	})
}

func (p *Provider) GetPipeline(ctx context.Context, project string, id int) (provider.Pipeline, error) {
	return call(ctx, p, "get", "pipeline", project, id, func() (provider.Pipeline, error) {
		return p.inner.GetPipeline(ctx, project, id)
	})
}

func (p *Provider) RunPipeline(ctx context.Context, project string, opts provider.RunPipelineOptions) (provider.Pipeline, error) {
	return call(ctx, p, "run", "pipeline", project, opts, func() (provider.Pipeline, error) {
		return p.inner.RunPipeline(ctx, project, opts)
	})
}

func (p *Provider) RetryPipeline(ctx context.Context, project string, id int) (provider.Pipeline, error) {
	return call(ctx, p, "retry", "pipeline", project, id, func() (provider.Pipeline, error) {
		return p.inner.RetryPipeline(ctx, project, id)
	})
}

func (p *Provider) CancelPipeline(ctx context.Context, project string, id int) error {
	return callErr(ctx, p, "cancel", "pipeline", project, id, func() error {
		return p.inner.CancelPipeline(ctx, project, id)
	})
}

func (p *Provider) ListPipelineJobs(ctx context.Context, project string, pipelineID int) ([]provider.Job, error) {
	return call(ctx, p, "list", "job", project, pipelineID, func() ([]provider.Job, error) {
		return p.inner.ListPipelineJobs(ctx, project, pipelineID)
	})
}

func (p *Provider) GetJobLogs(ctx context.Context, project string, jobID int) (string, error) {
	return call(ctx, p, "logs", "job", project, jobID, func() (string, error) {
		return p.inner.GetJobLogs(ctx, project, jobID)
	})
}

func (p *Provider) ListArtifacts(ctx context.Context, project string, pipelineID int) ([]provider.Artifact, error) {
	return call(ctx, p, "list", "artifact", project, pipelineID, func() ([]provider.Artifact, error) {
		return p.inner.ListArtifacts(ctx, project, pipelineID)
	})
}

func (p *Provider) DownloadArtifact(ctx context.Context, project string, artifactID int, destDir string) error {
	return callErr(ctx, p, "download", "artifact", project, struct {
		ArtifactID int
		DestDir    string
	}{artifactID, destDir}, func() error {
		return p.inner.DownloadArtifact(ctx, project, artifactID, destDir)
	})
}

func IsUncertain(err error) bool {
	var target *ReceiptUncertainError
	return errors.As(err, &target)
}
