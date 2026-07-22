package cmd

import (
	"fmt"
	"os"
	"strconv"

	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func issueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "issue",
		Short:   "Manage GitLab issues",
		Aliases: []string{"i"},
	}
	cmd.AddCommand(
		issueListCmd(),
		issueCreateCmd(),
		issueUpdateCmd(),
		issueCloseCmd(),
		issueCommentCmd(),
		issueMineCmd(),
	)
	return cmd
}

// ─── list ─────────────────────────────────────────────────────────────────────

func issueListCmd() *cobra.Command {
	var project, state, assignee, labels string
	var milestone string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List issues in a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required (or set GLVALET_DEFAULT_PROJECT)")
			}

			opts := provider.ListIssuesOptions{
				State:     state,
				Assignee:  assignee,
				Milestone: milestone,
				PerPage:   50,
			}
			if labels != "" {
				opts.Labels = []string{labels}
			}

			issues, err := glClient.Provider.ListIssues(cmd.Context(), project, opts)
			if err != nil {
				glClient.RecErr(journal.OpList, journal.EntityIssue, project, "", err.Error())
				return fmt.Errorf("list issues: %w", err)
			}

			glClient.Rec(journal.OpList, journal.EntityIssue, project, "", 0, 0,
				fmt.Sprintf("list %d issues (state=%s)", len(issues), state), "")

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"IID", "Title", "State", "Assignee", "Milestone", "Labels"})
			table.SetBorder(false)
			table.SetAutoWrapText(false)
			for _, iss := range issues {
				assigneeStr := ""
				if len(iss.Assignees) > 0 {
					assigneeStr = iss.Assignees[0].Username
				}
				lbls := ""
				for i, l := range iss.Labels {
					if i > 0 {
						lbls += ", "
					}
					lbls += l
				}
				table.Append([]string{
					strconv.Itoa(iss.IID),
					truncate(iss.Title, 55),
					iss.State,
					assigneeStr,
					iss.Milestone,
					truncate(lbls, 30),
				})
			}
			table.Render()
			info("project: %s  total: %d", project, len(issues))
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().StringVar(&state, "state", "opened", "State filter: opened|closed|all")
	cmd.Flags().StringVar(&assignee, "assignee", "", "Filter by assignee username")
	cmd.Flags().StringVar(&labels, "labels", "", "Comma-separated label filter")
	cmd.Flags().StringVar(&milestone, "milestone", "", "Milestone title filter")
	return cmd
}

// ─── mine ─────────────────────────────────────────────────────────────────────

func issueMineCmd() *cobra.Command {
	var state string

	cmd := &cobra.Command{
		Use:   "mine",
		Short: "List issues assigned to me (across all projects)",
		RunE: func(cmd *cobra.Command, args []string) error {
			issues, err := glClient.Provider.ListMyIssues(cmd.Context(), provider.ListMyIssuesOptions{
				State:   state,
				PerPage: 50,
			})
			if err != nil {
				glClient.RecErr(journal.OpList, journal.EntityIssue, "all", "", err.Error())
				return fmt.Errorf("list my issues: %w", err)
			}

			glClient.Rec(journal.OpList, journal.EntityIssue, "all", "", 0, 0,
				fmt.Sprintf("my issues: %d (state=%s)", len(issues), state), "")

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"IID", "Title", "Project", "State", "Milestone"})
			table.SetBorder(false)
			table.SetAutoWrapText(false)
			for _, iss := range issues {
				table.Append([]string{
					strconv.Itoa(iss.IID),
					truncate(iss.Title, 50),
					iss.Project,
					iss.State,
					iss.Milestone,
				})
			}
			table.Render()
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "opened", "State: opened|closed|all")
	return cmd
}

// ─── create ───────────────────────────────────────────────────────────────────

