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
        token: ***
        api_host: sc01-trt.example.ca/gitlab
    gitlab.example.com:
        api_protocol: https
        token: ***
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
