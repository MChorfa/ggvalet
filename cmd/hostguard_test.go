package cmd

import (
	"strings"
	"testing"

	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/spf13/cobra"
)

// buildTree returns a "ggvalet issue {list,create}" / "ggvalet mr {merge,close}" /
// "ggvalet journal show" command tree so CommandPath() resolves like production.
func buildTree() *cobra.Command {
	root := &cobra.Command{Use: "ggvalet"}
	issue := &cobra.Command{Use: "issue"} // fully migrated → host-neutral subtree
	issue.AddCommand(&cobra.Command{Use: "list"}, &cobra.Command{Use: "create"})
	mr := &cobra.Command{Use: "mr"} // fully migrated → host-neutral subtree
	mr.AddCommand(
		&cobra.Command{Use: "merge"},
		&cobra.Command{Use: "diff"},
		&cobra.Command{Use: "close"},
	)
	journal := &cobra.Command{Use: "journal"}
	journal.AddCommand(&cobra.Command{Use: "show"})
	epic := &cobra.Command{Use: "epic"} // GitLab-only → blocked off GitLab
	epic.AddCommand(&cobra.Command{Use: "list"})
	sync := &cobra.Command{Use: "sync"} // raw GL → blocked off GitLab
	wi := &cobra.Command{Use: "wi"}     // raw GitLab HTTP → blocked off GitLab
	wi.AddCommand(&cobra.Command{Use: "list"})
	report := &cobra.Command{Use: "report"}
	report.AddCommand(&cobra.Command{Use: "push"})
	standup := &cobra.Command{Use: "standup"} // migrated → host-neutral
	root.AddCommand(issue, mr, journal, epic, sync, wi, report, standup)
	return root
}

// find resolves a leaf command by its space-separated path under root.
func find(t *testing.T, root *cobra.Command, path string) *cobra.Command {
	t.Helper()
	parts := strings.Fields(path)[1:] // drop "ggvalet"
	cur := root
	for _, name := range parts {
		next, _, err := cur.Find([]string{name})
		if err != nil || next == cur {
			t.Fatalf("could not resolve %q (stuck at %q)", path, cur.Name())
		}
		cur = next
	}
	return cur
}

func TestEnsureHostNeutral_GitLab_AllowsEverything(t *testing.T) {
	// On a GitLab host, raw-SDK commands are native — nothing is blocked.
	root := buildTree()
	for _, path := range []string{"ggvalet epic list", "ggvalet sync", "ggvalet mr merge", "ggvalet journal show"} {
		if err := ensureHostNeutral(find(t, root, path), provider.KindGitLab); err != nil {
			t.Errorf("KindGitLab blocked %q: %v", path, err)
		}
	}
}

func TestEnsureHostNeutral_NonGitLab_BlocksRawSDKCommands(t *testing.T) {
	// The reason this matters: under GLVALET_PROVIDER=github|gitea a raw-GL
	// command would otherwise silently hit GitLab. It MUST fail loud instead.
	root := buildTree()
	for _, kind := range []provider.Kind{provider.KindGitHub, provider.KindGitea} {
		for _, path := range []string{"ggvalet sync", "ggvalet epic list", "ggvalet wi list"} {
			err := ensureHostNeutral(find(t, root, path), kind)
			if err == nil {
				t.Errorf("%s silently allowed not-yet-migrated %q", kind, path)
				continue
			}
			if !strings.Contains(err.Error(), "experimental") {
				t.Errorf("error for %q (%s) lacks [S] explanation: %v", path, kind, err)
			}
		}
	}
}

func TestEnsureHostNeutral_NonGitLab_AllowsMigratedAndNeutral(t *testing.T) {
	// Migrated leaves (issue create, mr close), fully-neutral subtrees
	// (journal), and host-neutral report/report push work on any host.
	root := buildTree()
	for _, kind := range []provider.Kind{provider.KindGitHub, provider.KindGitea} {
		for _, path := range []string{"ggvalet issue list", "ggvalet issue create", "ggvalet mr merge", "ggvalet mr close", "ggvalet journal show", "ggvalet report", "ggvalet report push", "ggvalet standup"} {
			if err := ensureHostNeutral(find(t, root, path), kind); err != nil {
				t.Errorf("%s blocked host-neutral %q: %v", kind, path, err)
			}
		}
	}
}
