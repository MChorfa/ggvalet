package cmd

import (
	"fmt"
	"strings"

	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/spf13/cobra"
)

// hostNeutralPrefixes are command subtrees that never issue raw GitLab-SDK
// calls — they are local-only (journal, cache), fully Provider-based (issue,
// mr, label, plan), or local-only report generation — so every leaf under them
// is safe under any host unless explicitly listed in hostBlockedLeaves.
var hostNeutralPrefixes = []string{
	"ggvalet hosts", "ggvalet completion",
	"ggvalet journal", "ggvalet cache",
	"ggvalet receipt",
	"ggvalet plan",
	"ggvalet report",
	"ggvalet issue",     // fully migrated to client.Provider (P8/P13/P16)
	"ggvalet mr",        // fully migrated to client.Provider (P10/P14/P15)
	"ggvalet label",     // fully migrated to client.Provider (P9/P16)
	"ggvalet standup",   // migrated to client.Provider (journal + ListMyIssues + CreateIssue)
	"ggvalet epic",      // migrated to client.Provider (ResolveGroup + ListGroupEpics + CreateGroupEpic + UpdateGroupEpic + ListEpicIssues)
	"ggvalet milestone", // migrated to client.Provider (ListMilestones + GetMilestone + CreateMilestone + UpdateMilestone)
	"ggvalet sync",      // migrated to client.Provider (ListIssues + CreateIssue + ListGroupEpics + CreateGroupEpic + ListMilestones + CreateMilestone + ResolveGroup)
	"ggvalet shields",   // migrated to client.Provider (GetProject + ListPipelines + ListMilestones + ListLabels + GetIssue)
	"ggvalet wi",        // migrated to client.Provider (ListWorkItems + CreateWorkItem + CloseWorkItem); GitLab-only surface, ErrUnsupported elsewhere
	"ggvalet renovate",  // project-scoped path migrated to client.Provider; all-projects path is GitLab-only and fails loud when glClient.GL == nil
}

// hostNeutralLeaves holds individual leaf commands that are host-neutral while
// their parent still has GitLab-only siblings. The issue/mr/label parents are
// now fully migrated (in hostNeutralPrefixes), so only the root remains. New
// per-leaf entries go here when a partially-migrated parent appears; see
// docs/VWP-ATTESTATION.md RES-01.
var hostNeutralLeaves = map[string]bool{
	"ggvalet": true, // root help / usage
}

// hostBlockedLeaves are leaves under a host-neutral prefix that still issue raw
// GitLab-SDK calls. They override the prefix allowlist. Add entries here as
// migration uncovers leaves that are not yet provider-based.
var hostBlockedLeaves = map[string]bool{
	// Credential rotation talks to GitLab's personal_access_tokens/self
	// endpoints directly. Neither has a provider abstraction, and running
	// either against a non-GitLab host would revoke nothing while reporting
	// success. Listed explicitly so a future host-neutral prefix cannot
	// accidentally cover them.
	"ggvalet rotate":    true,
	"ggvalet ssh audit": true,
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
	if hostBlockedLeaves[path] {
		return blockedError(path, kind)
	}
	if hostNeutralLeaves[path] {
		return nil
	}
	for _, p := range hostNeutralPrefixes {
		if path == p || strings.HasPrefix(path, p+" ") {
			return nil
		}
	}
	return blockedError(path, kind)
}

func blockedError(path string, kind provider.Kind) error {
	return fmt.Errorf(
		"command %q is not yet host-neutral; %s support is [S] experimental.\n"+
			"  host-neutral commands: issue create|update|close|comment, label list|create, "+
			"mr create|close, standup, report, and all plan/journal/cache commands.\n"+
			"  set GLVALET_PROVIDER=gitlab (or unset it) to run this command against GitLab",
		path, kind)
}
