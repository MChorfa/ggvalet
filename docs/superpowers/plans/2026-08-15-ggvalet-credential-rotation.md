# ggvalet Credential Rotation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add monthly GitLab PAT rotation with crash-safe recovery, plus a read-only SSH inventory, driven by a launchd agent.

**Architecture:** A new `internal/rotation` package implements a five-phase state machine (preflight → rotate → verify → commit → record) around a write-ahead escrow file, because GitLab's self-rotate endpoint revokes the old token and returns the replacement exactly once. Custody stays with glab's `config.yml`, which is edited surgically to preserve its comments. A separate `internal/sshaudit` package reports drift across three surfaces without writing anything.

**Tech Stack:** Go, cobra, `gopkg.in/yaml.v3` (already vendored — check `go.mod` before adding), `net/http/httptest` for API tests, launchd for scheduling.

**Spec:** `docs/superpowers/specs/2026-08-15-ggvalet-credential-rotation-design.md`

## Global Constraints

- Escrow directory mode `0700`; escrow files and any temp config file mode `0600`.
- The secret is never logged and never journaled. Journal entries carry token **id** and a last-4 fingerprint only.
- `config.yml` edits preserve every byte outside the single changed line. Never parse-and-remarshal.
- Locate `token:` by indentation-scoped walk (`hosts:` → 4-space host key → 8-space field), never a global regex.
- An unauthenticated host is a hard failure, never a skip.
- Default cadence 30 days; default PAT expiry 65 days, set explicitly on every rotate call, never inherited.
- `rotate` and `ssh` are host-specific — register as guarded leaves in `hostguard`, never in `hostNeutralPrefixes`.
- v1 never writes to `~/.ssh`.
- Existing repo coverage gate applies to all new packages.
- **TLS verification is never disabled at a new call site.** The repo already
  honours glab's `skip_tls_verify` in exactly one place; Task 4 extracts that
  into `observed.BaseTransport` so the count stays at one. Do not write
  `InsecureSkipVerify` anywhere else.

## File Structure

| File | Responsibility |
|---|---|
| `internal/rotation/config.go` | `rotation.yaml` load / propose / save |
| `internal/rotation/escrow.go` | write-ahead escrow records |
| `internal/rotation/configwrite.go` | comment-preserving glab `config.yml` edit |
| `internal/rotation/gitlab.go` | `self` and `self/rotate` API calls |
| `internal/rotation/rotate.go` | the five-phase state machine + recovery |
| `internal/sshaudit/audit.go` | three-surface SSH drift classification |
| `cmd/rotate.go` | `ggvalet rotate` |
| `cmd/ssh.go` | `ggvalet ssh audit` |
| `cmd/doctor.go` | modified: iterate all hosts |
| `cmd/hostguard.go` | modified: register new guarded leaves |
| `internal/journal/journal.go` | modified: add `OpRotate`, `EntityToken`, `EntitySSHKey` |
| `internal/state/store.go` | modified: `rotation_state` table |
| `deploy/launchd/com.mchorfa.ggvalet-rotate.plist` | monthly schedule |

---

### Task 1: Rotation profile config

**Files:**
- Create: `internal/rotation/config.go`
- Test: `internal/rotation/config_test.go`

**Interfaces:**
- Consumes: `config.HostConfig` from `internal/config`.
- Produces: `rotation.Config{Version int, Defaults, Hosts map[string]Profile}`, `Defaults{CadenceDays, PATExpiryDays int}`, `Profile{Credentials []string}`, `Load(path string) (*Config, error)`, `Propose(hosts map[string]*config.HostConfig) *Config`, `Save(path string, c *Config) error`, `(*Config).Uses(host, cred string) bool`.

- [ ] **Step 1: Write the failing test**

```go
package rotation

import (
	"path/filepath"
	"testing"
)

func TestLoad_ParsesHostProfiles(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rotation.yaml")
	if err := writeFile(p, []byte(`version: 1
defaults:
  cadence_days: 30
  pat_expiry_days: 65
hosts:
  gitlab.example.com:
    credentials: [pat, ssh]
  other.example.com:
    credentials: [pat]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Defaults.CadenceDays != 30 || c.Defaults.PATExpiryDays != 65 {
		t.Fatalf("defaults: %+v", c.Defaults)
	}
	if !c.Uses("gitlab.example.com", "ssh") {
		t.Error("expected ssh for gitlab.example.com")
	}
	if c.Uses("other.example.com", "ssh") {
		t.Error("other.example.com must not declare ssh")
	}
	if !c.Uses("other.example.com", "pat") {
		t.Error("expected pat for other.example.com")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rotation/ -run TestLoad_ParsesHostProfiles -v`
Expected: FAIL — `undefined: Load`, `undefined: writeFile`

- [ ] **Step 3: Write minimal implementation**

```go
// Package rotation implements credential rotation for configured hosts.
package rotation

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"gitlab.com/mchorfa/ggvalet/internal/config" // adjust to the module path in go.mod
)

const (
	DefaultCadenceDays   = 30
	DefaultPATExpiryDays = 65
)

type Profile struct {
	Credentials []string `yaml:"credentials"`
}

type Defaults struct {
	CadenceDays   int `yaml:"cadence_days"`
	PATExpiryDays int `yaml:"pat_expiry_days"`
}

type Config struct {
	Version  int                `yaml:"version"`
	Defaults Defaults           `yaml:"defaults"`
	Hosts    map[string]Profile `yaml:"hosts"`
}

// Uses reports whether host declares the named credential type.
func (c *Config) Uses(host, cred string) bool {
	p, ok := c.Hosts[host]
	if !ok {
		return false
	}
	for _, got := range p.Credentials {
		if got == cred {
			return true
		}
	}
	return false
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("rotation config parse: %w", err)
	}
	if c.Defaults.CadenceDays == 0 {
		c.Defaults.CadenceDays = DefaultCadenceDays
	}
	if c.Defaults.PATExpiryDays == 0 {
		c.Defaults.PATExpiryDays = DefaultPATExpiryDays
	}
	return &c, nil
}

