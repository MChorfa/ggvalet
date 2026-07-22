// cmd/timeline.go — Gantt-style terminal timeline for epics and milestones.
//
// Example output:
//
//	Timeline — gitlab.thalesdigital.io            ↓ today (2026-05-28)
//	2026  Apr       May       Jun       Jul       Aug       Sep
//	──────────────────────────────────────────────────────────────────
//	◆ Q3 Security Hardening                             65%
//	  ████████████████████████████████░░░░░░░░░░░░░░░░░
//	⬡ Sprint 12                    done
//	  ██████████████████████████████████  100%
//	⬡ Sprint 13
//	      ██████████████████░░░░░░░░░░░░  48%
//	⬡ Sprint 14
//	                    ░░░░░░░░░░░░░░░░   0%
package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/MChorfa/ggvalet/internal/parallel"
	"github.com/spf13/cobra"
	gl "github.com/xanzy/go-gitlab"
)

// ─── Timeline styles ──────────────────────────────────────────────────────────

var (
	tlEpicStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#bc96e6")).Bold(true)
	tlMSStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#38a3a5"))
	tlDoneStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	tlTodayStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#fcca46")).Bold(true)
	tlHeaderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	tlBarFull     = "█"
	tlBarEmpty    = "░"
	tlBarWidth    = 56 // characters for the bar region
)

// ─── Timeline item ────────────────────────────────────────────────────────────

type tlItem struct {
	label     string
	kind      string // "epic" | "milestone"
	startDate *time.Time
	dueDate   *time.Time
	pct       int // completion 0-100
	state     string
	url       string
}

// ─── Command ──────────────────────────────────────────────────────────────────

func timelineCmd() *cobra.Command {
	var project, group, since, until string
	var allHosts bool

	cmd := &cobra.Command{
		Use:   "timeline",
		Short: "Gantt-style timeline of epics and milestones",
		Example: `  ggvalet timeline --project group/project --group group
  ggvalet timeline --since 2026-01-01 --until 2026-12-31
  ggvalet timeline --all-hosts`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if group == "" {
				group = cfg.DefaultGroup
			}

			// Determine date window
			rangeStart, rangeEnd := parseDateWindow(since, until)

			// Collect items (parallel fetch of epics + milestones)
			var items []tlItem
			var fetchErr error

			type result struct {
				items []tlItem
				err   error
			}
			results := make(chan result, 2)

			pool := parallel.New(2)

			// Epics
			if group != "" {
				pool.Go(func() {
					epics, _, err := glClient.GL.Epics.ListGroupEpics(group,
						&gl.ListGroupEpicsOptions{
							State:       gl.Ptr("all"),
							ListOptions: gl.ListOptions{PerPage: 100},
						})
					if err != nil {
						results <- result{err: fmt.Errorf("epics: %w", err)}
						return
					}
					var its []tlItem
					for _, ep := range epics {
						its = append(its, tlItem{
							label:     fmt.Sprintf("&%d %s", ep.IID, ep.Title),
							kind:      "epic",
							startDate: isoToTime(ep.StartDate),
							dueDate:   isoToTime(ep.DueDate),
							pct:       0,
							state:     ep.State,
							url:       ep.WebURL,
						})
					}
					results <- result{items: its}
				})
			} else {
				results <- result{}
			}

			// Milestones
			if project != "" {
				pool.Go(func() {
					ms, _, err := glClient.GL.Milestones.ListMilestones(project,
						&gl.ListMilestonesOptions{
							State:       gl.Ptr("all"),
							ListOptions: gl.ListOptions{PerPage: 100},
						})
					if err != nil {
						results <- result{err: fmt.Errorf("milestones: %w", err)}
						return
					}
					var its []tlItem
					for _, m := range ms {
						its = append(its, tlItem{
							label:     fmt.Sprintf("⬡ %s", m.Title),
							kind:      "milestone",
							startDate: isoToTime(m.StartDate),
							dueDate:   isoToTime(m.DueDate),
							pct:       0,
							state:     m.State,
							url:       m.WebURL,
						})
					}
					results <- result{items: its}
				})
			} else {
				results <- result{}
			}

			pool.Wait()
			close(results)

			for r := range results {
				if r.err != nil {
					fetchErr = r.err
				}
				items = append(items, r.items...)
			}
			if fetchErr != nil {
				fmt.Fprintf(os.Stderr, "%s %v\n", colorErr("warn:"), fetchErr)
			}

			// Auto-expand range from items if not specified
			for _, it := range items {
				if it.startDate != nil && (rangeStart.IsZero() || it.startDate.Before(rangeStart)) {
					rangeStart = *it.startDate
				}
				if it.dueDate != nil && (rangeEnd.IsZero() || it.dueDate.After(rangeEnd)) {
					rangeEnd = *it.dueDate
				}
			}
			// Fallback: show ±3 months from today
			now := time.Now()
			if rangeStart.IsZero() {
				rangeStart = now.AddDate(0, -3, 0)
			}
			if rangeEnd.IsZero() {
				rangeEnd = now.AddDate(0, 3, 0)
			}
			// Pad by 2 weeks each side
			rangeStart = rangeStart.AddDate(0, 0, -14)
			rangeEnd = rangeEnd.AddDate(0, 0, 14)

			renderTimeline(items, rangeStart, rangeEnd, cfg.Host)
			return nil
		},
	}

	cmd.Flags().StringVarP(&project, "project", "p", "", "Project for milestones")
	cmd.Flags().StringVarP(&group, "group", "g", "", "Group for epics")
	cmd.Flags().StringVar(&since, "since", "", "Start date YYYY-MM-DD (auto-detected if omitted)")
	cmd.Flags().StringVar(&until, "until", "", "End date YYYY-MM-DD (auto-detected if omitted)")
	cmd.Flags().BoolVar(&allHosts, "all-hosts", false, "Fetch from all configured instances")
	return cmd
}

