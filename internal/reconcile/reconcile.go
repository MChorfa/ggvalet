// Package reconcile applies structured plans as resumable, idempotent runs.
package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ckodex/gitlabvalet/internal/observed"
	"github.com/ckodex/gitlabvalet/internal/plan"
	"github.com/ckodex/gitlabvalet/internal/provider"
	"github.com/ckodex/gitlabvalet/internal/state"
)

type Engine struct {
	Provider provider.Provider
	Store    *state.Store
}

func (e *Engine) Validate(p *plan.Plan) error {
	if err := plan.Validate(p); err != nil {
		return err
	}
	if p.Target.Provider != string(e.Provider.Kind()) {
		return fmt.Errorf("plan target provider %q != active provider %q", p.Target.Provider, e.Provider.Kind())
	}
	if e.Provider.Kind() != provider.KindGitLab {
		return fmt.Errorf("reconciliation is GitLab-first; provider %q is not enabled", e.Provider.Kind())
	}
	if len(p.Epics) > 0 {
		if _, ok := e.Provider.(provider.EpicLinker); !ok {
			return fmt.Errorf("provider %q cannot link issues to epics", e.Provider.Kind())
		}
	}
	return nil
}

func (e *Engine) Start(ctx context.Context, p *plan.Plan) (string, error) {
	if err := e.Validate(p); err != nil {
		return "", err
	}
	resources, err := plan.OrderedResources(p)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("marshal plan: %w", err)
	}
	steps := make([]state.PlanStep, len(resources))
	for i, resource := range resources {
		steps[i] = state.PlanStep{StepID: resource.StepID, Kind: resource.Kind}
	}
	target := fmt.Sprintf("%s:%s:%d:%d", p.Target.Provider, e.Provider.Host(), p.Target.GroupID, p.Target.ProjectID)
	return e.Store.StartRun(ctx, target, p.Target.Provider, data, steps)
}

func (e *Engine) Resume(ctx context.Context, runID string) error {
	run, steps, err := e.Store.Run(ctx, runID)
	if err != nil {
		return err
	}
	if run.Status == state.StatusSucceeded {
		return nil
	}
	for _, step := range steps {
		if step.Status == state.StatusUncertain {
			return fmt.Errorf("run %s contains uncertain step %s; run plan diff before retry", runID, step.StepID)
		}
		if step.Status == state.StatusFailed || step.Status == state.StatusBlocked {
			if err := e.Store.SetStep(ctx, runID, step.StepID, state.StatusPending, 0, 0, "", ""); err != nil {
				return err
			}
		}
	}
	if err := e.Store.SetRunStatus(ctx, runID, state.StatusRunning, ""); err != nil {
		return err
	}
	return e.Execute(ctx, runID)
}

func (e *Engine) Execute(ctx context.Context, runID string) error {
	run, steps, err := e.Store.Run(ctx, runID)
	if err != nil {
		return err
	}
	p, err := plan.ParseJSON([]byte(run.PlanJSON))
	if err != nil {
		return e.stopRun(ctx, runID, state.StatusFailed, err)
	}
	if err := e.Validate(p); err != nil {
		return e.stopRun(ctx, runID, state.StatusFailed, err)
	}
	resources, _ := plan.OrderedResources(p)
	stepByID := make(map[string]state.PlanStep, len(steps))
	for _, step := range steps {
		stepByID[step.StepID] = step
	}
	for _, resource := range resources {
		if err := e.executeResource(ctx, runID, p, resource, stepByID); err != nil {
			return err
		}
	}
	return e.Store.SetRunStatus(ctx, runID, state.StatusSucceeded, "")
}

