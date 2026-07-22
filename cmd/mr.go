// cmd/mr.go — merge request management with colored terminal diff.
package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/charmbracelet/lipgloss"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// ─── diff styles ──────────────────────────────────────────────────────────────

var (
	diffAdd  = lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
	diffDel  = lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))
	diffHunk = lipgloss.NewStyle().Foreground(lipgloss.Color("#38a3a5")).Bold(true)
	diffFile = lipgloss.NewStyle().Foreground(lipgloss.Color("#bc96e6")).Bold(true)
	diffCtx  = lipgloss.NewStyle().Foreground(lipgloss.Color("#777777"))
)

// ─── root ─────────────────────────────────────────────────────────────────────

// mrCmd groups merge-request subcommands. All run on glClient.Provider.* —
// the package no longer imports the GitLab SDK directly.
func mrCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "mr",
		Short:   "Manage merge requests",
		Aliases: []string{"mrs"},
	}
	cmd.AddCommand(
		mrListCmd(),
		mrMineCmd(),
		mrCreateCmd(),
		mrApproveCmd(),
		mrMergeCmd(),
		mrDiffCmd(),
		mrCloseCmd(),
	)
	return cmd
}

// ─── list ─────────────────────────────────────────────────────────────────────

func mrListCmd() *cobra.Command {
	var project, state, assignee, labels string
	var noCache bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List merge requests in a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}

			cacheKey := glClient.CacheKey("mr/list", project+state+assignee)
			var mrs []provider.MergeRequest

			if !noCache && glClient.Cache != nil && glClient.Cache.Unmarshal(cacheKey, &mrs) {
				info("(cached)")
			} else {
				var err error
				mrs, err = glClient.Provider.ListMergeRequests(cmd.Context(), project,
					provider.ListMergeRequestsOptions{State: state, PerPage: 50})
				if err != nil {
					glClient.RecErr(journal.OpList, journal.EntityMR, project, "", err.Error())
					return fmt.Errorf("list MRs: %w", err)
				}
				if glClient.Cache != nil {
					glClient.Cache.Set(cacheKey, mrs, 5*time.Minute)
				}
			}

			glClient.Rec(journal.OpList, journal.EntityMR, project, "", 0, 0,
				fmt.Sprintf("list %d MRs", len(mrs)), "")

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"IID", "Title", "State", "Author", "Assignee", "Branch", "Pipeline"})
			table.SetBorder(false)
			table.SetAutoWrapText(false)
			for _, mr := range mrs {
				assigneeStr := ""
				if len(mr.Assignees) > 0 {
					assigneeStr = mr.Assignees[0].Username
				}
				branch := mr.SourceBranch + " → " + mr.TargetBranch
				table.Append([]string{
					strconv.Itoa(mr.IID),
					truncate(mr.Title, 50),
					mr.State,
					mr.Author.Username,
					assigneeStr,
					truncate(branch, 30),
					mr.Pipeline,
				})
			}
			table.Render()
			info("project: %s  total: %d", project, len(mrs))
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().StringVar(&state, "state", "opened", "State: opened|closed|merged|all")
	cmd.Flags().StringVar(&assignee, "assignee", "", "Filter by assignee username")
	cmd.Flags().StringVar(&labels, "labels", "", "Label filter")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "Bypass cache and fetch fresh data")
	return cmd
}

// ─── mine ─────────────────────────────────────────────────────────────────────

func mrMineCmd() *cobra.Command {
	var state string

	cmd := &cobra.Command{
		Use:   "mine",
		Short: "List MRs authored by or assigned to me across all projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			mrs, err := glClient.Provider.ListMyMergeRequests(cmd.Context(),
				provider.ListMyMergeRequestsOptions{State: state, PerPage: 50})
			if err != nil {
				return fmt.Errorf("list my MRs: %w", err)
			}

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"IID", "Title", "Project", "State", "Branch", "Pipeline"})
			table.SetBorder(false)
			table.SetAutoWrapText(false)
			for _, mr := range mrs {
				table.Append([]string{
					strconv.Itoa(mr.IID),
					truncate(mr.Title, 45),
					mr.Project,
					mr.State,
					truncate(mr.SourceBranch, 25),
					mr.Pipeline,
				})
			}
			table.Render()
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "opened", "State filter")
	return cmd
}

