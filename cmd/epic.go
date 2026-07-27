package cmd

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func epicCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "epic",
		Short:   "Manage group epics (GitLab-native; GitHub/Gitea unsupported)",
		Aliases: []string{"e"},
	}
	cmd.AddCommand(
		epicListCmd(),
		epicCreateCmd(),
		epicUpdateCmd(),
		epicCloseCmd(),
		epicIssuesCmd(),
	)
	return cmd
}

// ─── list ─────────────────────────────────────────────────────────────────────

func epicListCmd() *cobra.Command {
	var group, state string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List epics in a group",
		RunE: func(cmd *cobra.Command, args []string) error {
			if group == "" {
				group = cfg.DefaultGroup
			}
			if group == "" {
				return fmt.Errorf("--group required (or set GLVALET_DEFAULT_GROUP)")
			}

			groupID, err := glClient.Provider.ResolveGroup(cmd.Context(), group)
			if err != nil {
				glClient.RecErr(journal.OpList, journal.EntityEpic, "", group, err.Error())
				return fmt.Errorf("resolve group: %w", err)
			}

			epics, err := glClient.Provider.ListGroupEpics(cmd.Context(), groupID, provider.ListGroupEpicsOptions{
				State:   state,
				PerPage: 50,
			})
			if err != nil {
				glClient.RecErr(journal.OpList, journal.EntityEpic, "", group, err.Error())
				return fmt.Errorf("list epics: %w", err)
			}

			glClient.Rec(journal.OpList, journal.EntityEpic, "", group, 0, 0,
				fmt.Sprintf("list %d epics (state=%s)", len(epics), state), "")

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"IID", "Title", "State", "Author", "Start", "Due", "Issues"})
			table.SetBorder(false)
			table.SetAutoWrapText(false)

			for _, ep := range epics {
				table.Append([]string{
					strconv.Itoa(ep.IID),
					truncate(ep.Title, 55),
					ep.State,
					ep.Author.Username,
					ep.StartDate,
					ep.DueDate,
					"n/a",
				})
			}
			table.Render()
			info("group: %s  total: %d", group, len(epics))
			return nil
		},
	}
	cmd.Flags().StringVarP(&group, "group", "g", "", "Group path or ID")
	cmd.Flags().StringVar(&state, "state", "opened", "State: opened|closed|all")
	return cmd
}

// ─── create ───────────────────────────────────────────────────────────────────

func epicCreateCmd() *cobra.Command {
	var group, title, description, startDate, dueDate string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an epic",
		RunE: func(cmd *cobra.Command, args []string) error {
			if group == "" {
				group = cfg.DefaultGroup
			}
			if group == "" {
				return fmt.Errorf("--group required")
			}
			if title == "" {
				return fmt.Errorf("--title required")
			}

			groupID, err := glClient.Provider.ResolveGroup(cmd.Context(), group)
			if err != nil {
				glClient.RecErr(journal.OpCreate, journal.EntityEpic, "", group, err.Error())
				return fmt.Errorf("resolve group: %w", err)
			}

			opts := provider.CreateEpicOptions{
				Title:       title,
				Description: description,
			}
			// Note: the host-neutral CreateEpicOptions does not carry
			// start/due dates because GitHub has no epic concept and Gitea
			// has no first-class epic. Start/due dates remain GitLab-only
			// for now; they are accepted as flags but ignored on the
			// neutral surface. A future Provider extension can add them
			// if a cross-host epic timeline emerges.
			if startDate != "" {
				if _, err := time.Parse("2006-01-02", startDate); err != nil {
					return fmt.Errorf("invalid --start-date (YYYY-MM-DD)")
				}
			}
			if dueDate != "" {
				if _, err := time.Parse("2006-01-02", dueDate); err != nil {
					return fmt.Errorf("invalid --due-date (YYYY-MM-DD)")
				}
			}

			ep, err := glClient.Provider.CreateGroupEpic(cmd.Context(), groupID, opts)
			if err != nil {
				glClient.RecErr(journal.OpCreate, journal.EntityEpic, "", group, err.Error())
				return fmt.Errorf("create epic: %w", err)
			}

			glClient.Rec(journal.OpCreate, journal.EntityEpic, "", group, ep.ID, ep.IID,
				ep.Title, ep.WebURL)
			ok("Epic &%d created: %s", ep.IID, ep.WebURL)
			return nil
		},
	}
	cmd.Flags().StringVarP(&group, "group", "g", "", "Group path or ID")
	cmd.Flags().StringVarP(&title, "title", "t", "", "Epic title (required)")
	cmd.Flags().StringVarP(&description, "description", "d", "", "Description (markdown)")
	cmd.Flags().StringVar(&startDate, "start-date", "", "Start date YYYY-MM-DD")
	cmd.Flags().StringVar(&dueDate, "due-date", "", "Due date YYYY-MM-DD")
	return cmd
}

