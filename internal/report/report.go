// Package report generates manager-ready summaries from journal entries.
package report

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/ckodex/gitlabvalet/internal/journal"
)

// Format controls report output.
type Format string

const (
	FormatMarkdown Format = "markdown"
	FormatPlain    Format = "plain"
)

// Options controls report generation.
type Options struct {
	Since  time.Time
	Until  time.Time
	Format Format
	Author string
}

// Generate writes a manager report to w from entries.
func Generate(w io.Writer, entries []journal.Entry, opts Options) {
	since := opts.Since.Format("2006-01-02")
	until := opts.Until.Format("2006-01-02")
	if opts.Until.IsZero() {
		until = time.Now().UTC().Format("2006-01-02")
	}
	stats := journal.ComputeStats(entries)

	switch opts.Format {
	case FormatPlain:
		generatePlain(w, entries, stats, since, until, opts.Author)
	default:
		generateMarkdown(w, entries, stats, since, until, opts.Author)
	}
}

// ─── Markdown ─────────────────────────────────────────────────────────────────

func generateMarkdown(w io.Writer, entries []journal.Entry, stats journal.Stats,
	since, until, author string) {

	h := func(format string, a ...any) { fmt.Fprintf(w, format+"\n", a...) }

	h("# Activity Report")
	h("")
	if author != "" {
		h("**Author:** %s  ", author)
	}
	h("**Period:** %s → %s  ", since, until)
	h("**Generated:** %s  ", time.Now().UTC().Format(time.RFC3339))
	h("")

	// ── Summary ──────────────────────────────────────────────────────────────
	h("## Summary")
	h("")
	h("| Metric | Count |")
	h("|--------|-------|")
	h("| Total operations | %d |", stats.Total)
	h("| Errors | %d |", stats.Errors)
	h("")

	// Per-host breakdown (only shown when multiple instances active)
	if len(stats.ByHost) > 1 {
		h("### By Instance")
		h("")
		h("| Hostname | Operations |")
		h("|----------|------------|")
		for _, host := range sortedStrings(stats.ByHost) {
			h("| `%s` | %d |", host, stats.ByHost[host])
		}
		h("")
	}

	h("### By Entity")
	h("")
	h("| Entity | Count |")
	h("|--------|-------|")
	for _, e := range sortedEntityKeys(stats.ByEntity) {
		h("| %s | %d |", e, stats.ByEntity[journal.Entity(e)])
	}
	h("")

	// ── Activity sections — grouped by host when multi-instance ──────────────
	byHost := groupByHost(entries)
	hostOrder := sortedStringSlice(byHost)

	for _, host := range hostOrder {
		hostEntries := byHost[host]

		// Only print the host header when there are multiple instances.
		if len(byHost) > 1 {
			h("---")
			h("")
			h("## Instance: `%s`", host)
			h("")
		}

		groups := groupByEntity(hostEntries)
		entityOrder := []journal.Entity{
			journal.EntityEpic, journal.EntityMilestone,
			journal.EntityIssue, journal.EntityWorkItem,
			journal.EntityMR, journal.EntityNote, journal.EntityLabel,
		}

		for _, entity := range entityOrder {
			subset, ok := groups[entity]
			if !ok || len(subset) == 0 {
				continue
			}
			h("### %s", capitalize(string(entity))+"s")
			h("")
			for _, e := range subset {
				if e.Outcome == journal.OutcomeErr {
					h("- ❌ **[%s]** %s (`%s`) — *%s*",
						e.Op, e.Title, e.Project, e.Detail)
				} else {
					urlPart := ""
					if e.URL != "" {
						urlPart = fmt.Sprintf(" — [view](%s)", e.URL)
					}
					projPart := ""
					if e.Project != "" {
						projPart = fmt.Sprintf(" `%s`", e.Project)
					} else if e.Group != "" {
						projPart = fmt.Sprintf(" `%s`", e.Group)
					}
					h("- ✅ **[%s]** %s%s%s _%s_",
						e.Op, e.Title, projPart, urlPart,
						e.Timestamp.Format("Jan 02 15:04"))
				}
			}
			h("")
		}
	}

	// ── Errors ───────────────────────────────────────────────────────────────
	var errs []journal.Entry
	for _, e := range entries {
		if e.Outcome == journal.OutcomeErr {
			errs = append(errs, e)
		}
	}
	if len(errs) > 0 {
		h("## Errors")
		h("")
		for _, e := range errs {
			h("- `%s/%s` on `%s` @ `%s` — %s (%s)",
				e.Entity, e.Op, e.Project, e.Host, e.Detail,
				e.Timestamp.Format("Jan 02 15:04"))
		}
		h("")
	}
}

// ─── Plain text ───────────────────────────────────────────────────────────────

func generatePlain(w io.Writer, entries []journal.Entry, stats journal.Stats,
	since, until, author string) {

	p := func(format string, a ...any) { fmt.Fprintf(w, format+"\n", a...) }
	sep := func() { p(strings.Repeat("─", 62)) }

	sep()
	p("  Activity Report")
	if author != "" {
		p("  Author   : %s", author)
	}
	p("  Period   : %s → %s", since, until)
	p("  Generated: %s", time.Now().UTC().Format(time.RFC3339))
	sep()
	p("")
	p("SUMMARY  total=%d  errors=%d", stats.Total, stats.Errors)
	p("")

	if len(stats.ByHost) > 1 {
		p("BY INSTANCE")
		for _, h := range sortedStrings(stats.ByHost) {
			p("  %-42s %d", h, stats.ByHost[h])
		}
		p("")
	}

	byHost := groupByHost(entries)
	for _, host := range sortedStringSlice(byHost) {
		hostEntries := byHost[host]
		if len(byHost) > 1 {
			sep()
			p("  INSTANCE: %s", host)
			sep()
			p("")
		}

		groups := groupByEntity(hostEntries)
		entityOrder := []journal.Entity{
			journal.EntityEpic, journal.EntityMilestone,
			journal.EntityIssue, journal.EntityWorkItem,
			journal.EntityMR, journal.EntityNote, journal.EntityLabel,
		}
		for _, entity := range entityOrder {
			subset, ok := groups[entity]
			if !ok || len(subset) == 0 {
				continue
			}
			p("%s (%d)", strings.ToUpper(string(entity))+"S", len(subset))
			for _, e := range subset {
				icon := "✓"
				if e.Outcome == journal.OutcomeErr {
					icon = "✗"
				}
				proj := e.Project
				if proj == "" {
					proj = e.Group
				}
				p("  %s [%s] %s (%s) %s",
					icon, e.Op, e.Title, proj,
					e.Timestamp.Format("Jan 02 15:04"))
			}
			p("")
		}
	}
	sep()
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func groupByHost(entries []journal.Entry) map[string][]journal.Entry {
	m := make(map[string][]journal.Entry)
	for _, e := range entries {
		m[e.Host] = append(m[e.Host], e)
	}
	return m
}

func groupByEntity(entries []journal.Entry) map[journal.Entity][]journal.Entry {
	m := make(map[journal.Entity][]journal.Entry)
	for _, e := range entries {
		m[e.Entity] = append(m[e.Entity], e)
	}
	return m
}

func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func sortedEntityKeys(m map[journal.Entity]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	return keys
}

// sortedStrings returns map keys sorted, for deterministic output.
func sortedStrings(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringSlice(m map[string][]journal.Entry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
