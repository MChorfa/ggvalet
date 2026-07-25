package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func ciCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ci",
		Short:   "Manage CI/CD pipelines, jobs, and artifacts",
		Aliases: []string{"pipeline"},
	}
	cmd.AddCommand(
		ciListCmd(),
		ciViewCmd(),
		ciRunCmd(),
		ciRetryCmd(),
		ciCancelCmd(),
		ciJobsCmd(),
		ciLogsCmd(),
		ciArtifactsCmd(),
		ciDownloadCmd(),
	)
	return cmd
}

// ─── list ─────────────────────────────────────────────────────────────────────

func ciListCmd() *cobra.Command {
	var project, status, ref, sha string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List CI/CD pipelines",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}

			pipes, err := glClient.Provider.ListPipelines(cmd.Context(), project, provider.ListPipelinesOptions{
				Status:  status,
				Ref:     ref,
				SHA:     sha,
				PerPage: 50,
			})
			if err != nil {
				glClient.RecErr(journal.OpList, journal.EntityPipeline, project, "", err.Error())
				return fmt.Errorf("list pipelines: %w", err)
			}

			glClient.Rec(journal.OpList, journal.EntityPipeline, project, "", 0, 0,
				fmt.Sprintf("list %d pipelines", len(pipes)), "")

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"ID", "Status", "Ref", "SHA", "Commit", "Author"})
			table.SetBorder(false)
			for _, p := range pipes {
				table.Append([]string{
					strconv.Itoa(p.ID),
					p.Status,
					truncate(p.Ref, 25),
					truncate(p.SHA, 8),
					truncate(p.CommitMsg, 40),
					p.Author.Username,
				})
			}
			table.Render()
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().StringVar(&status, "status", "all", "Status: running|pending|success|failed|canceled|all")
	cmd.Flags().StringVar(&ref, "ref", "", "Filter by branch/tag ref")
	cmd.Flags().StringVar(&sha, "sha", "", "Filter by commit SHA")
	return cmd
}

// ─── view ─────────────────────────────────────────────────────────────────────

func ciViewCmd() *cobra.Command {
	var project string
	var id int

	cmd := &cobra.Command{
		Use:   "view",
		Short: "Show details of a single pipeline",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if id == 0 {
				return fmt.Errorf("--id required (pipeline ID)")
			}

			p, err := glClient.Provider.GetPipeline(cmd.Context(), project, id)
			if err != nil {
				glClient.RecErr(journal.OpView, journal.EntityPipeline, project, "", err.Error())
				return fmt.Errorf("get pipeline: %w", err)
			}

			glClient.Rec(journal.OpView, journal.EntityPipeline, project, "", p.ID, p.IID,
				p.Ref, p.WebURL)

			fmt.Printf("\nPipeline #%d\n", p.ID)
			fmt.Printf("  Status   : %s\n", p.Status)
			fmt.Printf("  Ref      : %s\n", p.Ref)
			fmt.Printf("  SHA      : %s\n", p.SHA)
			fmt.Printf("  Commit   : %s\n", p.CommitMsg)
			fmt.Printf("  Author   : %s\n", p.Author.Username)
			fmt.Printf("  Created  : %s\n", p.CreatedAt.Format("2006-01-02 15:04:05"))
			if !p.UpdatedAt.IsZero() {
				fmt.Printf("  Updated  : %s\n", p.UpdatedAt.Format("2006-01-02 15:04:05"))
			}
			if p.FinishedAt != nil {
				fmt.Printf("  Finished : %s\n", p.FinishedAt.Format("2006-01-02 15:04:05"))
			}
			if p.WebURL != "" {
				fmt.Printf("  URL      : %s\n", p.WebURL)
			}
			fmt.Println()
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&id, "id", 0, "Pipeline ID (required)")
	return cmd
}

// ─── run ──────────────────────────────────────────────────────────────────────

