package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ckodex/gitlabvalet/internal/journal"
)

func TestEpicCommands(t *testing.T) {
	setupTestClientWithServer(t)
	cases := []struct {
		name string
		args []string
	}{
		{"list", []string{"list", "-g", "group"}},
		{"create", []string{"create", "-g", "group", "-t", "New epic", "--start-date", "2026-01-01", "--due-date", "2026-12-31"}},
		{"update", []string{"update", "-g", "group", "--iid", "1", "-t", "Updated epic"}},
		{"close", []string{"close", "-g", "group", "--iid", "1"}},
		{"issues", []string{"issues", "-g", "group", "--iid", "1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runCmd(t, epicCmd(), tc.args...); err != nil {
				t.Fatalf("epic %s: %v", tc.name, err)
			}
		})
	}
}

func TestMilestoneCommands(t *testing.T) {
	setupTestClientWithServer(t)
	cases := []struct {
		name string
		args []string
	}{
		{"list", []string{"list", "-p", "group/project"}},
		{"create", []string{"create", "-p", "group/project", "-t", "v2.0", "--start-date", "2026-01-01", "--due-date", "2026-12-31"}},
		{"close", []string{"close", "-p", "group/project", "--id", "1"}},
		{"stats", []string{"stats", "-p", "group/project", "--id", "1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runCmd(t, milestoneCmd(), tc.args...); err != nil {
				t.Fatalf("milestone %s: %v", tc.name, err)
			}
		})
	}
}

func TestSearchCommands(t *testing.T) {
	setupTestClientWithServer(t)
	cases := []struct {
		name string
		args []string
	}{
		{"issues", []string{"query", "--scope", "issues", "--project", "group/project"}},
		{"issues-no-project", []string{"query", "--scope", "issues"}},
		{"merge-requests", []string{"query", "--scope", "merge_requests"}},
		{"milestones", []string{"query", "--scope", "milestones"}},
		{"all-hosts", []string{"query", "--all-hosts"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runCmd(t, searchCmd(), tc.args...); err != nil {
				t.Fatalf("search %s: %v", tc.name, err)
			}
		})
	}
}

func TestStandupCommands(t *testing.T) {
	setupTestClientWithServer(t)
	addJournalEntry(t, journal.OpCreate, journal.EntityIssue, "standup issue")

	t.Run("stdout", func(t *testing.T) {
		if err := runCmd(t, standupCmd()); err != nil {
			t.Fatalf("standup stdout: %v", err)
		}
	})

	t.Run("output-file", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "standup.md")
		if err := runCmd(t, standupCmd(), "--output", out); err != nil {
			t.Fatalf("standup output file: %v", err)
		}
		if _, err := os.Stat(out); err != nil {
			t.Fatalf("standup file not created: %v", err)
		}
	})

	t.Run("push-issue", func(t *testing.T) {
		if err := runCmd(t, standupCmd(), "--push-issue", "--dst-project", "group/project"); err != nil {
			t.Fatalf("standup push issue: %v", err)
		}
	})

	t.Run("slack", func(t *testing.T) {
		if err := runCmd(t, standupCmd(), "--slack", cfg.GitLabURL); err != nil {
			t.Fatalf("standup slack: %v", err)
		}
	})

	t.Run("teams", func(t *testing.T) {
		if err := runCmd(t, standupCmd(), "--teams", cfg.GitLabURL); err != nil {
			t.Fatalf("standup teams: %v", err)
		}
	})
}

func TestTimelineCommand(t *testing.T) {
	setupTestClientWithServer(t)
	if err := runCmd(t, timelineCmd(), "--project", "group/project", "--group", "group", "--since", "2026-01-01", "--until", "2026-12-31"); err != nil {
		t.Fatalf("timeline: %v", err)
	}
}

func TestShieldsCommands(t *testing.T) {
	setupTestClientWithServer(t)
	t.Run("badge", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "badges.md")
		if err := runCmd(t, shieldsCmd(), "badge", "--project", "group/project", "--output", out); err != nil {
			t.Fatalf("shields badge: %v", err)
		}
		if _, err := os.Stat(out); err != nil {
			t.Fatalf("badge output file not created: %v", err)
		}
	})

	t.Run("badge-endpoint-json", func(t *testing.T) {
		if err := runCmd(t, shieldsCmd(), "badge", "--project", "group/project", "--endpoint-json"); err != nil {
			t.Fatalf("shields badge endpoint-json: %v", err)
		}
	})

	t.Run("chips-all", func(t *testing.T) {
		if err := runCmd(t, shieldsCmd(), "chips", "--project", "group/project"); err != nil {
			t.Fatalf("shields chips all: %v", err)
		}
	})

	t.Run("chips-issue", func(t *testing.T) {
		if err := runCmd(t, shieldsCmd(), "chips", "--project", "group/project", "--iid", "1"); err != nil {
			t.Fatalf("shields chips issue: %v", err)
		}
	})
}

func TestRenovateCommands(t *testing.T) {
	setupTestClientWithServer(t)
	t.Run("list", func(t *testing.T) {
		if err := runCmd(t, renovateCmd(), "list", "--project", "group/project"); err != nil {
			t.Fatalf("renovate list: %v", err)
		}
	})

	t.Run("list-all", func(t *testing.T) {
		if err := runCmd(t, renovateCmd(), "list", "--all"); err != nil {
			t.Fatalf("renovate list all: %v", err)
		}
	})

	t.Run("approve-dry-run", func(t *testing.T) {
		if err := runCmd(t, renovateCmd(), "approve", "--project", "group/project", "--dry-run"); err != nil {
			t.Fatalf("renovate approve dry-run: %v", err)
		}
	})

	t.Run("approve-bump-all", func(t *testing.T) {
		if err := runCmd(t, renovateCmd(), "approve", "--project", "group/project", "--bump", "all"); err != nil {
			t.Fatalf("renovate approve all: %v", err)
		}
	})

	t.Run("merge-dry-run", func(t *testing.T) {
		if err := runCmd(t, renovateCmd(), "merge", "--project", "group/project", "--bump", "all", "--dry-run"); err != nil {
			t.Fatalf("renovate merge dry-run: %v", err)
		}
	})

	t.Run("stats", func(t *testing.T) {
		if err := runCmd(t, renovateCmd(), "stats", "--project", "group/project"); err != nil {
			t.Fatalf("renovate stats: %v", err)
		}
	})
}

func TestWorkItemCommands(t *testing.T) {
	setupTestClientWithServer(t)
	t.Run("list", func(t *testing.T) {
		if err := runCmd(t, workItemCmd(), "list", "--project", "group/project"); err != nil {
			t.Fatalf("workitem list: %v", err)
		}
	})

	t.Run("create", func(t *testing.T) {
		if err := runCmd(t, workItemCmd(), "create", "--project", "group/project", "--title", "New task", "--type", "task"); err != nil {
			t.Fatalf("workitem create: %v", err)
		}
	})

	t.Run("close", func(t *testing.T) {
		if err := runCmd(t, workItemCmd(), "close", "--project", "group/project", "--id", "1"); err != nil {
			t.Fatalf("workitem close: %v", err)
		}
	})
}

func TestReportPushCommand(t *testing.T) {
	setupTestClientWithServer(t)
	addJournalEntry(t, journal.OpCreate, journal.EntityIssue, "report push issue")

	if err := runCmd(t, reportCmd(), "push", "--dst-project", "group/project", "--since", "7d"); err != nil {
		t.Fatalf("report push: %v", err)
	}
}
