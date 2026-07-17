// cmd/sync.go — bidirectional cross-instance sync for issues, epics, milestones.
//
// Deduplication strategy:
//
//	issues/epics  — hidden HTML comment appended to description:
//	  <!-- glv-sync-src: https://host/group/proj/-/issues/42 -->
//	  Safe to re-run: items already carrying this marker are skipped.
//	milestones    — title-match on the destination (milestones have no body).
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/ckodex/gitlabvalet/internal/client"
	"github.com/ckodex/gitlabvalet/internal/config"
	"github.com/ckodex/gitlabvalet/internal/journal"
	"github.com/spf13/cobra"
	gl "github.com/xanzy/go-gitlab"
)

// ─── Sync marker helpers ──────────────────────────────────────────────────────

const (
	syncPfx = "<!-- glv-sync-src: "
	syncSfx = " -->"
)

func syncFooter(srcURL string) string {
	return fmt.Sprintf(
		"\n\n---\n*Synced by [GitLab Valet](https://github.com/ckodex/gitlabvalet)*  \n%s%s%s",
		syncPfx, srcURL, syncSfx,
	)
}

func extractSyncSrc(description string) string {
	idx := strings.Index(description, syncPfx)
	if idx == -1 {
		return ""
	}
	start := idx + len(syncPfx)
	end := strings.Index(description[start:], syncSfx)
	if end == -1 {
		return ""
	}
	return description[start : start+end]
}

// ─── Dual-client builder ──────────────────────────────────────────────────────

// syncClients builds src and dst clients from the already-loaded glab hosts map.
func syncClients(srcHost, dstHost string) (*client.Client, *client.Client, error) {
	hosts := cfg.Hosts
	jp := cfg.JournalPath

	srcCfg, err := config.ForHost(hosts, srcHost, jp)
	if err != nil {
		return nil, nil, fmt.Errorf("src: %w", err)
	}
	dstCfg, err := config.ForHost(hosts, dstHost, jp)
	if err != nil {
		return nil, nil, fmt.Errorf("dst: %w", err)
	}
	srcC, err := client.New(srcCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("src client: %w", err)
	}
	dstC, err := client.New(dstCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("dst client: %w", err)
	}
	return srcC, dstC, nil
}

// ─── Root command ─────────────────────────────────────────────────────────────

func syncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Copy issues / epics / milestones between any two GitLab instances",
		Example: `  # Issues from thalesdigital → sc01 (dry-run first)
  glv sync issues \
    --src-host gitlab.thalesdigital.io \
    --src-project group/proj \
    --dst-host sc01-trt.thales-systems.ca/gitlab \
    --dst-project other/proj \
    --dry-run

  # Epics in the other direction
  glv sync epics \
    --src-host sc01-trt.thales-systems.ca/gitlab --src-group my-group \
    --dst-host gitlab.thalesdigital.io           --dst-group other-group

  # Milestones within the same host, different project
  glv sync milestones --src-project g/proj-a --dst-project g/proj-b`,
	}
	cmd.AddCommand(syncIssuesCmd(), syncEpicsCmd(), syncMilestonesCmd())
	return cmd
}

// ─── sync issues ──────────────────────────────────────────────────────────────

