package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ckodex/gitlabvalet/internal/journal"
	"github.com/ckodex/gitlabvalet/internal/plan"
	"github.com/ckodex/gitlabvalet/internal/provider"
	"github.com/ckodex/gitlabvalet/internal/reconcile"
	"github.com/spf13/cobra"
)

func runCmd(t *testing.T, root *cobra.Command, args ...string) error {
	t.Helper()
	root.SetArgs(args)
	return root.Execute()
}

func TestIssueCommands(t *testing.T) {
	setupTestClient(t)
	cases := []struct {
		name string
		args []string
	}{
		{"list", []string{"list", "-p", "group/project"}},
		{"list-with-labels", []string{"list", "-p", "group/project", "--labels", "bug"}},
		{"mine", []string{"mine", "--state", "opened"}},
		{"create", []string{"create", "-p", "group/project", "-t", "title", "--assignee", "alice"}},
		{"update", []string{"update", "-p", "group/project", "--iid", "1", "-t", "new"}},
		{"close", []string{"close", "-p", "group/project", "--iid", "1"}},
		{"comment", []string{"comment", "-p", "group/project", "--iid", "1", "-b", "body"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runCmd(t, issueCmd(), tc.args...); err != nil {
				t.Fatalf("issue %s: %v", tc.name, err)
			}
		})
	}
}

func TestMRMergeRequestCommands(t *testing.T) {
	setupTestClient(t)
	cases := []struct {
		name string
		args []string
	}{
		{"list", []string{"list", "-p", "group/project"}},
		{"list-no-cache", []string{"list", "-p", "group/project", "--no-cache"}},
		{"mine", []string{"mine", "--state", "opened"}},
		{"create", []string{"create", "-p", "group/project", "-t", "title", "--source", "feature"}},
		{"approve", []string{"approve", "-p", "group/project", "--iid", "1"}},
		{"merge", []string{"merge", "-p", "group/project", "--iid", "1"}},
		{"close", []string{"close", "-p", "group/project", "--iid", "1"}},
		{"diff", []string{"diff", "-p", "group/project", "--iid", "1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runCmd(t, mrCmd(), tc.args...); err != nil {
				t.Fatalf("mr %s: %v", tc.name, err)
			}
		})
	}
}

func TestLabelCommands(t *testing.T) {
	setupTestClient(t)
	cases := []struct {
		name string
		args []string
	}{
		{"list", []string{"list", "-p", "group/project"}},
		{"create", []string{"create", "-p", "group/project", "-n", "bug", "-c", "#FF0000"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runCmd(t, labelCmd(), tc.args...); err != nil {
				t.Fatalf("label %s: %v", tc.name, err)
			}
		})
	}
}

func TestJournalCommands(t *testing.T) {
	setupTestClient(t)
	addJournalEntry(t, journal.OpCreate, journal.EntityIssue, "recent issue")
	if err := glClient.RecErr(journal.OpCreate, journal.EntityIssue, "group/project", "", "boom"); err != nil {
		t.Fatalf("rec err: %v", err)
	}
	addOldJournalEntry(t)

	cases := []struct {
		name string
		args []string
	}{
		{"show", []string{"show"}},
		{"show-entity", []string{"show", "--entity", "issue"}},
		{"show-errors", []string{"show", "--errors"}},
		{"show-project", []string{"show", "--project", "group/project"}},
		{"stats", []string{"stats", "--since", "7d"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runCmd(t, journalCmd(), tc.args...); err != nil {
				t.Fatalf("journal %s: %v", tc.name, err)
			}
		})
	}
}

func TestCacheCommands(t *testing.T) {
	setupTestClient(t)
	glClient.Cache.Set("key", []byte("value"), 5*time.Minute)
	cases := []struct {
		name string
		args []string
	}{
		{"stats", []string{"stats"}},
		{"flush", []string{"flush"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runCmd(t, cacheCmd(), tc.args...); err != nil {
				t.Fatalf("cache %s: %v", tc.name, err)
			}
		})
	}
}

