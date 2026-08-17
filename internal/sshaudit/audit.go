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
	ClassMatched      = "MATCHED"
	ClassDanglingRef  = "DANGLING_REF"
	ClassCorruptName  = "CORRUPT_NAME"
	ClassOrphanLocal  = "ORPHAN_LOCAL"
	ClassOrphanRemote = "ORPHAN_REMOTE"
	ClassExpiring     = "EXPIRING"
)

// ConfigEntry is one Host block's IdentityFile reference, as found in an
// ssh_config file.
type ConfigEntry struct {
	Host         string
	IdentityFile string
}

// Finding is one unit of drift (or confirmed match) the audit reports.
// Managed marks whether ggvalet is responsible for the host the finding
// concerns — only managed drift should fail a caller's exit code.
type Finding struct {
	Class       string `json:"class"`
	Host        string `json:"host,omitempty"`
	Path        string `json:"path,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Detail      string `json:"detail"`
	Managed     bool   `json:"managed"`
}

// ParseSSHConfig extracts Host blocks and their IdentityFile references from
// an ssh_config file. It only reads the file; it never writes to it.
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

// Audit classifies drift between ssh_config references and the key files on
// disk. managed marks which hosts ggvalet is responsible for; only findings
// on those hosts should fail a caller's exit code. Audit only reads keyDir
// and the files entries point at — it never creates, replaces, or deletes a
// key.
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
