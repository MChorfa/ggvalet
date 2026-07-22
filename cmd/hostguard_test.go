package cmd

import (
	"strings"
	"testing"

	"github.com/ckodex/gitlabvalet/internal/provider"
	"github.com/spf13/cobra"
)

// buildTree returns a "glv issue {list,create}" / "glv mr {merge,close}" /
// "glv journal show" command tree so CommandPath() resolves like production.
func buildTree() *cobra.Command {
	root := &cobra.Command{Use: "glv"}
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
	root.AddCommand(issue, mr, journal, epic, sync, wi, report)
	return root
}

// find resolves a leaf command by its space-separated path under root.
func find(t *testing.T, root *cobra.Command, path string) *cobra.Command {
	t.Helper()
	parts := strings.Fields(path)[1:] // drop "glv"
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
	for _, path := range []string{"glv epic list", "glv sync", "glv mr merge", "glv journal show"} {
		if err := ensureHostNeutral(find(t, root, path), provider.KindGitLab); err != nil {
			t.Errorf("KindGitLab blocked %q: %v", path, err)
		}
	}
}

func TestEnsureHostNeutral_NonGitLab_BlocksRawSDKCommands(t *testing.T) {
	// The reason this matters: under GLVALET_PROVIDER=github a raw-GL command
	// would otherwise silently hit GitLab. It MUST fail loud instead.
	root := buildTree()
	for _, path := range []string{"glv sync", "glv epic list", "glv wi list"} {
		err := ensureHostNeutral(find(t, root, path), provider.KindGitHub)
		if err == nil {
			t.Errorf("KindGitHub silently allowed not-yet-migrated %q", path)
			continue
		}
		if !strings.Contains(err.Error(), "experimental") {
			t.Errorf("error for %q lacks [S] explanation: %v", path, err)
		}
	}
}

func TestEnsureHostNeutral_NonGitLab_AllowsMigratedAndNeutral(t *testing.T) {
	// Migrated leaves (issue create, mr close), fully-neutral subtrees
	// (journal), and host-neutral report/report push work on any host.
	root := buildTree()
	for _, path := range []string{"glv issue list", "glv issue create", "glv mr merge", "glv mr close", "glv journal show", "glv report", "glv report push"} {
		if err := ensureHostNeutral(find(t, root, path), provider.KindGitHub); err != nil {
			t.Errorf("KindGitHub blocked host-neutral %q: %v", path, err)
		}
	}
}
