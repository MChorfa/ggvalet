package reconcile

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ckodex/gitlabvalet/internal/plan"
	"github.com/ckodex/gitlabvalet/internal/provider"
	"github.com/ckodex/gitlabvalet/internal/state"
)

type fakeProvider struct {
	provider.Provider
	milestones []provider.Milestone
	epics      []provider.Epic
	issues     []provider.Issue
	failIssue  bool
	links      int
}

type noLinkProvider struct{ provider.Provider }

func (*noLinkProvider) Kind() provider.Kind { return provider.KindGitLab }
func (*noLinkProvider) Host() string        { return "gitlab.test" }

type githubProvider struct{ provider.Provider }

func (*githubProvider) Kind() provider.Kind { return provider.KindGitHub }
func (*githubProvider) Host() string        { return "github.test" }

func (f *fakeProvider) Kind() provider.Kind { return provider.KindGitLab }
func (f *fakeProvider) Host() string        { return "gitlab.test" }
func (f *fakeProvider) ListGroupMilestones(context.Context, int, provider.ListGroupMilestonesOptions) ([]provider.Milestone, error) {
	return f.milestones, nil
}
func (f *fakeProvider) CreateGroupMilestone(_ context.Context, _ int, opts provider.CreateMilestoneOptions) (provider.Milestone, error) {
	m := provider.Milestone{ID: 10, IID: 10, Title: opts.Title, Description: opts.Description, WebURL: "milestone"}
	f.milestones = append(f.milestones, m)
	return m, nil
}
func (f *fakeProvider) ListGroupEpics(context.Context, int, provider.ListGroupEpicsOptions) ([]provider.Epic, error) {
	return f.epics, nil
}
func (f *fakeProvider) CreateGroupEpic(_ context.Context, _ int, opts provider.CreateEpicOptions) (provider.Epic, error) {
	e := provider.Epic{ID: 20, IID: 20, Title: opts.Title, Description: opts.Description, WebURL: "epic"}
	f.epics = append(f.epics, e)
	return e, nil
}
func (f *fakeProvider) ListIssues(context.Context, string, provider.ListIssuesOptions) ([]provider.Issue, error) {
	return f.issues, nil
}
func (f *fakeProvider) CreateIssue(_ context.Context, project string, opts provider.CreateIssueOptions) (provider.Issue, error) {
	if f.failIssue {
		f.failIssue = false
		return provider.Issue{}, errors.New("injected issue failure")
	}
	i := provider.Issue{ID: 30, IID: 30, Project: project, Title: opts.Title, Body: opts.Description, WebURL: "issue"}
	f.issues = append(f.issues, i)
	return i, nil
}
func (f *fakeProvider) LinkIssueToEpic(context.Context, int, int, int) error { f.links++; return nil }

func TestEngineStopsAndResumesWithoutDuplicates(t *testing.T) {
	s, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fake := &fakeProvider{failIssue: true}
	engine := Engine{Provider: fake, Store: s}
	p := &plan.Plan{Version: "2", Target: plan.Target{Provider: "gitlab", GroupID: 1, ProjectID: 2},
		Milestones: []plan.Milestone{{ID: "m1", Title: "M1"}},
		Epics:      []plan.Epic{{ID: "e1", Title: "E1"}},
		Issues:     []plan.Issue{{ID: "i1", Title: "I1", Milestone: "m1", Epic: "e1"}},
	}
	runID, err := engine.Start(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Execute(context.Background(), runID); err == nil {
		t.Fatal("expected injected failure")
	}
	if len(fake.milestones) != 1 || len(fake.epics) != 1 || len(fake.issues) != 0 {
		t.Fatalf("first pass counts: %d %d %d", len(fake.milestones), len(fake.epics), len(fake.issues))
	}
	if err := engine.Resume(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
	if len(fake.milestones) != 1 || len(fake.epics) != 1 || len(fake.issues) != 1 || fake.links != 1 {
		t.Fatalf("resume counts: %d %d %d links=%d", len(fake.milestones), len(fake.epics), len(fake.issues), fake.links)
	}
	run, steps, err := s.Run(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != state.StatusSucceeded {
		t.Fatalf("run status = %s", run.Status)
	}
	for _, step := range steps {
		if step.Status != state.StatusSucceeded {
			t.Fatalf("step %#v", step)
		}
	}
	if err := engine.Resume(context.Background(), runID); err != nil {
		t.Fatalf("resume succeeded run: %v", err)
	}
	if err := engine.Execute(context.Background(), runID); err != nil {
		t.Fatalf("re-execute succeeded run: %v", err)
	}
	if len(fake.milestones) != 1 || len(fake.epics) != 1 || len(fake.issues) != 1 {
		t.Fatal("re-execution duplicated resources")
	}
}

func TestEngineValidationBoundaries(t *testing.T) {
	s, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	engine := Engine{Provider: &fakeProvider{}, Store: s}
	github := &plan.Plan{Version: "2", Target: plan.Target{Provider: "github", GroupID: 1}}
	if err := engine.Validate(github); err == nil {
		t.Fatal("expected provider mismatch")
	}
	bad := &plan.Plan{Version: "2", Target: plan.Target{Provider: "gitlab", GroupID: 1},
		Issues: []plan.Issue{{ID: "i", Title: "I", DependsOn: []string{"missing"}}}}
	if err := engine.Validate(bad); err == nil {
		t.Fatal("expected dependency validation error")
	}
	nonGitLab := Engine{Provider: &githubProvider{}, Store: s}
	if err := nonGitLab.Validate(github); err == nil {
		t.Fatal("expected GitLab-first boundary")
	}
	missingLink := Engine{Provider: &noLinkProvider{}, Store: s}
	withEpic := &plan.Plan{Version: "2", Target: plan.Target{Provider: "gitlab", GroupID: 1}, Epics: []plan.Epic{{ID: "e", Title: "E"}}}
	if err := missingLink.Validate(withEpic); err == nil {
		t.Fatal("expected epic-link capability error")
	}
}

func TestEngineRefusesUncertainResume(t *testing.T) {
	s, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	engine := Engine{Provider: &fakeProvider{}, Store: s}
	p := &plan.Plan{Version: "2", Target: plan.Target{Provider: "gitlab", GroupID: 1, ProjectID: 2}, Issues: []plan.Issue{{ID: "i", Title: "I"}}}
	runID, err := engine.Start(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStep(context.Background(), runID, "issue:i", state.StatusUncertain, 0, 0, "", "lost outcome"); err != nil {
		t.Fatal(err)
	}
	if err := engine.Resume(context.Background(), runID); err == nil {
		t.Fatal("expected uncertain resume refusal")
	}
}

func TestEngineStopsOnCorruptStoredPlan(t *testing.T) {
	s, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	runID, err := s.StartRun(context.Background(), "gitlab:test:1:2", "gitlab", []byte(`{`), nil)
	if err != nil {
		t.Fatal(err)
	}
	engine := Engine{Provider: &fakeProvider{}, Store: s}
	if err := engine.Execute(context.Background(), runID); err == nil {
		t.Fatal("expected corrupt plan error")
	}
	run, _, err := s.Run(context.Background(), runID)
	if err != nil || run.Status != state.StatusFailed {
		t.Fatalf("run=%#v err=%v", run, err)
	}
}