// ─── Renderer ─────────────────────────────────────────────────────────────────

func renderTimeline(items []tlItem, start, end time.Time, host string) {
	totalDays := int(end.Sub(start).Hours() / 24)
	if totalDays < 1 {
		totalDays = 1
	}
	scale := float64(totalDays) / float64(tlBarWidth)

	today := time.Now()
	todayOffset := int(today.Sub(start).Hours() / 24)
	todayChar := int(float64(todayOffset) / scale)
	if todayChar < 0 {
		todayChar = 0
	}
	if todayChar >= tlBarWidth {
		todayChar = tlBarWidth - 1
	}

	// ── Header ───────────────────────────────────────────────────────────────
	label := fmt.Sprintf("Timeline — %s", shortHostname(host))
	todayLabel := tlTodayStyle.Render(fmt.Sprintf("↓ today (%s)", today.Format("2006-01-02")))
	fmt.Printf("\n%-40s %s\n", colorInfo(label), todayLabel)

	// Month ruler
	fmt.Print(tlHeaderStyle.Render(buildMonthRuler(start, end, tlBarWidth)))
	fmt.Println()

	// Separator with today marker
	sep := []rune(strings.Repeat("─", tlBarWidth+2))
	if todayChar < len(sep) {
		sep[todayChar+1] = '┬'
	}
	fmt.Println(tlHeaderStyle.Render(string(sep)))

	if len(items) == 0 {
		fmt.Println(colorDim("  (no items with dates found — set start/due dates on epics and milestones)"))
		fmt.Println()
		return
	}

	// ── Items ─────────────────────────────────────────────────────────────────
	for _, it := range items {
		renderTimelineItem(it, start, totalDays, scale, todayChar)
	}
	fmt.Println()
}

