package cmd

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/MChorfa/ggvalet/internal/plan"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/MChorfa/ggvalet/internal/reconcile"
	"github.com/spf13/cobra"
)

func planCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Manage structured work plans",
	}
	cmd.AddCommand(
		planValidateCmd(),
		planApplyCmd(),
		planDiffCmd(),
		planResumeCmd(),
		planStatusCmd(false),
		planStatusCmd(true),
	)
	return cmd
}

func planApplyCmd() *cobra.Command {
	var dryRun bool
	var yes bool

	cmd := &cobra.Command{
		Use:   "apply <file>",
		Short: "Apply a plan (dry-run by default)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]

			p, err := plan.ParseFile(filePath)
			if err != nil {
				return err
			}

			if dryRun {
				dryRunPrint(filePath, p)
				return nil
			}

			// Flag validation before provider construction: keeps the --yes
			// check free of network/token dependencies, enabling unit testing.
			if !yes {
				return fmt.Errorf("apply mode requires --yes (mass-write to shared resource)")
			}

			engine := reconcile.Engine{Provider: glClient.Provider, Store: glClient.State}
			runID, err := engine.Start(cmd.Context(), p)
			if err != nil {
				return err
			}
			info("reconciliation run: %s", runID)
			if err := engine.Execute(cmd.Context(), runID); err != nil {
				return fmt.Errorf("run %s stopped: %w (resume with: ggvalet plan resume %s)", runID, err, runID)
			}
			ok("Plan applied: run %s", runID)
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", true, "Dry-run mode (default true)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Authorize mutation (required when --dry-run=false)")
	return cmd
}

func planValidateCmd() *cobra.Command {
	return &cobra.Command{Use: "validate <file>", Short: "Validate plan schema and dependency graph", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		p, err := plan.ParseFile(args[0])
		if err != nil {
			return err
		}
		resources, err := plan.OrderedResources(p)
		if err != nil {
			return err
		}
		ok("Plan valid: version=%s resources=%d", p.Version, len(resources))
		return nil
	}}
}

func planResumeCmd() *cobra.Command {
	return &cobra.Command{Use: "resume <run-id>", Short: "Resume a stopped reconciliation run", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		engine := reconcile.Engine{Provider: glClient.Provider, Store: glClient.State}
		if err := engine.Resume(cmd.Context(), args[0]); err != nil {
			return err
		}
		ok("Plan run %s succeeded", args[0])
		return nil
	}}
}

func planStatusCmd(explain bool) *cobra.Command {
	use, short := "status <run-id>", "Show reconciliation run status"
	if explain {
		use, short = "explain <run-id>", "Explain reconciliation progress and failures"
	}
	return &cobra.Command{Use: use, Short: short, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		run, steps, err := glClient.State.Run(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		fmt.Printf("run: %s\nstatus: %s\ntarget: %s\n", run.ID, run.Status, run.TargetKey)
		if explain && run.Error != "" {
			fmt.Printf("error: %s\n", run.Error)
		}
		for _, step := range steps {
			fmt.Printf("  %-28s %-10s", step.StepID, step.Status)
			if step.RemoteIID != 0 {
				fmt.Printf(" iid=%d", step.RemoteIID)
			}
			if explain && step.Error != "" {
				fmt.Printf(" error=%s", step.Error)
			}
			fmt.Println()
		}
		return nil
	}}
}

func dryRunPrint(filePath string, p *plan.Plan) {
	fmt.Printf("== plan: %s ==\n", filePath)
	projectID := ""
	if p.Target.ProjectID != 0 {
		projectID = fmt.Sprintf(", project_id=%d", p.Target.ProjectID)
	}
	fmt.Printf("target: %s (group_id=%d%s)\n\n", p.Target.Provider, p.Target.GroupID, projectID)
	printMilestones(p.Milestones)
	printEpics(p.Epics)
	printIssues(p.Issues)
}

func printMilestones(ms []plan.Milestone) {
	fmt.Printf("Milestones (%d):\n", len(ms))
	for _, m := range ms {
		parts := []string{fmt.Sprintf("[%s] %s", m.ID, m.Title)}
		if m.DueDate != "" {
			parts = append(parts, fmt.Sprintf("due=%s", m.DueDate))
		}
		if m.State != "" {
			parts = append(parts, fmt.Sprintf("state=%s", m.State))
		}
		fmt.Printf("  - %s\n", strings.Join(parts, "  "))
	}
}

