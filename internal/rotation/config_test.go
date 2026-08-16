package rotation

import (
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
