package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MChorfa/ggvalet/internal/config"
)

func TestExecuteVersion(t *testing.T) {
	os.Args = []string{"ggvalet", "--version"}
	Execute("test")
}

func TestLoadHostsForDisplay(t *testing.T) {
	t.Setenv("GLVALET_TOKEN", "test-token")
	t.Setenv("GLVALET_GITLAB_URL", "https://gitlab.example.com")
	hosts, defaultHost, provider, err := loadHostsForDisplay()
	if err != nil {
		t.Fatalf("loadHostsForDisplay: %v", err)
	}
	if len(hosts) == 0 {
		t.Fatal("expected at least one host")
	}
	if defaultHost == "" {
		t.Fatal("expected default host")
	}
	if provider == "" {
		t.Fatal("expected provider to be set")
	}
}

func TestLoadHostsForDisplay_Gitea(t *testing.T) {
	dir := t.TempDir()
	teaCfg := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(teaCfg, []byte(`
logins:
  - name: test-gitea
    url: https://gitea.example.com
    token: gitea-token
    default: true
    user: alice
`), 0o600); err != nil {
		t.Fatalf("write tea config: %v", err)
	}

	t.Setenv("HOME", t.TempDir())
	t.Setenv("GLVALET_PROVIDER", "gitea")
	t.Setenv("GLVALET_TEA_CONFIG", teaCfg)
	t.Setenv("GLVALET_TOKEN", "")
	t.Setenv("GLVALET_GITLAB_URL", "")
	hostFlag = ""

	hosts, defaultHost, provider, err := loadHostsForDisplay()
	if err != nil {
		t.Fatalf("loadHostsForDisplay: %v", err)
	}
	if provider != "gitea" {
		t.Fatalf("provider = %q, want %q", provider, "gitea")
	}
	if len(hosts) == 0 {
		t.Fatal("expected at least one Gitea host")
	}
	if defaultHost == "" {
		t.Fatal("expected default host")
	}
	if _, ok := hosts["test-gitea"]; !ok {
		t.Errorf("hosts map missing 'test-gitea'; keys: %v", hostKeys(hosts))
	}
}

func hostKeys(m map[string]*config.HostConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
