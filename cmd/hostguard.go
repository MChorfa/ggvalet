package cmd

import (
	"fmt"
	"strings"

	"github.com/ckodex/gitlabvalet/internal/provider"
	"github.com/spf13/cobra"
)

// hostNeutralPrefixes are command subtrees that never issue raw GitLab-SDK
// calls — they are local-only (journal, cache) or fully Provider-based (plan,
// wi) and self-guarding — so every leaf under them is safe under any host.
var hostNeutralPrefixes = []string{
	"glv hosts", "glv completion",
	"glv journal", "glv cache",
	"glv receipt",
	"glv plan", "glv wi",
	"glv issue", // fully migrated to client.Provider (P8/P13/P16)
	"glv mr",    // fully migrated to client.Provider (P10/P14/P15)
	"glv label", // fully migrated to client.Provider (P9/P16)
}

// hostNeutralLeaves holds individual leaf commands that are host-neutral while
// their parent still has GitLab-only siblings. The issue/mr/label parents are
// now fully migrated (in hostNeutralPrefixes), so only the root remains. New
// per-leaf entries go here when a partially-migrated parent appears; see
// docs/VWP-ATTESTATION.md RES-01.
var hostNeutralLeaves = map[string]bool{
	"glv": true, // root help / usage
}

// ensureHostNeutral fails loud when a command that still uses the raw GitLab
// SDK (client.GL) is run under a non-GitLab provider, instead of silently
// querying GitLab with GitLab credentials. This makes the GitHub host's
// [S]-experimental status truthful (RES-01) and honors fail-loud (Rule 12).
//
// It is keyed on the active provider Kind rather than the full Provider so the
// guard stays trivially testable. As command-layer migration completes, the
// allowlists grow until the guard is a no-op and can be retired.
func ensureHostNeutral(cmd *cobra.Command, kind provider.Kind) error {
	if kind == provider.KindGitLab {
		return nil // raw GitLab-SDK calls are native on a GitLab host
	}
	path := cmd.CommandPath()
	if hostNeutralLeaves[path] {
		return nil
	}
	for _, p := range hostNeutralPrefixes {
		if path == p || strings.HasPrefix(path, p+" ") {
			return nil
		}
	}
	return fmt.Errorf(
		"command %q is not yet host-neutral; %s support is [S] experimental.\n"+
			"  host-neutral commands: issue create|update|close|comment, label list|create, "+
			"mr create|close, and all plan/wi/journal/cache commands.\n"+
			"  set GLVALET_PROVIDER=gitlab (or unset it) to run this command against GitLab",
		path, kind)
}
