package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/rotation"
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

// writeTeaConfig writes a minimal tea config file for doctor tests and returns
// its path.
func writeTeaConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

func TestDoctor_GiteaHappyPath(t *testing.T) {
	silenceOutput(t)
	// httptest Gitea server that replies to the version check and the
	// ListMyIssues probe (/api/v1/repos/issues/search).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/version":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version": "1.22.0"}`))
		case "/api/v1/repos/issues/search":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("[]"))
		default:
			t.Errorf("unexpected Gitea probe path %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("GLVALET_PROVIDER", "gitea")
	t.Setenv("GLVALET_TEA_CONFIG", writeTeaConfig(t, fmt.Sprintf(`
logins:
  - name: test-gitea
    url: %s
    token: gitea-test-token
    default: true
    user: alice
    insecure: false
`, srv.URL)))
	t.Setenv("GLVALET_TOKEN", "")
	t.Setenv("GLVALET_GITLAB_URL", "")
	hostFlag = ""

	if err := runDoctor(); err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
}

func TestDoctor_GiteaNoLoginsError(t *testing.T) {
	silenceOutput(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GLVALET_PROVIDER", "gitea")
	t.Setenv("GLVALET_TEA_CONFIG", writeTeaConfig(t, "logins: []\n"))
	t.Setenv("GLVALET_TOKEN", "")
	t.Setenv("GLVALET_GITEA_URL", "")
	t.Setenv("GLVALET_GITLAB_URL", "")
	hostFlag = ""

	err := runDoctor()
	if err == nil {
		t.Fatal("runDoctor: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "config load failed") {
		t.Errorf("error = %q, expected config load failed", err.Error())
	}
}

// ─── doctorHostLines ─────────────────────────────────────────────────────────

func TestDoctor_ReportsEveryConfiguredHost(t *testing.T) {
	hosts := map[string]*config.HostConfig{
		"a.example.com": {Token: "tok-a"},
		"b.example.com": {Token: "tok-b"},
	}
	lines := doctorHostLines(hosts, func(host string) error {
		if host == "b.example.com" {
			return errors.New("401 Unauthorized")
		}
		return nil
	})
	if len(lines) != 2 {
		t.Fatalf("expected a line per host, got %d", len(lines))
	}
	var sawFailure bool
	for _, l := range lines {
		if strings.Contains(l, "b.example.com") && strings.Contains(l, "401") {
			sawFailure = true
		}
	}
	if !sawFailure {
		t.Error("a failing non-default host must be reported, not skipped")
	}
}

// ─── doctorTokenLines ────────────────────────────────────────────────────────

func fakeGetSelf(info *rotation.TokenInfo, err error) doctorGetSelfFunc {
	return func(ctx context.Context, baseURL, token string, skipTLS bool) (*rotation.TokenInfo, error) {
		return info, err
	}
}

func TestDoctorTokenLines_Healthy(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	hosts := map[string]*config.HostConfig{
		"gitlab.example.com": {Token: "glpat-fake"},
	}
	getSelf := fakeGetSelf(&rotation.TokenInfo{
		ID: 1, Active: true, Scopes: []string{"api", "self_rotate"},
		ExpiresAt: "2026-10-20",
	}, nil)

	lines := doctorTokenLines(context.Background(), hosts, []string{"gitlab.example.com"}, now, getSelf)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], "✓") {
		t.Errorf("expected a healthy line, got %q", lines[0])
	}
}

func TestDoctorTokenLines_NearExpiry(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	hosts := map[string]*config.HostConfig{
		"gitlab.example.com": {Token: "glpat-fake"},
	}
	// Three days out — inside the (DefaultPATExpiryDays - DefaultCadenceDays)
	// warning window.
	getSelf := fakeGetSelf(&rotation.TokenInfo{
		ID: 1, Active: true, Scopes: []string{"api", "self_rotate"},
		ExpiresAt: "2026-08-19",
	}, nil)

	lines := doctorTokenLines(context.Background(), hosts, []string{"gitlab.example.com"}, now, getSelf)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "✗") || !strings.Contains(lines[0], "expires") {
		t.Fatalf("expected a near-expiry failure line, got %v", lines)
	}
}

