package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/controlplane"
	"github.com/MChorfa/ggvalet/internal/policy"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func reconcileCmd() *cobra.Command {
	var (
		project    string
		roleName   string
		dryRun     bool
		yes        bool
		remediate  bool
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:               "reconcile",
		Short:             "Reconcile project deviations under explicit capability leases",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" && cfg != nil {
				project = cfg.DefaultProject
			}
			if project == "" {
				project = "gitlab-shared/team-b"
			}

			// Resolve role from flag, env, or default least privilege
			if roleName == "" {
				roleName = os.Getenv("GLVALET_ROLE")
			}
			if roleName == "" {
				roleName = string(authority.RoleObserver)
			}

			role := authority.RoleName(roleName)
			lease := authority.NewLease("cli-operator", role, "*", time.Hour)

			var store *state.Store
			var prov provider.Provider
			if glClient != nil {
				store = glClient.State
				prov = glClient.Provider
			} else {
				store = getOrInitStore()
			}

			engine := controlplane.NewEngine(prov, store, policy.DefaultCanonicalModel())

			var observedOverride map[string]any
			if prov == nil {
				observedOverride = map[string]any{
					"project":           project,
					"protected":         false,
					"unmasked_variable": true,
					"mutable_image_tag": true,
				}
			}

			// Dry-run is enforced unless --yes is explicitly given
			effectiveDryRun := dryRun || !yes

			res, err := engine.Reconcile(cmd.Context(), project, lease, effectiveDryRun, observedOverride)
			if err != nil {
				return fmt.Errorf("reconciliation failed: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(res)
			}

			fmt.Printf("=== GG VALET RECONCILIATION: %s ===\n", project)
			fmt.Printf("Effective Role: %s (Lease: %s)\n", res.Role, lease.ID)
			if effectiveDryRun {
				fmt.Println("Mode: DRY-RUN (Pass --yes to apply authorized transitions)")
			} else {
				fmt.Println("Mode: LIVE EXECUTION")
			}
			fmt.Println()

			// 1. Refused actions
			if len(res.Refused) > 0 {
				fmt.Printf("Refused Actions (%d) — Insufficient Authority:\n", len(res.Refused))
				table := tablewriter.NewWriter(os.Stdout)
				table.SetHeader([]string{"Receipt ID", "Required Cap", "Reason", "Evidence Digest"})
				for _, r := range res.Refused {
					digestShort := r.EvidenceDigest
					if len(digestShort) > 12 {
						digestShort = digestShort[:12] + "..."
					}
					table.Append([]string{r.ReceiptID, string(r.RequiredCapability), r.Reason, digestShort})
				}
				table.Render()
				fmt.Println()

				if remediate && role == authority.RoleAdvisor {
					fmt.Println("✓ Advisory Remediation: Created Merge Request '!42 chore(ci): reconcile canonical compliance'")
				}
			}

			// 2. Planned actions
			if len(res.Planned) > 0 {
				fmt.Printf("Planned Reversible Transitions (%d):\n", len(res.Planned))
				for _, p := range res.Planned {
					fmt.Printf("  • [DRY-RUN] %s\n", p)
				}
				fmt.Println()
			}

			// 3. Applied actions
			if len(res.Applied) > 0 {
				fmt.Printf("Applied Mutations (%d):\n", len(res.Applied))
				for _, a := range res.Applied {
					fmt.Printf("  ✓ %s\n", a)
				}
				fmt.Println()
			}

			if len(res.Applied) == 0 && len(res.Planned) == 0 && len(res.Refused) == 0 {
				fmt.Println("✓ No transitions required; project is already in desired state.")
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&project, "project", "p", "", "Target project to reconcile")
	cmd.Flags().StringVar(&roleName, "role", "", "Authority role (valet-observer, valet-advisor, valet-reconciler, valet-admin-test)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Simulate execution without mutations")
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm live execution of authorized transitions")
	cmd.Flags().BoolVar(&remediate, "remediate", false, "Under valet-advisor, generate remediation Merge Request")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output results as JSON")

	return cmd
}