func (e *Engine) executeResource(ctx context.Context, runID string, p *plan.Plan, resource plan.Resource, steps map[string]state.PlanStep) error {
	if steps[resource.StepID].Status == state.StatusSucceeded {
		return nil
	}
	for _, dependency := range resource.DependsOn {
		if steps[dependency].Status != state.StatusSucceeded {
			err := fmt.Errorf("step %s blocked by %s", resource.StepID, dependency)
			_ = e.Store.SetStep(ctx, runID, resource.StepID, state.StatusBlocked, 0, 0, "", err.Error())
			return e.stopRun(ctx, runID, state.StatusFailed, err)
		}
	}
	if err := e.Store.SetStep(ctx, runID, resource.StepID, state.StatusRunning, 0, 0, "", ""); err != nil {
		return e.stopRun(ctx, runID, state.StatusUncertain, err)
	}
	result, err := e.apply(ctx, p, resource, steps)
	if err != nil {
		status := state.StatusFailed
		if observed.IsUncertain(err) {
			status = state.StatusUncertain
		}
		_ = e.Store.SetStep(ctx, runID, resource.StepID, status, 0, 0, "", err.Error())
		return e.stopRun(ctx, runID, status, err)
	}
	if err := e.Store.SetStep(ctx, runID, resource.StepID, state.StatusSucceeded, result.RemoteID, result.RemoteIID, result.WebURL, ""); err != nil {
		return e.stopRun(ctx, runID, state.StatusUncertain, err)
	}
	result.RunID, result.StepID, result.Kind, result.Status = runID, resource.StepID, resource.Kind, state.StatusSucceeded
	steps[resource.StepID] = result
	return nil
}

func (e *Engine) stopRun(ctx context.Context, runID, status string, cause error) error {
	if err := e.Store.SetRunStatus(context.WithoutCancel(ctx), runID, status, cause.Error()); err != nil {
		return fmt.Errorf("%w; persist run state: %v", cause, err)
	}
	return cause
}

func (e *Engine) apply(ctx context.Context, p *plan.Plan, resource plan.Resource, steps map[string]state.PlanStep) (state.PlanStep, error) {
	switch resource.Kind {
	case "milestone":
		return e.applyMilestone(ctx, p, milestoneByID(p, resource.ID))
	case "epic":
		return e.applyEpic(ctx, p, epicByID(p, resource.ID))
	case "issue":
		return e.applyIssue(ctx, p, issueByID(p, resource.ID), steps)
	default:
		return state.PlanStep{}, fmt.Errorf("unknown resource kind %q", resource.Kind)
	}
}

func (e *Engine) applyMilestone(ctx context.Context, p *plan.Plan, m plan.Milestone) (state.PlanStep, error) {
	if existing, ok, err := e.findMilestone(ctx, p.Target.GroupID, plan.MilestoneIdentity(m)); err != nil {
		return state.PlanStep{}, err
	} else if ok {
		return state.PlanStep{RemoteID: existing.ID, RemoteIID: existing.IID, WebURL: existing.WebURL}, nil
	}
	created, err := e.Provider.CreateGroupMilestone(ctx, p.Target.GroupID, provider.CreateMilestoneOptions{
		Title: m.Title, Description: plan.EmbedMarker(m.Description, plan.MilestoneIdentity(m)), StartDate: m.StartDate, DueDate: m.DueDate,
	})
	return state.PlanStep{RemoteID: created.ID, RemoteIID: created.IID, WebURL: created.WebURL}, err
}

func (e *Engine) applyEpic(ctx context.Context, p *plan.Plan, epic plan.Epic) (state.PlanStep, error) {
	if existing, ok, err := e.findEpic(ctx, p.Target.GroupID, plan.EpicIdentity(epic)); err != nil {
		return state.PlanStep{}, err
	} else if ok {
		return state.PlanStep{RemoteID: existing.ID, RemoteIID: existing.IID, WebURL: existing.WebURL}, nil
	}
	created, err := e.Provider.CreateGroupEpic(ctx, p.Target.GroupID, provider.CreateEpicOptions{
		Title: epic.Title, Description: plan.EmbedMarker(epic.Description, plan.EpicIdentity(epic)), Labels: epic.Labels,
	})
	return state.PlanStep{RemoteID: created.ID, RemoteIID: created.IID, WebURL: created.WebURL}, err
}

