package observed

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/MChorfa/ggvalet/internal/state"
)

type completeProvider struct{ fakeProvider }

func (*completeProvider) ListMyIssues(context.Context, provider.ListMyIssuesOptions) ([]provider.Issue, error) {
	return nil, nil
}
func (*completeProvider) GetIssue(context.Context, string, int) (provider.Issue, error) {
	return provider.Issue{}, nil
}
func (*completeProvider) CreateIssue(context.Context, string, provider.CreateIssueOptions) (provider.Issue, error) {
	return provider.Issue{}, nil
}
func (*completeProvider) UpdateIssue(context.Context, string, int, provider.UpdateIssueOptions) (provider.Issue, error) {
	return provider.Issue{}, nil
}
func (*completeProvider) CreateIssueNote(context.Context, string, int, string) (provider.Note, error) {
	return provider.Note{}, nil
}
func (*completeProvider) ListMergeRequests(context.Context, string, provider.ListMergeRequestsOptions) ([]provider.MergeRequest, error) {
	return nil, nil
}
func (*completeProvider) ListMyMergeRequests(context.Context, provider.ListMyMergeRequestsOptions) ([]provider.MergeRequest, error) {
	return nil, nil
}
func (*completeProvider) GetMergeRequest(context.Context, string, int) (provider.MergeRequest, error) {
	return provider.MergeRequest{}, nil
}
func (*completeProvider) CreateMergeRequest(context.Context, string, provider.CreateMergeRequestOptions) (provider.MergeRequest, error) {
	return provider.MergeRequest{}, nil
}
func (*completeProvider) UpdateMergeRequest(context.Context, string, int, provider.UpdateMergeRequestOptions) (provider.MergeRequest, error) {
	return provider.MergeRequest{}, nil
}
func (*completeProvider) ApproveMergeRequest(context.Context, string, int) error { return nil }
func (*completeProvider) MergeMergeRequest(context.Context, string, int, provider.MergeOptions) (provider.MergeRequest, error) {
	return provider.MergeRequest{}, nil
}
func (*completeProvider) GetMergeRequestDiff(context.Context, string, int) ([]provider.FileDiff, error) {
	return nil, nil
}
func (*completeProvider) ListLabels(context.Context, string) ([]provider.Label, error) {
	return nil, nil
}
func (*completeProvider) CreateLabel(context.Context, string, provider.CreateLabelOptions) (provider.Label, error) {
	return provider.Label{}, nil
}
func (*completeProvider) ListUsers(context.Context, provider.ListUsersOptions) ([]provider.User, error) {
	return nil, nil
}
func (*completeProvider) CreateGroupMilestone(context.Context, int, provider.CreateMilestoneOptions) (provider.Milestone, error) {
	return provider.Milestone{}, nil
}
func (*completeProvider) ListGroupMilestones(context.Context, int, provider.ListGroupMilestonesOptions) ([]provider.Milestone, error) {
	return nil, nil
}
func (*completeProvider) ListMilestones(context.Context, string, provider.ListMilestonesOptions) ([]provider.Milestone, error) {
	return nil, nil
}
func (*completeProvider) ResolveGroup(context.Context, string) (int, error) {
	return 0, nil
}
func (*completeProvider) CreateGroupEpic(context.Context, int, provider.CreateEpicOptions) (provider.Epic, error) {
	return provider.Epic{}, nil
}
func (*completeProvider) ListGroupEpics(context.Context, int, provider.ListGroupEpicsOptions) ([]provider.Epic, error) {
	return nil, nil
}
func (*completeProvider) LinkIssueToEpic(context.Context, int, int, int) error { return nil }

type fakeProvider struct {
	provider.Provider
	calls int
	hook  func()
	err   error
}

func (f *fakeProvider) Kind() provider.Kind { return provider.KindGitLab }
func (f *fakeProvider) Host() string        { return "gitlab.test" }
func (f *fakeProvider) ListIssues(context.Context, string, provider.ListIssuesOptions) ([]provider.Issue, error) {
	f.calls++
	if f.hook != nil {
		f.hook()
	}
	return []provider.Issue{{ID: 1}}, f.err
}

