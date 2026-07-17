package cmd

import (
	"strings"
	"testing"

	gl "github.com/xanzy/go-gitlab"
)

func TestRenovateHelpers(t *testing.T) {
	cases := []struct {
		name   string
		branch string
		desc   string
		want   bumpType
	}{
		{"patch", "renovate/patch-foo", "patch", bumpPatch},
		{"minor", "renovate/minor-foo", "minor", bumpMinor},
		{"major", "renovate/major-foo", "major", bumpMajor},
		{"unknown", "renovate/unknown-foo", "", bumpUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mr := &gl.MergeRequest{
				Title:        "chore(deps): update",
				Description:  tc.desc,
				SourceBranch: tc.branch,
			}
			if got := detectBump(mr); got != tc.want {
				t.Fatalf("detectBump() = %v, want %v", got, tc.want)
			}
		})
	}

	if got := bumpLabel(bumpUnknown); !strings.Contains(got, "?") {
		t.Fatalf("bumpLabel(unknown) = %q", got)
	}

	if !isRenovate(&gl.MergeRequest{
		Author:       &gl.BasicUser{Username: "renovate-bot"},
		SourceBranch: "renovate/patch-foo",
	}) {
		t.Fatal("expected MR from renovate-bot to be recognised")
	}
	if isRenovate(&gl.MergeRequest{
		Author:       &gl.BasicUser{Username: "alice"},
		SourceBranch: "feature",
	}) {
		t.Fatal("non-Renovate MR should not be recognised")
	}
	if isRenovate(&gl.MergeRequest{SourceBranch: "feature"}) {
		t.Fatal("MR without author should not be recognised")
	}

	all := parseBumpFilter("all")
	if !all[bumpPatch] || !all[bumpMinor] || !all[bumpMajor] || !all[bumpUnknown] {
		t.Fatal("parseBumpFilter(all) should include all bump types")
	}
	combo := parseBumpFilter("minor, patch")
	if !combo[bumpMinor] || !combo[bumpPatch] || combo[bumpMajor] || combo[bumpUnknown] {
		t.Fatal("parseBumpFilter(minor,patch) wrong")
	}
}

func TestRenovateMergeApply(t *testing.T) {
	setupTestClientWithServer(t)
	if err := runCmd(t, renovateCmd(), "merge", "--project", "group/project", "--bump", "all"); err != nil {
		t.Fatalf("renovate merge apply: %v", err)
	}
}
