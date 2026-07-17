package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ckodex/gitlabvalet/internal/journal"
)

func TestGenerate_EmptyEntries_Markdown(t *testing.T) {
	var buf bytes.Buffer

	opts := Options{
		Since:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Until:  time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		Format: FormatMarkdown,
	}

	Generate(&buf, []journal.Entry{}, opts)

	output := buf.String()

	if !strings.Contains(output, "# Activity Report") {
		t.Error("missing header '# Activity Report'")
	}
	if !strings.Contains(output, "Total operations") {
		t.Error("missing 'Total operations' summary")
	}
	if !strings.Contains(output, "| 0 |") {
		t.Error("missing '| 0 |' for zero count")
	}
}

func TestGenerate_EmptyEntries_Plain(t *testing.T) {
	var buf bytes.Buffer

	opts := Options{
		Since:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Until:  time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		Format: FormatPlain,
	}

	Generate(&buf, []journal.Entry{}, opts)

	output := buf.String()

	if !strings.Contains(output, "Activity Report") {
		t.Error("missing 'Activity Report' in plain format")
	}
	if !strings.Contains(output, "SUMMARY") {
		t.Error("missing 'SUMMARY' section")
	}
	if !strings.Contains(output, "total=0") {
		t.Error("missing 'total=0'")
	}
}

func TestGenerate_DefaultFormat_Markdown(t *testing.T) {
	var buf bytes.Buffer

	opts := Options{
		Since: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Until: time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
	}

	Generate(&buf, []journal.Entry{}, opts)

	output := buf.String()

	if !strings.Contains(output, "|") {
		t.Error("default format should be markdown (contains '|' table syntax)")
	}
}

func TestGenerate_MarkdownVsPlain(t *testing.T) {
	ts := time.Date(2026, 5, 15, 10, 30, 0, 0, time.UTC)
	entries := []journal.Entry{
		{
			ID:        "id1",
			Timestamp: ts,
			Host:      "gitlab.example.com",
			Op:        journal.OpCreate,
			Entity:    journal.EntityIssue,
			Project:   "my-project",
			Title:     "Test issue",
			URL:       "https://example.com/issue/1",
			Outcome:   journal.OutcomeOK,
		},
	}

	var mdBuf, plainBuf bytes.Buffer

	Generate(&mdBuf, entries, Options{
		Since:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Until:  time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		Format: FormatMarkdown,
	})

	Generate(&plainBuf, entries, Options{
		Since:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Until:  time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		Format: FormatPlain,
	})

	mdOut := mdBuf.String()
	plainOut := plainBuf.String()

	if !strings.Contains(mdOut, "|") {
		t.Error("markdown should contain '|' table syntax")
	}
	if strings.Contains(plainOut, "| Create | ") {
		t.Error("plain text should not use '|' table syntax")
	}

	if !strings.Contains(plainOut, "─") {
		t.Error("plain format should contain separator character '─'")
	}
}

func TestGenerate_AuthorInHeader(t *testing.T) {
	var buf bytes.Buffer

	opts := Options{
		Since:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Until:  time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		Format: FormatMarkdown,
		Author: "Alice Smith",
	}

	Generate(&buf, []journal.Entry{}, opts)

	output := buf.String()

	if !strings.Contains(output, "Alice Smith") {
		t.Error("author not found in output")
	}
	if !strings.Contains(output, "**Author:**") {
		t.Error("author label missing in markdown format")
	}
}

func TestGenerate_PeriodInOutput(t *testing.T) {
	var buf bytes.Buffer

	since := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)

	opts := Options{
		Since:  since,
		Until:  until,
		Format: FormatMarkdown,
	}

	Generate(&buf, []journal.Entry{}, opts)

	output := buf.String()

	if !strings.Contains(output, "2026-05-01") {
		t.Error("Since date not in output")
	}
	if !strings.Contains(output, "2026-05-31") {
		t.Error("Until date not in output")
	}
}

func TestGenerate_UntilZero_UsesToday(t *testing.T) {
	var buf bytes.Buffer

	before := time.Now().UTC().Format("2006-01-02")

	opts := Options{
		Since:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Until:  time.Time{},
		Format: FormatMarkdown,
	}

	Generate(&buf, []journal.Entry{}, opts)

	after := time.Now().UTC().Format("2006-01-02")

	output := buf.String()

	if !strings.Contains(output, before) && !strings.Contains(output, after) {
		t.Errorf("until=zero should use today's date (before=%s or after=%s), not found in output", before, after)
	}
}

