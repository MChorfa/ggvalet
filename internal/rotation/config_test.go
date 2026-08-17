package rotation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MChorfa/ggvalet/internal/config"
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

func TestSave_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rotation.yaml")

	// Create a Config with explicit values
	original := &Config{
		Version: 1,
		Defaults: Defaults{
			CadenceDays:   30,
			PATExpiryDays: 65,
		},
		Hosts: map[string]Profile{
			"gitlab.example.com": {Credentials: []string{"pat", "ssh"}},
			"other.example.com":  {Credentials: []string{"pat"}},
		},
	}

	// Save it
	if err := Save(p, original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file permissions are 0600
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode: got %o, want 0600", info.Mode().Perm())
	}

	// Load it back
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Verify all fields round-trip
	if loaded.Version != original.Version {
		t.Errorf("Version: got %d, want %d", loaded.Version, original.Version)
	}
	if loaded.Defaults.CadenceDays != original.Defaults.CadenceDays {
		t.Errorf("CadenceDays: got %d, want %d", loaded.Defaults.CadenceDays, original.Defaults.CadenceDays)
	}
	if loaded.Defaults.PATExpiryDays != original.Defaults.PATExpiryDays {
		t.Errorf("PATExpiryDays: got %d, want %d", loaded.Defaults.PATExpiryDays, original.Defaults.PATExpiryDays)
	}

	// Verify Hosts map round-trips
	if len(loaded.Hosts) != len(original.Hosts) {
		t.Errorf("Hosts count: got %d, want %d", len(loaded.Hosts), len(original.Hosts))
	}
	for host, origProfile := range original.Hosts {
		loadedProfile, ok := loaded.Hosts[host]
		if !ok {
			t.Errorf("host %q missing in loaded config", host)
			continue
		}
		if len(loadedProfile.Credentials) != len(origProfile.Credentials) {
			t.Errorf("host %q credentials count: got %d, want %d", host, len(loadedProfile.Credentials), len(origProfile.Credentials))
			continue
		}
		for i, cred := range origProfile.Credentials {
			if loadedProfile.Credentials[i] != cred {
				t.Errorf("host %q credential[%d]: got %q, want %q", host, i, loadedProfile.Credentials[i], cred)
			}
		}
	}
}