// ─── update ───────────────────────────────────────────────────────────────────

func epicUpdateCmd() *cobra.Command {
	var group, title, description string
	var iid int

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update an epic",
		RunE: func(cmd *cobra.Command, args []string) error {
			if group == "" {
				group = cfg.DefaultGroup
			}
			if group == "" {
				return fmt.Errorf("--group required")
			}
			if iid == 0 {
				return fmt.Errorf("--iid required")
			}

			groupID, err := glClient.Provider.ResolveGroup(cmd.Context(), group)
			if err != nil {
				glClient.RecErr(journal.OpUpdate, journal.EntityEpic, "", group, err.Error())
				return fmt.Errorf("resolve group: %w", err)
			}

			opts := provider.UpdateEpicOptions{}
			if title != "" {
				opts.Title = &title
			}
			if description != "" {
				opts.Description = &description
			}

			ep, err := glClient.Provider.UpdateGroupEpic(cmd.Context(), groupID, iid, opts)
			if err != nil {
				glClient.RecErr(journal.OpUpdate, journal.EntityEpic, "", group, err.Error())
				return fmt.Errorf("update epic: %w", err)
			}

			glClient.Rec(journal.OpUpdate, journal.EntityEpic, "", group, ep.ID, ep.IID,
				ep.Title, ep.WebURL)
			ok("Epic &%d updated: %s", ep.IID, ep.Title)
			return nil
		},
	}
	cmd.Flags().StringVarP(&group, "group", "g", "", "Group path or ID")
	cmd.Flags().IntVar(&iid, "iid", 0, "Epic IID")
	cmd.Flags().StringVarP(&title, "title", "t", "", "New title")
	cmd.Flags().StringVarP(&description, "description", "d", "", "New description")
	return cmd
}

// ─── close ────────────────────────────────────────────────────────────────────

func epicCloseCmd() *cobra.Command {
	var group string
	var iid int

	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close an epic",
		RunE: func(cmd *cobra.Command, args []string) error {
			if group == "" {
				group = cfg.DefaultGroup
			}
			if group == "" {
				return fmt.Errorf("--group required")
			}
			if iid == 0 {
				return fmt.Errorf("--iid required")
			}

			groupID, err := glClient.Provider.ResolveGroup(cmd.Context(), group)
			if err != nil {
				glClient.RecErr(journal.OpClose, journal.EntityEpic, "", group, err.Error())
				return fmt.Errorf("resolve group: %w", err)
			}

			closed := "close"
			ep, err := glClient.Provider.UpdateGroupEpic(cmd.Context(), groupID, iid, provider.UpdateEpicOptions{
				State: &closed,
			})
			if err != nil {
				glClient.RecErr(journal.OpClose, journal.EntityEpic, "", group, err.Error())
				return fmt.Errorf("close epic: %w", err)
			}

			glClient.Rec(journal.OpClose, journal.EntityEpic, "", group, ep.ID, ep.IID,
				ep.Title, ep.WebURL)
			ok("Epic &%d closed", ep.IID)
			return nil
		},
	}
	cmd.Flags().StringVarP(&group, "group", "g", "", "Group path or ID")
	cmd.Flags().IntVar(&iid, "iid", 0, "Epic IID")
	return cmd
}

// ─── issues under epic ────────────────────────────────────────────────────────

func epicIssuesCmd() *cobra.Command {
	var group string
	var iid int

	cmd := &cobra.Command{
		Use:   "issues",
		Short: "List issues linked to an epic",
		RunE: func(cmd *cobra.Command, args []string) error {
			if group == "" {
				group = cfg.DefaultGroup
			}
			if group == "" {
				return fmt.Errorf("--group required")
			}
			if iid == 0 {
				return fmt.Errorf("--iid required")
			}

			groupID, err := glClient.Provider.ResolveGroup(cmd.Context(), group)
			if err != nil {
				return fmt.Errorf("resolve group: %w", err)
			}

			issues, err := glClient.Provider.ListEpicIssues(cmd.Context(), groupID, iid)
			if err != nil {
				return fmt.Errorf("list epic issues: %w", err)
			}

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"IID", "Title", "Project", "State"})
			table.SetBorder(false)
			for _, iss := range issues {
				table.Append([]string{
					strconv.Itoa(iss.IID),
					truncate(iss.Title, 55),
					iss.Project,
					iss.State,
				})
			}
			table.Render()
			return nil
		},
	}
	cmd.Flags().StringVarP(&group, "group", "g", "", "Group path or ID")
	cmd.Flags().IntVar(&iid, "iid", 0, "Epic IID")
	return cmd
}
