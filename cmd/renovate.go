// cmd/renovate.go — Renovate bot MR triage for dependency hygiene.
//
// Identifies Renovate MRs by author (renovate-bot, renovate[bot]) or
// source branch prefix "renovate/", groups by bump type (major/minor/patch),
// and enables bulk approve or merge.
package cmd

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/MChorfa/ggvalet/internal/parallel"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/charmbracelet/lipgloss"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	gl "github.com/xanzy/go-gitlab"
)

// ─── Semver bump detection ────────────────────────────────────────────────────

var (
	majorRE = regexp.MustCompile(`(?i)(major|breaking|v\d+\.\d+\.\d+ -> v\d+\.)`)
	minorRE = regexp.MustCompile(`(?i)(minor|feature|v\d+\.\d+\.\d+ -> v\d+\.\d+\.)`)
	patchRE = regexp.MustCompile(`(?i)(patch|fix|v\d+\.\d+\.\d+ -> v\d+\.\d+\.\d+)`)
)

type bumpType string

const (
	bumpMajor   bumpType = "major"
	bumpMinor   bumpType = "minor"
	bumpPatch   bumpType = "patch"
	bumpUnknown bumpType = "unknown"
)

var (
	majorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444")).Bold(true)
	minorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#fbbf24")).Bold(true)
	patchStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
	unknownStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
)

func detectBump(mr *provider.MergeRequest) bumpType {
	text := mr.Title + " " + mr.Body + " " + mr.SourceBranch
	if majorRE.MatchString(text) {
		return bumpMajor
	}
	if minorRE.MatchString(text) {
		return bumpMinor
	}
	if patchRE.MatchString(text) {
		return bumpPatch
	}
	return bumpUnknown
}

func isRenovate(mr *provider.MergeRequest) bool {
	if strings.Contains(strings.ToLower(mr.Author.Username), "renovate") {
		return true
	}
	if strings.HasPrefix(mr.SourceBranch, "renovate/") {
		return true
	}
	title := strings.ToLower(mr.Title)
	return strings.HasPrefix(title, "chore(deps)") ||
		strings.HasPrefix(title, "update dependency") ||
		strings.HasPrefix(title, "chore: update")
}

func bumpLabel(b bumpType) string {
	switch b {
	case bumpMajor:
		return majorStyle.Render("MAJOR")
	case bumpMinor:
		return minorStyle.Render("minor")
	case bumpPatch:
		return patchStyle.Render("patch")
	default:
		return unknownStyle.Render("  ?  ")
	}
}

// ─── Root command ─────────────────────────────────────────────────────────────

func renovateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "renovate",
		Short: "Triage Renovate bot merge requests — list, approve, merge",
		Long: `renovate identifies Renovate Bot MRs by:
  • author username containing "renovate"
  • source branch starting with "renovate/"
  • title starting with "chore(deps):" or "Update dependency"

Groups results by semver bump type (major/minor/patch) so you can
safely bulk-approve patches without touching majors.`,
	}
	cmd.AddCommand(
		renovateListCmd(),
		renovateApproveCmd(),
		renovateMergeCmd(),
		renovateStatsCmd(),
	)
	return cmd
}

// ─── list ─────────────────────────────────────────────────────────────────────

func renovateListCmd() *cobra.Command {
	var project string
	var allProjects bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all open Renovate MRs, grouped by bump type",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}

			mrs, err := fetchRenovateMRs(project, allProjects)
			if err != nil {
				return err
			}

			if len(mrs) == 0 {
				ok("No open Renovate MRs found")
				return nil
			}

			// Group
			groups := map[bumpType][]*provider.MergeRequest{
				bumpMajor: {}, bumpMinor: {}, bumpPatch: {}, bumpUnknown: {},
			}
			for _, mr := range mrs {
				b := detectBump(mr)
				groups[b] = append(groups[b], mr)
			}

			order := []bumpType{bumpMajor, bumpMinor, bumpPatch, bumpUnknown}
			for _, bump := range order {
				subset := groups[bump]
				if len(subset) == 0 {
					continue
				}
				fmt.Printf("\n%s (%d)\n", bumpLabel(bump), len(subset))
				table := tablewriter.NewWriter(os.Stdout)
				table.SetHeader([]string{"IID", "Title", "Branch", "Pipeline"})
				table.SetBorder(false)
				table.SetAutoWrapText(false)
				for _, mr := range subset {
					table.Append([]string{
						fmt.Sprintf("!%d", mr.IID),
						truncate(mr.Title, 55),
						truncate(mr.SourceBranch, 30),
						mr.Pipeline,
					})
				}
				table.Render()
			}
			fmt.Printf("\n%s  %s\n",
				colorDim("Totals:"),
				fmt.Sprintf("major:%d  minor:%d  patch:%d  unknown:%d",
					len(groups[bumpMajor]), len(groups[bumpMinor]),
					len(groups[bumpPatch]), len(groups[bumpUnknown])))
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().BoolVar(&allProjects, "all", false, "Search across all accessible projects")
	return cmd
}

