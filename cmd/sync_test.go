package cmd

import (
	"testing"
)

func TestSyncPureHelpers(t *testing.T) {
	srcURL := "https://gitlab.example.com/group/project/-/issues/1"
	footer := syncFooter(srcURL)

	if footer == "" {
		t.Fatal("syncFooter returned empty")
	}
	if extractSyncSrc(footer) != srcURL {
		t.Fatalf("extractSyncSrc did not recover URL: got %q", extractSyncSrc(footer))
	}
	if extractSyncSrc("no marker here") != "" {
		t.Fatal("extractSyncSrc should return empty without marker")
	}
}

func TestSyncIssuesDryRun(t *testing.T) {
	setupTestClientWithServer(t)
	if err := runCmd(t, syncCmd(), "issues", "--src-project", "group/project", "--dst-project", "group/project", "--dry-run"); err != nil {
		t.Fatalf("sync issues dry-run: %v", err)
	}
}

func TestSyncEpicsDryRun(t *testing.T) {
	setupTestClientWithServer(t)
	if err := runCmd(t, syncCmd(), "epics", "--src-group", "group", "--dst-group", "group", "--dry-run"); err != nil {
		t.Fatalf("sync epics dry-run: %v", err)
	}
}

func TestSyncMilestonesDryRun(t *testing.T) {
	setupTestClientWithServer(t)
	if err := runCmd(t, syncCmd(), "milestones", "--src-project", "group/project", "--dst-project", "group/project", "--dry-run"); err != nil {
		t.Fatalf("sync milestones dry-run: %v", err)
	}
}

func TestSyncIssuesApply(t *testing.T) {
	setupTestClientWithServer(t)
	if err := runCmd(t, syncCmd(), "issues", "--src-project", "group/project", "--dst-project", "other/project", "--limit", "1"); err != nil {
		t.Fatalf("sync issues apply: %v", err)
	}
}

func TestSyncEpicsApply(t *testing.T) {
	setupTestClientWithServer(t)
	if err := runCmd(t, syncCmd(), "epics", "--src-group", "group", "--dst-group", "other", "--limit", "1"); err != nil {
		t.Fatalf("sync epics apply: %v", err)
	}
}

func TestSyncMilestonesApply(t *testing.T) {
	setupTestClientWithServer(t)
	if err := runCmd(t, syncCmd(), "milestones", "--src-project", "group/project", "--dst-project", "other/project", "--limit", "1"); err != nil {
		t.Fatalf("sync milestones apply: %v", err)
	}
}
