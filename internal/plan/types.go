// Package plan defines types for structured work plans.
package plan

type Plan struct {
	Version    string      `yaml:"version"`
	Target     Target      `yaml:"target"`
	Milestones []Milestone `yaml:"milestones"`
	Epics      []Epic      `yaml:"epics"`
	Issues     []Issue     `yaml:"issues"`
}

type Target struct {
	Provider  string `yaml:"provider"`
	GroupID   int    `yaml:"group_id"`
	ProjectID int    `yaml:"project_id,omitempty"`
}

type Milestone struct {
	ID          string   `yaml:"id"`
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	StartDate   string   `yaml:"start_date,omitempty"`
	DueDate     string   `yaml:"due_date,omitempty"`
	State       string   `yaml:"state"`
	DependsOn   []string `yaml:"depends_on,omitempty"`
}

type Epic struct {
	ID          string   `yaml:"id"`
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Labels      []string `yaml:"labels"`
	Children    []string `yaml:"children"`
	DependsOn   []string `yaml:"depends_on,omitempty"`
}

type Issue struct {
	ID          string   `yaml:"id"`
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Labels      []string `yaml:"labels"`
	Milestone   string   `yaml:"milestone"`
	Epic        string   `yaml:"epic"`
	Weight      int      `yaml:"weight,omitempty"`
	DependsOn   []string `yaml:"depends_on,omitempty"`
}

type Resource struct {
	StepID, ID, Kind string
	DependsOn        []string
}
