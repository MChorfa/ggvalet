package provider_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ckodex/gitlabvalet/internal/provider"
)

// ─── sentinel ─────────────────────────────────────────────────────────────────

// TestErrUnsupported_IsSentinel verifies that ErrUnsupported satisfies
// errors.Is both directly and when wrapped.
func TestErrUnsupported_IsSentinel(t *testing.T) {
	t.Parallel()

	if !errors.Is(provider.ErrUnsupported, provider.ErrUnsupported) {
		t.Error("errors.Is(ErrUnsupported, ErrUnsupported) = false; want true")
	}

	wrapped := fmt.Errorf("op: %w", provider.ErrUnsupported)
	if !errors.Is(wrapped, provider.ErrUnsupported) {
		t.Error("errors.Is(wrapped ErrUnsupported) = false; want true")
	}
}

// ─── kind string values ───────────────────────────────────────────────────────

// TestKind_StringValues verifies the literal string values for each Kind
// constant — P6 depends on these being stable.
func TestKind_StringValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind provider.Kind
		want string
	}{
		{provider.KindGitLab, "gitlab"},
		{provider.KindGitHub, "github"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			if string(tc.kind) != tc.want {
				t.Errorf("Kind value = %q; want %q", tc.kind, tc.want)
			}
		})
	}
}

// ─── compile-time interface satisfaction ─────────────────────────────────────

// stubProvider is a minimal in-test implementation of provider.Provider.
// It returns ErrUnsupported from every method, proving the interface
// is satisfiable without a real host connection.
type stubProvider struct{}

func (s *stubProvider) Kind() provider.Kind { return provider.KindGitLab }
func (s *stubProvider) Host() string        { return "stub.local" }

func (s *stubProvider) ListIssues(_ context.Context, _ string, _ provider.ListIssuesOptions) ([]provider.Issue, error) {
	return nil, provider.ErrUnsupported
}

func (s *stubProvider) ListMyIssues(_ context.Context, _ provider.ListMyIssuesOptions) ([]provider.Issue, error) {
	return nil, provider.ErrUnsupported
}

func (s *stubProvider) GetIssue(_ context.Context, _ string, _ int) (provider.Issue, error) {
	return provider.Issue{}, provider.ErrUnsupported
}

func (s *stubProvider) CreateIssue(_ context.Context, _ string, _ provider.CreateIssueOptions) (provider.Issue, error) {
	return provider.Issue{}, provider.ErrUnsupported
}

func (s *stubProvider) UpdateIssue(_ context.Context, _ string, _ int, _ provider.UpdateIssueOptions) (provider.Issue, error) {
	return provider.Issue{}, provider.ErrUnsupported
}

func (s *stubProvider) CreateIssueNote(_ context.Context, _ string, _ int, _ string) (provider.Note, error) {
	return provider.Note{}, provider.ErrUnsupported
}

func (s *stubProvider) ListUsers(_ context.Context, _ provider.ListUsersOptions) ([]provider.User, error) {
	return nil, provider.ErrUnsupported
}

func (s *stubProvider) ListMergeRequests(_ context.Context, _ string, _ provider.ListMergeRequestsOptions) ([]provider.MergeRequest, error) {
	return nil, provider.ErrUnsupported
}

func (s *stubProvider) ListMyMergeRequests(_ context.Context, _ provider.ListMyMergeRequestsOptions) ([]provider.MergeRequest, error) {
	return nil, provider.ErrUnsupported
}

func (s *stubProvider) ApproveMergeRequest(_ context.Context, _ string, _ int) error {
	return provider.ErrUnsupported
}

func (s *stubProvider) MergeMergeRequest(_ context.Context, _ string, _ int, _ provider.MergeOptions) (provider.MergeRequest, error) {
	return provider.MergeRequest{}, provider.ErrUnsupported
}

func (s *stubProvider) GetMergeRequestDiff(_ context.Context, _ string, _ int) ([]provider.FileDiff, error) {
	return nil, provider.ErrUnsupported
}

func (s *stubProvider) GetMergeRequest(_ context.Context, _ string, _ int) (provider.MergeRequest, error) {
	return provider.MergeRequest{}, provider.ErrUnsupported
}

func (s *stubProvider) CreateMergeRequest(_ context.Context, _ string, _ provider.CreateMergeRequestOptions) (provider.MergeRequest, error) {
	return provider.MergeRequest{}, provider.ErrUnsupported
}

func (s *stubProvider) UpdateMergeRequest(_ context.Context, _ string, _ int, _ provider.UpdateMergeRequestOptions) (provider.MergeRequest, error) {
	return provider.MergeRequest{}, provider.ErrUnsupported
}

func (s *stubProvider) ListLabels(_ context.Context, _ string) ([]provider.Label, error) {
	return nil, provider.ErrUnsupported
}

func (s *stubProvider) CreateLabel(_ context.Context, _ string, _ provider.CreateLabelOptions) (provider.Label, error) {
	return provider.Label{}, provider.ErrUnsupported
}

func (s *stubProvider) CreateGroupMilestone(_ context.Context, _ int, _ provider.CreateMilestoneOptions) (provider.Milestone, error) {
	return provider.Milestone{}, provider.ErrUnsupported
}

func (s *stubProvider) CreateGroupEpic(_ context.Context, _ int, _ provider.CreateEpicOptions) (provider.Epic, error) {
	return provider.Epic{}, provider.ErrUnsupported
}

func (s *stubProvider) ListGroupMilestones(_ context.Context, _ int, _ provider.ListGroupMilestonesOptions) ([]provider.Milestone, error) {
	return nil, provider.ErrUnsupported
}

func (s *stubProvider) ListGroupEpics(_ context.Context, _ int, _ provider.ListGroupEpicsOptions) ([]provider.Epic, error) {
	return nil, provider.ErrUnsupported
}

// TestProviderInterface_CompileTime declares the compile-time assertion that
// *stubProvider satisfies provider.Provider and verifies stub return values.
func TestProviderInterface_CompileTime(t *testing.T) {
	t.Parallel()

	// Compile-time check: this line fails to build if Provider interface changes.
	var _ provider.Provider = (*stubProvider)(nil)

	s := &stubProvider{}
	ctx := context.Background()

	if s.Kind() != provider.KindGitLab {
		t.Errorf("stub Kind() = %q; want %q", s.Kind(), provider.KindGitLab)
	}
	if s.Host() != "stub.local" {
		t.Errorf("stub Host() = %q; want %q", s.Host(), "stub.local")
	}

	_, err := s.ListIssues(ctx, "p", provider.ListIssuesOptions{})
	if !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("ListIssues err = %v; want ErrUnsupported", err)
	}

	_, err = s.GetIssue(ctx, "p", 1)
	if !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("GetIssue err = %v; want ErrUnsupported", err)
	}

	_, err = s.CreateIssue(ctx, "p", provider.CreateIssueOptions{})
	if !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("CreateIssue err = %v; want ErrUnsupported", err)
	}

	_, err = s.ListMergeRequests(ctx, "p", provider.ListMergeRequestsOptions{})
	if !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("ListMergeRequests err = %v; want ErrUnsupported", err)
	}

	_, err = s.GetMergeRequest(ctx, "p", 1)
	if !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("GetMergeRequest err = %v; want ErrUnsupported", err)
	}

	_, err = s.ListLabels(ctx, "p")
	if !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("ListLabels err = %v; want ErrUnsupported", err)
	}
}
