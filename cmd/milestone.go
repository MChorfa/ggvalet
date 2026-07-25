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

func milestoneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "milestone",
		Short:   "Manage project milestones",
		Aliases: []string{"ms"},
	}
	cmd.AddCommand(
		milestoneListCmd(),
		milestoneCreateCmd(),
		milestoneCloseCmd(),
		milestoneStatsCmd(),
	)
	return cmd
}

// ─── list ─────────────────────────────────────────────────────────────────────

func milestoneListCmd() *cobra.Command {
	var project, state string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List milestones",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}

			ms, err := glClient.Provider.ListMilestones(cmd.Context(), project, provider.ListMilestonesOptions{
				State:   state,
				PerPage: 50,
			})
			if err != nil {
				glClient.RecErr(journal.OpList, journal.EntityMilestone, project, "", err.Error())
				return fmt.Errorf("list milestones: %w", err)
			}

			glClient.Rec(journal.OpList, journal.EntityMilestone, project, "", 0, 0,
				fmt.Sprintf("list %d milestones", len(ms)), "")

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"ID", "Title", "State", "Start", "Due", "Open Issues", "Closed Issues"})
			table.SetBorder(false)
			for _, m := range ms {
				table.Append([]string{
					strconv.Itoa(m.ID),
					truncate(m.Title, 45),
					m.State,
					m.StartDate,
					m.DueDate,
					"n/a",
					"n/a",
				})
			}
			table.Render()
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().StringVar(&state, "state", "active", "State: active|closed|all")
	return cmd
}

// ─── create ───────────────────────────────────────────────────────────────────

func milestoneCreateCmd() *cobra.Command {
	var project, title, description, startDate, dueDate string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a milestone",
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

			opts := provider.CreateMilestoneOptions{
				Title:       title,
				Description: description,
			}
			if startDate != "" {
				if _, err := time.Parse("2006-01-02", startDate); err != nil {
					return fmt.Errorf("invalid --start-date (YYYY-MM-DD)")
				}
				opts.StartDate = startDate
			}
			if dueDate != "" {
				if _, err := time.Parse("2006-01-02", dueDate); err != nil {
					return fmt.Errorf("invalid --due-date (YYYY-MM-DD)")
				}
				opts.DueDate = dueDate
			}

			m, err := glClient.Provider.CreateMilestone(cmd.Context(), project, opts)
			if err != nil {
				glClient.RecErr(journal.OpCreate, journal.EntityMilestone, project, "", err.Error())
				return fmt.Errorf("create milestone: %w", err)
			}

			glClient.Rec(journal.OpCreate, journal.EntityMilestone, project, "", m.ID, m.IID,
				m.Title, m.WebURL)
			ok("Milestone #%d created: %s", m.IID, m.WebURL)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().StringVarP(&title, "title", "t", "", "Milestone title (required)")
	cmd.Flags().StringVarP(&description, "description", "d", "", "Description")
	cmd.Flags().StringVar(&startDate, "start-date", "", "Start date YYYY-MM-DD")
	cmd.Flags().StringVar(&dueDate, "due-date", "", "Due date YYYY-MM-DD")
	return cmd
}

// ─── close ────────────────────────────────────────────────────────────────────

func milestoneCloseCmd() *cobra.Command {
	var project string
	var id int

	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close (deactivate) a milestone",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if id == 0 {
				return fmt.Errorf("--id required (milestone numeric ID, not IID)")
			}

			closed := "close"
			m, err := glClient.Provider.UpdateMilestone(cmd.Context(), project, id, provider.UpdateMilestoneOptions{
				State: &closed,
			})
			if err != nil {
				glClient.RecErr(journal.OpClose, journal.EntityMilestone, project, "", err.Error())
				return fmt.Errorf("close milestone: %w", err)
			}

			glClient.Rec(journal.OpClose, journal.EntityMilestone, project, "", m.ID, m.IID,
				m.Title, m.WebURL)
			ok("Milestone %q closed", m.Title)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&id, "id", 0, "Milestone numeric ID")
	return cmd
}

// ─── stats ────────────────────────────────────────────────────────────────────

func milestoneStatsCmd() *cobra.Command {
	var project string
	var id int

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show issue/MR statistics for a milestone",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if id == 0 {
				return fmt.Errorf("--id required")
			}

			m, err := glClient.Provider.GetMilestone(cmd.Context(), project, id)
			if err != nil {
				return fmt.Errorf("get milestone: %w", err)
			}

			fmt.Printf("\nMilestone: %s\n", m.Title)
			fmt.Printf("  State          : %s\n", m.State)
			if m.DueDate != "" {
				fmt.Printf("  Due            : %s\n", m.DueDate)
			}
			fmt.Printf("  Statistics     : unavailable in this client version\n")
			fmt.Println()
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&id, "id", 0, "Milestone numeric ID")
	return cmd
}