func TestGenerate_MultiHost_ShowsInstanceSection(t *testing.T) {
	entries := []journal.Entry{
		{
			ID:        "1",
			Timestamp: time.Now(),
			Host:      "gitlab1.example.com",
			Op:        journal.OpCreate,
			Entity:    journal.EntityIssue,
			Project:   "proj1",
			Title:     "Issue 1",
			Outcome:   journal.OutcomeOK,
		},
		{
			ID:        "2",
			Timestamp: time.Now(),
			Host:      "gitlab2.example.com",
			Op:        journal.OpCreate,
			Entity:    journal.EntityIssue,
			Project:   "proj2",
			Title:     "Issue 2",
			Outcome:   journal.OutcomeOK,
		},
	}

	var buf bytes.Buffer

	Generate(&buf, entries, Options{
		Since:  time.Now().Add(-24 * time.Hour),
		Until:  time.Now().Add(24 * time.Hour),
		Format: FormatMarkdown,
	})

	output := buf.String()

	if !strings.Contains(output, "### By Instance") {
		t.Error("multi-host report should have '### By Instance' section")
	}
	if !strings.Contains(output, "gitlab1.example.com") {
		t.Error("host 'gitlab1.example.com' not in output")
	}
	if !strings.Contains(output, "gitlab2.example.com") {
		t.Error("host 'gitlab2.example.com' not in output")
	}
}

func TestGenerate_SingleHost_NoInstanceSection(t *testing.T) {
	entries := []journal.Entry{
		{
			ID:        "1",
			Timestamp: time.Now(),
			Host:      "gitlab.example.com",
			Op:        journal.OpCreate,
			Entity:    journal.EntityIssue,
			Project:   "proj1",
			Title:     "Issue 1",
			Outcome:   journal.OutcomeOK,
		},
		{
			ID:        "2",
			Timestamp: time.Now(),
			Host:      "gitlab.example.com",
			Op:        journal.OpUpdate,
			Entity:    journal.EntityIssue,
			Project:   "proj1",
			Title:     "Issue 1 updated",
			Outcome:   journal.OutcomeOK,
		},
	}

	var buf bytes.Buffer

	Generate(&buf, entries, Options{
		Since:  time.Now().Add(-24 * time.Hour),
		Until:  time.Now().Add(24 * time.Hour),
		Format: FormatMarkdown,
	})

	output := buf.String()

	if strings.Contains(output, "### By Instance") {
		t.Error("single-host report should NOT have '### By Instance' section")
	}
}

func TestGenerate_EntitySections_CanonicalOrder(t *testing.T) {
	now := time.Now()
	entries := []journal.Entry{
		{
			ID:        "mr",
			Timestamp: now,
			Host:      "gitlab.example.com",
			Op:        journal.OpCreate,
			Entity:    journal.EntityMR,
			Title:     "MR 1",
			Outcome:   journal.OutcomeOK,
		},
		{
			ID:        "epic",
			Timestamp: now,
			Host:      "gitlab.example.com",
			Op:        journal.OpCreate,
			Entity:    journal.EntityEpic,
			Title:     "Epic 1",
			Outcome:   journal.OutcomeOK,
		},
		{
			ID:        "issue",
			Timestamp: now,
			Host:      "gitlab.example.com",
			Op:        journal.OpCreate,
			Entity:    journal.EntityIssue,
			Title:     "Issue 1",
			Outcome:   journal.OutcomeOK,
		},
	}

	var buf bytes.Buffer

	Generate(&buf, entries, Options{
		Since:  now.Add(-24 * time.Hour),
		Until:  now.Add(24 * time.Hour),
		Format: FormatMarkdown,
	})

	output := buf.String()

	epicPos := strings.Index(output, "### Epics")
	issuePos := strings.Index(output, "### Issues")
	mrPos := strings.Index(output, "### Mrs")

	if epicPos == -1 {
		t.Error("Epics section not found")
	}
	if issuePos == -1 {
		t.Error("Issues section not found")
	}
	if mrPos == -1 {
		t.Error("Mrs section not found")
	}

	if epicPos != -1 && issuePos != -1 && epicPos >= issuePos {
		t.Error("Epic section should appear before Issue section")
	}
	if issuePos != -1 && mrPos != -1 && issuePos >= mrPos {
		t.Error("Issue section should appear before MR section")
	}
}

