package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func journalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "journal",
		Short:   "Browse your GitLab activity ledger",
		Aliases: []string{"j"},
	}
	cmd.AddCommand(journalShowCmd(), journalStatsCmd())
	return cmd
}

func journalShowCmd() *cobra.Command {
	var since, until, entity, project, host string
	var errOnly bool

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show journal entries (default: last 24h)",
		Example: `  ggvalet journal show --since 7d
  ggvalet journal show --since 7d --host gitlab.thalesdigital.io
  ggvalet journal show --since 30d --entity epic
  ggvalet journal show --errors`,
		RunE: func(cmd *cobra.Command, args []string) error {
			f := journal.Filter{}

			sinceTime, untilTime, err := parseTimeRange(since, until)
			if err != nil {
				return err
			}
			f.Since = sinceTime
			f.Until = untilTime
			if entity != "" {
				f.Entities = []journal.Entity{journal.Entity(entity)}
			}
			if project != "" {
				f.Project = project
			}
			// --host here filters the journal, not the active API host.
			// Use the flag value; fall back to the active client host only if
			// the user explicitly passed --journal-host (they didn't, so default
			// is all hosts — better for a multi-instance ledger view).
			if host != "" {
				f.Host = host
			}
			if errOnly {
				f.Outcome = journal.OutcomeErr
			}

			entries, err := glClient.QueryEntries(cmd.Context(), f)
			if err != nil {
				return err
			}

			if len(entries) == 0 {
				fmt.Println(colorDim("No journal entries for the given filter."))
				return nil
			}

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Time", "Host", "Op", "Entity", "Title / Detail", "Project", "⚑"})
			table.SetBorder(false)
			table.SetAutoWrapText(false)
			table.SetColMinWidth(4, 40)

			for _, e := range entries {
				detail := e.Title
				if detail == "" {
					detail = e.Detail
				}
				status := colorOK("✓")
				if e.Outcome == journal.OutcomeErr {
					status = colorErr("✗")
				}
				proj := e.Project
				if proj == "" {
					proj = e.Group
				}
				// Shorten known long host prefixes for readability
				shortHost := shortHostname(e.Host)
				table.Append([]string{
					e.Timestamp.Local().Format("01-02 15:04"),
					shortHost,
					string(e.Op),
					string(e.Entity),
					truncate(detail, 42),
					truncate(proj, 22),
					status,
				})
			}
			table.Render()
			fmt.Printf("\n%s  total: %d\n",
				colorDim("since "+f.Since.Local().Format("Jan 02 15:04")),
				len(entries))
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "24h", "Duration ago: 1h, 24h, 7d, 30d")
	cmd.Flags().StringVar(&until, "until", "", "End of range (duration ago, e.g. 1h)")
	cmd.Flags().StringVar(&entity, "entity", "", "Filter: issue|epic|milestone|workitem|mr")
	cmd.Flags().StringVar(&project, "project", "", "Filter by project path")
	cmd.Flags().StringVar(&host, "host", "", "Filter by GitLab instance hostname")
	cmd.Flags().BoolVar(&errOnly, "errors", false, "Show only errors")
	return cmd
}

func journalStatsCmd() *cobra.Command {
	var since, until, host string

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show operation stats for a time window",
		RunE: func(cmd *cobra.Command, args []string) error {
			sinceTime, untilTime, err := parseTimeRange(since, until)
			if err != nil {
				return err
			}

			f := journal.Filter{Since: sinceTime, Until: untilTime}
			if host != "" {
				f.Host = host
			}

			entries, err := glClient.QueryEntries(cmd.Context(), f)
			if err != nil {
				return err
			}

			stats := journal.ComputeStats(entries)

			rangeLabel := since
			if until != "" {
				rangeLabel += " → " + until
			}
			fmt.Printf("\n%s\n", colorInfo("Activity stats — "+rangeLabel))
			fmt.Printf("  Total  : %d  |  Errors: %d\n\n", stats.Total, stats.Errors)

			if len(stats.ByHost) > 1 {
				fmt.Println("  By host:")
				for h, count := range stats.ByHost {
					fmt.Printf("    %-40s %d\n", shortHostname(h), count)
				}
				fmt.Println()
			}

			fmt.Println("  By entity:")
			for ent, count := range stats.ByEntity {
				fmt.Printf("    %-14s %d\n", ent, count)
			}
			fmt.Println()

			fmt.Println("  By operation:")
			for op, count := range stats.ByOp {
				fmt.Printf("    %-14s %d\n", op, count)
			}
			fmt.Println()
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "7d", "Duration: 1h, 24h, 7d, 30d")
	cmd.Flags().StringVar(&until, "until", "", "End of range (duration ago, e.g. 1h)")
	cmd.Flags().StringVar(&host, "host", "", "Scope stats to one GitLab instance")
	return cmd
}

// ─── Duration parser ──────────────────────────────────────────────────────────

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		var days int
		fmt.Sscanf(s[:len(s)-1], "%d", &days)
		return time.Duration(days) * 24 * time.Hour, nil
	}
	if strings.HasSuffix(s, "w") {
		var weeks int
		fmt.Sscanf(s[:len(s)-1], "%d", &weeks)
		return time.Duration(weeks) * 7 * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// parseTimeRange converts --since and optional --until (both durations ago)
// into absolute timestamps. Empty until defaults to now. It errors if the
// implied window is inverted (until before since).
func parseTimeRange(since, until string) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	sinceDur, err := parseDuration(since)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("--since: %w", err)
	}
	sinceTime := now.Add(-sinceDur)

	untilTime := now
	if until != "" {
		untilDur, err := parseDuration(until)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("--until: %w", err)
		}
		untilTime = now.Add(-untilDur)
	}
	if untilTime.Before(sinceTime) {
		return time.Time{}, time.Time{}, fmt.Errorf("--until (%s) is before --since (%s)", untilTime.Format(time.RFC3339), sinceTime.Format(time.RFC3339))
	}
	return sinceTime, untilTime, nil
}

// shortHostname returns a display-friendly version of a long hostname key.
// "sc01-trt.thales-systems.ca/gitlab" → "sc01-trt/gitlab"
func shortHostname(h string) string {
	if len(h) <= 24 {
		return h
	}
	// Keep first label + path tail
	parts := strings.SplitN(h, ".", 2)
	first := parts[0]
	tail := ""
	if idx := strings.Index(h, "/"); idx != -1 {
		tail = h[idx:]
	}
	if tail != "" {
		return first + tail
	}
	return h[:21] + "…"
}