// Propose builds a starting profile from observed hosts. Every host gets pat;
// ssh is never assumed, because a host that fails to authenticate cannot be
// probed to discover whether it uses SSH.
func Propose(hosts map[string]*config.HostConfig) *Config {
	c := &Config{
		Version:  1,
		Defaults: Defaults{CadenceDays: DefaultCadenceDays, PATExpiryDays: DefaultPATExpiryDays},
		Hosts:    map[string]Profile{},
	}
	names := make([]string, 0, len(hosts))
	for h := range hosts {
		names = append(names, h)
	}
	sort.Strings(names)
	for _, h := range names {
		c.Hosts[h] = Profile{Credentials: []string{"pat"}}
	}
	return c
}

func Save(path string, c *Config) error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFile(path, b, 0o600)
}

func writeFile(path string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, mode)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/rotation/ -run TestLoad_ParsesHostProfiles -v`
Expected: PASS

- [ ] **Step 5: Add the Propose test**

```go
func TestPropose_NeverAssumesSSH(t *testing.T) {
	c := Propose(map[string]*config.HostConfig{"a.example.com": {}, "b.example.com": {}})
	for h := range c.Hosts {
		if c.Uses(h, "ssh") {
			t.Errorf("%s: ssh must never be proposed automatically", h)
		}
		if !c.Uses(h, "pat") {
			t.Errorf("%s: pat should be proposed", h)
		}
	}
}
```

Run: `go test ./internal/rotation/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/rotation/config.go internal/rotation/config_test.go
git commit -m "feat(rotation): per-host credential profile config"
```

---

### Task 2: Write-ahead escrow store

**Files:**
- Create: `internal/rotation/escrow.go`
- Test: `internal/rotation/escrow_test.go`

**Interfaces:**
- Produces: `Escrow{Version int, Host, State, RotatedAt, NewToken, ExpiresAt, ConfigPath, ConfigSHA256 string; OldTokenID, NewTokenID int; Scopes []string}`, states `StateRotated = "ROTATED"` / `StateVerified = "VERIFIED"`, `WriteEscrow(dir string, e *Escrow) (string, error)`, `ListEscrows(dir string) ([]*Escrow, error)`, `LoadEscrow(path string) (*Escrow, error)`, `DeleteEscrow(path string) error`. Each `*Escrow` returned by `ListEscrows` carries its own `Path` field.

- [ ] **Step 1: Write the failing test**

```go
package rotation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteEscrow_PermissionsAndRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "escrow")
	e := &Escrow{
		Version: 1, Host: "gitlab.example.com", State: StateRotated,
		OldTokenID: 51629, NewTokenID: 51630, NewToken: "glpat-secret",
		ExpiresAt: "2026-10-20", Scopes: []string{"api", "self_rotate"},
		ConfigPath: "/tmp/config.yml", ConfigSHA256: "abc123",
	}
	path, err := WriteEscrow(dir, e)
	if err != nil {
		t.Fatalf("WriteEscrow: %v", err)
	}

	dfi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := dfi.Mode().Perm(); got != 0o700 {
		t.Errorf("dir mode = %o, want 700", got)
	}
	ffi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := ffi.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want 600", got)
	}

	got, err := LoadEscrow(path)
	if err != nil {
		t.Fatalf("LoadEscrow: %v", err)
	}
	if got.NewToken != "glpat-secret" || got.OldTokenID != 51629 || got.State != StateRotated {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rotation/ -run TestWriteEscrow -v`
Expected: FAIL — `undefined: Escrow`

- [ ] **Step 3: Write minimal implementation**

```go
package rotation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	StateRotated  = "ROTATED"
	StateVerified = "VERIFIED"
)

type Escrow struct {
	Version      int      `json:"version"`
	Host         string   `json:"host"`
	State        string   `json:"state"`
	RotatedAt    string   `json:"rotated_at"`
	OldTokenID   int      `json:"old_token_id"`
	NewTokenID   int      `json:"new_token_id"`
	NewToken     string   `json:"new_token"`
	ExpiresAt    string   `json:"expires_at"`
	Scopes       []string `json:"scopes"`
	ConfigPath   string   `json:"config_path"`
	ConfigSHA256 string   `json:"config_sha256_before"`

	Path string `json:"-"` // filled by ListEscrows/LoadEscrow
}

// WriteEscrow persists e and fsyncs both the file and its parent directory.
// Durability here is the whole point: GitLab has already revoked the old token
// by the time this is called, so an unsynced write means a lost credential.
func WriteEscrow(dir string, e *Escrow) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if e.RotatedAt == "" {
		e.RotatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%d.json", e.Host, time.Now().UTC().UnixNano())
	path := filepath.Join(dir, name)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	// fsync the directory so the new entry itself survives a crash.
	d, err := os.Open(dir)
	if err != nil {
		return "", err
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return "", err
	}
	e.Path = path
	return path, nil
}

func LoadEscrow(path string) (*Escrow, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var e Escrow
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("escrow parse %s: %w", path, err)
	}
	e.Path = path
	return &e, nil
}

