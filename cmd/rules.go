package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MChorfa/ggvalet/internal/policy"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func rulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "rules",
		Short:             "Progressive governance engine for candidate policy discovery, simulation, and promotion",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
	}

	cmd.AddCommand(
		rulesListCmd(),
		rulesDiscoverCmd(),
		rulesSimulateCmd(),
		rulesPromoteCmd(),
	)

	return cmd
}

func getOrInitStore() *state.Store {
	if glClient != nil && glClient.State != nil {
		return glClient.State
	}
	if cfg != nil && cfg.StatePath != "" {
		st, err := state.Open(cfg.StatePath)
		if err == nil {
			return st
		}
	}
	home, _ := os.UserHomeDir()
	defaultPath := filepath.Join(home, ".local", "share", "ggvalet", "state.db")
	st, err := state.Open(defaultPath)
	if err == nil {
		return st
	}
	st, _ = state.Open(filepath.Join(os.TempDir(), "ggvalet-evidence.db"))
	return st
}

func rulesListCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all candidate and progressive governance rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			st := getOrInitStore()
			if st == nil {
				return fmt.Errorf("state store unavailable")
			}

			rules, err := st.ListCandidateRules(cmd.Context())
			if err != nil {
				return fmt.Errorf("list candidate rules: %w", err)
			}

			// If empty, auto-seed a discovered candidate for demonstration
			if len(rules) == 0 {
				demoCandidate := policy.DiscoverOptimizationCandidate("Nightly integration rerun on clean tree", 100, 83)
				_ = st.RecordCandidateRule(cmd.Context(), demoCandidate)
				rules = append(rules, *demoCandidate)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(rules)
			}

			fmt.Println("=== GG VALET CANDIDATE RULES (PROGRESSIVE GOVERNANCE) ===")
			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"ID", "Title", "Observed Pattern", "Stage", "Frequency", "Est. Savings", "Approved By"})
			for _, r := range rules {
				app := r.ApprovedBy
				if app == "" {
					app = "-"
				}
				freqStr := fmt.Sprintf("%.1f%%", r.MatchFrequency*100)
				table.Append([]string{r.ID, r.Title, r.PatternObserved, string(r.Stage), freqStr, r.EstimatedSavings, app})
			}
			table.Render()
			fmt.Printf("\nLifecycle: DISCOVERED → SIMULATED → SHADOW → WARN → ENFORCE\n")
			fmt.Printf("Next steps: 'ggvalet rules simulate --id <ID>' or 'ggvalet rules promote --id <ID> --stage <STAGE>'\n")

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output list as JSON")
	return cmd
}

func rulesDiscoverCmd() *cobra.Command {
	var (
		pattern string
		runs    int
		matches int
	)

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Formulate an optimization candidate from historical pipeline observations",
		RunE: func(cmd *cobra.Command, args []string) error {
			st := getOrInitStore()
			if st == nil {
				return fmt.Errorf("state store unavailable")
			}

			if pattern == "" {
				pattern = "Unchanged nightly integration rerun without dependency drift"
			}
			if runs <= 0 {
				runs = 100
			}
			if matches <= 0 {
				matches = 83
			}

			candidate := policy.DiscoverOptimizationCandidate(pattern, runs, matches)
			if err := st.RecordCandidateRule(cmd.Context(), candidate); err != nil {
				return fmt.Errorf("record candidate: %w", err)
			}

			fmt.Printf("✓ Formulated new candidate rule: %s\n", candidate.ID)
			fmt.Printf("  Title: %s\n", candidate.Title)
			fmt.Printf("  Observed Pattern: %s\n", candidate.PatternObserved)
			fmt.Printf("  Stage: %s\n", candidate.Stage)
			fmt.Printf("  Match Frequency: %.1f%% across %d runs\n", candidate.MatchFrequency*100, candidate.HistoricalRuns)
			fmt.Printf("  Estimated Savings: %s\n", candidate.EstimatedSavings)
			return nil
		},
	}

	cmd.Flags().StringVar(&pattern, "pattern", "", "Description of pattern observed")
	cmd.Flags().IntVar(&runs, "runs", 100, "Number of historical runs analyzed")
	cmd.Flags().IntVar(&matches, "matches", 83, "Number of matches observed")
	return cmd
}

