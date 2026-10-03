package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/MChorfa/ggvalet/internal/agent"
	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/controlplane"
	"github.com/MChorfa/ggvalet/internal/policy"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/MChorfa/ggvalet/internal/syncindex"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func agentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "agent",
		Short:             "Next-gen governed autonomous agent operating over capability leases and vector state",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
	}

	cmd.AddCommand(
		agentRunCmd(),
		agentPlanCmd(),
		agentStatusCmd(),
	)

	return cmd
}

func agentRunCmd() *cobra.Command {
	var (
		goal       string
		project    string
		roleName   string
		actor      string
		ttlStr     string
		dryRun     bool
		yes        bool
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute an autonomous cognitive control loop under an attenuated capability lease",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" && cfg != nil {
				project = cfg.DefaultProject
			}
			if project == "" {
				project = "gitlab-shared/team-b"
			}
			if goal == "" {
				goal = "Assure Golden Pipeline v7 Conformance"
			}
			if actor == "" {
				actor = "agent-cortaix-v3"
			}
			if roleName == "" {
				roleName = os.Getenv("GLVALET_ROLE")
			}
			if roleName == "" {
				roleName = string(authority.RoleObserver)
			}

			budget := 15 * time.Minute
			if ttlStr != "" {
				if d, err := time.ParseDuration(ttlStr); err == nil {
					budget = d
				}
			}

			role := authority.RoleName(roleName)
			intent := agent.NewIntent(actor, goal, project, role, budget)

			var store *state.Store
			var prov provider.Provider
			if glClient != nil {
				store = glClient.State
				prov = glClient.Provider
			} else {
				store = getOrInitStore()
			}

			cpEngine := controlplane.NewEngine(prov, store, policy.DefaultCanonicalModel())
			fedEngine := syncindex.NewEngine(prov, prov, store)

			ag := agent.NewAgent(actor, cpEngine, fedEngine, store)
			if prov == nil {
				ag.ObservedOverride = map[string]any{
					"project":           project,
					"protected":         false,
					"unmasked_variable": true,
					"mutable_image_tag": true,
				}
			}

			effectiveDryRun := dryRun || !yes
			report, err := ag.Run(cmd.Context(), intent, effectiveDryRun)
			if err != nil {
				return fmt.Errorf("agent execution failed: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}

			// Render human-readable trajectory
			fmt.Printf("=== GG VALET AUTONOMOUS AGENT HARNESS ===\n")
			fmt.Printf("Agent ID:      %s\n", actor)
			fmt.Printf("Intent ID:     %s (Digest: %s)\n", intent.ID, intent.Digest()[:16]+"...")
			fmt.Printf("Goal:          %s\n", intent.Goal)
			fmt.Printf("Target:        %s\n", intent.Resource)
			fmt.Printf("Role Lease:    %s (Lease: %s, TTL: %v)\n", role, report.LeaseID, intent.BudgetDuration)
			if effectiveDryRun {
				fmt.Println("Mode:          DRY-RUN (Use --yes to authorize live mutations)")
			} else {
				fmt.Println("Mode:          LIVE MUTATION")
			}
			fmt.Println()

			fmt.Println("─── COGNITIVE TRAJECTORY STEPS ───")
			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"#", "Kind", "Disposition", "Description", "Evidence Digest"})
			for _, step := range report.Steps {
				dig := step.EvidenceDigest
				if len(dig) > 16 {
					dig = dig[:16] + "..."
				}
				table.Append([]string{
					fmt.Sprintf("%d", step.StepIndex),
					string(step.Kind),
					step.Disposition,
					step.Description,
					dig,
				})
			}
			table.Render()
			fmt.Println()

			if len(report.Refusals) > 0 {
				fmt.Printf("Authority Refusals (%d):\n", len(report.Refusals))
				for _, ref := range report.Refusals {
					fmt.Printf("  ✗ [%s] Capability %s denied: %s\n", ref.ReceiptID, ref.RequiredCapability, ref.Reason)
				}
				fmt.Println("  ↳ Agent pivoted to advisory remediation (obligation: create_advisory_mr).")
				fmt.Println()
			}

			fmt.Println("─── FINAL ASSURANCE VERDICT ───")
			fmt.Printf("Initial Vector: %s\n", report.InitialVector.String())
			fmt.Printf("Final Vector:   %s\n", report.FinalVector.String())
			fmt.Printf("Final Status:   [%s]\n", report.Status)

			switch report.Status {
			case "CONVERGED":
				fmt.Println(colorOK("✓ State successfully converged to Canonical Delivery Model."))
			case "SAFE_HOLD":
				fmt.Println(colorDim("! State placed in SAFE_HOLD. Insufficient authority for direct mutation; forensic evidence preserved."))
			case "PLAN_READY":
				fmt.Println(colorInfo("→ Minimal transition plan verified. Re-run with --yes to execute under authorized role."))
			default:
				fmt.Printf("Status: %s\n", report.Status)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&goal, "goal", "g", "", "Intent goal for the agent")
	cmd.Flags().StringVarP(&project, "project", "p", "", "Target resource / project")
	cmd.Flags().StringVar(&roleName, "role", "", "Authority role (valet-observer, valet-advisor, valet-reconciler, valet-admin-test)")
	cmd.Flags().StringVar(&actor, "actor", "", "Agent subject identity (default: agent-cortaix-v3)")
	cmd.Flags().StringVar(&ttlStr, "ttl", "15m", "Capability lease budget duration")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Simulate execution without mutations")
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm live execution of authorized transitions")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output full TrajectoryReport as JSON")

	return cmd
}

func agentPlanCmd() *cobra.Command {
	var (
		goal       string
		project    string
		roleName   string
		actor      string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Formulate a minimal transition plan for an intent without side effects",
		RunE: func(cmd *cobra.Command, args []string) error {
			runCmd := agentRunCmd()
			flags := []string{"--dry-run"}
			if goal != "" {
				flags = append(flags, "-g", goal)
			}
			if project != "" {
				flags = append(flags, "-p", project)
			}
			if roleName != "" {
				flags = append(flags, "--role", roleName)
			}
			if actor != "" {
				flags = append(flags, "--actor", actor)
			}
			if jsonOutput {
				flags = append(flags, "--json")
			}
			runCmd.SetArgs(flags)
			return runCmd.Execute()
		},
	}

	cmd.Flags().StringVarP(&goal, "goal", "g", "", "Intent goal for the agent")
	cmd.Flags().StringVarP(&project, "project", "p", "", "Target resource / project")
	cmd.Flags().StringVar(&roleName, "role", "", "Authority role (valet-observer, valet-advisor, valet-reconciler)")
	cmd.Flags().StringVar(&actor, "actor", "", "Agent subject identity")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output full TrajectoryReport as JSON")

	return cmd
}

func agentStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Inspect latest vector states and evidence ledger for autonomous agent runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			st := getOrInitStore()
			if st == nil {
				return fmt.Errorf("state store unavailable")
			}

			vectors, err := st.ListVectorStates(cmd.Context(), 10)
			if err != nil {
				return fmt.Errorf("list vector states: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(vectors)
			}

			fmt.Println("=== AGENT VECTOR STATE LEDGER (RECENT 10) ===")
			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Entity", "Presence", "Valence", "Anti", "Coherence", "Mode", "Epoch", "Updated"})
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