func ListEscrows(dir string) ([]*Escrow, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Escrow
	for _, en := range entries {
		if en.IsDir() || filepath.Ext(en.Name()) != ".json" {
			continue
		}
		e, err := LoadEscrow(filepath.Join(dir, en.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func DeleteEscrow(path string) error { return os.Remove(path) }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/rotation/ -run TestWriteEscrow -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rotation/escrow.go internal/rotation/escrow_test.go
git commit -m "feat(rotation): write-ahead escrow with fsync durability"
```

---

### Task 3: Comment-preserving config write-back

**Files:**
- Create: `internal/rotation/configwrite.go`
- Test: `internal/rotation/configwrite_test.go`

**Interfaces:**
- Produces: `FileDigest(path string) (string, error)` returning hex SHA-256, and `SetHostToken(path, host, token string) error`.

- [ ] **Step 1: Write the failing test**

This is the highest-risk file in the plan; the fixture mirrors the real config shape (comments, two hosts, 4-space indent).

```go
package rotation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const twoHostFixture = `# Your GitLab access token. Read the docs before changing this.
hosts:
    sc01-trt.example.ca:
        # What protocol to use.
        api_protocol: https
        token: OLD_SECOND
        api_host: sc01-trt.example.ca/gitlab
    gitlab.example.com:
        api_protocol: https
        token: OLD_FIRST
        user: someone
`

func TestSetHostToken_PreservesEverythingElse(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(p, []byte(twoHostFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetHostToken(p, "gitlab.example.com", "NEW_FIRST"); err != nil {
		t.Fatalf("SetHostToken: %v", err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)

	if !strings.Contains(out, "token: NEW_FIRST") {
		t.Error("target token not updated")
	}
	if !strings.Contains(out, "token: OLD_SECOND") {
		t.Error("the OTHER host's token must not change")
	}
	if !strings.Contains(out, "# Your GitLab access token.") ||
		!strings.Contains(out, "# What protocol to use.") {
		t.Error("comments must be preserved")
	}

	wantLines := strings.Count(twoHostFixture, "\n")
	if gotLines := strings.Count(out, "\n"); gotLines != wantLines {
		t.Errorf("line count changed: got %d want %d", gotLines, wantLines)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", fi.Mode().Perm())
	}
}

func TestSetHostToken_UnknownHostIsAnError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(p, []byte(twoHostFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetHostToken(p, "nope.example.com", "X"); err == nil {
		t.Fatal("expected error for unknown host")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rotation/ -run TestSetHostToken -v`
Expected: FAIL — `undefined: SetHostToken`

- [ ] **Step 3: Write minimal implementation**

```go
package rotation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func FileDigest(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// SetHostToken rewrites exactly one line: the `token:` field inside host's
// block. It walks by indentation rather than matching a regex globally,
// because every host block contains a `token:` line.
func SetHostToken(path, host, token string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")

	inHosts, inTarget := false, false
	hostIndent, updated := -1, false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))

		if indent == 0 {
			inHosts = trimmed == "hosts:"
			inTarget = false
			continue
		}
		if !inHosts {
			continue
		}
		// A host key is the first indent level inside `hosts:`.
		if hostIndent == -1 || indent == hostIndent {
			if strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, ": ") {
				hostIndent = indent
				inTarget = strings.TrimSuffix(trimmed, ":") == host
				continue
			}
		}
		if inTarget && indent > hostIndent && strings.HasPrefix(trimmed, "token:") {
			lines[i] = fmt.Sprintf("%stoken: %s", strings.Repeat(" ", indent), token)
			updated = true
			break
		}
	}
	if !updated {
		return fmt.Errorf("no token line found for host %q in %s", host, path)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config.yml.tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(strings.Join(lines, "\n")); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/rotation/ -run TestSetHostToken -v`
Expected: PASS — both cases

- [ ] **Step 5: Commit**

```bash
git add internal/rotation/configwrite.go internal/rotation/configwrite_test.go
git commit -m "feat(rotation): comment-preserving atomic glab config write-back"
```

---

### Task 4: GitLab self / self-rotate API calls

**Files:**
- Create: `internal/rotation/gitlab.go`
- Test: `internal/rotation/gitlab_test.go`

**Interfaces:**
- Produces: `TokenInfo{ID int, Name string, Scopes []string, ExpiresAt string, Active, Revoked bool}`, `GetSelfToken(ctx context.Context, baseURL, token string, skipTLS bool) (*TokenInfo, error)`, `RotateSelfToken(ctx context.Context, baseURL, token, expiresAt string, skipTLS bool) (*TokenInfo, string, error)` returning info plus the new secret.

- [ ] **Step 0: Extract the single TLS-skip call site**

`internal/client/client.go` already contains the only `InsecureSkipVerify` in the
repo, mirroring glab's `skip_tls_verify` (which is `true` for both configured
hosts). Rotation needs the same behaviour, so extract rather than duplicate.

Add to `internal/observed/http.go`:

```go
// BaseTransport returns the shared outbound transport. It is the ONLY place in
// this repository that may disable TLS verification, and it does so solely to
// mirror glab's per-host skip_tls_verify setting — ggvalet must not be stricter
// than the CLI whose config it reads, or it would fail on hosts glab can reach.
// Any new caller uses this function; nobody writes InsecureSkipVerify again.
func BaseTransport(skipTLS bool) *http.Transport {
	tr := &http.Transport{}
	if skipTLS {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // mirrors glab skip_tls_verify
	}
	return tr
}
```

Test it in `internal/observed/http_test.go`:

```go
func TestBaseTransport_VerifiesByDefault(t *testing.T) {
	if tr := BaseTransport(false); tr.TLSClientConfig != nil {
		t.Error("TLS verification must be on unless skip_tls_verify is set")
	}
	if tr := BaseTransport(true); tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("skip_tls_verify must be honoured when requested")
	}
}
```

Then replace the inline block in `internal/client/client.go` with
`baseTransport := observed.BaseTransport(cfg.SkipTLS)`, and confirm the repo has
exactly one occurrence:

```bash
go test ./internal/observed/ ./internal/client/ -v
rg -c 'InsecureSkipVerify' --glob '!*_test.go' internal/   # expect: 1 file, 1 match
```

- [ ] **Step 1: Write the failing test**

```go
package rotation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRotateSelfToken_SendsExpiryAndReturnsSecret(t *testing.T) {
	var gotExpiry, gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("PRIVATE-TOKEN")
		gotExpiry = r.URL.Query().Get("expires_at")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":51630,"name":"t","scopes":["api","self_rotate"],
			"expires_at":"2026-10-20","active":true,"revoked":false,"token":"glpat-NEW"}`))
	}))
	defer srv.Close()

	info, secret, err := RotateSelfToken(context.Background(), srv.URL, "glpat-OLD", "2026-10-20", false)
	if err != nil {
		t.Fatalf("RotateSelfToken: %v", err)
	}
	if secret != "glpat-NEW" {
		t.Errorf("secret = %q", secret)
	}
	if info.ID != 51630 {
		t.Errorf("id = %d", info.ID)
	}
	if gotExpiry != "2026-10-20" {
		t.Errorf("expires_at not sent, got %q", gotExpiry)
	}
	if gotAuth != "glpat-OLD" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if gotPath != "/api/v4/personal_access_tokens/self/rotate" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestGetSelfToken_SurfacesUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"401 Unauthorized"}`))
	}))
	defer srv.Close()

	if _, err := GetSelfToken(context.Background(), srv.URL, "bad", false); err == nil {
		t.Fatal("expected error on 401")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rotation/ -run 'TestRotateSelfToken|TestGetSelfToken' -v`
Expected: FAIL — `undefined: RotateSelfToken`

- [ ] **Step 3: Write minimal implementation**

```go
package rotation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gitlab.com/mchorfa/ggvalet/internal/observed" // adjust to the module path in go.mod
)

type TokenInfo struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at"`
	Active    bool     `json:"active"`
	Revoked   bool     `json:"revoked"`
	Token     string   `json:"token"` // only populated by the rotate response
}

func (t *TokenInfo) HasScope(s string) bool {
	for _, got := range t.Scopes {
		if got == s {
			return true
		}
	}
	return false
}

func httpClient(skipTLS bool) *http.Client {
	// Single TLS-skip call site lives in internal/observed — see Step 0.
	return &http.Client{Timeout: 30 * time.Second, Transport: observed.BaseTransport(skipTLS)}
}

func doJSON(ctx context.Context, method, endpoint, token string, skipTLS bool) (*TokenInfo, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	resp, err := httpClient(skipTLS).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s %s: HTTP %d", method, endpoint, resp.StatusCode)
	}
	var info TokenInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	return &info, nil
}

func apiBase(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/api/v4"
}

func GetSelfToken(ctx context.Context, baseURL, token string, skipTLS bool) (*TokenInfo, error) {
	return doJSON(ctx, http.MethodGet, apiBase(baseURL)+"/personal_access_tokens/self", token, skipTLS)
}

// RotateSelfToken revokes the current token server-side and returns its
// replacement. The secret is returned exactly once — the caller must persist it
// before doing anything else.
func RotateSelfToken(ctx context.Context, baseURL, token, expiresAt string, skipTLS bool) (*TokenInfo, string, error) {
	endpoint := apiBase(baseURL) + "/personal_access_tokens/self/rotate"
	if expiresAt != "" {
		endpoint += "?" + url.Values{"expires_at": {expiresAt}}.Encode()
	}
	info, err := doJSON(ctx, http.MethodPost, endpoint, token, skipTLS)
	if err != nil {
		return nil, "", err
	}
	if info.Token == "" {
		return nil, "", fmt.Errorf("rotate succeeded but response carried no token")
	}
	return info, info.Token, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/rotation/ -run 'TestRotateSelfToken|TestGetSelfToken' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rotation/gitlab.go internal/rotation/gitlab_test.go
git commit -m "feat(rotation): GitLab self and self/rotate API calls"
```

---

### Task 5: Journal and state extensions

**Files:**
- Modify: `internal/journal/journal.go` (constant blocks)
- Modify: `internal/state/store.go` (schema + accessors)
- Test: `internal/state/store_rotation_test.go`

**Interfaces:**
- Produces: `journal.OpRotate Op = "rotate"`, `journal.EntityToken Entity = "token"`, `journal.EntitySSHKey Entity = "sshkey"`, `(*state.Store).SetLastRotated(host string, at time.Time) error`, `(*state.Store).LastRotated(host string) (time.Time, bool, error)`.

- [ ] **Step 1: Write the failing test**

```go
package state

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLastRotated_RoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.LastRotated("gitlab.example.com"); err != nil || ok {
		t.Fatalf("expected no record; ok=%v err=%v", ok, err)
	}
	want := time.Now().UTC().Truncate(time.Second)
	if err := s.SetLastRotated("gitlab.example.com", want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.LastRotated("gitlab.example.com")
	if err != nil || !ok {
		t.Fatalf("expected record; ok=%v err=%v", ok, err)
	}
	if !got.Equal(want) {
		t.Errorf("got %v want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/state/ -run TestLastRotated -v`
Expected: FAIL — `s.LastRotated undefined`

- [ ] **Step 3: Write minimal implementation**

Add to the schema bootstrap in `internal/state/store.go` (alongside the existing `CREATE TABLE` statements):

```sql
CREATE TABLE IF NOT EXISTS rotation_state (
    host            TEXT PRIMARY KEY,
    last_rotated_at TEXT NOT NULL
);
```

Then append to `store.go`:

```go
// SetLastRotated records a successful rotation for host.
func (s *Store) SetLastRotated(host string, at time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO rotation_state (host, last_rotated_at) VALUES (?, ?)
		 ON CONFLICT(host) DO UPDATE SET last_rotated_at = excluded.last_rotated_at`,
		host, at.UTC().Format(time.RFC3339))
	return err
}

// LastRotated returns the last successful rotation for host. ok is false when
// the host has never been rotated.
func (s *Store) LastRotated(host string) (time.Time, bool, error) {
	var raw string
	err := s.db.QueryRow(`SELECT last_rotated_at FROM rotation_state WHERE host = ?`, host).Scan(&raw)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false, err
	}
	return t, true, nil
}
```

In `internal/journal/journal.go`, add to the existing const blocks:

```go
	OpRotate Op = "rotate"
```
```go
	EntityToken  Entity = "token"
	EntitySSHKey Entity = "sshkey"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/state/ ./internal/journal/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/state/store.go internal/state/store_rotation_test.go internal/journal/journal.go
git commit -m "feat(state): rotation_state table; journal rotate op and credential entities"
```

---

### Task 6: The rotation state machine

**Files:**
- Create: `internal/rotation/rotate.go`
- Test: `internal/rotation/rotate_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–5.
- Produces: `Deps{Now func() time.Time, GetSelf, Rotate, VerifyToken func(...), SetToken, Digest, Journal, Store}` (see code), `Rotator{Deps}`, `(*Rotator).Rotate(ctx context.Context, host string, opt Options) (*Result, error)`, `(*Rotator).RecoverIn(ctx context.Context, dir, configPath string) ([]*Result, error)`, `Options{Force bool, EscrowDir, ConfigPath string, ExpiryDays int}`, `Result{Host string, Phase string, Rotated bool, NewTokenID int}`.

Dependencies are injected as function fields so the fault-injection test can fail any phase without a network.

- [ ] **Step 1: Write the failing fault-injection test**

This is the test that earns the design.

```go
package rotation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestRotator(t *testing.T, failAt string) (*Rotator, string, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(cfgPath, []byte(twoHostFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	escrowDir := filepath.Join(dir, "escrow")

	fail := func(phase string) error {
		if phase == failAt {
			return errors.New("injected failure at " + phase)
		}
		return nil
	}
	r := &Rotator{Deps: Deps{
		Now: func() time.Time { return time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC) },
		GetSelf: func(ctx context.Context, host string) (*TokenInfo, error) {
			if err := fail("preflight"); err != nil {
				return nil, err
			}
			return &TokenInfo{ID: 51629, Scopes: []string{"api", "self_rotate"}, Active: true}, nil
		},
		Rotate: func(ctx context.Context, host, expiresAt string) (*TokenInfo, string, error) {
			if err := fail("rotate"); err != nil {
				return nil, "", err
			}
			return &TokenInfo{ID: 51630, ExpiresAt: expiresAt, Scopes: []string{"api", "self_rotate"}}, "glpat-NEW", nil
		},
		VerifyToken: func(ctx context.Context, host, token string) error { return fail("verify") },
		SetToken: func(path, host, token string) error {
			if err := fail("commit"); err != nil {
				return err
			}
			return SetHostToken(path, host, token)
		},
		Digest:  FileDigest,
		Journal: func(host string, tokenID int, ok bool) error { return fail("journal") },
		Store:   func(host string, at time.Time) error { return fail("store") },
	}}
	return r, cfgPath, escrowDir
}

// After any failure past the rotate call, the secret must be on disk and
// Recover must converge to a committed config.
func TestRotate_RecoversFromEveryPostRotateFailure(t *testing.T) {
	for _, phase := range []string{"verify", "commit", "journal", "store"} {
		t.Run(phase, func(t *testing.T) {
			r, cfgPath, escrowDir := newTestRotator(t, phase)
			opt := Options{EscrowDir: escrowDir, ConfigPath: cfgPath, ExpiryDays: 65, Force: true}

			if _, err := r.Rotate(context.Background(), "gitlab.example.com", opt); err == nil {
				t.Fatalf("expected failure at %s", phase)
			}
			escrows, err := ListEscrows(escrowDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(escrows) != 1 {
				t.Fatalf("secret not escrowed: got %d records", len(escrows))
			}
			if escrows[0].NewToken != "glpat-NEW" {
				t.Fatalf("escrow lost the secret: %+v", escrows[0])
			}

			// A clean rotator (nothing failing) must finish the job.
			healthy, _, _ := newTestRotator(t, "")
			healthy.Deps.Journal = func(string, int, bool) error { return nil }
			healthy.Deps.Store = func(string, time.Time) error { return nil }
			results, err := healthy.RecoverIn(context.Background(), escrowDir, cfgPath)
			if err != nil {
				t.Fatalf("Recover: %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("expected 1 recovery, got %d", len(results))
			}
			b, _ := os.ReadFile(cfgPath)
			if !contains(string(b), "token: glpat-NEW") {
				t.Errorf("config not committed after recovery:\n%s", b)
			}
			left, _ := ListEscrows(escrowDir)
			if len(left) != 0 {
				t.Errorf("escrow not cleaned up: %d left", len(left))
			}
		})
	}
}

// Preflight failures must not create an escrow or touch config.
func TestRotate_PreflightFailureIsANoOp(t *testing.T) {
	r, cfgPath, escrowDir := newTestRotator(t, "preflight")
	before, _ := os.ReadFile(cfgPath)
	if _, err := r.Rotate(context.Background(), "gitlab.example.com",
		Options{EscrowDir: escrowDir, ConfigPath: cfgPath, ExpiryDays: 65, Force: true}); err == nil {
		t.Fatal("expected preflight failure")
	}
	if e, _ := ListEscrows(escrowDir); len(e) != 0 {
		t.Errorf("preflight must not escrow, got %d", len(e))
	}
	after, _ := os.ReadFile(cfgPath)
	if string(before) != string(after) {
		t.Error("preflight must not modify config")
	}
}

func contains(hay, needle string) bool { return len(hay) >= len(needle) && (func() bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
})() }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rotation/ -run TestRotate_ -v`
Expected: FAIL — `undefined: Rotator`

- [ ] **Step 3: Write minimal implementation**

```go
package rotation

import (
	"context"
	"fmt"
	"time"
)

type Deps struct {
	Now         func() time.Time
	GetSelf     func(ctx context.Context, host string) (*TokenInfo, error)
	Rotate      func(ctx context.Context, host, expiresAt string) (*TokenInfo, string, error)
	VerifyToken func(ctx context.Context, host, token string) error
	SetToken    func(path, host, token string) error
	Digest      func(path string) (string, error)
	Journal     func(host string, tokenID int, ok bool) error
	Store       func(host string, at time.Time) error
}

type Options struct {
	Force      bool
	EscrowDir  string
	ConfigPath string
	ExpiryDays int
}

type Result struct {
	Host       string
	Phase      string
	Rotated    bool
	NewTokenID int
}

type Rotator struct{ Deps Deps }

// Rotate runs the five phases for one host. Everything after the Rotate call is
// recoverable because the secret is escrowed first.
func (r *Rotator) Rotate(ctx context.Context, host string, opt Options) (*Result, error) {
	res := &Result{Host: host, Phase: "preflight"}

	// Phase 0 — preflight. Mutates nothing.
	existing, err := ListEscrows(opt.EscrowDir)
	if err != nil {
		return res, err
	}
	for _, e := range existing {
		if e.Host == host {
			return res, fmt.Errorf("uncommitted escrow for %s at %s; run `ggvalet rotate --recover` first", host, e.Path)
		}
	}
	info, err := r.Deps.GetSelf(ctx, host)
	if err != nil {
		return res, fmt.Errorf("preflight %s: %w", host, err)
	}
	if !info.Active || info.Revoked {
		return res, fmt.Errorf("preflight %s: token inactive or revoked", host)
	}
	if !info.HasScope("self_rotate") {
		return res, fmt.Errorf("preflight %s: token lacks self_rotate scope", host)
	}
	digest, err := r.Deps.Digest(opt.ConfigPath)
	if err != nil {
		return res, fmt.Errorf("preflight %s: %w", host, err)
	}

	// Phase 1 — rotate, then escrow immediately. Nothing else happens between.
	expiresAt := r.Deps.Now().AddDate(0, 0, opt.ExpiryDays).Format("2006-01-02")
	res.Phase = "rotate"
	newInfo, secret, err := r.Deps.Rotate(ctx, host, expiresAt)
	if err != nil {
		return res, fmt.Errorf("rotate %s: %w", host, err)
	}
	esc := &Escrow{
		Version: 1, Host: host, State: StateRotated,
		RotatedAt:  r.Deps.Now().UTC().Format(time.RFC3339),
		OldTokenID: info.ID, NewTokenID: newInfo.ID, NewToken: secret,
		ExpiresAt: newInfo.ExpiresAt, Scopes: newInfo.Scopes,
		ConfigPath: opt.ConfigPath, ConfigSHA256: digest,
	}
	if _, err := WriteEscrow(opt.EscrowDir, esc); err != nil {
		return res, fmt.Errorf("CRITICAL: rotated %s but could not escrow the secret: %w", host, err)
	}
	res.NewTokenID = newInfo.ID
	res.Rotated = true

	return r.finish(ctx, esc, res)
}

// finish runs verify → commit → record for an escrowed secret. It is shared by
// Rotate and Recover, which is what makes recovery idempotent.
func (r *Rotator) finish(ctx context.Context, esc *Escrow, res *Result) (*Result, error) {
	res.Phase = "verify"
	if err := r.Deps.VerifyToken(ctx, esc.Host, esc.NewToken); err != nil {
		return res, fmt.Errorf("verify %s: %w", esc.Host, err)
	}

	res.Phase = "commit"
	current, err := r.Deps.Digest(esc.ConfigPath)
	if err != nil {
		return res, err
	}
	if current != esc.ConfigSHA256 && !r.configAlreadyHasToken(esc) {
		return res, fmt.Errorf("commit %s: config.yml changed since preflight; refusing to merge", esc.Host)
	}
	if !r.configAlreadyHasToken(esc) {
		if err := r.Deps.SetToken(esc.ConfigPath, esc.Host, esc.NewToken); err != nil {
			return res, fmt.Errorf("commit %s: %w", esc.Host, err)
		}
	}

	res.Phase = "record"
	if err := r.Deps.Journal(esc.Host, esc.NewTokenID, true); err != nil {
		return res, fmt.Errorf("journal %s: %w", esc.Host, err)
	}
	if err := r.Deps.Store(esc.Host, r.Deps.Now()); err != nil {
		return res, fmt.Errorf("state %s: %w", esc.Host, err)
	}
	if esc.Path != "" {
		if err := DeleteEscrow(esc.Path); err != nil {
			return res, fmt.Errorf("escrow cleanup %s: %w", esc.Host, err)
		}
	}
	res.Phase = "done"
	return res, nil
}

func (r *Rotator) configAlreadyHasToken(esc *Escrow) bool {
	b, err := readFileString(esc.ConfigPath)
	if err != nil {
		return false
	}
	return containsToken(b, esc.NewToken)
}

// RecoverIn replays every uncommitted escrow found in dir.
func (r *Rotator) RecoverIn(ctx context.Context, dir, configPath string) ([]*Result, error) {
	escrows, err := ListEscrows(dir)
	if err != nil {
		return nil, err
	}
	var out []*Result
	for _, e := range escrows {
		if e.ConfigPath == "" {
			e.ConfigPath = configPath
		}
		res, err := r.finish(ctx, e, &Result{Host: e.Host, Rotated: true, NewTokenID: e.NewTokenID})
		if err != nil {
			return out, err
		}
		out = append(out, res)
	}
	return out, nil
}
```

Add the two small helpers at the bottom of `configwrite.go`:

```go
func readFileString(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

func containsToken(haystack, token string) bool {
	return strings.Contains(haystack, "token: "+token)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rotation/ -run TestRotate_ -v`
Expected: PASS — all four fault-injection subtests plus the preflight no-op

- [ ] **Step 5: Commit**

```bash
git add internal/rotation/rotate.go internal/rotation/rotate_test.go internal/rotation/configwrite.go
git commit -m "feat(rotation): five-phase state machine with recoverable escrow"
```

---

### Task 7: `ggvalet rotate` command

**Files:**
- Create: `cmd/rotate.go`
- Test: `cmd/rotate_test.go`
- Modify: `cmd/root.go` (register in the existing `rootCmd.AddCommand(...)` call)
- Modify: `cmd/hostguard.go` (add guarded leaves)

**Interfaces:**
- Consumes: `rotation.Rotator`, `rotation.Load`, `rotation.Propose`, `config.Load`, `loadHostsForDisplay()` from `cmd/root.go`.
- Produces: `rotateCmd() *cobra.Command`.

- [ ] **Step 1: Write the failing test**

```go
package cmd

import (
	"strings"
	"testing"
)

func TestRotateCmd_Registered(t *testing.T) {
	c := rotateCmd()
	if c.Use != "rotate" {
		t.Fatalf("Use = %q", c.Use)
	}
	for _, f := range []string{"check", "force", "recover"} {
		if c.Flags().Lookup(f) == nil {
			t.Errorf("missing --%s flag", f)
		}
	}
}

func TestRotate_IsNotHostNeutral(t *testing.T) {
	for _, p := range hostNeutralPrefixes {
		if strings.HasPrefix(p, "ggvalet rotate") || strings.HasPrefix(p, "ggvalet ssh") {
			t.Errorf("%q must not be host-neutral: it issues host-specific API calls", p)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run 'TestRotateCmd_Registered|TestRotate_IsNotHostNeutral' -v`
Expected: FAIL — `undefined: rotateCmd`

- [ ] **Step 3: Write minimal implementation**

```go
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

func rotateCmd() *cobra.Command {
	var check, force, recover bool
	c := &cobra.Command{
		Use:   "rotate",
		Short: "Rotate personal access tokens on configured hosts",
		Long: "Rotates PATs using GitLab's self/rotate endpoint. The new secret is " +
			"written to a 0600 escrow file and fsynced before anything else, so an " +
			"interrupted run is recoverable with --recover.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRotate(cmd.Context(), check, force, recover)
		},
	}
	c.Flags().BoolVar(&check, "check", false, "report what would happen; exit 1 if anything is due or broken")
	c.Flags().BoolVar(&force, "force", false, "ignore cadence")
	c.Flags().BoolVar(&recover, "recover", false, "replay an uncommitted escrow")
	return c
}

func ggvaletHome() string {
	if v := os.Getenv("GLVALET_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ggvalet")
}

func rotationConfigPath() string { return filepath.Join(ggvaletHome(), "rotation.yaml") }
func escrowDir() string          { return filepath.Join(ggvaletHome(), "escrow") }
```

`runRotate` wires the pieces: load `rotation.yaml` (or propose one and refuse), build a `rotation.Rotator` whose `Deps` call `rotation.GetSelfToken` / `rotation.RotateSelfToken` / `rotation.SetHostToken` / `journal.Record` / `store.SetLastRotated`, then iterate hosts declaring `pat`.

```go
func runRotate(ctx context.Context, check, force, doRecover bool) error {
	hosts, _, _, err := loadHostsForDisplay()
	if err != nil {
		return err
	}
	rcPath := rotationConfigPath()
	rc, err := rotation.Load(rcPath)
	if os.IsNotExist(err) {
		proposed := rotation.Propose(hosts)
		if err := rotation.Save(rcPath, proposed); err != nil {
			return err
		}
		return fmt.Errorf("wrote a proposed profile to %s — review it, then re-run", rcPath)
	}
	if err != nil {
		return err
	}

	r := newRotator()
	if doRecover {
		results, err := r.RecoverIn(ctx, escrowDir(), glabConfigPath())
		for _, res := range results {
			fmt.Printf("recovered %s (token %d)\n", res.Host, res.NewTokenID)
		}
		return err
	}

	var problems int
	for host := range rc.Hosts {
		if !rc.Uses(host, "pat") {
			continue
		}
		due, reason := rotationDue(host, rc, force)
		if check {
			fmt.Printf("%-34s due=%v  %s\n", host, due, reason)
			if due {
				problems++
			}
			continue
		}
		if !due {
			continue
		}
		if _, err := r.Rotate(ctx, host, rotation.Options{
			EscrowDir:  escrowDir(),
			ConfigPath: glabConfigPath(),
			ExpiryDays: rc.Defaults.PATExpiryDays,
			Force:      force,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "rotate %s: %v\n", host, err)
			problems++
		}
	}
	if problems > 0 {
		return fmt.Errorf("%d host(s) need attention", problems)
	}
	return nil
}
```

Implement `newRotator()`, `rotationDue(host string, rc *rotation.Config, force bool) (bool, string)` (compares `store.LastRotated(host)` against `rc.Defaults.CadenceDays`, returning `true, "never rotated"` when absent), and `glabConfigPath()` (the path `internal/config` already resolves) in the same file.

Register the command in the existing `rootCmd.AddCommand(...)` list in `cmd/root.go`:

```go
		rotateCmd(),
		sshCmd(),
```

In `cmd/hostguard.go`, add to `hostBlockedLeaves` (the existing map/slice of host-specific leaves):

```go
	"ggvalet rotate",
	"ggvalet ssh audit",
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -run 'TestRotateCmd_Registered|TestRotate_IsNotHostNeutral' -v && go build ./...`
Expected: PASS and a clean build

- [ ] **Step 5: Commit**

```bash
git add cmd/rotate.go cmd/rotate_test.go cmd/root.go cmd/hostguard.go
git commit -m "feat(cmd): ggvalet rotate with check, force and recover"
```

---

### Task 8: SSH audit

**Files:**
- Create: `internal/sshaudit/audit.go`
- Test: `internal/sshaudit/audit_test.go`
- Create: `cmd/ssh.go`

**Interfaces:**
- Produces: `Finding{Class, Host, Path, Fingerprint, Detail string, Managed bool}`, classes `ClassMatched`/`ClassDanglingRef`/`ClassCorruptName`/`ClassOrphanLocal`/`ClassOrphanRemote`/`ClassExpiring`, `ParseSSHConfig(path string) ([]ConfigEntry, error)` where `ConfigEntry{Host, IdentityFile string}`, `Audit(entries []ConfigEntry, keyDir string, managed map[string]bool) ([]Finding, error)`, and `sshCmd() *cobra.Command`.

- [ ] **Step 1: Write the failing test**

```go
package sshaudit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAudit_ClassifiesRealWorldDrift(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "GOOD_KEY")
	if err := os.WriteFile(good+".pub", []byte("ssh-ed25519 AAAAC3Nz good@host\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(good, []byte("PRIVATE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A filename carrying a literal space+tab, as observed in the real estate.
	corrupt := filepath.Join(dir, "CORRUPT_KEY \t")
	if err := os.WriteFile(corrupt, []byte("PRIVATE\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	entries := []ConfigEntry{
		{Host: "gitlab.example.com", IdentityFile: good},
		{Host: "gitlab.com", IdentityFile: filepath.Join(dir, "MISSING_KEY")},
	}
	managed := map[string]bool{"gitlab.example.com": true}

	found, err := Audit(entries, dir, managed)
	if err != nil {
		t.Fatal(err)
	}

	byClass := map[string]Finding{}
	for _, f := range found {
		byClass[f.Class] = f
	}
	if _, ok := byClass[ClassDanglingRef]; !ok {
		t.Error("expected a DANGLING_REF for the missing key")
	}
	if _, ok := byClass[ClassCorruptName]; !ok {
		t.Error("expected a CORRUPT_NAME for the space+tab filename")
	}
	if f := byClass[ClassDanglingRef]; f.Managed {
		t.Error("gitlab.com is unmanaged; its drift must not be marked managed")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sshaudit/ -v`
Expected: FAIL — `undefined: Audit`

- [ ] **Step 3: Write minimal implementation**

```go
// Package sshaudit reports drift between SSH key files, ssh_config references,
// and keys registered on remote hosts. It never writes to ~/.ssh.
package sshaudit

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

const (
	ClassMatched     = "MATCHED"
	ClassDanglingRef = "DANGLING_REF"
	ClassCorruptName = "CORRUPT_NAME"
	ClassOrphanLocal = "ORPHAN_LOCAL"
	ClassOrphanRemote = "ORPHAN_REMOTE"
	ClassExpiring    = "EXPIRING"
)

type ConfigEntry struct {
	Host         string
	IdentityFile string
}

type Finding struct {
	Class       string `json:"class"`
	Host        string `json:"host,omitempty"`
	Path        string `json:"path,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Detail      string `json:"detail"`
	Managed     bool   `json:"managed"`
}

// ParseSSHConfig extracts Host blocks and their IdentityFile references.
func ParseSSHConfig(path string) ([]ConfigEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []ConfigEntry
	current := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "host":
			current = fields[1]
		case "identityfile":
			out = append(out, ConfigEntry{Host: current, IdentityFile: expandHome(fields[1])})
		}
	}
	return out, sc.Err()
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

