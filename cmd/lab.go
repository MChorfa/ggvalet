package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/MChorfa/ggvalet/internal/lab"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func labCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "lab",
		Short:             "Manage local GG Valet Delivery Laboratory (Milestone 0)",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
	}
	cmd.AddCommand(
		labUpCmd(),
		labSeedCmd(),
		labStatusCmd(),
		labDownCmd(),
	)
	return cmd
}

func labUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Spin up the simulated delivery world",
		RunE: func(cmd *cobra.Command, args []string) error {
			top := lab.DefaultTopology()
			fmt.Println("Starting GG Valet Delivery Laboratory (simulated delivery world)...")
			fmt.Printf("✓ %s (Shared multi-tenant instance)\n", top.Shared.Host)
			fmt.Printf("✓ %s (Dedicated high-assurance instance)\n", top.Dedicated.Host)
			fmt.Printf("✓ %s (Air-gap destination registry)\n", top.Airgap.Host)
			fmt.Printf("✓ %s (Trustwall gate)\n", top.Trustwall.Host)
			fmt.Printf("✓ %s (Transfer diode station)\n", top.Transfer.Host)
			fmt.Println("\nLaboratory active. Run 'glv lab seed' to populate test scenarios.")
			return nil
		},
	}
}

func labSeedCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Seed the 30 known delivery deviations into the laboratory",
		RunE: func(cmd *cobra.Command, args []string) error {
			deviations := lab.GenerateDeviations()
			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(deviations)
			}

			fmt.Printf("Seeding %d known deviations across security, supply chain, reliability, performance, cost, config, trust...\n\n", len(deviations))
			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"ID", "Category", "Project", "Title", "Reversible"})
			for _, d := range deviations {
				rev := "No"
				if d.Reversible {
					rev = "Yes"
				}
				table.Append([]string{d.ID, string(d.Category), d.Project, d.Title, rev})
			}
			table.Render()
			fmt.Println("\nDeviations seeded successfully.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output deviations matrix as JSON")
	return cmd
}

func labStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Inspect laboratory topology and domain health",
		RunE: func(cmd *cobra.Command, args []string) error {
			top := lab.DefaultTopology()
			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Domain", "Host", "Role", "Airgap"})
			table.Append([]string{"Shared", top.Shared.Host, "Multi-tenant (Team A/B/C)", "No"})
			table.Append([]string{"Dedicated", top.Dedicated.Host, "Controlled project", "No"})
			table.Append([]string{"Trustwall", top.Trustwall.Host, "Admission / Quarantine", "No"})
			table.Append([]string{"Transfer", top.Transfer.Host, "Unidirectional Diode", "No"})
			table.Append([]string{"Airgap", top.Airgap.Host, "Isolated Destination", "Yes"})
			table.Render()
			return nil
		},
	}
}

func labDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Tear down laboratory environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("GG Valet Delivery Laboratory torn down cleanly.")
			return nil
		},
	}
}
