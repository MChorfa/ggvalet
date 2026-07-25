package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCI_List(t *testing.T) {
	setupTestClient(t)
	if err := runCmd(t, ciCmd(), "list", "-p", "group/project"); err != nil {
		t.Fatalf("ci list: %v", err)
	}
}

func TestCI_List_MissingProject(t *testing.T) {
	setupTestClient(t)
	// Temporarily clear default project to exercise the required-flag path.
	orig := cfg.DefaultProject
	cfg.DefaultProject = ""
	t.Cleanup(func() { cfg.DefaultProject = orig })
	if err := runCmd(t, ciCmd(), "list"); err == nil {
		t.Fatal("ci list without --project should error")
	}
}

func TestCI_View(t *testing.T) {
	setupTestClient(t)
	if err := runCmd(t, ciCmd(), "view", "-p", "group/project", "--id", "1"); err != nil {
		t.Fatalf("ci view: %v", err)
	}
}

func TestCI_View_MissingID(t *testing.T) {
	setupTestClient(t)
	if err := runCmd(t, ciCmd(), "view", "-p", "group/project"); err == nil {
		t.Fatal("ci view without --id should error")
	}
}

func TestCI_Run(t *testing.T) {
	setupTestClient(t)
	if err := runCmd(t, ciCmd(), "run", "-p", "group/project", "--ref", "main"); err != nil {
		t.Fatalf("ci run: %v", err)
	}
}

func TestCI_Run_WithVars(t *testing.T) {
	setupTestClient(t)
	if err := runCmd(t, ciCmd(), "run", "-p", "group/project", "--ref", "main",
		"--var", "FOO=bar", "--var", "BAZ=qux"); err != nil {
		t.Fatalf("ci run with vars: %v", err)
	}
}

func TestCI_Run_MissingRef(t *testing.T) {
	setupTestClient(t)
	if err := runCmd(t, ciCmd(), "run", "-p", "group/project"); err == nil {
		t.Fatal("ci run without --ref should error")
	}
}

func TestCI_Run_InvalidVar(t *testing.T) {
	setupTestClient(t)
	if err := runCmd(t, ciCmd(), "run", "-p", "group/project", "--ref", "main",
		"--var", "no-equals"); err == nil {
		t.Fatal("ci run with invalid --var should error")
	}
}

func TestCI_Retry(t *testing.T) {
	setupTestClient(t)
	if err := runCmd(t, ciCmd(), "retry", "-p", "group/project", "--id", "1"); err != nil {
		t.Fatalf("ci retry: %v", err)
	}
}

func TestCI_Cancel(t *testing.T) {
	setupTestClient(t)
	if err := runCmd(t, ciCmd(), "cancel", "-p", "group/project", "--id", "1"); err != nil {
		t.Fatalf("ci cancel: %v", err)
	}
}

func TestCI_Jobs(t *testing.T) {
	setupTestClient(t)
	if err := runCmd(t, ciCmd(), "jobs", "-p", "group/project", "--id", "1"); err != nil {
		t.Fatalf("ci jobs: %v", err)
	}
}

func TestCI_Logs(t *testing.T) {
	setupTestClient(t)
	if err := runCmd(t, ciCmd(), "logs", "-p", "group/project", "--job", "11"); err != nil {
		t.Fatalf("ci logs: %v", err)
	}
}

func TestCI_Artifacts(t *testing.T) {
	setupTestClient(t)
	if err := runCmd(t, ciCmd(), "artifacts", "-p", "group/project", "--id", "1"); err != nil {
		t.Fatalf("ci artifacts: %v", err)
	}
}

func TestCI_Download(t *testing.T) {
	setupTestClient(t)
	dest := filepath.Join(t.TempDir(), "out")
	if err := runCmd(t, ciCmd(), "download", "-p", "group/project", "--job", "11",
		"--dest", dest); err != nil {
		t.Fatalf("ci download: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "artifacts.zip")); err != nil {
		t.Fatalf("artifacts.zip not written: %v", err)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{2048, "2.0 KiB"},
		{1048576, "1.0 MiB"},
	}
	for _, tc := range cases {
		if got := humanBytes(tc.in); got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