func ciRunCmd() *cobra.Command {
	var project, ref, workflow string
	var vars []string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Trigger a new pipeline on a ref",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if ref == "" {
				return fmt.Errorf("--ref required (branch or tag)")
			}

			opts := provider.RunPipelineOptions{Ref: ref, Workflow: workflow}
			for _, v := range vars {
				kv := strings.SplitN(v, "=", 2)
				if len(kv) != 2 {
					return fmt.Errorf("invalid --var %q (expected key=value)", v)
				}
				if opts.Variables == nil {
					opts.Variables = map[string]string{}
				}
				opts.Variables[kv[0]] = kv[1]
			}

			p, err := glClient.Provider.RunPipeline(cmd.Context(), project, opts)
			if err != nil {
				glClient.RecErr(journal.OpRun, journal.EntityPipeline, project, "", err.Error())
				return fmt.Errorf("run pipeline: %w", err)
			}

			glClient.Rec(journal.OpRun, journal.EntityPipeline, project, "", p.ID, p.IID,
				p.Ref, p.WebURL)
			ok("Pipeline #%d triggered on %s", p.ID, p.Ref)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().StringVar(&ref, "ref", "", "Branch or tag to run on (required)")
	cmd.Flags().StringVar(&workflow, "workflow", "", "Workflow file name (GitHub/Gitea Actions, e.g. ci.yml)")
	cmd.Flags().StringArrayVar(&vars, "var", nil, "CI/CD variable (key=value, repeatable)")
	return cmd
}

// ─── retry ────────────────────────────────────────────────────────────────────

func ciRetryCmd() *cobra.Command {
	var project string
	var id int

	cmd := &cobra.Command{
		Use:   "retry",
		Short: "Retry a failed or canceled pipeline",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if id == 0 {
				return fmt.Errorf("--id required (pipeline ID)")
			}

			p, err := glClient.Provider.RetryPipeline(cmd.Context(), project, id)
			if err != nil {
				glClient.RecErr(journal.OpRetry, journal.EntityPipeline, project, "", err.Error())
				return fmt.Errorf("retry pipeline: %w", err)
			}

			glClient.Rec(journal.OpRetry, journal.EntityPipeline, project, "", p.ID, p.IID,
				p.Ref, p.WebURL)
			ok("Pipeline #%d retried", p.ID)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&id, "id", 0, "Pipeline ID (required)")
	return cmd
}

// ─── cancel ───────────────────────────────────────────────────────────────────

func ciCancelCmd() *cobra.Command {
	var project string
	var id int

	cmd := &cobra.Command{
		Use:   "cancel",
		Short: "Cancel a running pipeline",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if id == 0 {
				return fmt.Errorf("--id required (pipeline ID)")
			}

			if err := glClient.Provider.CancelPipeline(cmd.Context(), project, id); err != nil {
				glClient.RecErr(journal.OpCancel, journal.EntityPipeline, project, "", err.Error())
				return fmt.Errorf("cancel pipeline: %w", err)
			}

			glClient.Rec(journal.OpCancel, journal.EntityPipeline, project, "", id, 0,
				"cancel", "")
			ok("Pipeline #%d canceled", id)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&id, "id", 0, "Pipeline ID (required)")
	return cmd
}

// ─── jobs ─────────────────────────────────────────────────────────────────────

func ciJobsCmd() *cobra.Command {
	var project string
	var id int

	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "List jobs in a pipeline",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if id == 0 {
				return fmt.Errorf("--id required (pipeline ID)")
			}

			jobs, err := glClient.Provider.ListPipelineJobs(cmd.Context(), project, id)
			if err != nil {
				glClient.RecErr(journal.OpList, journal.EntityJob, project, "", err.Error())
				return fmt.Errorf("list jobs: %w", err)
			}

			glClient.Rec(journal.OpList, journal.EntityJob, project, "", id, 0,
				fmt.Sprintf("list %d jobs", len(jobs)), "")

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"ID", "Name", "Stage", "Status", "Ref"})
			table.SetBorder(false)
			for _, j := range jobs {
				table.Append([]string{
					strconv.Itoa(j.ID),
					truncate(j.Name, 30),
					j.Stage,
					j.Status,
					truncate(j.Ref, 20),
				})
			}
			table.Render()
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&id, "id", 0, "Pipeline ID (required)")
	return cmd
}