// ─── create ───────────────────────────────────────────────────────────────────

func mrCreateCmd() *cobra.Command {
	var project, title, description, source, target, assignee string
	var draft bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a merge request",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if title == "" {
				return fmt.Errorf("--title required")
			}
			if source == "" {
				return fmt.Errorf("--source required (source branch)")
			}
			if target == "" {
				target = "main"
			}
			if draft {
				title = "Draft: " + title
			}

			ctx := cmd.Context()
			opts := provider.CreateMergeRequestOptions{
				Title:        title,
				Description:  description,
				SourceBranch: source,
				TargetBranch: target,
			}
			if assignee != "" {
				users, err := glClient.Provider.ListUsers(ctx, provider.ListUsersOptions{Search: assignee, PerPage: 1})
				if err == nil && len(users) > 0 {
					opts.AssigneeID = &users[0].ID
				}
			}

			mr, err := glClient.Provider.CreateMergeRequest(ctx, project, opts)
			if err != nil {
				glClient.RecErr(journal.OpCreate, journal.EntityMR, project, "", err.Error())
				return fmt.Errorf("create MR: %w", err)
			}

			glClient.Rec(journal.OpCreate, journal.EntityMR, project, "", mr.ID, mr.IID, mr.Title, mr.WebURL)
			ok("MR !%d created: %s", mr.IID, mr.WebURL)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().StringVarP(&title, "title", "t", "", "MR title (required)")
	cmd.Flags().StringVarP(&description, "description", "d", "", "MR description")
	cmd.Flags().StringVar(&source, "source", "", "Source branch (required)")
	cmd.Flags().StringVar(&target, "target", "main", "Target branch")
	cmd.Flags().StringVar(&assignee, "assignee", "", "Assignee username")
	cmd.Flags().BoolVar(&draft, "draft", false, "Mark as draft")
	return cmd
}

// ─── approve ──────────────────────────────────────────────────────────────────

func mrApproveCmd() *cobra.Command {
	var project string
	var iid int

	cmd := &cobra.Command{
		Use:   "approve",
		Short: "Approve a merge request",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if iid == 0 {
				return fmt.Errorf("--iid required")
			}

			err := glClient.Provider.ApproveMergeRequest(cmd.Context(), project, iid)
			if err != nil {
				glClient.RecErr(journal.OpUpdate, journal.EntityMR, project, "", err.Error())
				return fmt.Errorf("approve: %w", err)
			}

			glClient.Rec(journal.OpUpdate, journal.EntityMR, project, "", 0, iid,
				fmt.Sprintf("approved MR !%d", iid), "", "approved")
			ok("MR !%d approved", iid)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&iid, "iid", 0, "MR IID (required)")
	return cmd
}

// ─── merge ────────────────────────────────────────────────────────────────────

func mrMergeCmd() *cobra.Command {
	var project string
	var iid int
	var deleteSourceBranch, squash bool

	cmd := &cobra.Command{
		Use:   "merge",
		Short: "Merge an approved merge request",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if iid == 0 {
				return fmt.Errorf("--iid required")
			}

			mr, err := glClient.Provider.MergeMergeRequest(cmd.Context(), project, iid,
				provider.MergeOptions{RemoveSourceBranch: deleteSourceBranch, Squash: squash})
			if err != nil {
				glClient.RecErr(journal.OpUpdate, journal.EntityMR, project, "", err.Error())
				return fmt.Errorf("merge: %w", err)
			}

			glClient.Rec(journal.OpUpdate, journal.EntityMR, project, "", mr.ID, mr.IID,
				mr.Title, mr.WebURL, "merged")
			ok("MR !%d merged: %s", mr.IID, mr.WebURL)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&iid, "iid", 0, "MR IID (required)")
	cmd.Flags().BoolVar(&deleteSourceBranch, "delete-branch", true, "Delete source branch after merge")
	cmd.Flags().BoolVar(&squash, "squash", false, "Squash commits on merge")
	return cmd
}

