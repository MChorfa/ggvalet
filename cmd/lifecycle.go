package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/lifecycle"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func lifecycleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "lifecycle",
		Short:             "Persona lifecycle management (onboard, offboard, status) across all 5 personas",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
	}

	cmd.AddCommand(
		lifecycleOnboardCmd(),
		lifecycleOffboardCmd(),
		lifecycleStatusCmd(),
	)

	return cmd
}

func lifecycleOnboardCmd() *cobra.Command {
	var (
		identifier string
		roleStr    string
		scope      string
		ttlStr     string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "onboard <persona>",
		Short: "Onboard a persona (user, project, agent, service, auditor) into the governed fabric",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			personaRaw := strings.ToLower(args[0])
			persona := lifecycle.PersonaKind(personaRaw)

			if identifier == "" {
				return fmt.Errorf("--id flag is required (e.g. username, project path, agent ID)")
			}

			ttl := 8 * time.Hour
			if ttlStr != "" {
				if d, err := time.ParseDuration(ttlStr); err == nil {
					ttl = d
				}
			}

			var role authority.RoleName
			if roleStr != "" {
				role = authority.RoleName(roleStr)
			}

			var store *state.Store
			if glClient != nil {
				store = glClient.State
			} else {
				store = getOrInitStore()
			}

			engine := lifecycle.NewEngine(store)
			res, err := engine.Onboard(cmd.Context(), lifecycle.OnboardingRequest{
				Persona:    persona,
				Identifier: identifier,
				Role:       role,
				Scope:      scope,
				TTL:        ttl,
			})
			if err != nil {
				return fmt.Errorf("onboarding failed: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(res)
			}

			fmt.Println("=== GG VALET PERSONA LIFECYCLE: ONBOARD ===")
			fmt.Printf("Persona:        %s\n", strings.ToUpper(string(res.Persona)))
			fmt.Printf("Identifier:     %s\n", res.Identifier)
			fmt.Printf("Status:         [%s]\n", res.Status)
			fmt.Printf("Baseline S_0:   %s\n", res.InitialVector.String())
			fmt.Printf("Evidence Digest: %s\n", res.EvidenceDigest[:16]+"...")

			if res.Lease != nil {
				fmt.Printf("Standing Lease: %s (Role: %s, TTL: %v)\n", res.Lease.ID, res.Lease.Role, res.Lease.ExpiresAt.Sub(res.Lease.IssuedAt))
			}

			if len(res.Artifacts) > 0 {
				fmt.Println("\nGenerated Governance Artifacts:")
				for k, v := range res.Artifacts {
					if strings.Contains(v, "\n") {
						fmt.Printf("  • %s: (multiline template)\n", k)
					} else {
						fmt.Printf("  • %s: %s\n", k, v)
					}
				}
			}

			fmt.Println("\n" + colorOK("✓ Persona successfully bootstrapped with baseline vector S_0 in SQLite."))
			return nil
		},
	}

	cmd.Flags().StringVarP(&identifier, "id", "i", "", "Identifier of the entity (username, project path, agent ID)")
	cmd.Flags().StringVarP(&roleStr, "role", "r", "", "Authority role (valet-observer, valet-advisor, valet-reconciler, valet-trustwall)")
	cmd.Flags().StringVarP(&scope, "scope", "s", "*", "Capability scope glob pattern")
	cmd.Flags().StringVar(&ttlStr, "ttl", "", "Ephemeral lease TTL (e.g. 15m, 8h)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("id")

	return cmd
}

func lifecycleOffboardCmd() *cobra.Command {
	var (
		identifier string
		reason     string
		approver   string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "offboard <persona>",
		Short: "Safely offboard or archive a persona, revoke leases, and record terminal SAFE_HOLD vector",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			personaRaw := strings.ToLower(args[0])
			persona := lifecycle.PersonaKind(personaRaw)

			if identifier == "" {
				return fmt.Errorf("--id flag is required")
			}
			if reason == "" {
				reason = "Decommissioned via governance lifecycle"
			}
			if approver == "" {
				approver = os.Getenv("USER")
				if approver == "" {
					approver = "governance-lead"
				}
			}

			var store *state.Store
			if glClient != nil {
				store = glClient.State
			} else {
				store = getOrInitStore()
			}

			engine := lifecycle.NewEngine(store)
			res, err := engine.Offboard(cmd.Context(), lifecycle.OffboardingRequest{
				Persona:    persona,
				Identifier: identifier,
				Reason:     reason,
				Approver:   approver,
			})
			if err != nil {
				return fmt.Errorf("offboarding failed: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(res)
			}

			fmt.Println("=== GG VALET PERSONA LIFECYCLE: OFFBOARD ===")
			fmt.Printf("Persona:        %s\n", strings.ToUpper(string(res.Persona)))
			fmt.Printf("Identifier:     %s\n", res.Identifier)
			fmt.Printf("Terminal Status:[%s]\n", res.Status)
			fmt.Printf("Terminal Vector:%s\n", res.TerminalVector.String())
			fmt.Printf("Evidence Digest:%s\n", res.EvidenceDigest[:16]+"...")

			if len(res.RevokedLeases) > 0 {
				fmt.Printf("Revoked Leases: %s\n", strings.Join(res.RevokedLeases, ", "))
			}

			if len(res.Obligations) > 0 {
				fmt.Println("\nTeardown Obligations Recorded:")
				for _, obl := range res.Obligations {
					fmt.Printf("  ✓ %s\n", obl)
				}
			}

			fmt.Println("\n" + colorDim("! Persona offboarded; terminal state sealed in SAFE_HOLD in SQLite."))
			return nil
		},
	}

	cmd.Flags().StringVarP(&identifier, "id", "i", "", "Identifier of the entity")
	cmd.Flags().StringVarP(&reason, "reason", "R", "", "Reason for offboarding")
	cmd.Flags().StringVarP(&approver, "approver", "a", "", "Approver identity")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("id")

	return cmd
}

func lifecycleStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Inspect lifecycle vector states and persona assurance standing",
		RunE: func(cmd *cobra.Command, args []string) error {
			var store *state.Store
			if glClient != nil {
				store = glClient.State
			} else {
				store = getOrInitStore()
			}

			vectors, err := store.ListVectorStates(cmd.Context(), 20)
			if err != nil {
				return fmt.Errorf("list vector states: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(vectors)
			}

			fmt.Println("=== GG VALET PERSONA LIFECYCLE LEDGER ===")
			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Entity ID", "Presence", "Valence", "Anti", "Coherence", "Mode", "Epoch", "Updated"})
			for _, v := range vectors {
				table.Append([]string{
					v.EntityID,
					string(v.Presence),
					string(v.Valence),
					string(v.Anti),
					string(v.Coherence),
					string(v.Mode),
					fmt.Sprintf("%d", v.Epoch),
					v.Timestamp.Format(time.RFC3339),
				})
			}
			table.Render()
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}
