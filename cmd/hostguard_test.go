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
	epic := &cobra.Command{Use: "epic"} // migrated → host-neutral subtree (returns ErrUnsupported on GitHub/Gitea)
	epic.AddCommand(&cobra.Command{Use: "list"})
	sync := &cobra.Command{Use: "sync"} // migrated → host-neutral subtree (dual-client Provider)
	sync.AddCommand(&cobra.Command{Use: "issues"})
	wi := &cobra.Command{Use: "wi"} // migrated → host-neutral subtree (ErrUnsupported on GitHub/Gitea)
	wi.AddCommand(&cobra.Command{Use: "list"})
	report := &cobra.Command{Use: "report"}
	report.AddCommand(&cobra.Command{Use: "push"})
	standup := &cobra.Command{Use: "standup"} // migrated → host-neutral
	shields := &cobra.Command{Use: "shields"} // migrated → host-neutral subtree
	shields.AddCommand(
		&cobra.Command{Use: "badge"}, // migrated → host-neutral
		&cobra.Command{Use: "chips"}, // migrated → host-neutral leaf
	)
	root.AddCommand(issue, mr, journal, epic, sync, wi, report, standup, shields)
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
	// After the sync/shields/wi migration, no leaves in the test tree are
	// raw-GL anymore; this test now guards against regressions by checking
	// a synthetic blocked leaf.
	root := buildTree()
	// Inject a synthetic raw-GL leaf under journal to verify the guard still
	// fires for unmigrated leaves.
	rawGL := &cobra.Command{Use: "rawgl"}
	journal := find(t, root, "ggvalet journal")
	journal.AddCommand(rawGL)
	hostBlockedLeaves["ggvalet journal rawgl"] = true
	defer delete(hostBlockedLeaves, "ggvalet journal rawgl")

	for _, kind := range []provider.Kind{provider.KindGitHub, provider.KindGitea} {
		err := ensureHostNeutral(find(t, root, "ggvalet journal rawgl"), kind)
		if err == nil {
			t.Errorf("%s silently allowed not-yet-migrated %q", kind, "ggvalet journal rawgl")
			continue
		}
		if !strings.Contains(err.Error(), "experimental") {
			t.Errorf("error for %q (%s) lacks [S] explanation: %v", "ggvalet journal rawgl", kind, err)
		}
	}
}

func TestEnsureHostNeutral_NonGitLab_AllowsMigratedAndNeutral(t *testing.T) {
	// Migrated leaves (issue create, mr close), fully-neutral subtrees
	// (journal), host-neutral report/report push, and the newly-migrated
	// sync/shields/wi/epic surfaces all work on any host.
	root := buildTree()
	for _, kind := range []provider.Kind{provider.KindGitHub, provider.KindGitea} {
		for _, path := range []string{
			"ggvalet issue list", "ggvalet issue create",
			"ggvalet mr merge", "ggvalet mr close",
			"ggvalet journal show",
			"ggvalet report", "ggvalet report push",
			"ggvalet standup",
			"ggvalet shields chips", "ggvalet shields badge",
			"ggvalet epic list",
			"ggvalet sync issues",
			"ggvalet wi list",
		} {
			if err := ensureHostNeutral(find(t, root, path), kind); err != nil {
				t.Errorf("%s blocked host-neutral %q: %v", kind, path, err)
			}
		}
	}
}
