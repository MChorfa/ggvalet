package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/MChorfa/ggvalet/internal/syncindex"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func syncIndexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Manage local sync index of federated entity markers",
	}
	cmd.AddCommand(syncIndexScanCmd(), syncIndexListCmd())
	return cmd
}

func syncIndexScanCmd() *cobra.Command {
	var srcHost, srcProject, dstHost, dstProject string

	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan destination project and record cryptographic markers in local sync index",
		RunE: func(cmd *cobra.Command, args []string) error {
			if srcHost == "" && cfg != nil {
				srcHost = cfg.Host
			}
			if dstHost == "" && cfg != nil {
				dstHost = cfg.Host
			}
			if srcProject == "" && cfg != nil {
				srcProject = cfg.DefaultProject
			}
			if dstProject == "" && cfg != nil {
				dstProject = cfg.DefaultProject
			}

			st := getOrInitStore()
			var dstProv provider.Provider
			if glClient != nil {
				dstProv = glClient.Provider
			}

			engine := syncindex.NewEngine(nil, dstProv, st)
			if dstProv == nil {
				fmt.Println(colorDim("→ Offline mode: scanning local mock store"))
				return nil
			}

			indexed, err := engine.ScanAndIndex(cmd.Context(), srcHost, srcProject, dstHost, dstProject)
			if err != nil {
				return fmt.Errorf("scan and index: %w", err)
			}

			ok("Indexed %d markers for %s:%s", len(indexed), dstHost, dstProject)
			return nil
		},
	}

	cmd.Flags().StringVar(&srcHost, "src-host", "", "Source instance hostname")
	cmd.Flags().StringVar(&srcProject, "src-project", "", "Source project path")
	cmd.Flags().StringVar(&dstHost, "dst-host", "", "Destination instance hostname")
	cmd.Flags().StringVar(&dstProject, "dst-project", "", "Destination project path")
	return cmd
}

func syncIndexListCmd() *cobra.Command {
	var dstHost, dstProject string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all indexed sync mappings for a destination project",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dstHost == "" && cfg != nil {
				dstHost = cfg.Host
			}
			if dstProject == "" && cfg != nil {
				dstProject = cfg.DefaultProject
			}
			if dstHost == "" {
				dstHost = "gitlab-dedicated.local"
			}
			if dstProject == "" {
				dstProject = "cortaix/dedicated-repo"
			}

			st := getOrInitStore()
			if st == nil {
				return fmt.Errorf("state store unavailable")
			}

			list, err := st.ListSyncIndexEntries(cmd.Context(), dstHost, dstProject)
			if err != nil {
				return fmt.Errorf("list sync index: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(list)
			}

			fmt.Printf("=== GG VALET FEDERATED SYNC INDEX: %s:%s ===\n", dstHost, dstProject)
			if len(list) == 0 {
				fmt.Println("No indexed sync markers found. Run 'ggvalet sync index scan' or 'ggvalet sync reconcile'.")
				return nil
			}

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Source Origin", "Dst IID", "Status", "Content Digest", "Epoch", "Last Synced"})
			for _, item := range list {
				srcOrigin := fmt.Sprintf("%s:%s#%d", item.SrcHost, item.SrcProject, item.SrcIID)
				digestShort := item.ContentDigest
				if len(digestShort) > 16 {
					digestShort = digestShort[:16] + "..."
				}
				table.Append([]string{
					srcOrigin,
					fmt.Sprintf("#%d", item.DstIID),
					item.Status,
					digestShort,
					fmt.Sprintf("%d", item.SyncEpoch),
					item.LastSyncedAt.Format("2006-01-02 15:04"),
				})
			}
			table.Render()
			return nil
		},
	}

	cmd.Flags().StringVar(&dstHost, "dst-host", "", "Destination instance hostname")
	cmd.Flags().StringVar(&dstProject, "dst-project", "", "Destination project path")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}

