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

// findHostToken locates the `token:` line belonging to host inside the top-level
// `hosts:` block and reports its line index, its indentation, and the value
// already on it. idx is -1 when host has no token line.
//
// It walks by indentation rather than matching a regex globally, because every
// host block contains a `token:` line. Both the writer and the
// already-committed check go through here: two parsers over the same file would
// be free to disagree, and disagreeing about whether a token is already
// committed is how a secret gets dropped.
func findHostToken(lines []string, host string) (idx, indent int, value string) {
	inHosts, inTarget := false, false
	hostIndent := -1
	// fieldIndent is the indentation of the host block's own fields, learned
	// from the first line inside the block. Matching merely "deeper than the
	// host key" would also match a `token:` nested in a sub-map — glab writes
	// such sub-maps, and they carry their own token field, so a depth-only
	// walker rewrites a nested credential and leaves the host's revoked one in
	// place. -1 means the current block's field level is not yet known.
	fieldIndent := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lineIndent := len(line) - len(strings.TrimLeft(line, " "))

		if lineIndent == 0 {
			inHosts = trimmed == "hosts:"
			inTarget = false
			fieldIndent = -1
			continue
		}
		if !inHosts {
			continue
		}
		// A host key is the first indent level inside `hosts:`.
		if hostIndent == -1 || lineIndent == hostIndent {
			if strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, ": ") {
				hostIndent = lineIndent
				inTarget = strings.TrimSuffix(trimmed, ":") == host
				fieldIndent = -1
				continue
			}
		}
		if !inTarget || lineIndent <= hostIndent {
			continue
		}
		if fieldIndent == -1 {
			fieldIndent = lineIndent
		}
		if lineIndent == fieldIndent && strings.HasPrefix(trimmed, "token:") {
			return i, lineIndent, strings.TrimSpace(strings.TrimPrefix(trimmed, "token:"))
		}
	}
	return -1, 0, ""
}

// commitTempPrefix marks the throwaway config copy SetHostToken writes before
// renaming it into place. That copy holds every host's token in plaintext, so
// a crash between creation and rename leaves a second credential file behind;
// sweepStaleTemps clears it on the next run. Shared with the sweeper so the
// two cannot diverge.
const commitTempPrefix = ".config.yml.tmp-"

// SetHostToken rewrites exactly one line: the `token:` field inside host's
// block.
func SetHostToken(path, host, token string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")

	idx, indent, _ := findHostToken(lines, host)
	if idx < 0 {
		return fmt.Errorf("no token line found for host %q in %s", host, path)
	}
	lines[idx] = fmt.Sprintf("%stoken: %s", strings.Repeat(" ", indent), token)

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, commitTempPrefix+"*")
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
