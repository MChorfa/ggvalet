// Live-instance integration tests for the GitHub provider.
// These run against the real GitHub API and are skipped unless
// GLVALET_GITHUB_ENABLED=true and GLVALET_GITHUB_TOKEN are set.
package github_test

import (
	"context"
	"os"
	"testing"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/MChorfa/ggvalet/internal/providerfactory"
)

// TestProviderGitHubLive exercises the real GitHub API with a live token.
// It proves the provider can authenticate and perform read-only calls.
func TestProviderGitHubLive(t *testing.T) {
	if os.Getenv("GLVALET_GITHUB_ENABLED") != "true" {
		t.Skip("GLVALET_GITHUB_ENABLED is not 'true' — skipping live GitHub test")
	}
	if os.Getenv("GLVALET_GITHUB_TOKEN") == "" && os.Getenv("GLVALET_TOKEN") == "" {
		t.Skip("GLVALET_GITHUB_TOKEN or GLVALET_TOKEN is required — skipping live GitHub test")
	}

	t.Setenv("GLVALET_PROVIDER", "github")

	cfg, err := config.Load(config.Options{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	prov, err := providerfactory.NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("providerfactory.NewFromConfig: %v", err)
	}
	if prov.Kind() != provider.KindGitHub {
		t.Fatalf("expected github provider, got %v", prov.Kind())
	}

	ctx := context.Background()

	// 1. Token-validating read that works for any authenticated user.
	myIssues, err := prov.ListMyIssues(ctx, provider.ListMyIssuesOptions{PerPage: 1})
	if err != nil {
		t.Fatalf("ListMyIssues (live): %v", err)
	}
	// An empty result is valid (user may have no assigned issues); a non-nil
	// result without an auth error is sufficient evidence the wire path works.
	t.Logf("ListMyIssues returned %d issue(s)", len(myIssues))

	// 2. If a test project is provided, exercise project-scoped reads.
	if testProject := os.Getenv("GLVALET_GITHUB_TEST_PROJECT"); testProject != "" {
		labels, err := prov.ListLabels(ctx, testProject)
		if err != nil {
			t.Fatalf("ListLabels %q (live): %v", testProject, err)
		}
		t.Logf("ListLabels %q returned %d label(s)", testProject, len(labels))

		issues, err := prov.ListIssues(ctx, testProject, provider.ListIssuesOptions{State: "open", PerPage: 1})
		if err != nil {
			t.Fatalf("ListIssues %q (live): %v", testProject, err)
		}
		t.Logf("ListIssues %q returned %d issue(s)", testProject, len(issues))
	}
}