func (e *Engine) applyIssue(ctx context.Context, p *plan.Plan, issue plan.Issue, steps map[string]state.PlanStep) (state.PlanStep, error) {
	project := strconv.Itoa(p.Target.ProjectID)
	result, found, err := e.findIssue(ctx, project, plan.IssueIdentity(issue))
	if err != nil {
		return state.PlanStep{}, err
	}
	if !found {
		opts := provider.CreateIssueOptions{Title: issue.Title, Description: plan.EmbedMarker(issue.Description, plan.IssueIdentity(issue)), Labels: issue.Labels, Weight: issue.Weight}
		if issue.Milestone != "" {
			id := steps["milestone:"+issue.Milestone].RemoteIID
			opts.MilestoneID = &id
		}
		result, err = e.Provider.CreateIssue(ctx, project, opts)
		if err != nil {
			return state.PlanStep{}, err
		}
	}
	if issue.Epic != "" {
		linker := e.Provider.(provider.EpicLinker)
		if err := linker.LinkIssueToEpic(ctx, p.Target.GroupID, steps["epic:"+issue.Epic].RemoteIID, result.ID); err != nil {
			return state.PlanStep{}, err
		}
	}
	return state.PlanStep{RemoteID: result.ID, RemoteIID: result.IID, WebURL: result.WebURL}, nil
}

func (e *Engine) findMilestone(ctx context.Context, groupID int, hash string) (provider.Milestone, bool, error) {
	for page := 1; ; page++ {
		items, err := e.Provider.ListGroupMilestones(ctx, groupID, provider.ListGroupMilestonesOptions{Page: page, PerPage: 100})
		if err != nil {
			return provider.Milestone{}, false, err
		}
		for _, item := range items {
			if found, ok := plan.ExtractMarker(item.Description); ok && found == hash {
				return item, true, nil
			}
		}
		if len(items) < 100 {
			return provider.Milestone{}, false, nil
		}
	}
}

func (e *Engine) findEpic(ctx context.Context, groupID int, hash string) (provider.Epic, bool, error) {
	for page := 1; ; page++ {
		items, err := e.Provider.ListGroupEpics(ctx, groupID, provider.ListGroupEpicsOptions{State: "all", Page: page, PerPage: 100})
		if err != nil {
			return provider.Epic{}, false, err
		}
		for _, item := range items {
			if found, ok := plan.ExtractMarker(item.Description); ok && found == hash {
				return item, true, nil
			}
		}
		if len(items) < 100 {
			return provider.Epic{}, false, nil
		}
	}
}

func (e *Engine) findIssue(ctx context.Context, project, hash string) (provider.Issue, bool, error) {
	for page := 1; ; page++ {
		items, err := e.Provider.ListIssues(ctx, project, provider.ListIssuesOptions{State: "all", Page: page, PerPage: 100})
		if err != nil {
			return provider.Issue{}, false, err
		}
		for _, item := range items {
			if found, ok := plan.ExtractMarker(item.Body); ok && found == hash {
				return item, true, nil
			}
		}
		if len(items) < 100 {
			return provider.Issue{}, false, nil
		}
	}
}

func milestoneByID(p *plan.Plan, id string) plan.Milestone {
	for _, item := range p.Milestones {
		if item.ID == id {
			return item
		}
	}
	return plan.Milestone{}
}
func epicByID(p *plan.Plan, id string) plan.Epic {
	for _, item := range p.Epics {
		if item.ID == id {
			return item
		}
	}
	return plan.Epic{}
}
func issueByID(p *plan.Plan, id string) plan.Issue {
	for _, item := range p.Issues {
		if item.ID == id {
			return item
		}
	}
	return plan.Issue{}
}
