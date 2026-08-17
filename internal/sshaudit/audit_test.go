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
	if f := byClass[ClassMatched]; !f.Managed {
		t.Error("gitlab.example.com is managed; a matched key must say so")
	}
}

func TestParseSSHConfig_ExtractsHostBlocksAndExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	body := "# comment\n" +
		"Host gitlab.example.com\n" +
		"  IdentityFile ~/.ssh/id_ed25519\n" +
		"\n" +
		"Host gitlab.com\n" +
		"  IdentityFile " + filepath.Join(dir, "id_rsa") + "\n" +
		"  User git\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := ParseSSHConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want 2", entries)
	}
	if entries[0].Host != "gitlab.example.com" || entries[0].IdentityFile != filepath.Join(home, ".ssh", "id_ed25519") {
		t.Errorf("entries[0] = %+v", entries[0])
	}
	if entries[1].Host != "gitlab.com" || entries[1].IdentityFile != filepath.Join(dir, "id_rsa") {
		t.Errorf("entries[1] = %+v", entries[1])
	}
}

func TestParseSSHConfig_MissingFileErrors(t *testing.T) {
	if _, err := ParseSSHConfig(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("want an error for a missing ssh_config file")
	}
}

func TestAudit_MissingKeyDirIsAnError(t *testing.T) {
	if _, err := Audit(nil, filepath.Join(t.TempDir(), "nope"), nil); err == nil {
		t.Fatal("want an error when keyDir does not exist")
	}
}
