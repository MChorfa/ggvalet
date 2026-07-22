package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── HostConfig.SkipTLS() Tests ─────────────────────────────────────────────

func TestHostConfig_SkipTLS(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"true lowercase", "true", true},
		{"True mixed case", "True", true},
		{"TRUE uppercase", "TRUE", true},
		{"true with leading space", " true", true},
		{"true with trailing space", "true ", true},
		{"true with surrounding spaces", " true ", true},
		{"false lowercase", "false", false},
		{"False mixed case", "False", false},
		{"empty string", "", false},
		{"whitespace only", "   ", false},
		{"yes string", "yes", false},
		{"no string", "no", false},
		{"1 string", "1", false},
		{"truee typo", "truee", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc := &HostConfig{SkipTLSVerify: tt.input}
			got := hc.SkipTLS()
			if got != tt.expected {
				t.Errorf("SkipTLS() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

// ─── HostConfig.APIURL() Tests ─────────────────────────────────────────────

func TestHostConfig_APIURL(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		apiHost  string
		hostname string
		expected string
	}{
		{
			"empty protocol and host, default https",
			"",
			"",
			"gitlab.com",
			"https://gitlab.com",
		},
		{
			"http protocol",
			"http",
			"",
			"local.test",
			"http://local.test",
		},
		{
			"HTTP uppercase protocol",
			"HTTP",
			"",
			"local.test",
			"http://local.test",
		},
		{
			"https protocol",
			"https",
			"",
			"gitlab.example.com",
			"https://gitlab.example.com",
		},
		{
			"HTTPS uppercase protocol",
			"HTTPS",
			"",
			"gitlab.example.com",
			"https://gitlab.example.com",
		},
		{
			"apiHost takes precedence over hostname",
			"https",
			"sc01.example.ca",
			"gitlab.com",
			"https://sc01.example.ca",
		},
		{
			"apiHost with path segment",
			"https",
			"sc01.example.ca/gitlab",
			"ignored-hostname",
			"https://sc01.example.ca/gitlab",
		},
		{
			"apiHost already has http scheme",
			"https",
			"http://already-scheme.com/api",
			"ignored",
			"http://already-scheme.com/api",
		},
		{
			"apiHost already has https scheme",
			"http",
			"https://already-scheme.com/api",
			"ignored",
			"https://already-scheme.com/api",
		},
		{
			"http protocol with apiHost",
			"http",
			"internal.test",
			"external.test",
			"http://internal.test",
		},
		{
			"empty protocol defaults to https with apiHost",
			"",
			"custom.host",
			"hostname",
			"https://custom.host",
		},
		{
			"unknown protocol defaults to https",
			"gopher",
			"",
			"test.com",
			"https://test.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc := &HostConfig{
				APIProtocol: tt.protocol,
				APIHost:     tt.apiHost,
			}
			got := hc.APIURL(tt.hostname)
			if got != tt.expected {
				t.Errorf("APIURL(%q) = %q, expected %q", tt.hostname, got, tt.expected)
			}
		})
	}
}

// ─── Load() Tests ────────────────────────────────────────────────────────────

func TestLoad_HostFlagWins(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	yaml := `
host: env-default
hosts:
  env-default:
    token: env-token
    api_protocol: https
  flag-host:
    token: flag-token
    api_protocol: https
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_HOST", "")
	t.Setenv("GLVALET_GITLAB_URL", "")
	t.Setenv("GLVALET_TOKEN", "")

	opts := Options{HostFlag: "flag-host"}
	cfg, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Host != "flag-host" {
		t.Errorf("Host = %q, expected %q", cfg.Host, "flag-host")
	}
	if cfg.Token != "flag-token" {
		t.Errorf("Token = %q, expected %q", cfg.Token, "flag-token")
	}
}

func TestLoad_EnvVarHostWins(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	yaml := `
host: config-default
hosts:
  config-default:
    token: config-token
    api_protocol: https
  env-host:
    token: env-token
    api_protocol: https
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_HOST", "env-host")
	t.Setenv("GLVALET_GITLAB_URL", "")
	t.Setenv("GLVALET_TOKEN", "")
	t.Setenv("GLVALET_GLAB_CONFIG", configFile)

	opts := Options{HostFlag: ""}
	cfg, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Host != "env-host" {
		t.Errorf("Host = %q, expected %q", cfg.Host, "env-host")
	}
	if cfg.Token != "env-token" {
		t.Errorf("Token = %q, expected %q", cfg.Token, "env-token")
	}
}