func hasCorruptName(name string) bool {
	return strings.ContainsAny(name, " \t\r\n")
}

// Audit classifies drift. managed marks which hosts ggvalet is responsible for;
// only findings on those hosts should fail a caller's exit code.
func Audit(entries []ConfigEntry, keyDir string, managed map[string]bool) ([]Finding, error) {
	var out []Finding

	for _, e := range entries {
		if _, err := os.Stat(e.IdentityFile); os.IsNotExist(err) {
			out = append(out, Finding{
				Class: ClassDanglingRef, Host: e.Host, Path: e.IdentityFile,
				Detail:  "ssh_config references a file that does not exist",
				Managed: managed[e.Host],
			})
			continue
		}
		out = append(out, Finding{
			Class: ClassMatched, Host: e.Host, Path: e.IdentityFile,
			Detail: "key file present and referenced", Managed: managed[e.Host],
		})
	}

	files, err := os.ReadDir(keyDir)
	if err != nil {
		return out, err
	}
	for _, fi := range files {
		if fi.IsDir() || !hasCorruptName(fi.Name()) {
			continue
		}
		out = append(out, Finding{
			Class: ClassCorruptName, Path: filepath.Join(keyDir, fi.Name()),
			Detail: "filename contains whitespace or control characters", Managed: false,
		})
	}
	return out, nil
}
```

`cmd/ssh.go` adds the `ssh` group with an `audit` subcommand that calls `ParseSSHConfig(~/.ssh/config)`, builds `managed` from `rotation.Config.Uses(host, "ssh")`, prints a table (or `--json`), and returns a non-nil error when any finding has `Managed == true` and `Class != ClassMatched`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sshaudit/ ./cmd/ -v && go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sshaudit cmd/ssh.go
git commit -m "feat(sshaudit): read-only SSH drift inventory with managed scoping"
```

