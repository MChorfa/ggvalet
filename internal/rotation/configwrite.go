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
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	// fsync the directory so the new entry itself survives a crash.
	// The token has already been rotated upstream by the time this runs,
	// so an unsynced write is a lost credential.
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return err
	}
	return nil
}