func printEpics(es []plan.Epic) {
	fmt.Printf("\nEpics (%d):\n", len(es))
	for _, e := range es {
		labels := ""
		if len(e.Labels) > 0 {
			labels = fmt.Sprintf("  labels=%v", e.Labels)
		}
		children := ""
		if len(e.Children) > 0 {
			children = fmt.Sprintf("  children=%d", len(e.Children))
		}
		fmt.Printf("  - [%s] %s%s%s\n", e.ID, e.Title, labels, children)
	}
}

func printIssues(is []plan.Issue) {
	fmt.Printf("\nIssues (%d):\n", len(is))
	for _, i := range is {
		fmt.Printf("  - [%s] %s\n", i.ID, i.Title)
		parts := []string{}
		if i.Milestone != "" {
			parts = append(parts, fmt.Sprintf("milestone=%s", i.Milestone))
		}
		if i.Epic != "" {
			parts = append(parts, fmt.Sprintf("epic=%s", i.Epic))
		}
		if len(i.Labels) > 0 {
			parts = append(parts, fmt.Sprintf("labels=%v", i.Labels))
		}
		if i.Weight != 0 {
			parts = append(parts, fmt.Sprintf("weight=%d", i.Weight))
		}
		if len(parts) > 0 {
			fmt.Printf("      %s\n", strings.Join(parts, "  "))
		}
	}
}

func planDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <file>",
		Short: "Show diff between plan and remote (matched, missing, orphan)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]

			p, err := plan.ParseFile(filePath)
			if err != nil {
				return err
			}

			prov := glClient.Provider
			if p.Target.Provider != string(prov.Kind()) {
				return fmt.Errorf("plan target provider %q != active provider %q (set GLVALET_PROVIDER or fix plan)",
					p.Target.Provider, prov.Kind())
			}

			ctx := cmd.Context()

			result, err := computeDiff(ctx, prov, p)
			if err != nil {
				return err
			}

			printDiffResult(result)
			return nil
		},
	}
	return cmd
}

type diffResult struct {
	matched []diffEntry
	missing []diffEntry
	orphan  []diffEntry
}

type diffEntry struct {
	kind   string
	id     string
	title  string
	webURL string
}

func computeDiff(ctx context.Context, prov provider.Provider, p *plan.Plan) (*diffResult, error) {
	result := &diffResult{}
	remoteByHash := make(map[string]diffEntry)

	if err := diffMilestones(ctx, prov, p, result, remoteByHash); err != nil {
		return nil, err
	}

	if err := diffEpics(ctx, prov, p, result, remoteByHash); err != nil {
		return nil, err
	}

	if p.Target.ProjectID != 0 {
		if err := diffIssues(ctx, prov, p, result, remoteByHash); err != nil {
			return nil, err
		}
	} else {
		info("project_id not set, skipping issue diff")
	}

	for _, entry := range remoteByHash {
		result.orphan = append(result.orphan, entry)
	}

	return result, nil
}

func diffMilestones(ctx context.Context, prov provider.Provider, p *plan.Plan, result *diffResult, remoteByHash map[string]diffEntry) error {
	unsupported := false
	for page := 1; ; page++ {
		ms, err := prov.ListGroupMilestones(ctx, p.Target.GroupID, provider.ListGroupMilestonesOptions{Page: page, PerPage: 100})
		if err != nil {
			if errors.Is(err, provider.ErrUnsupported) {
				info("group milestones not supported on %s, skipping", prov.Kind())
				unsupported = true
				break
			}
			return fmt.Errorf("list milestones: %w", err)
		}
		if len(ms) == 0 {
			break
		}

		for _, m := range ms {
			if hash, ok := plan.ExtractMarker(m.Description); ok {
				remoteByHash[hash] = diffEntry{kind: "milestone", id: fmt.Sprintf("!%d", m.IID), title: m.Title, webURL: m.WebURL}
			}
		}
	}

	for _, m := range p.Milestones {
		if unsupported {
			continue
		}
		hash := plan.MilestoneIdentity(m)
		if remote, found := remoteByHash[hash]; found {
			result.matched = append(result.matched, remote)
			delete(remoteByHash, hash)
			continue
		}
		result.missing = append(result.missing, diffEntry{kind: "milestone", id: m.ID, title: m.Title})
	}

	return nil
}