func TestLoad_GlabConfigDefaultWins(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	yaml := `
host: glab-default
hosts:
  glab-default:
    token: glab-token
    api_protocol: https
  other-host:
    token: other-token
    api_protocol: https
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_HOST", "")
	t.Setenv("GLVALET_GITLAB_URL", "")
	t.Setenv("GLVALET_TOKEN", "")

	opts := Options{HostFlag: ""}
	cfg, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Host != "glab-default" {
		t.Errorf("Host = %q, expected %q", cfg.Host, "glab-default")
	}
	if cfg.Token != "glab-token" {
		t.Errorf("Token = %q, expected %q", cfg.Token, "glab-token")
	}
}

func TestLoad_GLVALETGitLabURLWins(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	yaml := `
hosts:
  config-host:
    token: config-token
    api_protocol: https
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_HOST", "")
	t.Setenv("GLVALET_GITLAB_URL", "https://gitlab.url/path")
	t.Setenv("GLVALET_TOKEN", "glpat-from-env")
	t.Setenv("HOME", "/nonexistent")
	t.Setenv("XDG_CONFIG_HOME", "")

	opts := Options{HostFlag: ""}
	cfg, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Host != "gitlab.url/path" {
		t.Errorf("Host = %q, expected %q", cfg.Host, "gitlab.url/path")
	}
	if cfg.Token != "glpat-from-env" {
		t.Errorf("Token = %q, expected %q", cfg.Token, "glpat-from-env")
	}
}

func TestLoad_AlphabeticalFallback(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	yaml := `
hosts:
  zebra.example.com:
    token: zebra-token
    api_protocol: https
  alpha.example.com:
    token: alpha-token
    api_protocol: https
  middle.example.com:
    token: middle-token
    api_protocol: https
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_HOST", "")
	t.Setenv("GLVALET_GITLAB_URL", "")
	t.Setenv("GLVALET_TOKEN", "")
	t.Setenv("HOME", "/nonexistent")
	t.Setenv("XDG_CONFIG_HOME", "")

	opts := Options{HostFlag: ""}
	cfg, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Host != "alpha.example.com" {
		t.Errorf("Host = %q, expected %q (alphabetically first)", cfg.Host, "alpha.example.com")
	}
	if cfg.Token != "alpha-token" {
		t.Errorf("Token = %q, expected %q", cfg.Token, "alpha-token")
	}
}

func TestLoad_TokenBackfillFromEnv(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	yaml := `
hosts:
  gitlab.example.com:
    token: ""
    user: alice
    api_protocol: https
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_HOST", "gitlab.example.com")
	t.Setenv("GLVALET_TOKEN", "glpat-backfilled")
	t.Setenv("GLVALET_GITLAB_URL", "")

	opts := Options{HostFlag: ""}
	cfg, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Token != "glpat-backfilled" {
		t.Errorf("Token = %q, expected %q", cfg.Token, "glpat-backfilled")
	}
}

