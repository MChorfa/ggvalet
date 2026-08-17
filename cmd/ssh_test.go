package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MChorfa/ggvalet/internal/rotation"
	"github.com/MChorfa/ggvalet/internal/sshaudit"
)

func TestSSHCmd_Registered(t *testing.T) {
	c := sshCmd()
	if c.Use != "ssh" {
		t.Fatalf("Use = %q", c.Use)
	}
	found := false
	for _, sub := range c.Commands() {
		if sub.Use == "audit" {
			found = true
			if sub.Flags().Lookup("json") == nil {
				t.Error("audit subcommand missing --json flag")
			}
		}
	}
	if !found {
		t.Fatal("ssh command has no audit subcommand")
	}
}

// writeRotationYAML writes a minimal rotation.Config to path.
func writeRotationYAML(t *testing.T, path string, hosts map[string]rotation.Profile) {
	t.Helper()
	if err := rotation.Save(path, &rotation.Config{
		Version: 1,
		Hosts:   hosts,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// sshFixture lays out an ssh_config + key dir + rotation.yaml under one temp
// dir, and returns their paths for sshAuditRun.
type sshFixture struct {
	dir          string
	configPath   string
	keyDir       string
	rotationPath string
	out, errOut  *bytes.Buffer
}

func newSSHFixture(t *testing.T) *sshFixture {
	t.Helper()
	dir := t.TempDir()
	keyDir := filepath.Join(dir, "sshkeys")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return &sshFixture{
		dir:          dir,
		configPath:   filepath.Join(dir, "config"),
		keyDir:       keyDir,
		rotationPath: filepath.Join(dir, "rotation.yaml"),
		out:          &bytes.Buffer{},
		errOut:       &bytes.Buffer{},
	}
}

func (f *sshFixture) writeConfig(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(f.configPath, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func (f *sshFixture) writeKey(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(f.keyDir, name)
	if err := os.WriteFile(p, []byte("PRIVATE\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

func (f *sshFixture) run(t *testing.T, asJSON bool) error {
	t.Helper()
	return sshAuditRun(f.out, f.errOut, f.configPath, f.keyDir, f.rotationPath, asJSON)
}

// ─── the four operator-visible cases ─────────────────────────────────────────

// Case: key present, referenced, host managed by rotation.yaml for ssh — the
// quiet, healthy case.
func TestSSHAuditRun_KeyPresentAndManagedPasses(t *testing.T) {
	f := newSSHFixture(t)
	key := f.writeKey(t, "id_managed")
	f.writeConfig(t, "Host gitlab.example.com\n  IdentityFile "+key+"\n")
	writeRotationYAML(t, f.rotationPath, map[string]rotation.Profile{
		"gitlab.example.com": {Credentials: []string{"ssh"}},
	})

	if err := f.run(t, false); err != nil {
		t.Fatalf("a matched, managed key must not fail: %v", err)
	}
	if !strings.Contains(f.out.String(), sshaudit.ClassMatched) {
		t.Fatalf("want a MATCHED line:\n%s", f.out.String())
	}
}

// Case: rotation.yaml declares ssh for a host that has no ssh_config entry at
// all — the gap patHosts() used to leave silent on the PAT side; this command
// must say something instead of nothing.
func TestSSHAuditRun_ManagedHostMissingFromConfigFails(t *testing.T) {
	f := newSSHFixture(t)
	// No ssh_config file at all.
	writeRotationYAML(t, f.rotationPath, map[string]rotation.Profile{
		"gitlab.example.com": {Credentials: []string{"ssh"}},
	})

	err := f.run(t, false)
	if err == nil {
		t.Fatal("a managed host with no ssh_config entry must fail the command")
	}
	printed := f.out.String() + f.errOut.String()
	if !strings.Contains(printed, "gitlab.example.com") {
		t.Fatalf("the host must be named:\n%s", printed)
	}
}

// Case: a host uses PAT only and declares no ssh credential at all — absence
// of SSH must not read as a fault.
func TestSSHAuditRun_HostWithNoSSHIsNotAFault(t *testing.T) {
	f := newSSHFixture(t)
	key := f.writeKey(t, "id_pat_host")
	f.writeConfig(t, "Host gitlab.example.com\n  IdentityFile "+key+"\n")
	writeRotationYAML(t, f.rotationPath, map[string]rotation.Profile{
		"gitlab.example.com": {Credentials: []string{"pat"}}, // no "ssh"
	})

	if err := f.run(t, false); err != nil {
		t.Fatalf("a pat-only host must not fail an ssh audit: %v", err)
	}
	// It is still reported (unmanaged), just not fatal.
	if !strings.Contains(f.out.String(), "gitlab.example.com") {
		t.Fatalf("the unmanaged entry should still be listed:\n%s", f.out.String())
	}
}

// Case: rotation.yaml itself is missing — the audit still runs and reports,
// it just has no managed hosts to scope failures to.
func TestSSHAuditRun_MissingRotationConfigIsNotFatal(t *testing.T) {
	f := newSSHFixture(t)
	key := f.writeKey(t, "id_unmanaged")
	f.writeConfig(t, "Host gitlab.example.com\n  IdentityFile "+key+"\n")
	// rotation.yaml intentionally not written.

	if err := f.run(t, false); err != nil {
		t.Fatalf("a missing rotation.yaml must not fail the audit: %v", err)
	}
	if !strings.Contains(f.errOut.String(), "rotation.yaml") {
		t.Fatalf("the absence should be noted:\n%s", f.errOut.String())
	}
}

// A dangling reference on a managed host must fail; the same drift on an
// unmanaged host must not.
func TestSSHAuditRun_DanglingRefFailsOnlyWhenManaged(t *testing.T) {
	f := newSSHFixture(t)
	f.writeConfig(t,
		"Host managed.example.com\n  IdentityFile "+filepath.Join(f.keyDir, "missing_managed")+"\n"+
			"Host unmanaged.example.com\n  IdentityFile "+filepath.Join(f.keyDir, "missing_unmanaged")+"\n")
	writeRotationYAML(t, f.rotationPath, map[string]rotation.Profile{
		"managed.example.com":   {Credentials: []string{"ssh"}},
		"unmanaged.example.com": {Credentials: []string{"pat"}},
	})

	err := f.run(t, false)
	if err == nil {
		t.Fatal("the managed host's dangling reference must fail the command")
	}
	if !strings.Contains(err.Error(), "1 managed") {
		t.Fatalf("exactly one managed finding should be counted: %v", err)
	}
}

// A corrupt filename under the key dir is reported but never marked managed —
// Audit has no host to attribute it to.
func TestSSHAuditRun_CorruptNameIsReportedNotFatal(t *testing.T) {
	f := newSSHFixture(t)
	if err := os.WriteFile(filepath.Join(f.keyDir, "bad key\t"), []byte("PRIVATE\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := f.run(t, false); err != nil {
		t.Fatalf("an unattributed corrupt name must not fail the command: %v", err)
	}
	if !strings.Contains(f.out.String(), sshaudit.ClassCorruptName) {
		t.Fatalf("want a CORRUPT_NAME line:\n%s", f.out.String())
	}
}

func TestSSHAuditRun_JSONOutputIsValid(t *testing.T) {
	f := newSSHFixture(t)
	key := f.writeKey(t, "id_json")
	f.writeConfig(t, "Host gitlab.example.com\n  IdentityFile "+key+"\n")
	writeRotationYAML(t, f.rotationPath, map[string]rotation.Profile{
		"gitlab.example.com": {Credentials: []string{"ssh"}},
	})

	if err := f.run(t, true); err != nil {
		t.Fatalf("run: %v", err)
	}
	var findings []sshaudit.Finding
	if err := json.Unmarshal(f.out.Bytes(), &findings); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, f.out.String())
	}
	if len(findings) != 1 || findings[0].Class != sshaudit.ClassMatched {
		t.Fatalf("findings = %+v", findings)
	}
}

// Never prints private key contents, even though the fixture key file holds
// literal "PRIVATE" bytes.
func TestSSHAuditRun_NeverPrintsKeyMaterial(t *testing.T) {
	f := newSSHFixture(t)
	key := f.writeKey(t, "id_secret")
	f.writeConfig(t, "Host gitlab.example.com\n  IdentityFile "+key+"\n")
	writeRotationYAML(t, f.rotationPath, map[string]rotation.Profile{
		"gitlab.example.com": {Credentials: []string{"ssh"}},
	})

	_ = f.run(t, false)
	if strings.Contains(f.out.String()+f.errOut.String(), "PRIVATE") {
		t.Fatal("key material reached the operator's output")
	}
}

// ─── path resolution ──────────────────────────────────────────────────────────

func TestSSHConfigPath_HonoursTheOverride(t *testing.T) {
	t.Setenv("GLVALET_SSH_CONFIG", "/somewhere/else/config")
	if got := sshConfigPath(); got != "/somewhere/else/config" {
		t.Fatalf("sshConfigPath() = %q", got)
	}
}

func TestSSHConfigPath_DefaultsUnderTheUserHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GLVALET_SSH_CONFIG", "")
	t.Setenv("HOME", home)
	if got := sshConfigPath(); got != filepath.Join(home, ".ssh", "config") {
		t.Fatalf("sshConfigPath() = %q", got)
	}
}

func TestSSHKeyDir_HonoursTheOverride(t *testing.T) {
	t.Setenv("GLVALET_SSH_DIR", "/somewhere/else/keys")
	if got := sshKeyDir(); got != "/somewhere/else/keys" {
		t.Fatalf("sshKeyDir() = %q", got)
	}
}

func TestSSHKeyDir_DefaultsUnderTheUserHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GLVALET_SSH_DIR", "")
	t.Setenv("HOME", home)
	if got := sshKeyDir(); got != filepath.Join(home, ".ssh") {
		t.Fatalf("sshKeyDir() = %q", got)
	}
}

// runSSHAudit is the thin wrapper Cobra calls; this exercises it end to end
// against a real (empty) HOME so it is not left uncovered by sshAuditRun's
// own tests, which call the parameterised form directly.
func TestRunSSHAudit_WiresRealPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GLVALET_SSH_CONFIG", "")
	t.Setenv("GLVALET_SSH_DIR", "")
	t.Setenv("GLVALET_HOME", filepath.Join(home, ".ggvalet"))

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	// No ssh_config, no rotation.yaml: both absences are reported, neither is
	// fatal.
	if err := runSSHAudit(out, errOut, false); err != nil {
		t.Fatalf("runSSHAudit: %v", err)
	}
	if !strings.Contains(out.String(), "nothing to audit") {
		t.Fatalf("want the no-ssh_config line:\n%s", out.String())
	}
}

// ─── hostguard consistency ───────────────────────────────────────────────────

func TestSSHAudit_IsAnExplicitlyBlockedLeaf(t *testing.T) {
	if !hostBlockedLeaves["ggvalet ssh audit"] {
		t.Fatal(`"ggvalet ssh audit" should be in hostBlockedLeaves — it is scoped by rotation.yaml, a GitLab-specific concept`)
	}
}
