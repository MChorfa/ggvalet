package cmd

import (
	"os"
	"testing"
)

func TestExecuteVersion(t *testing.T) {
	os.Args = []string{"glv", "--version"}
	Execute("test")
}

func TestLoadHostsForDisplay(t *testing.T) {
	t.Setenv("GLVALET_TOKEN", "test-token")
	t.Setenv("GLVALET_GITLAB_URL", "https://gitlab.example.com")
	hosts, defaultHost, err := loadHostsForDisplay()
	if err != nil {
		t.Fatalf("loadHostsForDisplay: %v", err)
	}
	if len(hosts) == 0 {
		t.Fatal("expected at least one host")
	}
	if defaultHost == "" {
		t.Fatal("expected default host")
	}
}