func TestGenerate_ErrorEntries_MarkedWithCheckmark(t *testing.T) {
	now := time.Now()
	entries := []journal.Entry{
		{
			ID:        "ok",
			Timestamp: now,
			Host:      "gitlab.example.com",
			Op:        journal.OpCreate,
			Entity:    journal.EntityIssue,
			Title:     "Success",
			Outcome:   journal.OutcomeOK,
		},
		{
			ID:        "err",
			Timestamp: now,
			Host:      "gitlab.example.com",
			Op:        journal.OpCreate,
			Entity:    journal.EntityIssue,
			Title:     "Failed",
			Detail:    "network error",
			Outcome:   journal.OutcomeErr,
		},
	}

	var mdBuf, plainBuf bytes.Buffer

	Generate(&mdBuf, entries, Options{
		Since:  now.Add(-24 * time.Hour),
		Until:  now.Add(24 * time.Hour),
		Format: FormatMarkdown,
	})

	Generate(&plainBuf, entries, Options{
		Since:  now.Add(-24 * time.Hour),
		Until:  now.Add(24 * time.Hour),
		Format: FormatPlain,
	})

	mdOut := mdBuf.String()
	plainOut := plainBuf.String()

	if !strings.Contains(mdOut, "❌") {
		t.Error("markdown error entry should use ❌ emoji")
	}
	if !strings.Contains(plainOut, "✗") {
		t.Error("plain text error entry should use ✗ character")
	}
}

func TestGenerate_ErrorSection_Markdown(t *testing.T) {
	now := time.Now()
	entries := []journal.Entry{
		{
			ID:        "err1",
			Timestamp: now,
			Host:      "gitlab.example.com",
			Op:        journal.OpCreate,
			Entity:    journal.EntityIssue,
			Project:   "proj1",
			Title:     "Failed op",
			Detail:    "timeout",
			Outcome:   journal.OutcomeErr,
		},
	}

	var buf bytes.Buffer

	Generate(&buf, entries, Options{
		Since:  now.Add(-24 * time.Hour),
		Until:  now.Add(24 * time.Hour),
		Format: FormatMarkdown,
	})

	output := buf.String()

	if !strings.Contains(output, "## Errors") {
		t.Error("markdown report should have '## Errors' section for error entries")
	}
	if !strings.Contains(output, "timeout") {
		t.Error("error detail should be in Errors section")
	}
}

func TestGenerate_NoErrorSection_WhenNoErrors(t *testing.T) {
	now := time.Now()
	entries := []journal.Entry{
		{
			ID:        "ok1",
			Timestamp: now,
			Host:      "gitlab.example.com",
			Op:        journal.OpCreate,
			Entity:    journal.EntityIssue,
			Title:     "Success",
			Outcome:   journal.OutcomeOK,
		},
	}

	var buf bytes.Buffer

	Generate(&buf, entries, Options{
		Since:  now.Add(-24 * time.Hour),
		Until:  now.Add(24 * time.Hour),
		Format: FormatMarkdown,
	})

	output := buf.String()

	if strings.Contains(output, "## Errors") {
		t.Error("report with no errors should not have '## Errors' section")
	}
}

func TestGenerate_Deterministic_SameInputsSameOutput(t *testing.T) {
	now := time.Date(2026, 5, 15, 10, 30, 0, 0, time.UTC)
	entries := []journal.Entry{
		{
			ID:        "id1",
			Timestamp: now,
			Host:      "gitlab.example.com",
			Op:        journal.OpCreate,
			Entity:    journal.EntityIssue,
			Project:   "proj1",
			Title:     "Issue 1",
			Outcome:   journal.OutcomeOK,
		},
		{
			ID:        "id2",
			Timestamp: now,
			Host:      "gitlab.example.com",
			Op:        journal.OpUpdate,
			Entity:    journal.EntityMR,
			Project:   "proj1",
			Title:     "MR 1",
			Outcome:   journal.OutcomeOK,
		},
	}

	opts := Options{
		Since:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Until:  time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		Format: FormatMarkdown,
		Author: "Test User",
	}

	var buf1, buf2 bytes.Buffer

	Generate(&buf1, entries, opts)
	Generate(&buf2, entries, opts)

	out1 := buf1.String()
	out2 := buf2.String()

	if out1 != out2 {
		t.Error("Generate() is not deterministic: same inputs produced different outputs")
	}
}

