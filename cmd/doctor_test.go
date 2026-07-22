package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// silenceOutput redirects os.Stdout and os.Stderr to /dev/null for the test
// and restores them on cleanup. runDoctor is verbose; we only care about its
// returned error.
func silenceOutput(t *testing.T) {
	t.Helper()
	oldErr := os.Stderr
	oldOut := os.Stdout
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	os.Stderr = null
	os.Stdout = null
	t.Cleanup(func() {
		os.Stderr = oldErr
		os.Stdout = oldOut
		_ = null.Close()
	})
}

// writeGlabConfig writes a minimal glab config file for doctor tests and
// returns its path.
func writeGlabConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

func TestDoctor_GitLabHappyPath(t *testing.T) {
	silenceOutput(t)
	// httptest server that replies to the GitLab cross-project issues endpoint
	// used by probeProvider.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v4/issues" {
			t.Errorf("expected path /api/v4/issues, got %s", r.URL.Path)
		}
		scope := r.URL.Query().Get("scope")
		if scope != "assigned_to_me" {
			t.Errorf("expected scope=assigned_to_me, got %s", scope)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GLVALET_GLAB_CONFIG", writeGlabConfig(t, fmt.Sprintf(`
host: test.gitlab.local
hosts:
  test.gitlab.local:
    token: glpat-test
    api_protocol: https
    api_host: %s
`, srv.URL)))
	t.Setenv("GLVALET_HOST", "test.gitlab.local")
	t.Setenv("GLVALET_GITLAB_URL", "")
	t.Setenv("GLVALET_TOKEN", "")
	t.Setenv("GLVALET_PROVIDER", "")
	hostFlag = ""

	if err := runDoctor(); err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
}

func TestDoctor_GitHubMissingToken(t *testing.T) {
	silenceOutput(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GLVALET_GLAB_CONFIG", writeGlabConfig(t, "hosts: {}\n"))
	t.Setenv("GLVALET_PROVIDER", "github")
	t.Setenv("GLVALET_GITHUB_TOKEN", "")
	t.Setenv("GLVALET_TOKEN", "")
	t.Setenv("GLVALET_GITHUB_ENABLED", "")
	hostFlag = ""

	err := runDoctor()
	if err == nil {
		t.Fatal("runDoctor: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "config load failed") {
		t.Errorf("error = %q, expected config load failed", err.Error())
	}
}

func TestDoctor_GitHubProbeFails(t *testing.T) {
	silenceOutput(t)
	// GitHub provider builds with the fake token; the probe call returns 401.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GLVALET_GLAB_CONFIG", writeGlabConfig(t, "hosts: {}\n"))
	t.Setenv("GLVALET_PROVIDER", "github")
	t.Setenv("GLVALET_GITHUB_ENABLED", "true")
	t.Setenv("GLVALET_GITHUB_TOKEN", "ghp-fake-token")
	t.Setenv("GLVALET_TOKEN", "")
	hostFlag = ""

	err := runDoctor()
	if err == nil {
		t.Fatal("runDoctor: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "diagnostics failed") {
		t.Errorf("error = %q, expected diagnostics failed", err.Error())
	}
}
