package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ckodex/gitlabvalet/internal/plan"
	"github.com/ckodex/gitlabvalet/internal/provider"
)

func TestPlanApply_RequiresYes_WhenApply(t *testing.T) {
	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "plan.yaml")

	fixture := `version: "1"
target:
  provider: gitlab
  group_id: 1
`
	if err := os.WriteFile(fixturePath, []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	t.Setenv("GLVALET_PROVIDER", "")

	cmd := planApplyCmd()
	cmd.SetArgs([]string{fixturePath, "--dry-run=false"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "requires --yes") {
		t.Fatalf("expected error containing %q, got: %v", "requires --yes", err)
	}
}

// ─── stub provider for diff tests ─────────────────────────────────────────────

type testProvider struct {
	milestones    []provider.Milestone
	epics         []provider.Epic
	issues        []provider.Issue
	milestonesErr error
	epicsErr      error
}

var _ provider.Provider = (*testProvider)(nil)

func (t *testProvider) Kind() provider.Kind { return provider.KindGitLab }
func (t *testProvider) Host() string        { return "test.local" }

func (t *testProvider) ListIssues(ctx context.Context, project string, opts provider.ListIssuesOptions) ([]provider.Issue, error) {
	if opts.Page > 1 {
		return nil, nil
	}
	return t.issues, nil
}

func (t *testProvider) ListMyIssues(ctx context.Context, opts provider.ListMyIssuesOptions) ([]provider.Issue, error) {
	if opts.Page > 1 {
		return nil, nil
	}
	return t.issues, nil
}

func (t *testProvider) GetIssue(ctx context.Context, project string, iid int) (provider.Issue, error) {
	return provider.Issue{}, provider.ErrUnsupported
}

func (t *testProvider) CreateIssue(ctx context.Context, project string, opts provider.CreateIssueOptions) (provider.Issue, error) {
	return provider.Issue{}, provider.ErrUnsupported
}

func (t *testProvider) UpdateIssue(ctx context.Context, project string, iid int, opts provider.UpdateIssueOptions) (provider.Issue, error) {
	return provider.Issue{}, provider.ErrUnsupported
}

func (t *testProvider) CreateIssueNote(ctx context.Context, project string, iid int, body string) (provider.Note, error) {
	return provider.Note{}, provider.ErrUnsupported
}

func (t *testProvider) ListUsers(ctx context.Context, opts provider.ListUsersOptions) ([]provider.User, error) {
	return nil, provider.ErrUnsupported
}

func (t *testProvider) ListMergeRequests(ctx context.Context, project string, opts provider.ListMergeRequestsOptions) ([]provider.MergeRequest, error) {
	return nil, provider.ErrUnsupported
}

func (t *testProvider) ListMyMergeRequests(ctx context.Context, opts provider.ListMyMergeRequestsOptions) ([]provider.MergeRequest, error) {
	return nil, provider.ErrUnsupported
}

func (t *testProvider) ApproveMergeRequest(ctx context.Context, project string, iid int) error {
	return provider.ErrUnsupported
}

func (t *testProvider) MergeMergeRequest(ctx context.Context, project string, iid int, opts provider.MergeOptions) (provider.MergeRequest, error) {
	return provider.MergeRequest{}, provider.ErrUnsupported
}

func (t *testProvider) GetMergeRequestDiff(ctx context.Context, project string, iid int) ([]provider.FileDiff, error) {
	return nil, provider.ErrUnsupported
}

func (t *testProvider) GetMergeRequest(ctx context.Context, project string, iid int) (provider.MergeRequest, error) {
	return provider.MergeRequest{}, provider.ErrUnsupported
}

func (t *testProvider) CreateMergeRequest(ctx context.Context, project string, opts provider.CreateMergeRequestOptions) (provider.MergeRequest, error) {
	return provider.MergeRequest{}, provider.ErrUnsupported
}

func (t *testProvider) UpdateMergeRequest(ctx context.Context, project string, iid int, opts provider.UpdateMergeRequestOptions) (provider.MergeRequest, error) {
	return provider.MergeRequest{}, provider.ErrUnsupported
}

func (t *testProvider) ListLabels(ctx context.Context, project string) ([]provider.Label, error) {
	return nil, provider.ErrUnsupported
}

func (t *testProvider) CreateLabel(ctx context.Context, project string, opts provider.CreateLabelOptions) (provider.Label, error) {
	return provider.Label{}, provider.ErrUnsupported
}

func (t *testProvider) CreateGroupMilestone(ctx context.Context, groupID int, opts provider.CreateMilestoneOptions) (provider.Milestone, error) {
	return provider.Milestone{}, provider.ErrUnsupported
}

func (t *testProvider) CreateGroupEpic(ctx context.Context, groupID int, opts provider.CreateEpicOptions) (provider.Epic, error) {
	return provider.Epic{}, provider.ErrUnsupported
}

func (t *testProvider) ListGroupMilestones(ctx context.Context, groupID int, opts provider.ListGroupMilestonesOptions) ([]provider.Milestone, error) {
	if opts.Page > 1 {
		return nil, nil
	}
	if t.milestonesErr != nil {
		return nil, t.milestonesErr
	}
	return t.milestones, nil
}

func (t *testProvider) ListGroupEpics(ctx context.Context, groupID int, opts provider.ListGroupEpicsOptions) ([]provider.Epic, error) {
	if opts.Page > 1 {
		return nil, nil
	}
	if t.epicsErr != nil {
		return nil, t.epicsErr
	}
	return t.epics, nil
}

// ─── diff tests ──────────────────────────────────────────────────────────────

func TestPlanDiff_Matched_WhenHashFoundInRemote(t *testing.T) {
	m := plan.Milestone{ID: "m1", Title: "Milestone One", DueDate: "2026-06-01"}
	hash := plan.MilestoneIdentity(m)
	desc := plan.EmbedMarker("", hash)

	remoteMilestone := provider.Milestone{
		IID:         1,
		Title:       "Milestone One",
		Description: desc,
		WebURL:      "https://test.local/group/-/milestones/1",
	}

	testProv := &testProvider{milestones: []provider.Milestone{remoteMilestone}}

	planStruct := &plan.Plan{
		Target:     plan.Target{Provider: "gitlab", GroupID: 1},
		Milestones: []plan.Milestone{m},
	}

	result, err := computeDiff(context.Background(), testProv, planStruct)
	if err != nil {
		t.Fatalf("computeDiff: %v", err)
	}

	if len(result.matched) != 1 {
		t.Errorf("matched count = %d; want 1", len(result.matched))
	}
	if len(result.missing) != 0 {
		t.Errorf("missing count = %d; want 0", len(result.missing))
	}
	if len(result.orphan) != 0 {
		t.Errorf("orphan count = %d; want 0", len(result.orphan))
	}

	if result.matched[0].kind != "milestone" {
		t.Errorf("matched[0].kind = %q; want milestone", result.matched[0].kind)
	}
	if result.matched[0].id != "!1" {
		t.Errorf("matched[0].id = %q; want !1", result.matched[0].id)
	}
}

func TestPlanDiff_Missing_WhenHashNotFoundInRemote(t *testing.T) {
	m := plan.Milestone{ID: "m1", Title: "Milestone One", DueDate: "2026-06-01"}

	testProv := &testProvider{milestones: []provider.Milestone{}}

	planStruct := &plan.Plan{
		Target:     plan.Target{Provider: "gitlab", GroupID: 1},
		Milestones: []plan.Milestone{m},
	}

	result, err := computeDiff(context.Background(), testProv, planStruct)
	if err != nil {
		t.Fatalf("computeDiff: %v", err)
	}

	if len(result.matched) != 0 {
		t.Errorf("matched count = %d; want 0", len(result.matched))
	}
	if len(result.missing) != 1 {
		t.Errorf("missing count = %d; want 1", len(result.missing))
	}
	if len(result.orphan) != 0 {
		t.Errorf("orphan count = %d; want 0", len(result.orphan))
	}

	if result.missing[0].kind != "milestone" {
		t.Errorf("missing[0].kind = %q; want milestone", result.missing[0].kind)
	}
	if result.missing[0].id != "m1" {
		t.Errorf("missing[0].id = %q; want m1", result.missing[0].id)
	}
}

func TestPlanDiff_Orphan_WhenRemoteMarkerNotInPlan(t *testing.T) {
	m := plan.Milestone{ID: "unused", Title: "Orphan Milestone", DueDate: "2026-07-01"}
	hash := plan.MilestoneIdentity(m)
	desc := plan.EmbedMarker("", hash)

	remoteMilestone := provider.Milestone{
		IID:         2,
		Title:       "Orphan Milestone",
		Description: desc,
		WebURL:      "https://test.local/group/-/milestones/2",
	}

	testProv := &testProvider{milestones: []provider.Milestone{remoteMilestone}}

	planStruct := &plan.Plan{
		Target:     plan.Target{Provider: "gitlab", GroupID: 1},
		Milestones: []plan.Milestone{},
	}

	result, err := computeDiff(context.Background(), testProv, planStruct)
	if err != nil {
		t.Fatalf("computeDiff: %v", err)
	}

	if len(result.matched) != 0 {
		t.Errorf("matched count = %d; want 0", len(result.matched))
	}
	if len(result.missing) != 0 {
		t.Errorf("missing count = %d; want 0", len(result.missing))
	}
	if len(result.orphan) != 1 {
		t.Errorf("orphan count = %d; want 1", len(result.orphan))
	}

	if result.orphan[0].kind != "milestone" {
		t.Errorf("orphan[0].kind = %q; want milestone", result.orphan[0].kind)
	}
	if result.orphan[0].id != "!2" {
		t.Errorf("orphan[0].id = %q; want !2", result.orphan[0].id)
	}
}

func TestPlanDiff_MilestonesUnsupported_SilentlySkips(t *testing.T) {
	m := plan.Milestone{ID: "m1", Title: "Milestone One", DueDate: "2026-06-01"}

	testProv := &testProvider{milestonesErr: provider.ErrUnsupported}

	planStruct := &plan.Plan{
		Target:     plan.Target{Provider: "gitlab", GroupID: 1},
		Milestones: []plan.Milestone{m},
	}

	result, err := computeDiff(context.Background(), testProv, planStruct)
	if err != nil {
		t.Fatalf("computeDiff: %v", err)
	}

	if len(result.matched) != 0 {
		t.Errorf("matched count = %d; want 0", len(result.matched))
	}
	if len(result.missing) != 0 {
		t.Errorf("missing count = %d; want 0", len(result.missing))
	}
	if len(result.orphan) != 0 {
		t.Errorf("orphan count = %d; want 0", len(result.orphan))
	}
}

func TestPlanDiff_EpicsUnsupported_SilentlySkips(t *testing.T) {
	e := plan.Epic{ID: "e1", Title: "Epic One"}

	testProv := &testProvider{epicsErr: provider.ErrUnsupported}

	planStruct := &plan.Plan{
		Target: plan.Target{Provider: "gitlab", GroupID: 1},
		Epics:  []plan.Epic{e},
	}

	result, err := computeDiff(context.Background(), testProv, planStruct)
	if err != nil {
		t.Fatalf("computeDiff: %v", err)
	}

	if len(result.matched) != 0 {
		t.Errorf("matched count = %d; want 0", len(result.matched))
	}
	if len(result.missing) != 0 {
		t.Errorf("missing count = %d; want 0", len(result.missing))
	}
	if len(result.orphan) != 0 {
		t.Errorf("orphan count = %d; want 0", len(result.orphan))
	}
}

func TestPlanDiff_Issues_ClassifiedCorrectly(t *testing.T) {
	i := plan.Issue{ID: "i1", Title: "Issue One", Milestone: "", Epic: ""}
	hash := plan.IssueIdentity(i)
	desc := plan.EmbedMarker("", hash)

	remoteIssue := provider.Issue{
		IID:    42,
		Title:  "Issue One",
		Body:   desc,
		WebURL: "https://test.local/project/-/issues/42",
	}

	testProv := &testProvider{issues: []provider.Issue{remoteIssue}}

	planStruct := &plan.Plan{
		Target:     plan.Target{Provider: "gitlab", GroupID: 1, ProjectID: 5},
		Issues:     []plan.Issue{i},
		Milestones: []plan.Milestone{},
		Epics:      []plan.Epic{},
	}

	result, err := computeDiff(context.Background(), testProv, planStruct)
	if err != nil {
		t.Fatalf("computeDiff: %v", err)
	}

	if len(result.matched) != 1 {
		t.Errorf("matched count = %d; want 1", len(result.matched))
	}
	if result.matched[0].id != "#42" {
		t.Errorf("matched[0].id = %q; want #42", result.matched[0].id)
	}
}
