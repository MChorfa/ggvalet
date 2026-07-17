package cmd

import (
	"fmt"
	"os"

	"github.com/ckodex/gitlabvalet/internal/journal"
	"github.com/ckodex/gitlabvalet/internal/provider"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func labelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "label",
		Short:   "Manage project labels",
		Aliases: []string{"lbl"},
	}
	cmd.AddCommand(labelListCmd(), labelCreateCmd(), labelSyncCmd())
	return cmd
}

func labelListCmd() *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List project labels",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}

			lbls, err := glClient.Provider.ListLabels(cmd.Context(), project)
			if err != nil {
				glClient.RecErr(journal.OpList, journal.EntityLabel, project, "", err.Error())
				return fmt.Errorf("list labels: %w", err)
			}

			glClient.Rec(journal.OpList, journal.EntityLabel, project, "", 0, 0,
				fmt.Sprintf("list %d labels", len(lbls)), "")

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"ID", "Name", "Color", "Open Issues"})
			table.SetBorder(false)
			for _, l := range lbls {
				table.Append([]string{
					fmt.Sprintf("%d", l.ID),
					l.Name,
					l.Color,
					fmt.Sprintf("%d", l.OpenIssuesCount),
				})
			}
			table.Render()
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	return cmd
}

func labelCreateCmd() *cobra.Command {
	var project, name, color, description string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a label",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if name == "" {
				return fmt.Errorf("--name required")
			}
			if color == "" {
				return fmt.Errorf("--color required (hex, e.g. #FF0000)")
			}

			lbl, err := glClient.Provider.CreateLabel(cmd.Context(), project, provider.CreateLabelOptions{
				Name:        name,
				Color:       color,
				Description: description,
			})
			if err != nil {
				glClient.RecErr(journal.OpCreate, journal.EntityLabel, project, "", err.Error())
				return fmt.Errorf("create label: %w", err)
			}

			glClient.Rec(journal.OpCreate, journal.EntityLabel, project, "", lbl.ID, 0, lbl.Name, "")
			ok("Label %q created (ID: %d)", lbl.Name, lbl.ID)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().StringVarP(&name, "name", "n", "", "Label name (required)")
	cmd.Flags().StringVarP(&color, "color", "c", "", "Label color hex e.g. #428BCA (required)")
	cmd.Flags().StringVarP(&description, "description", "d", "", "Optional description")
	return cmd
}

// labelSyncCmd copies all labels from one project to another.
// Colors are preserved. Existing labels (matched by name) are skipped.
func labelSyncCmd() *cobra.Command {
	var srcHost, srcProject, dstHost, dstProject string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Copy labels from source project to destination project",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			if srcHost == "" {
				srcHost = cfg.Host
			}
			if dstHost == "" {
				dstHost = cfg.Host
			}

			srcC, dstC, err := syncClients(srcHost, dstHost)
			if err != nil {
				return err
			}

			printSyncHeader("labels", srcHost, srcProject, dstHost, dstProject, dryRun)

			ctx := cmd.Context()
			srcLabels, err := srcC.Provider.ListLabels(ctx, srcProject)
			if err != nil {
				return fmt.Errorf("list src labels: %w", err)
			}

			// Build dst name set
			dstLabels, _ := dstC.Provider.ListLabels(ctx, dstProject)
			existing := make(map[string]struct{}, len(dstLabels))
			for _, l := range dstLabels {
				existing[l.Name] = struct{}{}
			}

			created, skipped := 0, 0
			for _, lbl := range srcLabels {
				if _, ok := existing[lbl.Name]; ok {
					skipped++
					continue
				}
				if dryRun {
					fmt.Printf("  %s %s (%s)\n", colorDim("[dry]"), lbl.Name, lbl.Color)
					created++
					continue
				}
				_, err := dstC.Provider.CreateLabel(ctx, dstProject, provider.CreateLabelOptions{
					Name:        lbl.Name,
					Color:       lbl.Color,
					Description: lbl.Description,
				})
				if err != nil {
					fmt.Fprintf(os.Stderr, "  %s %s: %v\n", colorErr("✗"), lbl.Name, err)
					continue
				}
				chip := renderChip(lbl.Name, lbl.Color)
				fmt.Printf("  %s %s\n", colorOK("✓"), chip)
				created++
			}
			printSyncSummary(created, skipped, 0, dryRun)
			return nil
		},
	}

	cmd.Flags().StringVar(&srcHost, "src-host", "", "Source hostname")
	cmd.Flags().StringVar(&srcProject, "src-project", "", "Source project path or ID")
	cmd.Flags().StringVar(&dstHost, "dst-host", "", "Destination hostname")
	cmd.Flags().StringVar(&dstProject, "dst-project", "", "Destination project path or ID")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without creating")
	return cmd
}