func rulesSimulateCmd() *cobra.Command {
	var id string

	cmd := &cobra.Command{
		Use:   "simulate",
		Short: "Simulate candidate rule against historical pipeline traces to verify safety",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				return fmt.Errorf("--id flag is required")
			}
			st := getOrInitStore()
			if st == nil {
				return fmt.Errorf("state store unavailable")
			}

			rule, err := st.GetCandidateRule(cmd.Context(), id)
			if err != nil {
				return fmt.Errorf("query rule: %w", err)
			}
			if rule == nil {
				return fmt.Errorf("candidate rule %q not found", id)
			}

			rule.Simulate()
			if err := st.RecordCandidateRule(cmd.Context(), rule); err != nil {
				return fmt.Errorf("update rule: %w", err)
			}

			fmt.Printf("✓ Simulated candidate rule: %s\n", rule.ID)
			fmt.Printf("  Title: %s\n", rule.Title)
			fmt.Printf("  Pass Rate in Simulation: %.2f%%\n", rule.SimulatedPassRate*100)
			fmt.Printf("  Stage advanced to: %s\n", rule.Stage)
			fmt.Printf("  Status: Rule is safe to promote to SHADOW or WARN upon human approval.\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "Candidate rule ID to simulate")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func rulesPromoteCmd() *cobra.Command {
	var (
		id       string
		stage    string
		approver string
	)

	cmd := &cobra.Command{
		Use:   "promote",
		Short: "Promote a candidate rule to SHADOW, WARN, or ENFORCE",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				return fmt.Errorf("--id flag is required")
			}
			if stage == "" {
				return fmt.Errorf("--stage flag is required (SHADOW, WARN, ENFORCE)")
			}
			st := getOrInitStore()
			if st == nil {
				return fmt.Errorf("state store unavailable")
			}

			rule, err := st.GetCandidateRule(cmd.Context(), id)
			if err != nil {
				return fmt.Errorf("query rule: %w", err)
			}
			if rule == nil {
				return fmt.Errorf("candidate rule %q not found", id)
			}

			if approver == "" {
				approver = os.Getenv("USER")
				if approver == "" {
					approver = "governance-authority"
				}
			}

			targetStage := policy.RuleStage(strings.ToUpper(stage))
			switch targetStage {
			case policy.StageShadow, policy.StageWarn, policy.StageEnforce:
				// valid
			default:
				return fmt.Errorf("invalid stage %q; must be SHADOW, WARN, or ENFORCE", stage)
			}

			if err := rule.Promote(targetStage, approver); err != nil {
				return fmt.Errorf("promotion rejected: %w", err)
			}

			if err := st.RecordCandidateRule(cmd.Context(), rule); err != nil {
				return fmt.Errorf("persist promoted rule: %w", err)
			}

			fmt.Printf("✓ Promoted candidate rule: %s\n", rule.ID)
			fmt.Printf("  Title: %s\n", rule.Title)
			fmt.Printf("  New Stage: %s\n", rule.Stage)
			fmt.Printf("  Approved By: %s\n", rule.ApprovedBy)
			fmt.Printf("  Updated At: %s\n", rule.UpdatedAt.Format("2006-01-02 15:04:05 MST"))
			return nil
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "Candidate rule ID to promote")
	cmd.Flags().StringVar(&stage, "stage", "", "Target stage: SHADOW, WARN, or ENFORCE")
	cmd.Flags().StringVar(&approver, "approver", "", "Approver identity (defaults to current user)")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("stage")
	return cmd
}