---

### Task 9: `doctor` probes every host

**Files:**
- Modify: `cmd/doctor.go` (`runDoctor`)
- Test: `cmd/doctor_test.go`

**Interfaces:**
- Consumes: `loadHostsForDisplay()`, `config.ForHost`, existing `probeProvider`.

- [ ] **Step 1: Write the failing test**

```go
func TestDoctor_ReportsEveryConfiguredHost(t *testing.T) {
	hosts := map[string]*config.HostConfig{
		"a.example.com": {Token: "x"},
		"b.example.com": {Token: "y"},
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestDoctor_ReportsEveryConfiguredHost -v`
Expected: FAIL — `undefined: doctorHostLines`

- [ ] **Step 3: Write minimal implementation**

Extract the per-host probe into a testable helper and call it from `runDoctor` for every host:

```go
// doctorHostLines probes each configured host and returns one report line per
// host. A failing non-default host is reported, never skipped — the previous
// single-host probe is why a dead second host stayed invisible.
func doctorHostLines(hosts map[string]*config.HostConfig, probe func(host string) error) []string {
	names := make([]string, 0, len(hosts))
	for h := range hosts {
		names = append(names, h)
	}
	sort.Strings(names)

	lines := make([]string, 0, len(names))
	for _, h := range names {
		if err := probe(h); err != nil {
			lines = append(lines, fmt.Sprintf("✗ %s: %v", h, err))
			continue
		}
		lines = append(lines, fmt.Sprintf("✓ %s: reachable", h))
	}
	return lines
}
```