func syncDriftCmd() *cobra.Command {
	var srcHost, srcProject, dstHost, dstProject string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "drift",
		Short: "Analyze content drift and vector state between source and destination entities",
		RunE: func(cmd *cobra.Command, args []string) error {
			if srcHost == "" && cfg != nil {
				srcHost = cfg.Host
			}
			if dstHost == "" && cfg != nil {
				dstHost = cfg.Host
			}
			if srcProject == "" && cfg != nil {
				srcProject = cfg.DefaultProject
			}
			if dstProject == "" && cfg != nil {
				dstProject = cfg.DefaultProject
			}
			if srcHost == "" {
				srcHost = "gitlab-shared.local"
			}
			if dstHost == "" {
				dstHost = "gitlab-dedicated.local"
			}
			if srcProject == "" {
				srcProject = "cortaix/platform"
			}
			if dstProject == "" {
				dstProject = "cortaix/dedicated-repo"
			}

			st := getOrInitStore()
			engine := syncindex.NewEngine(nil, nil, st)

			// Demo fixture issues when offline
			srcIssues := []provider.Issue{
				{ID: 1, IID: 101, Title: "Golden Pipeline v7 Standard", Body: "Adopt canonical security stages", State: "opened"},
				{ID: 2, IID: 102, Title: "Container image signing key", Body: "Updated with cosign keyless root", State: "opened"},
				{ID: 3, IID: 103, Title: "Hardened runner cluster token", Body: "Local modification on source", State: "opened"},
			}
			dstIssues := []provider.Issue{
				{
					ID: 11, IID: 201, Title: "Golden Pipeline v7 Standard",
					Body: syncindex.EmbedMarker("Adopt canonical security stages", syncindex.SyncMarker{
						Version:       syncindex.MarkerVersion,
						SrcHost:       srcHost,
						SrcProject:    srcProject,
						EntityType:    "issue",
						SrcIID:        101,
						ContentDigest: syncindex.ComputeContentDigest("Golden Pipeline v7 Standard", "Adopt canonical security stages", nil, "opened"),
						SyncEpoch:     1,
					}),
					State: "opened",
				},
				{
					ID: 13, IID: 203, Title: "Hardened runner cluster token",
					Body: syncindex.EmbedMarker("Conflicting independent edit on dedicated instance", syncindex.SyncMarker{
						Version:       syncindex.MarkerVersion,
						SrcHost:       srcHost,
						SrcProject:    srcProject,
						EntityType:    "issue",
						SrcIID:        103,
						ContentDigest: "sha256:baseline-digest",
						SyncEpoch:     1,
					}),
					State: "opened",
				},
			}

			analyses, err := engine.EvaluateDrift(cmd.Context(), srcHost, srcProject, dstHost, dstProject, srcIssues, dstIssues)
			if err != nil {
				return fmt.Errorf("drift analysis failed: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(analyses)
			}

			fmt.Printf("=== GG VALET FEDERATED DRIFT ANALYSIS ===\n")
			fmt.Printf("Source:      %s:%s\n", srcHost, srcProject)
			fmt.Printf("Destination: %s:%s\n\n", dstHost, dstProject)

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Source IID", "Status", "Vector State", "Recommended Action"})
			for _, a := range analyses {
				table.Append([]string{
					fmt.Sprintf("#%d", a.SrcIID),
					string(a.Status),
					a.Vector.String(),
					a.RecommendedAction,
				})
			}
			table.Render()

			var conflictCount int
			for _, a := range analyses {
				if a.Status == syncindex.StatusConflict {
					conflictCount++
				}
			}
			if conflictCount > 0 {
				fmt.Printf("\n%s %d conflict(s) detected and automatically moved to SAFE_HOLD / QUARANTINE.\n",
					colorErr("⚠"), conflictCount)
				fmt.Println("Inspect with: 'ggvalet sync quarantine list'")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&srcHost, "src-host", "", "Source instance hostname")
	cmd.Flags().StringVar(&srcProject, "src-project", "", "Source project path")
	cmd.Flags().StringVar(&dstHost, "dst-host", "", "Destination instance hostname")
	cmd.Flags().StringVar(&dstProject, "dst-project", "", "Destination project path")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}

func syncReconcileCmd() *cobra.Command {
	var srcHost, srcProject, dstHost, dstProject string
	var roleName string
	var dryRun, yes, jsonOutput bool

	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Reconcile federated drift under explicit capability leases",
		RunE: func(cmd *cobra.Command, args []string) error {
			if srcHost == "" && cfg != nil {
				srcHost = cfg.Host
			}
			if dstHost == "" && cfg != nil {
				dstHost = cfg.Host
			}
			if srcProject == "" && cfg != nil {
				srcProject = cfg.DefaultProject
			}
			if dstProject == "" && cfg != nil {
				dstProject = cfg.DefaultProject
			}
			if srcHost == "" {
				srcHost = "gitlab-shared.local"
			}
			if dstHost == "" {
				dstHost = "gitlab-dedicated.local"
			}
			if srcProject == "" {
				srcProject = "cortaix/platform"
			}
			if dstProject == "" {
				dstProject = "cortaix/dedicated-repo"
			}

			if roleName == "" {
				roleName = os.Getenv("GLVALET_ROLE")
			}
			if roleName == "" {
				roleName = string(authority.RoleObserver)
			}

			role := authority.RoleName(roleName)
			lease := authority.NewLease("cli-operator", role, "*", time.Hour)

			st := getOrInitStore()
			engine := syncindex.NewEngine(nil, nil, st)

			// Demo fixture
			srcIssues := []provider.Issue{
				{ID: 1, IID: 101, Title: "Golden Pipeline v7 Standard", Body: "Adopt canonical security stages", State: "opened"},
				{ID: 2, IID: 102, Title: "Container image signing key", Body: "Updated with cosign keyless root", State: "opened"},
			}

			effectiveDryRun := dryRun || !yes
			res, err := engine.Reconcile(cmd.Context(), srcHost, srcProject, dstHost, dstProject, lease, effectiveDryRun, srcIssues, nil)
			if err != nil {
				return fmt.Errorf("federated reconciliation failed: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(res)
			}

			fmt.Printf("=== GG VALET FEDERATED RECONCILIATION ===\n")
			fmt.Printf("Target: %s:%s  Role: %s (Lease: %s)\n", dstHost, dstProject, res.Role, lease.ID)
			if effectiveDryRun {
				fmt.Println("Mode: DRY-RUN (Pass --yes to apply authorized transitions)")
			} else {
				fmt.Println("Mode: LIVE EXECUTION")
			}
			fmt.Println()

			if len(res.Refused) > 0 {
				fmt.Printf("Refused Actions (%d) — Insufficient Authority:\n", len(res.Refused))
				table := tablewriter.NewWriter(os.Stdout)
				table.SetHeader([]string{"Receipt ID", "Required Cap", "Reason", "Resource"})
				for _, r := range res.Refused {
					table.Append([]string{r.ReceiptID, string(r.RequiredCapability), r.Reason, r.AttemptedResource})
				}
				table.Render()
				fmt.Println()
			}

			if len(res.Planned) > 0 {
				fmt.Printf("Planned Federated Transitions (%d):\n", len(res.Planned))
				for _, p := range res.Planned {
					fmt.Printf("  • [DRY-RUN] %s\n", p)
				}
				fmt.Println()
			}

			if len(res.Applied) > 0 {
				fmt.Printf("Applied Mutations (%d):\n", len(res.Applied))
				for _, a := range res.Applied {
					fmt.Printf("  ✓ %s\n", a)
				}
				fmt.Println()
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&srcHost, "src-host", "", "Source instance hostname")
	cmd.Flags().StringVar(&srcProject, "src-project", "", "Source project path")
	cmd.Flags().StringVar(&dstHost, "dst-host", "", "Destination instance hostname")
	cmd.Flags().StringVar(&dstProject, "dst-project", "", "Destination project path")
	cmd.Flags().StringVar(&roleName, "role", "", "Authority role (valet-observer, valet-advisor, valet-reconciler)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview reconciliation without making changes")
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm execution of authorized reconciliation")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}

func syncQuarantineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quarantine",
		Short: "Inspect and resolve quarantined conflict records",
	}
	cmd.AddCommand(syncQuarantineListCmd(), syncQuarantineInspectCmd())
	return cmd
}

func syncQuarantineListCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all active conflict quarantine records",
		RunE: func(cmd *cobra.Command, args []string) error {
			st := getOrInitStore()
			if st == nil {
				return fmt.Errorf("state store unavailable")
			}

			records, err := st.ListQuarantineRecords(cmd.Context())
			if err != nil {
				return fmt.Errorf("list quarantine: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(records)
			}

			fmt.Println("=== GG VALET CONFLICT QUARANTINE (SAFE_HOLD) ===")
			if len(records) == 0 {
				fmt.Println("✓ No quarantined entities. All federated instances are coherent.")
				return nil
			}

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Quarantine ID", "Entity Key", "Reason", "Status", "Quarantined At"})
			for _, q := range records {
				table.Append([]string{
					q.ID,
					q.EntityKey,
					q.QuarantineReason,
					q.ResolutionStatus,
					q.QuarantinedAt.Format("2006-01-02 15:04:05"),
				})
			}
			table.Render()
			fmt.Println("\nTo inspect details: 'ggvalet sync quarantine inspect --id <ID>'")
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}

func syncQuarantineInspectCmd() *cobra.Command {
	var id string

	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect forensic snapshots of a quarantined conflict record",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				return fmt.Errorf("--id flag is required")
			}
			st := getOrInitStore()
			if st == nil {
				return fmt.Errorf("state store unavailable")
			}

			records, err := st.ListQuarantineRecords(cmd.Context())
			if err != nil {
				return fmt.Errorf("list quarantine: %w", err)
			}

			var target *state.QuarantineRecord
			for _, r := range records {
				if r.ID == id {
					target = &r
					break
				}
			}

			if target == nil {
				return fmt.Errorf("quarantine record %q not found", id)
			}

			fmt.Printf("=== QUARANTINE FORENSIC DOSSIER: %s ===\n", target.ID)
			fmt.Printf("Entity Key:         %s\n", target.EntityKey)
			fmt.Printf("Source:             %s:%s#%d\n", target.SrcHost, target.SrcProject, target.SrcIID)
			fmt.Printf("Destination:        %s:%s#%d\n", target.DstHost, target.DstProject, target.DstIID)
			fmt.Printf("Quarantined At:     %s\n", target.QuarantinedAt.Format(time.RFC3339))
			fmt.Printf("Baseline Digest:    %s\n", target.BaselineDigest)
			fmt.Printf("Reason:             %s\n\n", target.QuarantineReason)

			fmt.Println("Source Forensic Snapshot:")
			fmt.Println("  " + target.SrcSnapshot)
			fmt.Println("\nDestination Forensic Snapshot:")
			fmt.Println("  " + target.DstSnapshot)
			return nil
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "Quarantine record ID to inspect")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