func issueCreateCmd() *cobra.Command {
	var project, title, description, assignee, labels, milestone string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new issue",
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

			ctx := cmd.Context()
			opts := provider.CreateIssueOptions{
				Title:       title,
				Description: description,
			}

			if assignee != "" {
				users, err := glClient.Provider.ListUsers(ctx, provider.ListUsersOptions{
					Search:  assignee,
					PerPage: 1,
				})
				if err == nil && len(users) > 0 {
					opts.AssigneeIDs = []int{users[0].ID}
				}
			}

			iss, err := glClient.Provider.CreateIssue(ctx, project, opts)
			if err != nil {
				glClient.RecErr(journal.OpCreate, journal.EntityIssue, project, "", err.Error())
				return fmt.Errorf("create issue: %w", err)
			}

			glClient.Rec(journal.OpCreate, journal.EntityIssue, project, "", iss.ID, iss.IID,
				iss.Title, iss.WebURL)
			ok("Issue #%d created: %s", iss.IID, iss.WebURL)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().StringVarP(&title, "title", "t", "", "Issue title (required)")
	cmd.Flags().StringVarP(&description, "description", "d", "", "Issue description (markdown)")
	cmd.Flags().StringVar(&assignee, "assignee", "", "Assignee username")
	cmd.Flags().StringVar(&labels, "labels", "", "Comma-separated labels")
	cmd.Flags().StringVar(&milestone, "milestone", "", "Milestone title")
	return cmd
}

// ─── update ───────────────────────────────────────────────────────────────────

func issueUpdateCmd() *cobra.Command {
	var project, title, description, labels string
	var iid int

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update an existing issue",
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

			opts := provider.UpdateIssueOptions{}
			if title != "" {
				opts.Title = &title
			}
			if description != "" {
				opts.Description = &description
			}

			iss, err := glClient.Provider.UpdateIssue(cmd.Context(), project, iid, opts)
			if err != nil {
				glClient.RecErr(journal.OpUpdate, journal.EntityIssue, project, "", err.Error())
				return fmt.Errorf("update issue: %w", err)
			}

			glClient.Rec(journal.OpUpdate, journal.EntityIssue, project, "", iss.ID, iss.IID,
				iss.Title, iss.WebURL)
			ok("Issue #%d updated: %s", iss.IID, iss.Title)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&iid, "iid", 0, "Issue IID (required)")
	cmd.Flags().StringVarP(&title, "title", "t", "", "New title")
	cmd.Flags().StringVarP(&description, "description", "d", "", "New description")
	cmd.Flags().StringVar(&labels, "labels", "", "New labels")
	return cmd
}

// ─── close ────────────────────────────────────────────────────────────────────

func issueCloseCmd() *cobra.Command {
	var project string
	var iid int

	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close an issue",
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
			iss, err := glClient.Provider.UpdateIssue(cmd.Context(), project, iid, provider.UpdateIssueOptions{
				State: &closed,
			})
			if err != nil {
				glClient.RecErr(journal.OpClose, journal.EntityIssue, project, "", err.Error())
				return fmt.Errorf("close issue: %w", err)
			}

			glClient.Rec(journal.OpClose, journal.EntityIssue, project, "", iss.ID, iss.IID,
				iss.Title, iss.WebURL)
			ok("Issue #%d closed: %s", iss.IID, iss.Title)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&iid, "iid", 0, "Issue IID (required)")
	return cmd
}

// ─── comment ──────────────────────────────────────────────────────────────────

func issueCommentCmd() *cobra.Command {
	var project, body string
	var iid int

	cmd := &cobra.Command{
		Use:   "comment",
		Short: "Add a comment to an issue",
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
			if body == "" {
				return fmt.Errorf("--body required")
			}

			note, err := glClient.Provider.CreateIssueNote(cmd.Context(), project, iid, body)
			if err != nil {
				glClient.RecErr(journal.OpComment, journal.EntityIssue, project, "", err.Error())
				return fmt.Errorf("comment: %w", err)
			}

			glClient.Rec(journal.OpComment, journal.EntityIssue, project, "", note.ID, iid,
				truncate(body, 60), "")
			ok("Comment added to issue #%d", iid)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&iid, "iid", 0, "Issue IID")
	cmd.Flags().StringVarP(&body, "body", "b", "", "Comment body (markdown)")
	return cmd
}

// ─── util ─────────────────────────────────────────────────────────────────────

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