func syncIssuesCmd() *cobra.Command {
	var srcHost, srcProject, dstHost, dstProject string
	var state, labels string
	var dryRun bool
	var limit int

	cmd := &cobra.Command{
		Use:   "issues",
		Short: "Sync issues from source project → destination project",
		RunE: func(cmd *cobra.Command, args []string) error {
			if srcHost == "" {
				srcHost = cfg.Host
			}
			if dstHost == "" {
				dstHost = cfg.Host
			}
			if srcProject == "" {
				srcProject = cfg.DefaultProject
			}
			if dstProject == "" {
				dstProject = cfg.DefaultProject
			}
			if srcProject == "" {
				return fmt.Errorf("--src-project required")
			}
			if dstProject == "" {
				return fmt.Errorf("--dst-project required")
			}

			srcC, dstC, err := syncClients(srcHost, dstHost)
			if err != nil {
				return err
			}

			printSyncHeader("issues", srcHost, srcProject, dstHost, dstProject, dryRun)

			// List source issues
			listOpts := &gl.ListProjectIssuesOptions{
				State:       gl.Ptr(state),
				ListOptions: gl.ListOptions{PerPage: 100},
			}
			if labels != "" {
				lv := gl.LabelOptions{labels}
				listOpts.Labels = &lv
			}
			srcIssues, _, err := srcC.GL.Issues.ListProjectIssues(srcProject, listOpts)
			if err != nil {
				return fmt.Errorf("list src issues: %w", err)
			}
			if limit > 0 && len(srcIssues) > limit {
				srcIssues = srcIssues[:limit]
			}

			// Build destination dedup index
			dstIndex, err := buildIssueSyncIndex(dstC, dstProject)
			if err != nil {
				return fmt.Errorf("build dst index: %w", err)
			}
			info("src: %d issues  already synced: %d", len(srcIssues), len(dstIndex))
			fmt.Println()

			created, skipped, failed := 0, 0, 0
			for _, iss := range srcIssues {
				if _, exists := dstIndex[iss.WebURL]; exists {
					skipped++
					continue
				}

				desc := iss.Description + syncFooter(iss.WebURL)
				lbls := gl.LabelOptions(iss.Labels)

				if dryRun {
					fmt.Printf("  %s #%d  %s\n",
						colorDim("[dry]"), iss.IID, truncate(iss.Title, 60))
					created++
					continue
				}

				newIss, _, err := dstC.GL.Issues.CreateIssue(dstProject, &gl.CreateIssueOptions{
					Title:       gl.Ptr(iss.Title),
					Description: gl.Ptr(desc),
					Labels:      &lbls,
				})
				if err != nil {
					fmt.Fprintf(os.Stderr, "  %s %s: %v\n",
						colorErr("✗"), truncate(iss.Title, 50), err)
					dstC.RecErr(journal.OpCreate, journal.EntityIssue, dstProject, "", err.Error())
					failed++
					continue
				}

				srcC.Rec(journal.OpList, journal.EntityIssue, srcProject, "",
					iss.ID, iss.IID, iss.Title, iss.WebURL, "sync-src")
				dstC.Rec(journal.OpCreate, journal.EntityIssue, dstProject, "",
					newIss.ID, newIss.IID, newIss.Title, newIss.WebURL, "sync-dst")
				fmt.Printf("  %s #%d → #%d  %s\n",
					colorOK("✓"), iss.IID, newIss.IID, truncate(iss.Title, 55))
				created++
			}

			printSyncSummary(created, skipped, failed, dryRun)
			return nil
		},
	}

	addSyncProjectFlags(cmd, &srcHost, &srcProject, &dstHost, &dstProject, &dryRun, &limit)
	cmd.Flags().StringVar(&state, "state", "opened", "Issue state: opened|closed|all")
	cmd.Flags().StringVar(&labels, "labels", "", "Comma-separated label filter on source")
	return cmd
}

func buildIssueSyncIndex(dstC *client.Client, dstProject string) (map[string]struct{}, error) {
	index := make(map[string]struct{})
	page := 1
	for {
		issues, resp, err := dstC.GL.Issues.ListProjectIssues(dstProject,
			&gl.ListProjectIssuesOptions{
				State:       gl.Ptr("all"),
				ListOptions: gl.ListOptions{PerPage: 100, Page: page},
			})
		if err != nil {
			return nil, err
		}
		for _, iss := range issues {
			if src := extractSyncSrc(iss.Description); src != "" {
				index[src] = struct{}{}
			}
		}
		if resp.CurrentPage >= resp.TotalPages {
			break
		}
		page++
	}
	return index, nil
}

// ─── sync epics ───────────────────────────────────────────────────────────────

