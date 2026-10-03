package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/MChorfa/ggvalet/internal/controlplane"
	"github.com/MChorfa/ggvalet/internal/policy"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func auditCmd() *cobra.Command {
	var (
		project    string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:               "audit",
		Short:             "Audit project compliance against canonical delivery standards",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" && cfg != nil {
				project = cfg.DefaultProject
			}
			if project == "" {
				project = "gitlab-shared/team-b" // default demonstration target
			}

			var store *state.Store
			var prov provider.Provider
			if glClient != nil {
				store = glClient.State
				prov = glClient.Provider
			} else {
				store = getOrInitStore()
			}

			engine := controlplane.NewEngine(prov, store, policy.DefaultCanonicalModel())

			// If provider is not live, inspect default adverse scenarios for lab demonstrations
			var observedOverride map[string]any
			if prov == nil {
				observedOverride = map[string]any{
					"project":           project,
					"protected":         false, // DEV-001
					"unmasked_variable": true,  // DEV-003
					"mutable_image_tag": true,  // DEV-006
				}
			}

			assessment, err := engine.Audit(cmd.Context(), project, observedOverride)
			if err != nil {
				return fmt.Errorf("audit failed: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(assessment)
			}

			fmt.Printf("=== GG VALET AUDIT: %s ===\n", project)
			fmt.Printf("Canonical Standard: %s\n", assessment.CanonicalModel.Version)
			fmt.Printf("Vector State: %s\n\n", assessment.CompositeVector.String())

			if len(assessment.Findings) == 0 {
				fmt.Println("✓ Project is fully conformant with all canonical standards.")
				return nil
			}

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"ID", "Kind", "Category", "Title", "Required Cap", "Reversible"})
			for _, f := range assessment.Findings {
				rev := "No"
				if f.Reversible {
					rev = "Yes"
				}
				table.Append([]string{f.ID, string(f.Kind), f.Category, f.Title, string(f.RequiredCapability), rev})
			}
			table.Render()

			fmt.Println("\nRecommended Actions:")
			for _, f := range assessment.Findings {
				fmt.Printf("  • [%s] %s\n    Action: %s\n", f.ID, f.Title, f.RecommendedAction)
				if f.ProposedPatch != "" {
					fmt.Printf("    Proposed Patch: %s\n", f.ProposedPatch)
				}
			}

			if len(assessment.Transitions) > 0 {
				fmt.Println("\nProposed Conformance Transitions:")
				for _, t := range assessment.Transitions {
					fmt.Printf("  → Transition %s: %s (Disposition: %s, Digest: %s)\n",
						t.ID, t.Stimulus.Type, t.Disposition, t.EvidenceDigest[:12])
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&project, "project", "p", "", "Target project to audit")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output assessment as JSON")

	return cmd
}