In `runDoctor`, replace the single-host probe with a call to `doctorHostLines`, print each line, and return a non-nil error if any line starts with `✗`. Keep `--host` working by filtering `hosts` to one entry when `hostFlag != ""`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/doctor.go cmd/doctor_test.go
git commit -m "fix(doctor): probe every configured host, not just the default"
```

---

### Task 10: launchd agent and operator docs

**Files:**
- Create: `deploy/launchd/com.mchorfa.ggvalet-rotate.plist`
- Create: `docs/rotation.md`
- Modify: `README.md` (add `rotate` and `ssh audit` to the command list)

- [ ] **Step 1: Write the plist**

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.mchorfa.ggvalet-rotate</string>

    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/ggvalet</string>
        <string>rotate</string>
    </array>

    <!-- 1st of each month at 04:00. launchd coalesces missed calendar events,
         so a machine asleep on the 1st rotates at next wake. -->
    <key>StartCalendarInterval</key>
    <dict>
        <key>Day</key><integer>1</integer>
        <key>Hour</key><integer>4</integer>
        <key>Minute</key><integer>0</integer>
    </dict>

    <key>ProcessType</key><string>Background</string>
    <key>LowPriorityIO</key><true/>
    <key>StandardOutPath</key><string>/Users/mchorfa/.local/log/ggvalet-rotate.log</string>
    <key>StandardErrorPath</key><string>/Users/mchorfa/.local/log/ggvalet-rotate.log</string>
</dict>
</plist>
```