func TestReceiptCommands(t *testing.T) {
	setupTestClient(t)
	if _, err := glClient.Provider.ListIssues(context.Background(), "group/project", provider.ListIssuesOptions{}); err != nil {
		t.Fatalf("list issues: %v", err)
	}

	t.Run("export", func(t *testing.T) {
		if err := runCmd(t, receiptCmd(), "export"); err != nil {
			t.Fatalf("receipt export: %v", err)
		}
	})

	t.Run("export-to-file", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "receipts.jsonl")
		if err := runCmd(t, receiptCmd(), "export", "-o", out); err != nil {
			t.Fatalf("receipt export file: %v", err)
		}
		if _, err := os.Stat(out); err != nil {
			t.Fatalf("output file not created: %v", err)
		}
	})
}

func TestReportCommands(t *testing.T) {
	setupTestClient(t)
	addJournalEntry(t, journal.OpCreate, journal.EntityIssue, "report issue")

	cases := []struct {
		name string
		args []string
	}{
		{"markdown", []string{"--since", "7d"}},
		{"plain", []string{"--since", "7d", "--format", "plain"}},
		{"author", []string{"--since", "7d", "--author", "Alice"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runCmd(t, reportCmd(), tc.args...); err != nil {
				t.Fatalf("report %s: %v", tc.name, err)
			}
		})
	}

	t.Run("output-to-file", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "report.md")
		if err := runCmd(t, reportCmd(), "--since", "7d", "-o", out); err != nil {
			t.Fatalf("report output: %v", err)
		}
		if _, err := os.Stat(out); err != nil {
			t.Fatalf("output file not created: %v", err)
		}
	})
}

func TestHostsCommand(t *testing.T) {
	t.Setenv("GLVALET_TOKEN", "test-token")
	t.Setenv("GLVALET_GITLAB_URL", "https://gitlab.example.com")
	if err := runCmd(t, hostsCmd()); err != nil {
		t.Fatalf("hosts: %v", err)
	}
}

func TestPlanCommands(t *testing.T) {
	setupTestClient(t)
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.yaml")
	planYAML := `version: "1"
target:
  provider: gitlab
  group_id: 1
  project_id: 1
milestones:
  - id: m1
    title: v1.0
    due_date: "2026-07-01"
epics:
  - id: e1
    title: Epic One
issues:
  - id: i1
    title: Issue One
    milestone: m1
    epic: e1
`
	if err := os.WriteFile(planPath, []byte(planYAML), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	t.Run("validate", func(t *testing.T) {
		if err := runCmd(t, planCmd(), "validate", planPath); err != nil {
			t.Fatalf("plan validate: %v", err)
		}
	})

	t.Run("dry-run", func(t *testing.T) {
		if err := runCmd(t, planCmd(), "apply", "--dry-run", planPath); err != nil {
			t.Fatalf("plan apply dry-run: %v", err)
		}
	})

	t.Run("diff", func(t *testing.T) {
		if err := runCmd(t, planCmd(), "diff", planPath); err != nil {
			t.Fatalf("plan diff: %v", err)
		}
	})

	t.Run("apply", func(t *testing.T) {
		if err := runCmd(t, planCmd(), "apply", "--dry-run=false", "--yes", planPath); err != nil {
			t.Fatalf("plan apply: %v", err)
		}
	})

	t.Run("status", func(t *testing.T) {
		ctx := context.Background()
		p, err := plan.ParseFile(planPath)
		if err != nil {
			t.Fatalf("parse plan: %v", err)
		}
		engine := reconcile.Engine{Provider: glClient.Provider, Store: glClient.State}
		runID, err := engine.Start(ctx, p)
		if err != nil {
			t.Fatalf("engine start: %v", err)
		}
		if err := engine.Execute(ctx, runID); err != nil {
			t.Fatalf("engine execute: %v", err)
		}
		if err := runCmd(t, planCmd(), "status", runID); err != nil {
			t.Fatalf("plan status: %v", err)
		}
		if err := runCmd(t, planCmd(), "explain", runID); err != nil {
			t.Fatalf("plan explain: %v", err)
		}
		if err := runCmd(t, planCmd(), "resume", runID); err != nil {
			t.Fatalf("plan resume: %v", err)
		}
	})
}