func syncEpicsCmd() *cobra.Command {
	var srcHost, srcGroup, dstHost, dstGroup string
	var state string
	var dryRun bool
	var limit int

	cmd := &cobra.Command{
		Use:   "epics",
		Short: "Sync epics from source group → destination group",
		RunE: func(cmd *cobra.Command, args []string) error {
			if srcHost == "" {
				srcHost = cfg.Host
			}
			if dstHost == "" {
				dstHost = cfg.Host
			}
			if srcGroup == "" {
				srcGroup = cfg.DefaultGroup
			}
			if dstGroup == "" {
				dstGroup = cfg.DefaultGroup
			}
			if srcGroup == "" {
				return fmt.Errorf("--src-group required")
			}
			if dstGroup == "" {
				return fmt.Errorf("--dst-group required")
			}

			srcC, dstC, err := syncClients(srcHost, dstHost)
			if err != nil {
				return err
			}

			printSyncHeader("epics", srcHost, srcGroup, dstHost, dstGroup, dryRun)

			srcEpics, _, err := srcC.GL.Epics.ListGroupEpics(srcGroup,
				&gl.ListGroupEpicsOptions{
					State:       gl.Ptr(state),
					ListOptions: gl.ListOptions{PerPage: 100},
				})
			if err != nil {
				return fmt.Errorf("list src epics: %w", err)
			}
			if limit > 0 && len(srcEpics) > limit {
				srcEpics = srcEpics[:limit]
			}

			dstIndex, err := buildEpicSyncIndex(dstC, dstGroup)
			if err != nil {
				return fmt.Errorf("build dst index: %w", err)
			}
			info("src: %d epics  already synced: %d", len(srcEpics), len(dstIndex))
			fmt.Println()

			created, skipped, failed := 0, 0, 0
			for _, ep := range srcEpics {
				if _, exists := dstIndex[ep.WebURL]; exists {
					skipped++
					continue
				}

				desc := ep.Description + syncFooter(ep.WebURL)

				if dryRun {
					fmt.Printf("  %s &%d  %s\n",
						colorDim("[dry]"), ep.IID, truncate(ep.Title, 60))
					created++
					continue
				}

				opts := &gl.CreateEpicOptions{
					Title:       gl.Ptr(ep.Title),
					Description: gl.Ptr(desc),
				}
				if ep.StartDate != nil {
					opts.StartDateFixed = ep.StartDate
					opts.StartDateIsFixed = gl.Ptr(true)
				}
				if ep.DueDate != nil {
					opts.DueDateFixed = ep.DueDate
					opts.DueDateIsFixed = gl.Ptr(true)
				}

				newEp, _, err := dstC.GL.Epics.CreateEpic(dstGroup, opts)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  %s %s: %v\n",
						colorErr("✗"), truncate(ep.Title, 50), err)
					dstC.RecErr(journal.OpCreate, journal.EntityEpic, "", dstGroup, err.Error())
					failed++
					continue
				}

				srcC.Rec(journal.OpList, journal.EntityEpic, "", srcGroup,
					ep.ID, ep.IID, ep.Title, ep.WebURL, "sync-src")
				dstC.Rec(journal.OpCreate, journal.EntityEpic, "", dstGroup,
					newEp.ID, newEp.IID, newEp.Title, newEp.WebURL, "sync-dst")
				fmt.Printf("  %s &%d → &%d  %s\n",
					colorOK("✓"), ep.IID, newEp.IID, truncate(ep.Title, 55))
				created++
			}

			printSyncSummary(created, skipped, failed, dryRun)
			return nil
		},
	}

	cmd.Flags().StringVar(&srcHost, "src-host", "", "Source hostname (default: active host)")
	cmd.Flags().StringVar(&srcGroup, "src-group", "", "Source group path or ID")
	cmd.Flags().StringVar(&dstHost, "dst-host", "", "Destination hostname (default: active host)")
	cmd.Flags().StringVar(&dstGroup, "dst-group", "", "Destination group path or ID")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without creating anything")
	cmd.Flags().IntVar(&limit, "limit", 0, "Max epics to process (0 = all)")
	cmd.Flags().StringVar(&state, "state", "opened", "Epic state: opened|closed|all")
	return cmd
}

func buildEpicSyncIndex(dstC *client.Client, dstGroup string) (map[string]struct{}, error) {
	index := make(map[string]struct{})
	epics, _, err := dstC.GL.Epics.ListGroupEpics(dstGroup,
		&gl.ListGroupEpicsOptions{
			State:       gl.Ptr("all"),
			ListOptions: gl.ListOptions{PerPage: 100},
		})
	if err != nil {
		return nil, err
	}
	for _, ep := range epics {
		if src := extractSyncSrc(ep.Description); src != "" {
			index[src] = struct{}{}
		}
	}
	return index, nil
}

// ─── sync milestones ──────────────────────────────────────────────────────────