func TestDoctorTokenLines_MissingScope(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	hosts := map[string]*config.HostConfig{
		"gitlab.example.com": {Token: "glpat-fake"},
	}
	getSelf := fakeGetSelf(&rotation.TokenInfo{
		ID: 1, Active: true, Scopes: []string{"api"},
		ExpiresAt: "2026-12-20",
	}, nil)

	lines := doctorTokenLines(context.Background(), hosts, []string{"gitlab.example.com"}, now, getSelf)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "✗") || !strings.Contains(lines[0], "self_rotate") {
		t.Fatalf("expected a missing-scope failure line, got %v", lines)
	}
}

func TestDoctorTokenLines_UnreachableHostRedactsToken(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	hosts := map[string]*config.HostConfig{
		"gitlab.example.com": {Token: "glpat-super-secret"},
	}
	getSelf := fakeGetSelf(nil, errors.New("dial glpat-super-secret@gitlab.example.com: connection refused"))

	lines := doctorTokenLines(context.Background(), hosts, []string{"gitlab.example.com"}, now, getSelf)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "✗") {
		t.Fatalf("expected an unreachable failure line, got %v", lines)
	}
	if strings.Contains(lines[0], "glpat-super-secret") {
		t.Errorf("token value leaked into doctor output: %q", lines[0])
	}
}

func TestDoctorTokenLines_HostMissingFromGlabConfig(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	hosts := map[string]*config.HostConfig{}

	lines := doctorTokenLines(context.Background(), hosts, []string{"ghost.example.com"}, now, fakeGetSelf(nil, nil))
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "✗") {
		t.Fatalf("expected a failure line for a host missing from the glab config, got %v", lines)
	}
}

// ─── doctorEscrowLines ───────────────────────────────────────────────────────

func TestDoctorEscrowLines_UncommittedEscrowReported(t *testing.T) {
	dir := t.TempDir()
	esc := &rotation.Escrow{
		Host: "gitlab.example.com", State: rotation.StateRotated,
		OldTokenID: 1, NewTokenID: 2, NewToken: "glpat-new-secret",
	}
	if _, err := rotation.WriteEscrow(dir, esc); err != nil {
		t.Fatalf("WriteEscrow: %v", err)
	}

	lines, err := doctorEscrowLines(dir)
	if err != nil {
		t.Fatalf("doctorEscrowLines: %v", err)
	}
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "✗") {
		t.Fatalf("expected 1 failure line, got %v", lines)
	}
	if !strings.Contains(lines[0], "gitlab.example.com") || !strings.Contains(lines[0], "--recover") {
		t.Errorf("line does not point at the remedy: %q", lines[0])
	}
	if strings.Contains(lines[0], "glpat-new-secret") {
		t.Errorf("escrowed token value leaked into doctor output: %q", lines[0])
	}
}

func TestDoctorEscrowLines_CorruptEscrowNamed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	lines, err := doctorEscrowLines(dir)
	if err != nil {
		t.Fatalf("doctorEscrowLines: %v", err)
	}
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "✗") || !strings.Contains(lines[0], "broken.json") {
		t.Fatalf("expected the corrupt file to be named, got %v", lines)
	}
}

