package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/MChorfa/ggvalet/internal/client"
	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/MChorfa/ggvalet/internal/report"
	"github.com/spf13/cobra"
)

func reportCmd() *cobra.Command {
	var since, until, format, author, output string

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Generate a manager-ready activity report from the journal",
		Example: `  ggvalet report --since 7d                       # print markdown to stdout
  ggvalet report --since 7d --until 1d            # last week, excluding today
  ggvalet report --since 7d --output weekly.md    # write to file
  ggvalet report --since 30d --format plain       # plain text
  ggvalet report --since 7d --author "Noufel C."  # include your name
  ggvalet report push --since 7d \
    --dst-host sc01-trt.thales-systems.ca/gitlab \
    --dst-project management/status-reports   # push as issue`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sinceTime, untilTime, err := parseTimeRange(since, until)
			if err != nil {
				return err
			}

			entries, err := glClient.QueryEntries(cmd.Context(), journal.Filter{Since: sinceTime, Until: untilTime})
			if err != nil {
				return err
			}

			if len(entries) == 0 {
				fmt.Println(colorDim("No activity in journal for that period."))
				return nil
			}

			opts := report.Options{
				Since:  sinceTime,
				Until:  untilTime,
				Author: author,
			}
			switch format {
			case "plain", "text":
				opts.Format = report.FormatPlain
			default:
				opts.Format = report.FormatMarkdown
			}

			w := os.Stdout
			if output != "" {
				f, err := os.Create(output)
				if err != nil {
					return fmt.Errorf("create output: %w", err)
				}
				defer f.Close()
				w = f
				defer func() { ok("Report written to %s", output) }()
			}

			report.Generate(w, entries, opts)
			return nil
		},
	}

	cmd.Flags().StringVar(&since, "since", "7d", "Start of range (duration ago: 1h, 24h, 7d, 30d, 2w)")
	cmd.Flags().StringVar(&until, "until", "", "End of range (duration ago, e.g. 1h); defaults to now")
	cmd.Flags().StringVar(&format, "format", "markdown", "Output format: markdown|plain")
	cmd.Flags().StringVar(&author, "author", "", "Your name for the report header")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Write to file instead of stdout")

	cmd.AddCommand(reportPushCmd())
	return cmd
}

// reportPushCmd generates a report from the journal and opens it as an issue
// on any GitLab project — great for posting weekly status to a management board.
func reportPushCmd() *cobra.Command {
	var since, until, author, dstHost, dstProject, title, reportHost string

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Post the activity report as an issue on a target project",
		Example: `  # Post weekly report to a management board on sc01
  ggvalet report push \
    --since 7d \
    --author "Noufel Chorfa" \
    --dst-host sc01-trt.thales-systems.ca/gitlab \
    --dst-project management/weekly-status

  # Scope the report to one instance, push to another
  ggvalet report push \
    --report-host gitlab.thalesdigital.io \
    --dst-host    sc01-trt.thales-systems.ca/gitlab \
    --dst-project management/reports`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// ── Resolve destination ──────────────────────────────────────────
			if dstProject == "" {
				return fmt.Errorf("--dst-project required")
			}
			if dstHost == "" {
				dstHost = cfg.Host
			}

			// ── Build report from journal ────────────────────────────────────
			sinceTime, untilTime, err := parseTimeRange(since, until)
			if err != nil {
				return err
			}

			jFilter := journal.Filter{Since: sinceTime, Until: untilTime}
			if reportHost != "" {
				jFilter.Host = reportHost
			}

			entries, err := glClient.QueryEntries(cmd.Context(), jFilter)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Println(colorDim("No activity in journal for that period — nothing to push."))
				return nil
			}

			var buf strings.Builder
			report.Generate(&buf, entries, report.Options{
				Since:  sinceTime,
				Until:  untilTime,
				Author: author,
				Format: report.FormatMarkdown,
			})
			body := buf.String()

			// ── Auto-generate title if not provided ──────────────────────────
			if title == "" {
				_, week := sinceTime.ISOWeek()
				title = fmt.Sprintf("Activity Report %s (W%02d)",
					sinceTime.Format("2006"), week)
			}

			// ── Build destination client ─────────────────────────────────────
			dstCfg, err := config.ForHost(cfg.Hosts, dstHost, cfg.JournalPath)
			if err != nil {
				return fmt.Errorf("dst host: %w", err)
			}
			dstC, err := client.New(dstCfg)
			if err != nil {
				return fmt.Errorf("dst client: %w", err)
			}

			// ── Create the issue ─────────────────────────────────────────────
			info("Pushing report to %s:%s", shortHostname(dstHost), dstProject)

			iss, err := dstC.Provider.CreateIssue(cmd.Context(), dstProject, provider.CreateIssueOptions{
				Title:       title,
				Description: body,
				Labels:      []string{"report", "status"},
			})
			if err != nil {
				dstC.RecErr(journal.OpCreate, journal.EntityIssue, dstProject, "", err.Error())
				return fmt.Errorf("create report issue: %w", err)
			}

			dstC.Rec(journal.OpCreate, journal.EntityIssue, dstProject, "",
				iss.ID, iss.IID, iss.Title, iss.WebURL, "report-push")
			ok("Report issue #%d created: %s", iss.IID, iss.WebURL)
			return nil
		},
	}

	cmd.Flags().StringVar(&since, "since", "7d", "Start of range (duration ago: 1h, 24h, 7d, 30d, 2w)")
	cmd.Flags().StringVar(&until, "until", "", "End of range (duration ago, e.g. 1h); defaults to now")
	cmd.Flags().StringVar(&author, "author", "", "Author name shown in report header")
	cmd.Flags().StringVar(&dstHost, "dst-host", "", "Destination hostname (default: active host)")
	cmd.Flags().StringVar(&dstProject, "dst-project", "", "Destination project path or ID (required)")
	cmd.Flags().StringVar(&title, "title", "", "Issue title (auto-generated if omitted)")
	cmd.Flags().StringVar(&reportHost, "report-host", "",
		"Scope journal entries to a single host (default: all hosts)")
	return cmd
}
