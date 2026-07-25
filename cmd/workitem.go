// Work Items are a newer GitLab feature, available via both REST and GraphQL.
// This file uses the host-neutral provider.WorkItem surface, which maps to
// GitLab's /projects/:id/work_items REST endpoint. On GitHub/Gitea the
// provider returns ErrUnsupported.
// Requires GitLab 15.1+ with work_items feature flag enabled on your instance.
package cmd

import (
	"fmt"
	"os"

	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// wiType maps friendly names to GitLab work item type IDs.
var wiType = map[string]string{
	"issue":     "ISSUE",
	"task":      "TASK",
	"objective": "OBJECTIVE",
	"keyresult": "KEY_RESULT",
	"kr":        "KEY_RESULT",
}

func workItemCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wi",
		Short: "Manage GitLab work items (tasks, objectives, key results)",
	}
	cmd.AddCommand(
		wiListCmd(),
		wiCreateCmd(),
		wiCloseCmd(),
	)
	return cmd
}

// ─── list ─────────────────────────────────────────────────────────────────────

func wiListCmd() *cobra.Command {
	var project, state, witype string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List work items in a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}

			opts := provider.ListWorkItemsOptions{
				State:   state,
				PerPage: 50,
			}
			if witype != "" {
				if mapped, ok := wiType[witype]; ok {
					opts.Type = mapped
				} else {
					opts.Type = witype
				}
			}

			items, err := glClient.Provider.ListWorkItems(cmd.Context(), project, opts)
			if err != nil {
				glClient.RecErr(journal.OpList, journal.EntityWorkItem, project, "", err.Error())
				return err
			}

			glClient.Rec(journal.OpList, journal.EntityWorkItem, project, "", 0, 0,
				fmt.Sprintf("list %d work items (state=%s)", len(items), state), "")

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"IID", "Type", "Title", "State", "Web URL"})
			table.SetBorder(false)
			table.SetAutoWrapText(false)
			for _, wi := range items {
				table.Append([]string{
					fmt.Sprintf("%d", wi.IID),
					wi.Type,
					truncate(wi.Title, 50),
					wi.State,
					wi.WebURL,
				})
			}
			table.Render()
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().StringVar(&state, "state", "opened", "State: opened|closed|all")
	cmd.Flags().StringVar(&witype, "type", "", "Type: task|objective|kr|issue")
	return cmd
}

// ─── create ───────────────────────────────────────────────────────────────────

func wiCreateCmd() *cobra.Command {
	var project, title, wtype string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a work item (task, objective, key result)",
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

			typeName := "TASK"
			if wtype != "" {
				if mapped, ok := wiType[wtype]; ok {
					typeName = mapped
				} else {
					typeName = wtype
				}
			}

			wi, err := glClient.Provider.CreateWorkItem(cmd.Context(), project, provider.CreateWorkItemOptions{
				Title: title,
				Type:  typeName,
			})
			if err != nil {
				glClient.RecErr(journal.OpCreate, journal.EntityWorkItem, project, "", err.Error())
				return err
			}

			glClient.Rec(journal.OpCreate, journal.EntityWorkItem, project, "", 0, wi.IID,
				wi.Title, wi.WebURL)
			ok("Work item #%d created (%s): %s", wi.IID, typeName, wi.WebURL)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().StringVarP(&title, "title", "t", "", "Title (required)")
	cmd.Flags().StringVar(&wtype, "type", "task", "Type: task|objective|kr|issue")
	return cmd
}

// ─── close ────────────────────────────────────────────────────────────────────

func wiCloseCmd() *cobra.Command {
	var project string
	var id int

	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close a work item",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if id == 0 {
				return fmt.Errorf("--id required (work item ID)")
			}

			if err := glClient.Provider.CloseWorkItem(cmd.Context(), project, id); err != nil {
				glClient.RecErr(journal.OpClose, journal.EntityWorkItem, project, "", err.Error())
				return err
			}

			glClient.Rec(journal.OpClose, journal.EntityWorkItem, project, "", id, 0, "work item", "")
			ok("Work item #%d closed", id)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&id, "id", 0, "Work item ID")
	return cmd
}