func renderTimelineItem(it tlItem, rangeStart time.Time, totalDays int, scale float64, todayChar int) {
	// Label line
	labelStyle := tlMSStyle
	if it.kind == "epic" {
		labelStyle = tlEpicStyle
	}
	if it.state == "closed" {
		labelStyle = tlDoneStyle
	}
	fmt.Printf("%s\n", labelStyle.Render(truncate(it.label, 50)))

	if it.startDate == nil || it.dueDate == nil {
		fmt.Printf("  %s\n", colorDim("(no dates set)"))
		return
	}

	// Bar computation
	startOff := int(it.startDate.Sub(rangeStart).Hours() / 24)
	endOff := int(it.dueDate.Sub(rangeStart).Hours() / 24)

	barStart := int(float64(startOff) / scale)
	barEnd := int(float64(endOff) / scale)
	if barStart < 0 {
		barStart = 0
	}
	if barEnd > tlBarWidth {
		barEnd = tlBarWidth
	}
	if barEnd < barStart {
		barEnd = barStart + 1
	}
	barLen := barEnd - barStart
	filledLen := barLen * it.pct / 100

	// Build bar string
	bar := make([]rune, tlBarWidth)
	for i := range bar {
		bar[i] = ' '
	}
	for i := barStart; i < barEnd; i++ {
		if i < barStart+filledLen {
			bar[i] = '█'
		} else {
			bar[i] = '░'
		}
	}

	// Color the bar
	barStr := string(bar)
	filled := barStr[:barStart] +
		colorFill(string(bar[barStart:barStart+filledLen]), it.pct, true) +
		colorFill(string(bar[barStart+filledLen:barEnd]), it.pct, false) +
		barStr[barEnd:]

	// Today marker on bar
	if todayChar >= barStart && todayChar < barEnd {
		// inject today marker
		runes := []rune(barStr)
		runes[todayChar] = '│'
		filled = string(runes[:barStart]) +
			colorFill(string(runes[barStart:barStart+filledLen]), it.pct, true) +
			colorFill(string(runes[barStart+filledLen:barEnd]), it.pct, false) +
			string(runes[barEnd:])
	}

	pctStr := fmt.Sprintf(" %3d%%", it.pct)
	fmt.Printf("  %s%s\n", filled, colorDim(pctStr))
}

func colorFill(s string, pct int, filled bool) string {
	if s == "" {
		return ""
	}
	var c lipgloss.Color
	if filled {
		if pct >= 75 {
			c = lipgloss.Color("#22c55e")
		} else if pct >= 40 {
			c = lipgloss.Color("#fbbf24")
		} else {
			c = lipgloss.Color("#f97316")
		}
	} else {
		c = lipgloss.Color("#374151")
	}
	return lipgloss.NewStyle().Foreground(c).Render(s)
}

// buildMonthRuler returns a string like "Apr       May       Jun       Jul"
// aligned to the bar width.
func buildMonthRuler(start, end time.Time, width int) string {
	totalDays := int(end.Sub(start).Hours() / 24)
	if totalDays < 1 {
		totalDays = 1
	}
	scale := float64(totalDays) / float64(width)

	buf := make([]byte, width+2)
	for i := range buf {
		buf[i] = ' '
	}

	cur := start
	for cur.Before(end) {
		// Position of first day of this month
		firstOfMonth := time.Date(cur.Year(), cur.Month(), 1, 0, 0, 0, 0, time.UTC)
		offset := int(firstOfMonth.Sub(start).Hours() / 24)
		pos := int(float64(offset) / scale)
		if pos >= 0 && pos < width {
			label := firstOfMonth.Format("Jan")
			if pos == 0 || firstOfMonth.Month() == time.January {
				label = firstOfMonth.Format("Jan 2006")
			}
			for i, ch := range label {
				if pos+i < width {
					buf[pos+i] = byte(ch)
				}
			}
		}
		cur = cur.AddDate(0, 1, 0)
	}
	return string(buf)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func parseDateWindow(since, until string) (time.Time, time.Time) {
	var s, u time.Time
	if since != "" {
		t, err := time.Parse("2006-01-02", since)
		if err == nil {
			s = t
		}
	}
	if until != "" {
		t, err := time.Parse("2006-01-02", until)
		if err == nil {
			u = t
		}
	}
	return s, u
}

func isoToTime(iso *gl.ISOTime) *time.Time {
	if iso == nil {
		return nil
	}
	t := time.Time(*iso)
	return &t
}