func TestGenerate_AllEntityTypes(t *testing.T) {
	now := time.Now()
	entities := []journal.Entity{
		journal.EntityEpic,
		journal.EntityMilestone,
		journal.EntityIssue,
		journal.EntityWorkItem,
		journal.EntityMR,
		journal.EntityNote,
		journal.EntityLabel,
	}

	var entries []journal.Entry
	for i, ent := range entities {
		entries = append(entries, journal.Entry{
			ID:        string(rune(i)),
			Timestamp: now,
			Host:      "gitlab.example.com",
			Op:        journal.OpCreate,
			Entity:    ent,
			Title:     string(ent) + " entry",
			Outcome:   journal.OutcomeOK,
		})
	}

	var buf bytes.Buffer

	Generate(&buf, entries, Options{
		Since:  now.Add(-24 * time.Hour),
		Until:  now.Add(24 * time.Hour),
		Format: FormatMarkdown,
	})

	output := buf.String()

	expectedSections := []string{"### Epics", "### Milestones", "### Issues", "### Workitems", "### Mrs", "### Notes", "### Labels"}

	for _, section := range expectedSections {
		if !strings.Contains(output, section) {
			t.Errorf("missing section: %s", section)
		}
	}
}

func TestGenerate_PlainFormat_ByInstanceSection(t *testing.T) {
	entries := []journal.Entry{
		{
			ID:        "1",
			Timestamp: time.Now(),
			Host:      "gitlab1.example.com",
			Op:        journal.OpCreate,
			Entity:    journal.EntityIssue,
			Title:     "Issue 1",
			Outcome:   journal.OutcomeOK,
		},
		{
			ID:        "2",
			Timestamp: time.Now(),
			Host:      "gitlab2.example.com",
			Op:        journal.OpCreate,
			Entity:    journal.EntityIssue,
			Title:     "Issue 2",
			Outcome:   journal.OutcomeOK,
		},
	}

	var buf bytes.Buffer

	Generate(&buf, entries, Options{
		Since:  time.Now().Add(-24 * time.Hour),
		Until:  time.Now().Add(24 * time.Hour),
		Format: FormatPlain,
	})

	output := buf.String()

	if !strings.Contains(output, "BY INSTANCE") {
		t.Error("multi-host plain format should have 'BY INSTANCE' section")
	}
}

func TestGenerate_SuccessfulEntry_MarkdownFormat(t *testing.T) {
	now := time.Now()
	entries := []journal.Entry{
		{
			ID:        "ok",
			Timestamp: now,
			Host:      "gitlab.example.com",
			Op:        journal.OpCreate,
			Entity:    journal.EntityIssue,
			Project:   "my-project",
			Title:     "Test issue",
			URL:       "https://gitlab.example.com/issues/1",
			Outcome:   journal.OutcomeOK,
		},
	}

	var buf bytes.Buffer

	Generate(&buf, entries, Options{
		Since:  now.Add(-24 * time.Hour),
		Until:  now.Add(24 * time.Hour),
		Format: FormatMarkdown,
	})

	output := buf.String()

	if !strings.Contains(output, "✅") {
		t.Error("successful markdown entry should use ✅ emoji")
	}
	if !strings.Contains(output, "Test issue") {
		t.Error("entry title should be in output")
	}
	if !strings.Contains(output, "[view](https://gitlab.example.com/issues/1)") {
		t.Error("entry URL should be linked in markdown")
	}
}

func TestGenerate_PlainFormat_SuccessfulEntry(t *testing.T) {
	now := time.Now()
	entries := []journal.Entry{
		{
			ID:        "ok",
			Timestamp: now,
			Host:      "gitlab.example.com",
			Op:        journal.OpCreate,
			Entity:    journal.EntityIssue,
			Project:   "my-project",
			Title:     "Test issue",
			Outcome:   journal.OutcomeOK,
		},
	}

	var buf bytes.Buffer

	Generate(&buf, entries, Options{
		Since:  now.Add(-24 * time.Hour),
		Until:  now.Add(24 * time.Hour),
		Format: FormatPlain,
	})

	output := buf.String()

	if !strings.Contains(output, "✓") {
		t.Error("successful plain entry should use ✓ character")
	}
	if !strings.Contains(output, "Test issue") {
		t.Error("entry title should be in output")
	}
}

func TestGenerate_GroupField_PlainFormat(t *testing.T) {
	now := time.Now()
	entries := []journal.Entry{
		{
			ID:        "1",
			Timestamp: now,
			Host:      "gitlab.example.com",
			Op:        journal.OpCreate,
			Entity:    journal.EntityEpic,
			Group:     "my-group",
			Title:     "Test epic",
			Outcome:   journal.OutcomeOK,
		},
	}

	var buf bytes.Buffer

	Generate(&buf, entries, Options{
		Since:  now.Add(-24 * time.Hour),
		Until:  now.Add(24 * time.Hour),
		Format: FormatPlain,
	})

	output := buf.String()

	if !strings.Contains(output, "my-group") {
		t.Error("group field should appear in plain format when project is empty")
	}
}