func syncMilestonesCmd() *cobra.Command {
	var srcHost, srcProject, dstHost, dstProject string
	var dryRun bool
	var limit int

	cmd := &cobra.Command{
		Use:   "milestones",
		Short: "Sync milestones from source project → destination project",
		RunE: func(cmd *cobra.Command, args []string) error {
			if srcHost == "" {
				srcHost = cfg.Host
			}
			if dstHost == "" {
				dstHost = cfg.Host
			}
			if srcProject == "" {
				srcProject = cfg.DefaultProject
			}
			if dstProject == "" {
				dstProject = cfg.DefaultProject
			}
			if srcProject == "" {
				return fmt.Errorf("--src-project required")
			}
			if dstProject == "" {
				return fmt.Errorf("--dst-project required")
			}

			srcC, dstC, err := syncClients(srcHost, dstHost)
			if err != nil {
				return err
			}

			printSyncHeader("milestones", srcHost, srcProject, dstHost, dstProject, dryRun)

			srcMs, _, err := srcC.GL.Milestones.ListMilestones(srcProject,
				&gl.ListMilestonesOptions{
					State:       gl.Ptr("all"),
					ListOptions: gl.ListOptions{PerPage: 100},
				})
			if err != nil {
				return fmt.Errorf("list src milestones: %w", err)
			}
			if limit > 0 && len(srcMs) > limit {
				srcMs = srcMs[:limit]
			}

			// Milestones: dedup by title (no URL marker possible)
			dstTitles, err := buildMilestoneTitleIndex(dstC, dstProject)
			if err != nil {
				return fmt.Errorf("build dst index: %w", err)
			}
			info("src: %d milestones  already present: %d", len(srcMs), len(dstTitles))
			fmt.Println()

			created, skipped, failed := 0, 0, 0
			for _, ms := range srcMs {
				if _, exists := dstTitles[ms.Title]; exists {
					skipped++
					continue
				}

				if dryRun {
					fmt.Printf("  %s %q\n", colorDim("[dry]"), truncate(ms.Title, 60))
					created++
					continue
				}

				opts := &gl.CreateMilestoneOptions{
					Title: gl.Ptr(ms.Title),
				}
				if ms.Description != "" {
					opts.Description = gl.Ptr(ms.Description)
				}

				newMs, _, err := dstC.GL.Milestones.CreateMilestone(dstProject, opts)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  %s %s: %v\n",
						colorErr("✗"), truncate(ms.Title, 50), err)
					dstC.RecErr(journal.OpCreate, journal.EntityMilestone, dstProject, "", err.Error())
					failed++
					continue
				}

				srcC.Rec(journal.OpList, journal.EntityMilestone, srcProject, "",
					ms.ID, ms.IID, ms.Title, ms.WebURL, "sync-src")
				dstC.Rec(journal.OpCreate, journal.EntityMilestone, dstProject, "",
					newMs.ID, newMs.IID, newMs.Title, newMs.WebURL, "sync-dst")
				fmt.Printf("  %s %q  (id %d)\n", colorOK("✓"), ms.Title, newMs.ID)
				created++
			}

			printSyncSummary(created, skipped, failed, dryRun)
			return nil
		},
	}

	addSyncProjectFlags(cmd, &srcHost, &srcProject, &dstHost, &dstProject, &dryRun, &limit)
	return cmd
}

func buildMilestoneTitleIndex(dstC *client.Client, dstProject string) (map[string]struct{}, error) {
	index := make(map[string]struct{})
	ms, _, err := dstC.GL.Milestones.ListMilestones(dstProject,
		&gl.ListMilestonesOptions{
			State:       gl.Ptr("all"),
			ListOptions: gl.ListOptions{PerPage: 100},
		})
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		index[m.Title] = struct{}{}
	}
	return index, nil
}

// ─── shared flag helpers ──────────────────────────────────────────────────────

func addSyncProjectFlags(cmd *cobra.Command,
	srcHost, srcProject, dstHost, dstProject *string,
	dryRun *bool, limit *int) {

	cmd.Flags().StringVar(srcHost, "src-host", "",
		"Source hostname (default: active host from glab config)")
	cmd.Flags().StringVar(srcProject, "src-project", "",
		"Source project path or numeric ID")
	cmd.Flags().StringVar(dstHost, "dst-host", "",
		"Destination hostname (default: active host from glab config)")
	cmd.Flags().StringVar(dstProject, "dst-project", "",
		"Destination project path or numeric ID")
	cmd.Flags().BoolVar(dryRun, "dry-run", false,
		"Show what would be created without writing anything")
	cmd.Flags().IntVar(limit, "limit", 0,
		"Maximum items to process per run (0 = all)")
}

// ─── display helpers ──────────────────────────────────────────────────────────

func printSyncHeader(entity, srcHost, srcScope, dstHost, dstScope string, dry bool) {
	dryTag := ""
	if dry {
		dryTag = colorDim(" [dry-run]")
	}
	fmt.Printf("\n%s%s\n",
		colorInfo(fmt.Sprintf("Syncing %s: ", entity)),
		colorDim(fmt.Sprintf("%s:%s  →  %s:%s",
			shortHostname(srcHost), srcScope,
			shortHostname(dstHost), dstScope))+dryTag)
	fmt.Println()
}

func printSyncSummary(created, skipped, failed int, dry bool) {
	verb := "created"
	if dry {
		verb = "would create"
	}
	errStr := colorDim("—")
	if failed > 0 {
		errStr = colorErr(fmt.Sprintf("%d failed", failed))
	}
	fmt.Printf("\n%s %d %s  %s  %s\n\n",
		colorOK("✓"), created, verb,
		colorDim(fmt.Sprintf("%d skipped", skipped)),
		errStr)
}
