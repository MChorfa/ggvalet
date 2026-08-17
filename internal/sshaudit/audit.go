// Package sshaudit reports drift between SSH key files and the ssh_config
// references that point at them. It never writes to ~/.ssh.
//
// Scope: this is a local inventory. It does not list keys registered on a
// remote host, and it does not fingerprint key material, so it cannot report
// a local key absent from the remote or vice versa. Spec §9 describes both;
// they are deferred past v1 rather than half-built, because a class that is
// declared but never produced reads to a consumer as "checked, nothing found".
package sshaudit

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// The classes Audit can actually produce. ORPHAN_LOCAL, ORPHAN_REMOTE and
// EXPIRING were declared here before anything emitted them; they are omitted
// until the remote key listing that would produce them exists.
const (
	ClassMatched     = "MATCHED"
	ClassDanglingRef = "DANGLING_REF"
	ClassCorruptName = "CORRUPT_NAME"
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
// Fingerprint is deliberately absent: nothing here reads key material, so a
// fingerprint field would be present in the --json contract and never
// populated.
type Finding struct {
	Class   string `json:"class"`
	Host    string `json:"host,omitempty"`
	Path    string `json:"path,omitempty"`
	Detail  string `json:"detail"`
	Managed bool   `json:"managed"`
}

// ParseSSHConfig extracts Host blocks and their IdentityFile references from
// an ssh_config file. It only reads the file; it never writes to it.
func ParseSSHConfig(path string) ([]ConfigEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// A block is buffered rather than emitted line by line because HostName may
	// appear after IdentityFile. The Host token is an alias — `Host gitlab-work`
	// — while callers match findings against real GitLab hostnames from
	// rotation.yaml. Reporting the alias marks every genuine finding unmanaged
	// (so it cannot fail the exit code, which is the point of managed) while a
	// missing-config finding is manufactured for the real hostname and does
	// fail it: wrong in both directions at once. Resolve HostName when the
	// block declares one, and fall back to the alias when it does not.
	var out []ConfigEntry
	alias, hostName := "", ""
	var identities []string
	flush := func() {
		name := hostName
		if name == "" {
			name = alias
		}
		for _, id := range identities {
			out = append(out, ConfigEntry{Host: name, IdentityFile: id})
		}
		identities = nil
	}

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
			flush()
			alias, hostName = fields[1], ""
		case "hostname":
			hostName = fields[1]
		case "identityfile":
			identities = append(identities, expandHome(fields[1]))
		}
	}
	flush()
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
