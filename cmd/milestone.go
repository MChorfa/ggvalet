package cmd

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	gl "github.com/xanzy/go-gitlab"
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

			ms, _, err := glClient.GL.Milestones.ListMilestones(project, &gl.ListMilestonesOptions{
				State:       gl.Ptr(state),
				ListOptions: gl.ListOptions{PerPage: 50},
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
				start, due := "", ""
				if m.StartDate != nil {
					start = m.StartDate.String()
				}
				if m.DueDate != nil {
					due = m.DueDate.String()
				}
				table.Append([]string{
					strconv.Itoa(m.ID),
					truncate(m.Title, 45),
					m.State,
					start, due,
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

			opts := &gl.CreateMilestoneOptions{
				Title: gl.Ptr(title),
			}
			if description != "" {
				opts.Description = gl.Ptr(description)
			}
			if startDate != "" {
				t, err := time.Parse("2006-01-02", startDate)
				if err != nil {
					return fmt.Errorf("invalid --start-date: %w", err)
				}
				iso := gl.ISOTime(t)
				opts.StartDate = &iso
			}
			if dueDate != "" {
				t, err := time.Parse("2006-01-02", dueDate)
				if err != nil {
					return fmt.Errorf("invalid --due-date: %w", err)
				}
				iso := gl.ISOTime(t)
				opts.DueDate = &iso
			}

			m, _, err := glClient.GL.Milestones.CreateMilestone(project, opts)
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

			m, _, err := glClient.GL.Milestones.UpdateMilestone(project, id, &gl.UpdateMilestoneOptions{
				StateEvent: gl.Ptr("close"),
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

			m, _, err := glClient.GL.Milestones.GetMilestone(project, id)
			if err != nil {
				return fmt.Errorf("get milestone: %w", err)
			}

			fmt.Printf("\nMilestone: %s\n", m.Title)
			fmt.Printf("  State          : %s\n", m.State)
			if m.DueDate != nil {
				fmt.Printf("  Due            : %s\n", m.DueDate.String())
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