// ─── approve ──────────────────────────────────────────────────────────────────

func renovateApproveCmd() *cobra.Command {
	var project, bumpFilter string
	var dryRun bool
	var concurrency int

	cmd := &cobra.Command{
		Use:   "approve",
		Short: "Bulk-approve Renovate MRs by bump type",
		Example: `  ggvalet renovate approve --project g/p --bump patch        # approve all patches
  ggvalet renovate approve --project g/p --bump minor,patch  # approve minor + patch
  ggvalet renovate approve --project g/p --bump all          # approve everything (careful!)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("a project is required — set --project or configure a default project in your environment")
			}

			allowed := parseBumpFilter(bumpFilter)
			mrs, err := fetchRenovateMRs(project, false)
			if err != nil {
				return err
			}

			var targets []*provider.MergeRequest
			for _, mr := range mrs {
				b := detectBump(mr)
				if allowed[b] {
					targets = append(targets, mr)
				}
			}

			if len(targets) == 0 {
				info("No Renovate MRs match bump filter %q", bumpFilter)
				return nil
			}

			info("Approving %d MR(s) [bump: %s]…", len(targets), bumpFilter)
			if dryRun {
				for _, mr := range targets {
					fmt.Printf("  %s !%d  %s\n", colorDim("[dry]"), mr.IID, mr.Title)
				}
				return nil
			}

			pool := parallel.New(concurrency)
			var approved, failed atomic.Int64

			for _, mr := range targets {
				mr := mr
				pool.GoErr(func() error {
					if err := glClient.Provider.ApproveMergeRequest(context.Background(), project, mr.IID); err != nil {
						fmt.Fprintf(os.Stderr, "  %s !%d: %v\n", colorErr("✗"), mr.IID, err)
						failed.Add(1)
						return err
					}
					glClient.Rec(journal.OpUpdate, journal.EntityMR, project, "",
						0, mr.IID, mr.Title, mr.WebURL, "renovate-approve")
					fmt.Printf("  %s !%d  %s\n", colorOK("✓"), mr.IID, truncate(mr.Title, 60))
					approved.Add(1)
					return nil
				})
			}
			pool.Wait()

			fmt.Printf("\n%s %d approved  %s\n", colorOK("✓"), approved.Load(),
				func() string {
					if n := failed.Load(); n > 0 {
						return colorErr(fmt.Sprintf("%d failed", n))
					}
					return ""
				}())
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().StringVar(&bumpFilter, "bump", "patch", "Bump types to approve: patch|minor|major|all (comma-separated)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without approving")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "Parallel approve requests")
	return cmd
}

// ─── merge ────────────────────────────────────────────────────────────────────

func renovateMergeCmd() *cobra.Command {
	var project, bumpFilter string
	var dryRun bool
	var concurrency int
	var approvedOnly bool

	cmd := &cobra.Command{
		Use:   "merge",
		Short: "Bulk-merge approved Renovate MRs by bump type",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("a project is required — set --project or configure a default project in your environment")
			}

			allowed := parseBumpFilter(bumpFilter)
			mrs, err := fetchRenovateMRs(project, false)
			if err != nil {
				return err
			}

			var targets []*provider.MergeRequest
			for _, mr := range mrs {
				b := detectBump(mr)
				if !allowed[b] {
					continue
				}
				targets = append(targets, mr)
			}

			if len(targets) == 0 {
				info("No mergeable Renovate MRs found for bump filter %q", bumpFilter)
				return nil
			}

			info("Merging %d MR(s)…", len(targets))
			if dryRun {
				for _, mr := range targets {
					fmt.Printf("  %s !%d  %s\n", colorDim("[dry]"), mr.IID, mr.Title)
				}
				return nil
			}

			pool := parallel.New(concurrency)
			var merged, failed atomic.Int64

			for _, mr := range targets {
				mr := mr
				pool.GoErr(func() error {
					if _, err := glClient.Provider.MergeMergeRequest(context.Background(), project, mr.IID,
						provider.MergeOptions{RemoveSourceBranch: true}); err != nil {
						fmt.Fprintf(os.Stderr, "  %s !%d: %v\n", colorErr("✗"), mr.IID, err)
						failed.Add(1)
						return err
					}
					glClient.Rec(journal.OpUpdate, journal.EntityMR, project, "",
						0, mr.IID, mr.Title, mr.WebURL, "renovate-merge")
					fmt.Printf("  %s !%d  %s\n", colorOK("✓"), mr.IID, truncate(mr.Title, 60))
					merged.Add(1)
					return nil
				})
			}
			pool.Wait()

			fmt.Printf("\n%s %d merged  %s\n", colorOK("✓"), merged.Load(),
				func() string {
					if n := failed.Load(); n > 0 {
						return colorErr(fmt.Sprintf("%d failed", n))
					}
					return ""
				}())
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().StringVar(&bumpFilter, "bump", "patch", "Bump types: patch|minor|major|all")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without merging")
	cmd.Flags().BoolVar(&approvedOnly, "approved-only", false, "Only merge already-approved MRs")
	cmd.Flags().IntVar(&concurrency, "concurrency", 2, "Parallel merge requests (keep low)")
	return cmd
}

// ─── stats ────────────────────────────────────────────────────────────────────

func renovateStatsCmd() *cobra.Command {
	var project string

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Summary of Renovate MR status across bump types",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			mrs, err := fetchRenovateMRs(project, false)
			if err != nil {
				return err
			}

			counts := map[bumpType]int{}
			pipelineOK := map[bumpType]int{}
			for _, mr := range mrs {
				b := detectBump(mr)
				counts[b]++
				if mr.Pipeline == "success" {
					pipelineOK[b]++
				}
			}

			fmt.Printf("\nRenovate MR stats — %s\n\n", shortHostname(cfg.Host))
			for _, b := range []bumpType{bumpMajor, bumpMinor, bumpPatch, bumpUnknown} {
				if counts[b] == 0 {
					continue
				}
				fmt.Printf("  %s  total:%-4d  pipeline ok:%d\n",
					bumpLabel(b), counts[b], pipelineOK[b])
			}
			fmt.Printf("\n  Total: %d open Renovate MRs\n\n", len(mrs))
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	return cmd
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func fetchRenovateMRs(project string, allProjects bool) ([]*provider.MergeRequest, error) {
	ctx := context.Background()

	if allProjects || project == "" {
		if glClient.Provider == nil || glClient.Provider.Kind() != provider.KindGitLab || glClient.GL == nil {
			return nil, fmt.Errorf("searching across all projects requires a GitLab provider — set --project to scope to a specific repository")
		}
		// GitLab-only global MR list (no Provider equivalent for cross-project MR search).
		mrs, _, err := glClient.GL.MergeRequests.ListMergeRequests(
			&gl.ListMergeRequestsOptions{
				State:       gl.Ptr("opened"),
				ListOptions: gl.ListOptions{PerPage: 100},
			})
		if err != nil {
			return nil, fmt.Errorf("could not list open merge requests: %w", err)
		}
		var renovate []*provider.MergeRequest
		for _, mr := range mrs {
			pm := gitlabMRToProvider(mr)
			if isRenovate(pm) {
				renovate = append(renovate, pm)
			}
		}
		return renovate, nil
	}

	mrs, err := glClient.Provider.ListMergeRequests(ctx, project, provider.ListMergeRequestsOptions{
		State:   "opened",
		PerPage: 100,
	})
	if err != nil {
		return nil, fmt.Errorf("could not list open merge requests for %s: %w", project, err)
	}

	var renovate []*provider.MergeRequest
	for i := range mrs {
		if isRenovate(&mrs[i]) {
			renovate = append(renovate, &mrs[i])
		}
	}
	return renovate, nil
}

func parseBumpFilter(s string) map[bumpType]bool {
	m := map[bumpType]bool{}
	if s == "all" {
		m[bumpMajor] = true
		m[bumpMinor] = true
		m[bumpPatch] = true
		m[bumpUnknown] = true
		return m
	}
	for _, part := range strings.Split(s, ",") {
		switch strings.TrimSpace(strings.ToLower(part)) {
		case "major":
			m[bumpMajor] = true
		case "minor":
			m[bumpMinor] = true
		case "patch":
			m[bumpPatch] = true
		case "unknown":
			m[bumpUnknown] = true
		}
	}
	return m
}

// gitlabMRToProvider converts a raw go-gitlab MergeRequest to the host-neutral
// provider.MergeRequest. Used only for the GitLab-only global MR list path.
func gitlabMRToProvider(mr *gl.MergeRequest) *provider.MergeRequest {
	out := &provider.MergeRequest{
		ID:           mr.ID,
		IID:          mr.IID,
		Title:        mr.Title,
		Body:         mr.Description,
		State:        mr.State,
		SourceBranch: mr.SourceBranch,
		TargetBranch: mr.TargetBranch,
		WebURL:       mr.WebURL,
	}
	if mr.HeadPipeline != nil {
		out.Pipeline = mr.HeadPipeline.Status
	}
	if mr.Author != nil {
		out.Author = provider.User{Username: mr.Author.Username}
	}
	return out
}