func TestProviderPersistsIntentAndOutcome(t *testing.T) {
	s, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fake := &fakeProvider{}
	p := NewProvider(fake, s, "gitlab.test")
	if _, err := p.ListIssues(context.Background(), "g/p", provider.ListIssuesOptions{}); err != nil {
		t.Fatal(err)
	}
	receipts, err := s.Receipts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || len(receipts) != 2 || receipts[1].Status != state.StatusSucceeded {
		t.Fatalf("calls=%d receipts=%#v", fake.calls, receipts)
	}
}

func TestProviderFailsClosedBeforeRemoteCall(t *testing.T) {
	s, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	fake := &fakeProvider{}
	p := NewProvider(fake, s, "gitlab.test")
	if _, err := p.ListIssues(context.Background(), "g/p", provider.ListIssuesOptions{}); err == nil {
		t.Fatal("expected error")
	}
	if fake.calls != 0 {
		t.Fatalf("remote calls = %d, want 0", fake.calls)
	}
}

func TestProviderMarksPostCallReceiptFailureUncertain(t *testing.T) {
	s, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeProvider{hook: func() { _ = s.Close() }}
	p := NewProvider(fake, s, "gitlab.test")
	_, err = p.ListIssues(context.Background(), "g/p", provider.ListIssuesOptions{})
	var uncertain *ReceiptUncertainError
	if !errors.As(err, &uncertain) {
		t.Fatalf("error = %v, want ReceiptUncertainError", err)
	}
}

func TestProviderDecoratesCompleteInterface(t *testing.T) {
	s, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := NewProvider(&completeProvider{}, s, "gitlab.test")
	ctx := context.Background()
	_, _ = p.ListMyIssues(ctx, provider.ListMyIssuesOptions{})
	_, _ = p.GetIssue(ctx, "p", 1)
	_, _ = p.CreateIssue(ctx, "p", provider.CreateIssueOptions{})
	_, _ = p.UpdateIssue(ctx, "p", 1, provider.UpdateIssueOptions{})
	_, _ = p.CreateIssueNote(ctx, "p", 1, "note")
	_, _ = p.ListMergeRequests(ctx, "p", provider.ListMergeRequestsOptions{})
	_, _ = p.ListMyMergeRequests(ctx, provider.ListMyMergeRequestsOptions{})
	_, _ = p.GetMergeRequest(ctx, "p", 1)
	_, _ = p.CreateMergeRequest(ctx, "p", provider.CreateMergeRequestOptions{})
	_, _ = p.UpdateMergeRequest(ctx, "p", 1, provider.UpdateMergeRequestOptions{})
	_ = p.ApproveMergeRequest(ctx, "p", 1)
	_, _ = p.MergeMergeRequest(ctx, "p", 1, provider.MergeOptions{})
	_, _ = p.GetMergeRequestDiff(ctx, "p", 1)
	_, _ = p.ListLabels(ctx, "p")
	_, _ = p.CreateLabel(ctx, "p", provider.CreateLabelOptions{})
	_, _ = p.ListUsers(ctx, provider.ListUsersOptions{})
	_, _ = p.CreateGroupMilestone(ctx, 1, provider.CreateMilestoneOptions{})
	_, _ = p.ListGroupMilestones(ctx, 1, provider.ListGroupMilestonesOptions{})
	_, _ = p.ListMilestones(ctx, "g/p", provider.ListMilestonesOptions{})
	_, _ = p.ResolveGroup(ctx, "g")
	_, _ = p.CreateGroupEpic(ctx, 1, provider.CreateEpicOptions{})
	_, _ = p.ListGroupEpics(ctx, 1, provider.ListGroupEpicsOptions{})
	if err := p.LinkIssueToEpic(ctx, 1, 2, 3); err != nil {
		t.Fatal(err)
	}
	receipts, err := s.Receipts(ctx)
	if err != nil || len(receipts) != 46 {
		t.Fatalf("receipts=%d err=%v", len(receipts), err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportPersistsHTTPReceipts(t *testing.T) {
	s, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
	})
	transport := &Transport{Base: base, Store: s, Provider: "gitlab", Host: "gitlab.test"}
	req, _ := http.NewRequest(http.MethodGet, "https://gitlab.test/api/v4/issues?private_token=secret", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	receipts, _ := s.Receipts(context.Background())
	if len(receipts) != 2 || strings.Contains(receipts[0].Target, "secret") {
		t.Fatalf("receipts=%#v", receipts)
	}
}