// ─── logs ─────────────────────────────────────────────────────────────────────

func ciLogsCmd() *cobra.Command {
	var project string
	var jobID int

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Print the log output of a single job",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if jobID == 0 {
				return fmt.Errorf("--job required (job ID)")
			}

			logs, err := glClient.Provider.GetJobLogs(cmd.Context(), project, jobID)
			if err != nil {
				glClient.RecErr(journal.OpView, journal.EntityJob, project, "", err.Error())
				return fmt.Errorf("get job logs: %w", err)
			}

			glClient.Rec(journal.OpView, journal.EntityJob, project, "", jobID, 0,
				fmt.Sprintf("logs (%d bytes)", len(logs)), "")
			fmt.Print(logs)
			if !strings.HasSuffix(logs, "\n") {
				fmt.Println()
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&jobID, "job", 0, "Job ID (required)")
	return cmd
}

// ─── artifacts ────────────────────────────────────────────────────────────────

func ciArtifactsCmd() *cobra.Command {
	var project string
	var id int

	cmd := &cobra.Command{
		Use:   "artifacts",
		Short: "List artifacts produced by a pipeline",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if id == 0 {
				return fmt.Errorf("--id required (pipeline ID)")
			}

			arts, err := glClient.Provider.ListArtifacts(cmd.Context(), project, id)
			if err != nil {
				glClient.RecErr(journal.OpList, journal.EntityArtifact, project, "", err.Error())
				return fmt.Errorf("list artifacts: %w", err)
			}

			glClient.Rec(journal.OpList, journal.EntityArtifact, project, "", id, 0,
				fmt.Sprintf("list %d artifacts", len(arts)), "")

			if len(arts) == 0 {
				info("No artifacts found for pipeline #%d", id)
				return nil
			}

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Name", "Size", "Expired"})
			table.SetBorder(false)
			for _, a := range arts {
				table.Append([]string{
					a.Name,
					humanBytes(a.Size),
					strconv.FormatBool(a.Expired),
				})
			}
			table.Render()
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&id, "id", 0, "Pipeline ID (required)")
	return cmd
}

// ─── download ─────────────────────────────────────────────────────────────────

func ciDownloadCmd() *cobra.Command {
	var project, dest string
	var jobID int

	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download artifacts of a job to a directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if jobID == 0 {
				return fmt.Errorf("--job required (job ID)")
			}
			if dest == "" {
				dest = "."
			}

			absDest, err := filepath.Abs(dest)
			if err != nil {
				return fmt.Errorf("resolve dest: %w", err)
			}
			if err := os.MkdirAll(absDest, 0o755); err != nil {
				return fmt.Errorf("create dest dir: %w", err)
			}

			if err := glClient.Provider.DownloadArtifact(cmd.Context(), project, jobID, absDest); err != nil {
				glClient.RecErr(journal.OpDownload, journal.EntityArtifact, project, "", err.Error())
				return fmt.Errorf("download artifact: %w", err)
			}

			glClient.Rec(journal.OpDownload, journal.EntityArtifact, project, "", jobID, 0,
				fmt.Sprintf("downloaded to %s", absDest), "")
			ok("Artifacts for job #%d downloaded to %s", jobID, absDest)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&jobID, "job", 0, "Job ID (required)")
	cmd.Flags().StringVar(&dest, "dest", ".", "Destination directory")
	return cmd
}

// humanBytes renders a byte count in a human-friendly form.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for d := n / unit; d >= unit; d /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
