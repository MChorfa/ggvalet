package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/MChorfa/ggvalet/internal/provider"
)

func TestShortHostname(t *testing.T) {
	if got := shortHostname("gitlab.example.com"); got != "gitlab.example.com" {
		t.Fatalf("shortHostname(gitlab.example.com) = %q", got)
	}
	long := "sc01-trt.thales-systems.ca/gitlab"
	if got := shortHostname(long); got != "sc01-trt/gitlab" {
		t.Fatalf("shortHostname(%q) = %q", long, got)
	}
	noPath := "very-long-hostname.example.com"
	if got := shortHostname(noPath); !strings.HasPrefix(got, noPath[:21]) {
		t.Fatalf("shortHostname(%q) = %q", noPath, got)
	}
}

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"7d", 7 * 24 * time.Hour},
		{"2w", 2 * 7 * 24 * time.Hour},
		{"24h", 24 * time.Hour},
	}
	for _, tc := range cases {
		got, err := parseDuration(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("parseDuration(%q) = %v, %v", tc.in, got, err)
		}
	}
	if _, err := parseDuration("not-a-duration"); err == nil {
		t.Fatal("parseDuration should error on invalid input")
	}
}

func TestOutputHelpers(t *testing.T) {
	ok("test ok")
	info("test info")
	fail("test fail")
}

func TestRenderLabelChips(t *testing.T) {
	colorMap := map[string]string{"bug": "#FF0000", "blocked": "#FFA500"}
	chips := RenderLabelChips([]string{"bug", "blocked", "unknown"}, colorMap)
	if len(chips) != 3 {
		t.Fatalf("RenderLabelChips returned %d chips", len(chips))
	}
}

func TestTUIProgressBar(t *testing.T) {
	if got := tuiProgressBar(0, 10); got == "" {
		t.Fatal("tuiProgressBar(0,10) empty")
	}
	if got := tuiProgressBar(50, 10); got == "" {
		t.Fatal("tuiProgressBar(50,10) empty")
	}
	if got := tuiProgressBar(100, 10); got == "" {
		t.Fatal("tuiProgressBar(100,10) empty")
	}
}

func TestTUIListItemFilterValue(t *testing.T) {
	issue := &provider.Issue{IID: 42, Title: "issue title", Labels: []string{"bug"}}
	iit := issItem{issue}
	if got := iit.FilterValue(); got != "42 issue title bug" {
		t.Fatalf("issItem.FilterValue = %q", got)
	}

	epic := &provider.Epic{Title: "epic title"}
	eit := epItem{epic}
	if got := eit.FilterValue(); got != "epic title" {
		t.Fatalf("epItem.FilterValue = %q", got)
	}

	milestone := &provider.Milestone{Title: "v1.0"}
	mit := msItem{milestone}
	if got := mit.FilterValue(); got != "v1.0" {
		t.Fatalf("msItem.FilterValue = %q", got)
	}

	journal := jItem{title: "journal entry", entity: "issue", op: "create"}
	if got := journal.FilterValue(); got != "journal entry issue create" {
		t.Fatalf("jItem.FilterValue = %q", got)
	}
}