func TestDoctorEscrowLines_NoneIsQuiet(t *testing.T) {
	dir := t.TempDir()
	lines, err := doctorEscrowLines(dir)
	if err != nil {
		t.Fatalf("doctorEscrowLines: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("expected no lines for an empty escrow dir, got %v", lines)
	}
}

func TestDoctorEscrowLines_MissingDirIsQuiet(t *testing.T) {
	lines, err := doctorEscrowLines(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("doctorEscrowLines: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("expected no lines for a missing escrow dir, got %v", lines)
	}
}

// ─── patHostNames ────────────────────────────────────────────────────────────

func TestPatHostNames_OnlyHostsDeclaringPAT(t *testing.T) {
	rc := &rotation.Config{Hosts: map[string]rotation.Profile{
		"pat-only.example.com": {Credentials: []string{"pat"}},
		"ssh-only.example.com": {Credentials: []string{"ssh"}},
		"both.example.com":     {Credentials: []string{"pat", "ssh"}},
	}}
	got := patHostNames(rc, "")
	want := []string{"both.example.com", "pat-only.example.com"}
	if len(got) != len(want) {
		t.Fatalf("patHostNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("patHostNames() = %v, want %v", got, want)
		}
	}
}

// A doctor run scoped with --host must answer about that host alone. Before
// this, doctorTokenHealth probed every PAT host regardless of the flag, so an
// operator scoping a check to one host silently got remote calls for all of
// them.
func TestPatHostNames_ScopedToOneHost(t *testing.T) {
	rc := &rotation.Config{Hosts: map[string]rotation.Profile{
		"pat-only.example.com": {Credentials: []string{"pat"}},
		"ssh-only.example.com": {Credentials: []string{"ssh"}},
		"both.example.com":     {Credentials: []string{"pat", "ssh"}},
	}}
	got := patHostNames(rc, "both.example.com")
	if len(got) != 1 || got[0] != "both.example.com" {
		t.Fatalf("patHostNames(rc, %q) = %v, want exactly [both.example.com]", "both.example.com", got)
	}
	if scoped := patHostNames(rc, "ssh-only.example.com"); len(scoped) != 0 {
		t.Fatalf("a host declaring no PAT must yield no token checks, got %v", scoped)
	}
}

// ─── doctorTokenLine — revoked/inactive branches ────────────────────────────

func TestDoctorTokenLine_Revoked(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	info := &rotation.TokenInfo{ID: 9, Active: true, Revoked: true, Scopes: []string{"api", "self_rotate"}, ExpiresAt: "2026-12-01"}
	line := doctorTokenLine("gitlab.example.com", info, now)
	if !strings.HasPrefix(line, "✗") || !strings.Contains(line, "revoked") {
		t.Fatalf("expected a revoked failure line, got %q", line)
	}
}

func TestDoctorTokenLine_Inactive(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	info := &rotation.TokenInfo{ID: 9, Active: false, Scopes: []string{"api", "self_rotate"}, ExpiresAt: "2026-12-01"}
	line := doctorTokenLine("gitlab.example.com", info, now)
	if !strings.HasPrefix(line, "✗") || !strings.Contains(line, "inactive") {
		t.Fatalf("expected an inactive failure line, got %q", line)
	}
}

// tokenHealthWarnWithin must be derived from rotation's own constants, not a
// hand-tuned literal — a hardcoded duration here would stop tracking the
// value the moment either constant moved, the exact failure the derivation
// removes.
func TestTokenHealthWarnWithin_DerivedFromRotationConstants(t *testing.T) {
	want := time.Duration(rotation.DefaultPATExpiryDays-rotation.DefaultCadenceDays) * 24 * time.Hour
	if tokenHealthWarnWithin != want {
		t.Fatalf("tokenHealthWarnWithin = %v, want %v (DefaultPATExpiryDays - DefaultCadenceDays)", tokenHealthWarnWithin, want)
	}
}

// A token with slightly more than one full rotation cycle of life left is
// healthy: even if the next scheduled rotation slipped by a day, there is
// still time before doctor needs to say anything.
func TestDoctorTokenLine_JustOverOneCadenceOfLifeLeft_NoWarning(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	exp := now.Add(tokenHealthWarnWithin + 24*time.Hour)
	info := &rotation.TokenInfo{ID: 1, Active: true, Scopes: []string{"api", "self_rotate"}, ExpiresAt: exp.Format("2006-01-02")}

	line := doctorTokenLine("gitlab.example.com", info, now)
	if !strings.HasPrefix(line, "✓") {
		t.Fatalf("expected a healthy line with just over one cadence of life left, got %q", line)
	}
}

// A token with slightly less than one full rotation cycle of life left means
// at least one scheduled rotation has already been missed — doctor must warn.
func TestDoctorTokenLine_JustUnderOneCadenceOfLifeLeft_Warns(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	exp := now.Add(tokenHealthWarnWithin - 24*time.Hour)
	info := &rotation.TokenInfo{ID: 1, Active: true, Scopes: []string{"api", "self_rotate"}, ExpiresAt: exp.Format("2006-01-02")}

	line := doctorTokenLine("gitlab.example.com", info, now)
	if !strings.HasPrefix(line, "✗") || !strings.Contains(line, "expires") {
		t.Fatalf("expected a near-expiry warning with just under one cadence of life left, got %q", line)
	}
}

// ─── doctorTokenHealth — the two paths that resolve without a network call ──

func TestDoctorTokenHealth_NotConfiguredIsNotAFailure(t *testing.T) {
	silenceOutput(t)
	t.Setenv("GLVALET_HOME", t.TempDir())

	if !doctorTokenHealth() {
		t.Error("a fresh install with no rotation.yaml must not fail doctor")
	}
}

func TestDoctorTokenHealth_NoPATHostsIsNotAFailure(t *testing.T) {
	silenceOutput(t)
	home := t.TempDir()
	t.Setenv("GLVALET_HOME", home)
	if err := rotation.Save(filepath.Join(home, "rotation.yaml"), &rotation.Config{
		Version: 1,
		Hosts:   map[string]rotation.Profile{"ssh-only.example.com": {Credentials: []string{"ssh"}}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if !doctorTokenHealth() {
		t.Error("a profile with no PAT hosts has nothing to check remotely")
	}
}

// ─── doctorEscrowHealth ──────────────────────────────────────────────────────

func TestDoctorEscrowHealth_UncommittedEscrowFailsDoctor(t *testing.T) {
	silenceOutput(t)
	home := t.TempDir()
	t.Setenv("GLVALET_HOME", home)
	if _, err := rotation.WriteEscrow(escrowDir(), &rotation.Escrow{
		Host: "gitlab.example.com", NewTokenID: 2, NewToken: "glpat-new",
	}); err != nil {
		t.Fatalf("WriteEscrow: %v", err)
	}

	if doctorEscrowHealth() {
		t.Error("an uncommitted escrow must fail doctor")
	}
}

func TestDoctorEscrowHealth_NoneIsHealthy(t *testing.T) {
	silenceOutput(t)
	t.Setenv("GLVALET_HOME", t.TempDir())

	if !doctorEscrowHealth() {
		t.Error("no escrow files should pass doctor")
	}
}

// ─── doctorProbeOtherHosts — the offline-deterministic path ────────────────

// A host rotation.yaml never even mentions here: cfg.Hosts carries an entry
// with no token, which config.ForHost rejects before any remote call — so
// this stays deterministic offline, the same trick TestRunRotate uses.
func TestDoctorProbeOtherHosts_MissingTokenFailsWithoutNetwork(t *testing.T) {
	silenceOutput(t)
	cfg := &config.Config{
		Host: "default.example.com",
		Hosts: map[string]*config.HostConfig{
			"default.example.com": {Token: "tok-default"},
			"other.example.com":   {Token: ""},
		},
	}

	if doctorProbeOtherHosts(cfg) {
		t.Error("a second host with no token must fail, not pass silently")
	}
}

func TestDoctorProbeOtherHosts_NoOtherHostsIsAlwaysTrue(t *testing.T) {
	cfg := &config.Config{
		Host:  "default.example.com",
		Hosts: map[string]*config.HostConfig{"default.example.com": {Token: "tok-default"}},
	}
	if !doctorProbeOtherHosts(cfg) {
		t.Error("nothing to probe beyond the default host must not fail")
	}
}
