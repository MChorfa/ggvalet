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

func IsUncertain(err error) bool {
	var target *ReceiptUncertainError
	return errors.As(err, &target)
}
