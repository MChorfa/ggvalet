package plan

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"

	"gopkg.in/yaml.v3"
)

func ParseFile(path string) (*Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	return Parse(data)
}

func Parse(data []byte) (*Plan, error) {
	var p Plan
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	if err := normalize(&p); err != nil {
		return nil, err
	}
	if err := Validate(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

func ParseJSON(data []byte) (*Plan, error) {
	var p Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse stored plan: %w", err)
	}
	if err := normalize(&p); err != nil {
		return nil, err
	}
	if err := Validate(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

func Validate(p *Plan) error {
	if p.Version != "1" && p.Version != "2" {
		return fmt.Errorf("plan: unsupported version %q (want \"1\" or \"2\")", p.Version)
	}
	if p.Target.Provider != "gitlab" && p.Target.Provider != "github" {
		return fmt.Errorf("plan: target.provider must be gitlab or github (got %q)", p.Target.Provider)
	}
	milestoneIDs, err := validateMilestones(p.Milestones)
	if err != nil {
		return err
	}
	epicIDs, err := validateEpics(p.Epics)
	if err != nil {
		return err
	}
	issueIDs, err := validateIssues(p.Issues)
	if err != nil {
		return err
	}
	if err := validateRefs(p, milestoneIDs, epicIDs, issueIDs); err != nil {
		return err
	}
	_, err = OrderedResources(p)
	return err
}

func normalize(p *Plan) error {
	for _, epic := range p.Epics {
		for _, child := range epic.Children {
			for i := range p.Issues {
				if p.Issues[i].ID != child {
					continue
				}
				if p.Issues[i].Epic != "" && p.Issues[i].Epic != epic.ID {
					return fmt.Errorf("plan: issue %q belongs to conflicting epics %q and %q", child, p.Issues[i].Epic, epic.ID)
				}
				p.Issues[i].Epic = epic.ID
			}
		}
	}
	return nil
}

func validateMilestones(ms []Milestone) (map[string]bool, error) {
	ids := make(map[string]bool)
	for _, m := range ms {
		if m.ID == "" {
			return nil, fmt.Errorf("plan: milestone missing id")
		}
		if m.Title == "" {
			return nil, fmt.Errorf("plan: milestone %q missing title", m.ID)
		}
		if ids[m.ID] {
			return nil, fmt.Errorf("plan: duplicate milestone id %q", m.ID)
		}
		ids[m.ID] = true
	}
	return ids, nil
}

func validateEpics(es []Epic) (map[string]bool, error) {
	ids := make(map[string]bool)
	for _, e := range es {
		if e.ID == "" {
			return nil, fmt.Errorf("plan: epic missing id")
		}
		if e.Title == "" {
			return nil, fmt.Errorf("plan: epic %q missing title", e.ID)
		}
		if ids[e.ID] {
			return nil, fmt.Errorf("plan: duplicate epic id %q", e.ID)
		}
		ids[e.ID] = true
	}
	return ids, nil
}

func validateIssues(is []Issue) (map[string]bool, error) {
	ids := make(map[string]bool)
	for _, i := range is {
		if i.ID == "" {
			return nil, fmt.Errorf("plan: issue missing id")
		}
		if i.Title == "" {
			return nil, fmt.Errorf("plan: issue %q missing title", i.ID)
		}
		if ids[i.ID] {
			return nil, fmt.Errorf("plan: duplicate issue id %q", i.ID)
		}
		ids[i.ID] = true
	}
	return ids, nil
}

func validateRefs(p *Plan, milestoneIDs, epicIDs, issueIDs map[string]bool) error {
	for _, i := range p.Issues {
		if i.Milestone != "" && !milestoneIDs[i.Milestone] {
			return fmt.Errorf("plan: issue %q references unknown milestone %q", i.ID, i.Milestone)
		}
		if i.Epic != "" && !epicIDs[i.Epic] {
			return fmt.Errorf("plan: issue %q references unknown epic %q", i.ID, i.Epic)
		}
	}
	for _, e := range p.Epics {
		for _, child := range e.Children {
			if !issueIDs[child] {
				return fmt.Errorf("plan: epic %q references unknown child issue %q", e.ID, child)
			}
		}
	}
	return nil
}

func OrderedResources(p *Plan) ([]Resource, error) {
	resources, lookup, err := buildResources(p)
	if err != nil {
		return nil, err
	}
	byStep, err := resolveDependencies(resources, lookup)
	if err != nil {
		return nil, err
	}
	order := &dependencyOrder{resources: byStep, visiting: map[string]bool{}, visited: map[string]bool{}}
	for _, resource := range resources {
		if err := order.visit(resource.StepID); err != nil {
			return nil, err
		}
	}
	return order.ordered, nil
}

func buildResources(p *Plan) ([]Resource, map[string]string, error) {
	var resources []Resource
	lookup := map[string]string{}
	add := func(kind, id string, deps []string) error {
		if prior, exists := lookup[id]; exists && p.Version == "2" {
			return fmt.Errorf("plan: v2 resource id %q is shared by %s and %s", id, prior, kind)
		}
		lookup[id] = kind + ":" + id
		resources = append(resources, Resource{StepID: kind + ":" + id, ID: id, Kind: kind, DependsOn: slices.Clone(deps)})
		return nil
	}
	for _, m := range p.Milestones {
		if err := add("milestone", m.ID, m.DependsOn); err != nil {
			return nil, nil, err
		}
	}
	for _, e := range p.Epics {
		if err := add("epic", e.ID, e.DependsOn); err != nil {
			return nil, nil, err
		}
	}
	for _, i := range p.Issues {
		deps := slices.Clone(i.DependsOn)
		if i.Milestone != "" {
			deps = append(deps, "milestone:"+i.Milestone)
		}
		if i.Epic != "" {
			deps = append(deps, "epic:"+i.Epic)
		}
		if err := add("issue", i.ID, deps); err != nil {
			return nil, nil, err
		}
	}
	return resources, lookup, nil
}

func resolveDependencies(resources []Resource, lookup map[string]string) (map[string]Resource, error) {
	byStep := make(map[string]Resource, len(resources))
	for i := range resources {
		resolved := make([]string, 0, len(resources[i].DependsOn))
		for _, dep := range resources[i].DependsOn {
			step := dep
			if !slices.Contains([]string{"milestone", "epic", "issue"}, prefix(dep)) {
				step = lookup[dep]
			}
			if step == "" {
				return nil, fmt.Errorf("plan: resource %q depends on unknown resource %q", resources[i].ID, dep)
			}
			if !slices.Contains(resolved, step) {
				resolved = append(resolved, step)
			}
		}
		resources[i].DependsOn = resolved
		byStep[resources[i].StepID] = resources[i]
	}
	return byStep, nil
}

type dependencyOrder struct {
	resources         map[string]Resource
	visiting, visited map[string]bool
	ordered           []Resource
}

func (o *dependencyOrder) visit(step string) error {
	if o.visiting[step] {
		return fmt.Errorf("plan: dependency cycle includes %q", step)
	}
	if o.visited[step] {
		return nil
	}
	resource, ok := o.resources[step]
	if !ok {
		return fmt.Errorf("plan: unknown dependency step %q", step)
	}
	o.visiting[step] = true
	for _, dep := range resource.DependsOn {
		if err := o.visit(dep); err != nil {
			return err
		}
	}
	o.visiting[step], o.visited[step] = false, true
	o.ordered = append(o.ordered, resource)
	return nil
}

func prefix(value string) string {
	for i := range value {
		if value[i] == ':' {
			return value[:i]
		}
	}
	return ""
}