func TestLoad_TokenAndURLCreateHost(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	yaml := `
hosts: {}
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_GITLAB_URL", "https://gitlab.synthesis.local")
	t.Setenv("GLVALET_TOKEN", "glpat-synthetic")
	t.Setenv("GLVALET_HOST", "")
	t.Setenv("HOME", "/nonexistent")
	t.Setenv("XDG_CONFIG_HOME", "")

	opts := Options{HostFlag: ""}
	cfg, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Host != "gitlab.synthesis.local" {
		t.Errorf("Host = %q, expected %q", cfg.Host, "gitlab.synthesis.local")
	}
	if cfg.Token != "glpat-synthetic" {
		t.Errorf("Token = %q, expected %q", cfg.Token, "glpat-synthetic")
	}
}

func TestLoad_NoHostsError(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	yaml := `
hosts: {}
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_GITLAB_URL", "")
	t.Setenv("GLVALET_TOKEN", "")
	t.Setenv("GLVALET_HOST", "")
	t.Setenv("HOME", "/nonexistent")
	t.Setenv("XDG_CONFIG_HOME", "")

	opts := Options{HostFlag: ""}
	_, err := Load(opts)
	if err == nil {
		t.Fatal("Load: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no GitLab hosts found") {
		t.Errorf("error = %q, expected to contain 'no GitLab hosts found'", err.Error())
	}
}

func TestLoad_GitHubOnly_WithToken(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	// No GitLab hosts configured — only GitHub credentials.
	yaml := `
hosts: {}
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_PROVIDER", "github")
	t.Setenv("GLVALET_GITHUB_TOKEN", "ghp-test-token")
	t.Setenv("GLVALET_GITHUB_URL", "")
	t.Setenv("GLVALET_TOKEN", "")
	t.Setenv("GLVALET_GITLAB_URL", "")
	t.Setenv("GLVALET_HOST", "")
	t.Setenv("HOME", "/nonexistent")
	t.Setenv("XDG_CONFIG_HOME", "")

	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "github" {
		t.Errorf("Provider = %q, expected %q", cfg.Provider, "github")
	}
	if cfg.Host != "github.com" {
		t.Errorf("Host = %q, expected %q", cfg.Host, "github.com")
	}
	if cfg.GitHubToken != "ghp-test-token" {
		t.Errorf("GitHubToken = %q, expected %q", cfg.GitHubToken, "ghp-test-token")
	}
	if cfg.Token != "ghp-test-token" {
		t.Errorf("Token = %q, expected %q", cfg.Token, "ghp-test-token")
	}
	if cfg.GitLabURL != "" {
		t.Errorf("GitLabURL = %q, expected empty", cfg.GitLabURL)
	}
}

func TestLoad_GitHubOnly_EnterpriseURL(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	yaml := `
hosts: {}
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_PROVIDER", "github")
	t.Setenv("GLVALET_GITHUB_TOKEN", "ghp-enterprise")
	t.Setenv("GLVALET_GITHUB_URL", "https://github.example.com/api/v3")
	t.Setenv("GLVALET_TOKEN", "")
	t.Setenv("GLVALET_GITLAB_URL", "")
	t.Setenv("HOME", "/nonexistent")
	t.Setenv("XDG_CONFIG_HOME", "")

	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Host != "github.example.com" {
		t.Errorf("Host = %q, expected %q", cfg.Host, "github.example.com")
	}
	if cfg.GitHubURL != "https://github.example.com/api/v3" {
		t.Errorf("GitHubURL = %q, expected %q", cfg.GitHubURL, "https://github.example.com/api/v3")
	}
}

func TestLoad_GitHubOnly_FallbackToGLVALETToken(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	yaml := `
hosts: {}
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_PROVIDER", "github")
	t.Setenv("GLVALET_GITHUB_TOKEN", "")
	t.Setenv("GLVALET_TOKEN", "ghp-fallback")
	t.Setenv("GLVALET_GITLAB_URL", "")
	t.Setenv("HOME", "/nonexistent")
	t.Setenv("XDG_CONFIG_HOME", "")

	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GitHubToken != "ghp-fallback" {
		t.Errorf("GitHubToken = %q, expected %q", cfg.GitHubToken, "ghp-fallback")
	}
}

func TestLoad_GitHubOnly_MissingToken(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	yaml := `
hosts: {}
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_PROVIDER", "github")
	t.Setenv("GLVALET_GITHUB_TOKEN", "")
	t.Setenv("GLVALET_TOKEN", "")
	t.Setenv("GLVALET_GITLAB_URL", "")
	t.Setenv("HOME", "/nonexistent")
	t.Setenv("XDG_CONFIG_HOME", "")

	_, err := Load(Options{})
	if err == nil {
		t.Fatal("Load: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no GitHub token") {
		t.Errorf("error = %q, expected to contain 'no GitHub token'", err.Error())
	}
}

func TestLoad_UnknownHostError(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	yaml := `
hosts:
  known.example.com:
    token: token
    api_protocol: https
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_HOST", "unknown.example.com")
	t.Setenv("GLVALET_GITLAB_URL", "")
	t.Setenv("GLVALET_TOKEN", "")

	opts := Options{HostFlag: ""}
	_, err := Load(opts)
	if err == nil {
		t.Fatal("Load: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown.example.com") {
		t.Errorf("error = %q, expected to contain 'unknown.example.com'", err.Error())
	}
	if !strings.Contains(err.Error(), "known.example.com") {
		t.Errorf("error = %q, expected to list available hosts", err.Error())
	}
}

func TestLoad_NoTokenError(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	yaml := `
hosts:
  gitlab.example.com:
    token: ""
    api_protocol: https
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_HOST", "gitlab.example.com")
	t.Setenv("GLVALET_TOKEN", "")
	t.Setenv("GLVALET_GITLAB_URL", "")

	opts := Options{HostFlag: ""}
	_, err := Load(opts)
	if err == nil {
		t.Fatal("Load: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no token") {
		t.Errorf("error = %q, expected to contain 'no token'", err.Error())
	}
}

func TestLoad_PopulatesHostsMap(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	yaml := `
hosts:
  gitlab.com:
    token: token1
    user: user1
    api_protocol: https
  github.local:
    token: token2
    user: user2
    api_protocol: http
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_HOST", "gitlab.com")
	t.Setenv("GLVALET_GITLAB_URL", "")
	t.Setenv("GLVALET_TOKEN", "")
	t.Setenv("HOME", "/nonexistent")
	t.Setenv("XDG_CONFIG_HOME", "")

	opts := Options{HostFlag: ""}
	cfg, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Hosts) != 2 {
		t.Errorf("len(Hosts) = %d, expected 2", len(cfg.Hosts))
	}
	if _, ok := cfg.Hosts["gitlab.com"]; !ok {
		t.Error("gitlab.com not in Hosts map")
	}
	if _, ok := cfg.Hosts["github.local"]; !ok {
		t.Error("github.local not in Hosts map")
	}
}

func TestLoad_DerivedFields(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	yaml := `
hosts:
  gitlab.example.com:
    token: test-token
    user: testuser
    api_protocol: https
    skip_tls_verify: "true"
    api_host: custom.api.example.com
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_HOST", "gitlab.example.com")
	t.Setenv("GLVALET_GITLAB_URL", "")
	t.Setenv("GLVALET_TOKEN", "")

	opts := Options{HostFlag: ""}
	cfg, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.User != "testuser" {
		t.Errorf("User = %q, expected %q", cfg.User, "testuser")
	}
	if !cfg.SkipTLS {
		t.Errorf("SkipTLS = %v, expected true", cfg.SkipTLS)
	}
	if !strings.Contains(cfg.GitLabURL, "custom.api.example.com") {
		t.Errorf("GitLabURL = %q, expected to contain custom.api.example.com", cfg.GitLabURL)
	}
}

// ─── Gitea / tea config tests ─────────────────────────────────────────────────

func writeTeaConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLoad_Gitea_FromTeaConfig(t *testing.T) {
	dir := t.TempDir()
	writeTeaConfig(t, dir, `
logins:
  - name: try
    url: https://gitea.try.example.com
    token: gitea-token
    default: true
    user: alice
    insecure: false
  - name: local
    url: http://localhost:3000
    token: local-token
    user: bob
`)

	t.Setenv("GLVALET_PROVIDER", "gitea")
	t.Setenv("GLVALET_TEA_CONFIG", filepath.Join(dir, "config.yml"))
	t.Setenv("HOME", "/nonexistent")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GLVALET_TOKEN", "")
	t.Setenv("GLVALET_GITEA_URL", "")

	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "gitea" {
		t.Errorf("Provider = %q, expected %q", cfg.Provider, "gitea")
	}
	if cfg.Host != "try" {
		t.Errorf("Host = %q, expected %q", cfg.Host, "try")
	}
	if cfg.Token != "gitea-token" {
		t.Errorf("Token = %q, expected %q", cfg.Token, "gitea-token")
	}
	if cfg.User != "alice" {
		t.Errorf("User = %q, expected %q", cfg.User, "alice")
	}
	if cfg.GiteaURL != "https://gitea.try.example.com" {
		t.Errorf("GiteaURL = %q, expected %q", cfg.GiteaURL, "https://gitea.try.example.com")
	}
	if len(cfg.Hosts) != 2 {
		t.Errorf("len(Hosts) = %d, expected 2", len(cfg.Hosts))
	}
	if cfg.Hosts["try"].Provider != "gitea" {
		t.Errorf("Hosts['try'].Provider = %q, expected %q", cfg.Hosts["try"].Provider, "gitea")
	}
}

func TestLoad_Gitea_EnvURL(t *testing.T) {
	dir := t.TempDir()
	writeTeaConfig(t, dir, "logins: []")

	t.Setenv("GLVALET_PROVIDER", "gitea")
	t.Setenv("GLVALET_TEA_CONFIG", filepath.Join(dir, "config.yml"))
	t.Setenv("GLVALET_GITEA_URL", "https://gitea.env.example.com/path")
	t.Setenv("GLVALET_TOKEN", "env-token")
	t.Setenv("HOME", "/nonexistent")
	t.Setenv("XDG_CONFIG_HOME", "")

	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Host != "gitea.env.example.com/path" {
		t.Errorf("Host = %q, expected %q", cfg.Host, "gitea.env.example.com/path")
	}
	if cfg.GiteaURL != "https://gitea.env.example.com/path" {
		t.Errorf("GiteaURL = %q, expected %q", cfg.GiteaURL, "https://gitea.env.example.com/path")
	}
	if cfg.Token != "env-token" {
		t.Errorf("Token = %q, expected %q", cfg.Token, "env-token")
	}
}

func TestLoad_Gitea_NoLoginsError(t *testing.T) {
	dir := t.TempDir()
	writeTeaConfig(t, dir, "logins: []")

	t.Setenv("GLVALET_PROVIDER", "gitea")
	t.Setenv("GLVALET_TEA_CONFIG", filepath.Join(dir, "config.yml"))
	t.Setenv("GLVALET_TOKEN", "")
	t.Setenv("GLVALET_GITEA_URL", "")
	t.Setenv("HOME", "/nonexistent")
	t.Setenv("XDG_CONFIG_HOME", "")

	_, err := Load(Options{})
	if err == nil {
		t.Fatal("Load: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no Gitea logins") {
		t.Errorf("error = %q, expected to contain 'no Gitea logins'", err.Error())
	}
}

// ─── ForHost() Tests ─────────────────────────────────────────────────────────

func TestForHost_ValidHost(t *testing.T) {
	hosts := map[string]*HostConfig{
		"gitlab.example.com": {
			Token:       "glpat-abc123",
			User:        "alice",
			APIProtocol: "https",
		},
		"gitlab.internal": {
			Token:       "glpat-xyz",
			User:        "bob",
			APIProtocol: "http",
		},
	}

	journalPath := filepath.Join(t.TempDir(), "journal.jsonl")
	cfg, err := ForHost(hosts, "gitlab.example.com", journalPath)
	if err != nil {
		t.Fatalf("ForHost: %v", err)
	}
	if cfg.Host != "gitlab.example.com" {
		t.Errorf("Host = %q, expected %q", cfg.Host, "gitlab.example.com")
	}
	if cfg.Token != "glpat-abc123" {
		t.Errorf("Token = %q, expected %q", cfg.Token, "glpat-abc123")
	}
	if cfg.User != "alice" {
		t.Errorf("User = %q, expected %q", cfg.User, "alice")
	}
	if cfg.JournalPath != journalPath {
		t.Errorf("JournalPath = %q, expected %q", cfg.JournalPath, journalPath)
	}
}

func TestForHost_Gitea(t *testing.T) {
	hosts := map[string]*HostConfig{
		"mygitea": {
			Token:       "gitea-token",
			User:        "alice",
			APIProtocol: "https",
			APIHost:     "gitea.example.com",
			Provider:    "gitea",
		},
	}

	cfg, err := ForHost(hosts, "mygitea", "/tmp/journal.jsonl")
	if err != nil {
		t.Fatalf("ForHost: %v", err)
	}
	if cfg.Provider != "gitea" {
		t.Errorf("Provider = %q, expected %q", cfg.Provider, "gitea")
	}
	if cfg.GiteaURL != "https://gitea.example.com" {
		t.Errorf("GiteaURL = %q, expected %q", cfg.GiteaURL, "https://gitea.example.com")
	}
	if cfg.GitLabURL != "" {
		t.Errorf("GitLabURL = %q, expected empty", cfg.GitLabURL)
	}
}

func TestForHost_TokenBackfill(t *testing.T) {
	hosts := map[string]*HostConfig{
		"gitlab.example.com": {
			Token:       "",
			User:        "alice",
			APIProtocol: "https",
		},
	}

	t.Setenv("GLVALET_TOKEN", "glpat-backfilled")
	journalPath := filepath.Join(t.TempDir(), "journal.jsonl")
	cfg, err := ForHost(hosts, "gitlab.example.com", journalPath)
	if err != nil {
		t.Fatalf("ForHost: %v", err)
	}
	if cfg.Token != "glpat-backfilled" {
		t.Errorf("Token = %q, expected %q", cfg.Token, "glpat-backfilled")
	}
}

func TestForHost_MissingHost(t *testing.T) {
	hosts := map[string]*HostConfig{
		"gitlab.example.com": {
			Token: "token",
		},
		"other.host": {
			Token: "token2",
		},
	}

	journalPath := filepath.Join(t.TempDir(), "journal.jsonl")
	_, err := ForHost(hosts, "unknown.host", journalPath)
	if err == nil {
		t.Fatal("ForHost: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown.host") {
		t.Errorf("error = %q, expected to contain 'unknown.host'", err.Error())
	}
	if !strings.Contains(err.Error(), "gitlab.example.com") {
		t.Errorf("error = %q, expected to list available hosts", err.Error())
	}
}

func TestForHost_NoToken(t *testing.T) {
	hosts := map[string]*HostConfig{
		"gitlab.example.com": {
			Token: "",
		},
	}

	t.Setenv("GLVALET_TOKEN", "")
	_, err := ForHost(hosts, "gitlab.example.com", "/tmp/journal.jsonl")
	if err == nil {
		t.Fatal("ForHost: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no token") {
		t.Errorf("error = %q, expected to contain 'no token'", err.Error())
	}
}

func TestForHost_PopulatesHostsMap(t *testing.T) {
	hosts := map[string]*HostConfig{
		"gitlab.example.com": {
			Token: "token1",
		},
		"github.local": {
			Token: "token2",
		},
	}

	cfg, err := ForHost(hosts, "gitlab.example.com", "/tmp/journal.jsonl")
	if err != nil {
		t.Fatalf("ForHost: %v", err)
	}
	if len(cfg.Hosts) != 2 {
		t.Errorf("len(Hosts) = %d, expected 2", len(cfg.Hosts))
	}
	if _, ok := cfg.Hosts["gitlab.example.com"]; !ok {
		t.Error("gitlab.example.com not in Hosts")
	}
	if _, ok := cfg.Hosts["github.local"]; !ok {
		t.Error("github.local not in Hosts")
	}
}

// ─── Helper Function Tests ──────────────────────────────────────────────────

func TestStripScheme(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://gitlab.com", "gitlab.com"},
		{"https://gitlab.com/path", "gitlab.com/path"},
		{"http://localhost:3000", "localhost:3000"},
		{"http://localhost:3000/api", "localhost:3000/api"},
		{"gitlab.com", "gitlab.com"},
		{"", ""},
		{"ftp://unsupported.com", "ftp://unsupported.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripScheme(tt.input)
			if got != tt.expected {
				t.Errorf("stripScheme(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		name     string
		inputs   []string
		expected string
	}{
		{"first non-empty", []string{"", "second", "third"}, "second"},
		{"first is non-empty", []string{"first", "second"}, "first"},
		{"all empty", []string{"", "", ""}, ""},
		{"whitespace only", []string{"  ", "  "}, ""},
		{"mixed whitespace", []string{"  ", "value", "  "}, "value"},
		{"single empty", []string{""}, ""},
		{"single non-empty", []string{"value"}, "value"},
		{"no inputs", []string{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstNonEmpty(tt.inputs...)
			if got != tt.expected {
				t.Errorf("firstNonEmpty(%v) = %q, expected %q", tt.inputs, got, tt.expected)
			}
		})
	}
}

func TestSortedKeys(t *testing.T) {
	hosts := map[string]*HostConfig{
		"zebra.com":  {},
		"alpha.com":  {},
		"middle.com": {},
		"beta.com":   {},
	}

	keys := sortedKeys(hosts)
	expected := []string{"alpha.com", "beta.com", "middle.com", "zebra.com"}

	if len(keys) != len(expected) {
		t.Fatalf("len(keys) = %d, expected %d", len(keys), len(expected))
	}

	for i, key := range keys {
		if key != expected[i] {
			t.Errorf("keys[%d] = %q, expected %q", i, key, expected[i])
		}
	}
}

func TestSortedKeys_Empty(t *testing.T) {
	hosts := make(map[string]*HostConfig)
	keys := sortedKeys(hosts)
	if len(keys) != 0 {
		t.Errorf("len(keys) = %d, expected 0", len(keys))
	}
}

// ─── Integration Tests ───────────────────────────────────────────────────────

func TestLoad_FullIntegration(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	yaml := `
host: default.example.com
hosts:
  default.example.com:
    token: glpat-default
    user: alice
    api_protocol: https
    api_host: api.default.example.com
    skip_tls_verify: "false"
    git_protocol: ssh
  secondary.example.com:
    token: glpat-secondary
    user: bob
    api_protocol: http
    skip_tls_verify: "true"
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_HOST", "")
	t.Setenv("GLVALET_GITLAB_URL", "")
	t.Setenv("GLVALET_TOKEN", "")
	t.Setenv("HOME", "/nonexistent")
	t.Setenv("XDG_CONFIG_HOME", "")

	opts := Options{HostFlag: ""}
	cfg, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Host != "default.example.com" {
		t.Errorf("Host = %q, expected %q", cfg.Host, "default.example.com")
	}
	if cfg.Token != "glpat-default" {
		t.Errorf("Token = %q, expected %q", cfg.Token, "glpat-default")
	}
	if cfg.User != "alice" {
		t.Errorf("User = %q, expected %q", cfg.User, "alice")
	}
	if cfg.SkipTLS {
		t.Errorf("SkipTLS = %v, expected false", cfg.SkipTLS)
	}
	if !strings.Contains(cfg.GitLabURL, "api.default.example.com") {
		t.Errorf("GitLabURL = %q, expected to contain api.default.example.com", cfg.GitLabURL)
	}
	if len(cfg.Hosts) != 2 {
		t.Errorf("len(Hosts) = %d, expected 2", len(cfg.Hosts))
	}
}

func TestLoad_EnvironmentVariablePaths(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yml")

	yaml := `
hosts:
  test.example.com:
    token: test-token
    api_protocol: https
`
	if err := os.WriteFile(configFile, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GLVALET_GLAB_CONFIG", configFile)
	t.Setenv("GLVALET_HOST", "test.example.com")
	t.Setenv("GLVALET_DEFAULT_PROJECT", "my-project")
	t.Setenv("GLVALET_DEFAULT_GROUP", "my-group")

	opts := Options{HostFlag: ""}
	cfg, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultProject != "my-project" {
		t.Errorf("DefaultProject = %q, expected %q", cfg.DefaultProject, "my-project")
	}
	if cfg.DefaultGroup != "my-group" {
		t.Errorf("DefaultGroup = %q, expected %q", cfg.DefaultGroup, "my-group")
	}
}

func TestAPIURL_CaseInsensitiveProtocol(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		expected string
	}{
		{"http lowercase", "http", "http://"},
		{"HTTP uppercase", "HTTP", "http://"},
		{"Http mixed", "Http", "http://"},
		{"https lowercase", "https", "https://"},
		{"HTTPS uppercase", "HTTPS", "https://"},
		{"Https mixed", "Https", "https://"},
		{"empty defaults to https", "", "https://"},
		{"unknown defaults to https", "gopher", "https://"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc := &HostConfig{APIProtocol: tt.protocol}
			url := hc.APIURL("test.com")
			if !strings.HasPrefix(url, tt.expected) {
				t.Errorf("APIURL() = %q, expected prefix %q", url, tt.expected)
			}
		})
	}
}
