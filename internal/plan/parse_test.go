package plan

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestParseFile_Valid(t *testing.T) {
	yaml := `version: "1"
target:
  provider: gitlab
  group_id: 159792
milestones:
  - id: m-w7
    title: "Week 7"
    description: "First week"
    start_date: "2026-10-08"
    due_date: "2026-10-14"
    state: active
epics:
  - id: e-trusted
    title: "Trusted Images"
    description: "Epic description"
    labels: [label1, label2]
    children: [i-1]
issues:
  - id: i-1
    title: "Issue 1"
    description: "Issue description"
    labels: [label1]
    milestone: m-w7
    epic: e-trusted
    weight: 3
`
	file := writeTemp(t, yaml)
	defer os.Remove(file)

	p, err := ParseFile(file)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if p.Version != "1" {
		t.Errorf("Version: got %q, want \"1\"", p.Version)
	}
	if p.Target.Provider != "gitlab" {
		t.Errorf("Target.Provider: got %q, want gitlab", p.Target.Provider)
	}
	if p.Target.GroupID != 159792 {
		t.Errorf("Target.GroupID: got %d, want 159792", p.Target.GroupID)
	}
	if len(p.Milestones) != 1 {
		t.Errorf("Milestones: got %d, want 1", len(p.Milestones))
	}
	if p.Milestones[0].ID != "m-w7" {
		t.Errorf("Milestone ID: got %q, want m-w7", p.Milestones[0].ID)
	}
	if p.Milestones[0].Title != "Week 7" {
		t.Errorf("Milestone Title: got %q, want Week 7", p.Milestones[0].Title)
	}
	if len(p.Epics) != 1 {
		t.Errorf("Epics: got %d, want 1", len(p.Epics))
	}
	if p.Epics[0].ID != "e-trusted" {
		t.Errorf("Epic ID: got %q, want e-trusted", p.Epics[0].ID)
	}
	if len(p.Issues) != 1 {
		t.Errorf("Issues: got %d, want 1", len(p.Issues))
	}
	if p.Issues[0].ID != "i-1" {
		t.Errorf("Issue ID: got %q, want i-1", p.Issues[0].ID)
	}
	if p.Issues[0].Milestone != "m-w7" {
		t.Errorf("Issue Milestone: got %q, want m-w7", p.Issues[0].Milestone)
	}
	if p.Issues[0].Epic != "e-trusted" {
		t.Errorf("Issue Epic: got %q, want e-trusted", p.Issues[0].Epic)
	}
	if p.Issues[0].Weight != 3 {
		t.Errorf("Issue Weight: got %d, want 3", p.Issues[0].Weight)
	}
}

func TestParseFile_UnsupportedVersion(t *testing.T) {
	yaml := `version: "3"
target:
  provider: gitlab
  group_id: 1
`
	file := writeTemp(t, yaml)
	defer os.Remove(file)

	_, err := ParseFile(file)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported version") {
		t.Errorf("Error message: got %q, want to contain unsupported version", err.Error())
	}
}

func TestParse_V2OrdersDependenciesAndNormalizesEpicChildren(t *testing.T) {
	p, err := Parse([]byte(`version: "2"
target: {provider: gitlab, group_id: 1, project_id: 2}
milestones: [{id: m1, title: M1}]
epics: [{id: e1, title: E1, children: [i1]}]
issues: [{id: i1, title: I1, milestone: m1}]
`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Issues[0].Epic != "e1" {
		t.Fatalf("epic = %q, want e1", p.Issues[0].Epic)
	}
	resources, err := OrderedResources(p)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{resources[0].StepID, resources[1].StepID, resources[2].StepID}
	want := []string{"milestone:m1", "epic:e1", "issue:i1"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestParse_V2RejectsDependencyCycle(t *testing.T) {
	_, err := Parse([]byte(`version: "2"
target: {provider: gitlab, group_id: 1}
milestones:
  - {id: a, title: A, depends_on: [b]}
  - {id: b, title: B, depends_on: [a]}
`))
	if err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("error = %v, want dependency cycle", err)
	}
}

func TestParseFile_UnknownProvider(t *testing.T) {
	yaml := `version: "1"
target:
  provider: bitbucket
  group_id: 1
`
	file := writeTemp(t, yaml)
	defer os.Remove(file)

	_, err := ParseFile(file)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "target.provider") {
		t.Errorf("Error message: got %q, want to contain target.provider", err.Error())
	}
}

func TestParseFile_UnknownMilestoneRef(t *testing.T) {
	yaml := `version: "1"
target:
  provider: gitlab
  group_id: 1
milestones:
  - id: m-1
    title: "Milestone 1"
    state: active
issues:
  - id: i-1
    title: "Issue 1"
    milestone: m-nonexistent
`
	file := writeTemp(t, yaml)
	defer os.Remove(file)

	_, err := ParseFile(file)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown milestone") {
		t.Errorf("Error message: got %q, want to contain unknown milestone", err.Error())
	}
}

func TestParseFile_UnknownEpicChild(t *testing.T) {
	yaml := `version: "1"
target:
  provider: gitlab
  group_id: 1
epics:
  - id: e-1
    title: "Epic 1"
    children: [i-nonexistent]
issues:
  - id: i-1
    title: "Issue 1"
`
	file := writeTemp(t, yaml)
	defer os.Remove(file)

	_, err := ParseFile(file)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown child issue") {
		t.Errorf("Error message: got %q, want to contain unknown child issue", err.Error())
	}
}

func TestParseFile_MissingMilestoneTitle(t *testing.T) {
	yaml := `version: "1"
target:
  provider: gitlab
  group_id: 1
milestones:
  - id: m-1
    state: active
`
	file := writeTemp(t, yaml)
	defer os.Remove(file)

	_, err := ParseFile(file)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "missing title") {
		t.Errorf("Error message: got %q, want to contain missing title", err.Error())
	}
}

func TestParseFile_DuplicateMilestoneID(t *testing.T) {
	yaml := `version: "1"
target:
  provider: gitlab
  group_id: 1
milestones:
  - id: m-1
    title: "Milestone 1"
    state: active
  - id: m-1
    title: "Milestone 2"
    state: active
`
	file := writeTemp(t, yaml)
	defer os.Remove(file)

	_, err := ParseFile(file)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate milestone id") {
		t.Errorf("Error message: got %q, want to contain duplicate milestone id", err.Error())
	}
}

func TestParseFile_UnknownEpicRef(t *testing.T) {
	yaml := `version: "1"
target:
  provider: gitlab
  group_id: 1
epics:
  - id: e-1
    title: "Epic 1"
issues:
  - id: i-1
    title: "Issue 1"
    epic: e-nonexistent
`
	file := writeTemp(t, yaml)
	defer os.Remove(file)

	_, err := ParseFile(file)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown epic") {
		t.Errorf("Error message: got %q, want to contain unknown epic", err.Error())
	}
}

func writeTemp(t *testing.T, content string) string {
	f, err := os.CreateTemp(t.TempDir(), "plan-*.yaml")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	return f.Name()
}