func diffEpics(ctx context.Context, prov provider.Provider, p *plan.Plan, result *diffResult, remoteByHash map[string]diffEntry) error {
	unsupported := false
	for page := 1; ; page++ {
		es, err := prov.ListGroupEpics(ctx, p.Target.GroupID, provider.ListGroupEpicsOptions{Page: page, PerPage: 100})
		if err != nil {
			if errors.Is(err, provider.ErrUnsupported) {
				info("epics not supported on %s, skipping", prov.Kind())
				unsupported = true
				break
			}
			return fmt.Errorf("list epics: %w", err)
		}
		if len(es) == 0 {
			break
		}

		for _, e := range es {
			if hash, ok := plan.ExtractMarker(e.Description); ok {
				remoteByHash[hash] = diffEntry{kind: "epic", id: fmt.Sprintf("&%d", e.IID), title: e.Title, webURL: e.WebURL}
			}
		}
	}

	for _, e := range p.Epics {
		if unsupported {
			continue
		}
		hash := plan.EpicIdentity(e)
		if remote, found := remoteByHash[hash]; found {
			result.matched = append(result.matched, remote)
			delete(remoteByHash, hash)
			continue
		}
		result.missing = append(result.missing, diffEntry{kind: "epic", id: e.ID, title: e.Title})
	}

	return nil
}

func diffIssues(ctx context.Context, prov provider.Provider, p *plan.Plan, result *diffResult, remoteByHash map[string]diffEntry) error {
	projectStr := strconv.Itoa(p.Target.ProjectID)

	for page := 1; ; page++ {
		is, err := prov.ListIssues(ctx, projectStr, provider.ListIssuesOptions{Page: page, PerPage: 100})
		if err != nil {
			return fmt.Errorf("list issues: %w", err)
		}
		if len(is) == 0 {
			break
		}

		for _, i := range is {
			if hash, ok := plan.ExtractMarker(i.Body); ok {
				remoteByHash[hash] = diffEntry{kind: "issue", id: fmt.Sprintf("#%d", i.IID), title: i.Title, webURL: i.WebURL}
			}
		}
	}

	for _, i := range p.Issues {
		hash := plan.IssueIdentity(i)
		if remote, found := remoteByHash[hash]; found {
			result.matched = append(result.matched, remote)
			delete(remoteByHash, hash)
		} else {
			result.missing = append(result.missing, diffEntry{kind: "issue", id: i.ID, title: i.Title})
		}
	}

	return nil
}

func printDiffResult(result *diffResult) {
	fmt.Println()

	if len(result.matched) > 0 {
		fmt.Printf("Matched (%d):\n", len(result.matched))
		for _, e := range result.matched {
			fmt.Printf("  %s %s [%s] %s\n", colorDim("="), e.kind, e.id, e.title)
			if e.webURL != "" {
				fmt.Printf("      %s\n", colorDim(e.webURL))
			}
		}
		fmt.Println()
	}

	if len(result.missing) > 0 {
		fmt.Printf("Missing (%d):\n", len(result.missing))
		for _, e := range result.missing {
			fmt.Printf("  %s %s [%s] %s\n", colorInfo("+"), e.kind, e.id, e.title)
		}
		fmt.Println()
	}

	if len(result.orphan) > 0 {
		slices.SortFunc(result.orphan, func(a, b diffEntry) int {
			return strings.Compare(a.kind+a.id, b.kind+b.id)
		})
		fmt.Printf("Orphan (%d):\n", len(result.orphan))
		for _, e := range result.orphan {
			fmt.Printf("  %s %s [%s] %s\n", colorErr("!"), e.kind, e.id, e.title)
			if e.webURL != "" {
				fmt.Printf("      %s\n", colorDim(e.webURL))
			}
		}
		fmt.Println()
	}

	fmt.Printf("Summary: %d matched, %d missing, %d orphan\n", len(result.matched), len(result.missing), len(result.orphan))
}
