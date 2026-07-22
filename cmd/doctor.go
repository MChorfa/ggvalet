package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ckodex/gitlabvalet/internal/config"
	"github.com/ckodex/gitlabvalet/internal/journal"
	"github.com/ckodex/gitlabvalet/internal/provider"
	"github.com/ckodex/gitlabvalet/internal/providerfactory"
	"github.com/ckodex/gitlabvalet/internal/state"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check glv configuration, local state, and API reachability",
		Long: `Diagnose the glv environment.

Checks performed:
  1. Configuration loads (glab config + env + flags).
  2. State database (~/.gitlabvalet/state.db) is writable.
  3. Journal file (~/.gitlabvalet/journal.jsonl) is writable.
  4. Provider builds (gitlab or github).
  5. One lightweight API call confirms the token is accepted by the host.

This command never writes to the journal or creates remote resources.`,
		// Override the normal pre-run so doctor can report config errors itself
		// instead of failing before the command body runs.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor()
		},
	}
}

func runDoctor() error {
	okChecks := true
	header := color.CyanString("── glv doctor ──")
	fmt.Fprintln(os.Stderr, header)

	// 1. Configuration
	cfg, err := config.Load(config.Options{HostFlag: hostFlag})
	if err != nil {
		fail("config: %v", err)
		switch providerHint() {
		case "github":
			info("hint: set GLVALET_GITHUB_TOKEN (or GLVALET_TOKEN) and GLVALET_GITHUB_ENABLED=true")
		default:
			info("hint: run `glab auth login` or set GLVALET_TOKEN + GLVALET_GITLAB_URL")
		}
		return fmt.Errorf("config load failed")
	}
	ok("config: loaded")
	info("  host: %s", cfg.Host)
	if cfg.Provider != "" {
		info("  provider: %s", cfg.Provider)
	}
	if cfg.User != "" {
		info("  user: %s", cfg.User)
	}
	info("  journal: %s", cfg.JournalPath)
	info("  state:   %s", cfg.StatePath)

	// 2. State store
	st, err := state.Open(cfg.StatePath)
	if err != nil {
		fail("state.db: %v", err)
		okChecks = false
	} else {
		_ = st.Close()
		ok("state.db: writable")
	}

	// 3. Journal
	if _, err := journal.Open(cfg.JournalPath); err != nil {
		fail("journal: %v", err)
		okChecks = false
	} else {
		ok("journal: writable")
	}

	// 4. Provider build
	prov, err := providerfactory.NewFromConfig(cfg)
	if err != nil {
		fail("provider: %v", err)
		if cfg.Provider == "github" {
			info("hint: set GLVALET_GITHUB_ENABLED=true to enable the GitHub provider")
		}
		okChecks = false
	} else {
		ok("provider: %s", prov.Kind())

		// 5. API probe — a single lightweight call that validates the token.
		if err := probeProvider(context.Background(), prov, cfg.Host); err != nil {
			fail("api probe: %v", err)
			okChecks = false
		} else {
			ok("api probe: %s reachable", cfg.Host)
		}
	}

	if !okChecks {
		fail("doctor: some checks failed")
		return fmt.Errorf("diagnostics failed")
	}
	ok("doctor: all checks passed")
	return nil
}

// probeProvider runs a token-validating call that is safe for either host.
// ListMyIssues returns an empty list with a nil error when the token is valid
// but the user has no assigned issues; any auth/network problem surfaces as an
// error.
func probeProvider(ctx context.Context, prov provider.Provider, host string) error {
	_, err := prov.ListMyIssues(ctx, provider.ListMyIssuesOptions{PerPage: 1})
	if err != nil {
		return fmt.Errorf("%s: %w", host, err)
	}
	return nil
}

// providerHint returns the canonical provider name from GLVALET_PROVIDER, or
// empty when unset. Used for hint text before config.Load has a Config object.
func providerHint() string {
	return strings.TrimSpace(strings.ToLower(os.Getenv("GLVALET_PROVIDER")))
}
