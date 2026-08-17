package rotation

import (
	"crypto/sha256"
	"encoding/hex"
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
        token: ***
        api_host: sc01-trt.example.ca/gitlab
    gitlab.example.com:
        api_protocol: https
        token: ***
        user: someone
`

// glab writes nested sub-maps under a host, and those sub-maps carry their own
// `token:` field. The host's own token sits at the host's field level; a nested
// one sits deeper. Here the nested one comes first in file order, which is what
// a depth-only walker gets wrong.
const nestedHostFixture = `hosts:
    gitlab.example.com:
        api_protocol: https
        container_registry:
            host: registry.example.com
            token: REGISTRY_TOKEN
        token: HOST_TOKEN
        user: someone
`

func TestSetHostToken_DoesNotWriteIntoANestedSubMap(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(p, []byte(nestedHostFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetHostToken(p, "gitlab.example.com", "NEW_HOST_TOKEN"); err != nil {
		t.Fatalf("SetHostToken: %v", err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)

	// Line-exact: a substring check would be satisfied by the nested line,
	// whose deeper indentation still ends in the shallower one.
	if !strings.Contains(out, "\n        token: NEW_HOST_TOKEN\n") {
		t.Errorf("the host's own token line was not updated:\n%s", out)
	}
	if !strings.Contains(out, "\n            token: REGISTRY_TOKEN\n") {
		t.Errorf("a nested credential must not be overwritten:\n%s", out)
	}
}

func TestConfigHasToken_IgnoresANestedSubMapToken(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(p, []byte(nestedHostFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	nested, err := configHasToken(p, "gitlab.example.com", "REGISTRY_TOKEN")
	if err != nil {
		t.Fatalf("configHasToken: %v", err)
	}
	if nested {
		t.Error("a nested sub-map token must not count as the host's own token")
	}
	own, err := configHasToken(p, "gitlab.example.com", "HOST_TOKEN")
	if err != nil {
		t.Fatalf("configHasToken: %v", err)
	}
	if !own {
		t.Error("the host's own token was not found")
	}
}

func TestFileDigest_ComputesCorrectSHA256(t *testing.T) {
	p := filepath.Join(t.TempDir(), "test.txt")
	content := []byte("hello world")
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}

	// Compute expected digest in the test, not hardcoded
	expected := sha256.Sum256(content)
	expectedHex := hex.EncodeToString(expected[:])

	got, err := FileDigest(p)
	if err != nil {
		t.Fatalf("FileDigest: %v", err)
	}
	if got != expectedHex {
		t.Errorf("digest mismatch: got %q want %q", got, expectedHex)
	}
}

func TestFileDigest_ChangesWhenFileModified(t *testing.T) {
	p := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(p, []byte("version1"), 0o600); err != nil {
		t.Fatal(err)
	}

	digest1, err := FileDigest(p)
	if err != nil {
		t.Fatalf("FileDigest first: %v", err)
	}

	// Modify the file
	if err := os.WriteFile(p, []byte("version2"), 0o600); err != nil {
		t.Fatal(err)
	}

	digest2, err := FileDigest(p)
	if err != nil {
		t.Fatalf("FileDigest second: %v", err)
	}

	if digest1 == digest2 {
		t.Error("digest did not change after file modification")
	}
}

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
	if !strings.Contains(out, "token: ***") {
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