- [ ] **Step 2: Validate and install**

```bash
plutil -lint deploy/launchd/com.mchorfa.ggvalet-rotate.plist
cp deploy/launchd/com.mchorfa.ggvalet-rotate.plist ~/Library/LaunchAgents/
launchctl load ~/Library/LaunchAgents/com.mchorfa.ggvalet-rotate.plist
launchctl list | grep ggvalet-rotate
```
Expected: `OK`, then a line showing the agent loaded.

- [ ] **Step 3: Write `docs/rotation.md`**

Cover: what `rotation.yaml` means, the five phases, what to do when `--recover` is needed, and the one unrecoverable window (rotate response lost before escrow fsync → create a PAT in the GitLab UI and paste it into `config.yml`). State the rollout order: `--check` first, then one watched `--force`, then load the agent.

- [ ] **Step 4: Run the full suite**

```bash
go test ./... && go build ./...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add deploy/launchd docs/rotation.md README.md
git commit -m "feat(ops): monthly launchd agent and rotation runbook"
```

---

## Rollout (after all tasks)

1. `ggvalet ssh audit` — expect 3 `DANGLING_REF` (unmanaged: gitlab.com, GitHub) and 1 `CORRUPT_NAME`. Exit 0, because none is on a managed host.
2. `ggvalet doctor` — expect `✗ sc01-trt.thales-systems.ca: 401`. Fix that credential before step 3.
3. `ggvalet rotate --check` — writes a proposed `rotation.yaml` on first run. Edit it so `gitlab.thalesdigital.io` declares `[pat, ssh]` and `sc01-trt.thales-systems.ca` declares `[pat]`.
4. One watched `ggvalet rotate --force --host gitlab.thalesdigital.io`. Confirm `glab api /version` still works afterwards.
5. Load the launchd agent.