// ─── close ────────────────────────────────────────────────────────────────────

func mrCloseCmd() *cobra.Command {
	var project string
	var iid int

	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close (abandon) a merge request without merging",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if iid == 0 {
				return fmt.Errorf("--iid required")
			}
			closed := "closed"
			mr, err := glClient.Provider.UpdateMergeRequest(cmd.Context(), project, iid,
				provider.UpdateMergeRequestOptions{State: &closed})
			if err != nil {
				return fmt.Errorf("close MR: %w", err)
			}
			glClient.Rec(journal.OpClose, journal.EntityMR, project, "", mr.ID, mr.IID, mr.Title, mr.WebURL)
			ok("MR !%d closed", mr.IID)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&iid, "iid", 0, "MR IID (required)")
	return cmd
}

// ─── diff ─────────────────────────────────────────────────────────────────────

func mrDiffCmd() *cobra.Command {
	var project string
	var iid, maxFiles int

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Show colored unified diff for a merge request",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if iid == 0 {
				return fmt.Errorf("--iid required")
			}

			ctx := cmd.Context()

			// Fetch MR header
			mr, err := glClient.Provider.GetMergeRequest(ctx, project, iid)
			if err != nil {
				return fmt.Errorf("get MR: %w", err)
			}

			// Fetch diff
			changes, err := glClient.Provider.GetMergeRequestDiff(ctx, project, iid)
			if err != nil {
				return fmt.Errorf("get diff: %w", err)
			}

			glClient.Rec(journal.OpList, journal.EntityMR, project, "", mr.ID, mr.IID,
				mr.Title, mr.WebURL, "diff")

			// Print header
			fmt.Printf("\n%s\n", diffFile.Render(fmt.Sprintf("MR !%d — %s", mr.IID, mr.Title)))
			fmt.Printf("%s → %s\n",
				lipgloss.NewStyle().Foreground(lipgloss.Color("#bc96e6")).Render(mr.SourceBranch),
				lipgloss.NewStyle().Foreground(lipgloss.Color("#38a3a5")).Render(mr.TargetBranch))
			fmt.Printf("%s %d file(s)\n\n",
				colorDim("changes:"),
				len(changes))

			// Render each file diff
			shown := 0
			for _, ch := range changes {
				if maxFiles > 0 && shown >= maxFiles {
					fmt.Printf("%s\n", colorDim(fmt.Sprintf("… %d more files (use --max-files 0 to show all)", len(changes)-shown)))
					break
				}
				renderFileDiff(ch)
				shown++
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&iid, "iid", 0, "MR IID (required)")
	cmd.Flags().IntVar(&maxFiles, "max-files", 10, "Max files to show (0 = all)")
	return cmd
}

// renderFileDiff prints a single file's diff with ANSI colors.
func renderFileDiff(ch provider.FileDiff) {
	header := ch.NewPath
	if ch.NewPath != ch.OldPath {
		header = ch.OldPath + " → " + ch.NewPath
	}
	status := ""
	if ch.NewFile {
		status = " [new]"
	} else if ch.DeletedFile {
		status = " [deleted]"
	} else if ch.RenamedFile {
		status = " [renamed]"
	}
	fmt.Println(diffFile.Render("── " + header + status))

	if ch.Diff == "" {
		fmt.Println(colorDim("  (binary or empty)"))
		fmt.Println()
		return
	}

	lines := strings.Split(ch.Diff, "\n")
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			// skip file markers — already shown above
		case strings.HasPrefix(line, "@@"):
			fmt.Println(diffHunk.Render(line))
		case strings.HasPrefix(line, "+"):
			fmt.Println(diffAdd.Render(line))
		case strings.HasPrefix(line, "-"):
			fmt.Println(diffDel.Render(line))
		default:
			fmt.Println(diffCtx.Render(line))
		}
	}
	fmt.Println()
}
