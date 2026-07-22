package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ckodex/gitlabvalet/internal/client"
	"github.com/ckodex/gitlabvalet/internal/config"
	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var (
	glClient *client.Client
	cfg      *config.Config
	hostFlag string
)

var rootCmd = &cobra.Command{
	Use:   "glv",
	Short: "GitLab Valet — multi-instance GitLab agent with automatic activity journaling",
	Long: color.CyanString(`
╔═══════════════════════════════════════════════════╗
║          GitLab Valet  (ckodex/gitlabvalet)       ║
║  Manage · Record · Report  — never miss a thing  ║
╚═══════════════════════════════════════════════════╝`) + `

Reads your glab CLI config (~/.config/glab-cli/config.yml) automatically.
Tokens, TLS settings, and API hosts are all inherited from glab — no extra setup.

  glv hosts                                          # list all configured instances
  glv issue mine                                     # issues on default host
  glv --host sc01-trt.thales-systems.ca/gitlab issue mine
  glv report --since 7d --author "Noufel Chorfa"    # manager report
`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "completion" || cmd.Name() == "__complete" {
			return nil
		}
		var err error
		cfg, err = config.Load(config.Options{HostFlag: hostFlag})
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
		glClient, err = client.New(cfg)
		if err != nil {
			return fmt.Errorf("client init: %w", err)
		}
		// Fail loud if a not-yet-host-neutral command runs under a non-GitLab
		// provider, rather than silently querying GitLab (RES-01, Rule 12).
		if err := ensureHostNeutral(cmd, glClient.Provider.Kind()); err != nil {
			return err
		}
		tls := ""
		if cfg.SkipTLS {
			tls = colorDim(" [tls-skip]")
		}
		user := ""
		if cfg.User != "" {
			user = colorDim(" as " + cfg.User)
		}
		fmt.Fprintf(os.Stderr, "%s %s%s%s\n",
			colorDim("→ host:"), colorInfo(cfg.Host), user, tls)
		return nil
	},
}

func Execute(version string) {
	// Setting Version makes Cobra auto-wire `--version` (and `-v` is left free).
	// The version check short-circuits before PersistentPreRunE, so it prints
	// without loading config or building a client.
	rootCmd.Version = version
	rootCmd.SetVersionTemplate("glv {{.Version}}\n")

	rootCmd.PersistentFlags().StringVarP(&hostFlag, "host", "H", "",
		"GitLab instance hostname (overrides glab default)")

	rootCmd.AddCommand(
		hostsCmd(),
		tuiCmd(),
		issueCmd(),
		epicCmd(),
		milestoneCmd(),
		workItemCmd(),
		mrCmd(),
		syncCmd(),
		searchCmd(),
		standupCmd(),
		timelineCmd(),
		renovateCmd(),
		shieldsCmd(),
		journalCmd(),
		reportCmd(),
		labelCmd(),
		cacheCmd(),
		receiptCmd(),
		planCmd(),
		doctorCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, color.RedString("error: %s", err))
		os.Exit(1)
	}
}

// ─── hosts command ────────────────────────────────────────────────────────────

func hostsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hosts",
		Short: "List configured hosts and the active provider",
		// Override PersistentPreRunE: we enumerate hosts without needing an
		// active client (useful when a token is temporarily empty).
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			hosts, defaultHost, provider, err := loadHostsForDisplay()
			if err != nil {
				return err
			}

			// Print a provider-aware header first; this makes GitHub-mode users
			// realize why GLVALET_HOST is not the active selector.
			if provider == "github" {
				fmt.Println(colorOK("✓ Active provider: github") + "  " + defaultHost)
				if len(hosts) > 0 {
					fmt.Println(colorDim("\nGitLab destinations (used by report push, sync, etc.):"))
				}
			} else if provider == "gitlab" {
				fmt.Println(colorOK("✓ Active provider: gitlab") + "  " + defaultHost)
			}

			if len(hosts) == 0 {
				fmt.Println(colorDim("No GitLab hosts configured. Run: glab auth login"))
				if provider == "github" {
					printGitHubHints()
				}
				return nil
			}

			keys := make([]string, 0, len(hosts))
			for k := range hosts {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Hostname", "User", "API URL", "TLS Skip", "Default"})
			table.SetBorder(false)
			table.SetAutoWrapText(false)
			table.SetColMinWidth(0, 30)

			for _, h := range keys {
				hc := hosts[h]
				isDefault := ""
				if h == defaultHost {
					isDefault = "✓"
				}
				skipTLS := ""
				if hc.SkipTLS() {
					skipTLS = "yes"
				}
				table.Append([]string{
					h,
					hc.User,
					hc.APIURL(h),
					skipTLS,
					isDefault,
				})
			}
			table.Render()

			if provider == "github" {
				printGitHubHints()
			} else {
				fmt.Printf("\n%s  glv --host <hostname> <command>\n",
					colorDim("To switch:"))
				fmt.Printf("%s  export GLVALET_HOST=%s\n\n",
					colorDim("Persistent:"), defaultHost)
			}
			return nil
		},
	}
}

// printGitHubHints prints host-switching guidance for the GitHub provider.
func printGitHubHints() {
	fmt.Println(colorDim("\nTo keep using GitHub:"))
	fmt.Println("  export GLVALET_PROVIDER=github")
	fmt.Println("  export GLVALET_GITHUB_TOKEN=<token>")
	fmt.Println(colorDim("\nTo switch to a GitLab host:"))
	fmt.Println("  unset GLVALET_PROVIDER   # or set GLVALET_PROVIDER=gitlab")
	fmt.Printf("  %s\n\n", colorDim("export GLVALET_HOST=<hostname>"))
}

// loadHostsForDisplay calls config internals without requiring a valid token.
// It loads the glab file and returns the raw host map + default host string +
// active provider.
func loadHostsForDisplay() (map[string]*config.HostConfig, string, string, error) {
	provider := strings.TrimSpace(strings.ToLower(os.Getenv("GLVALET_PROVIDER")))
	// Use Load with no host flag; if it fails due to an empty token, we still
	// want to show the host list. Peek at the map directly.
	c, err := config.Load(config.Options{HostFlag: hostFlag})
	if err != nil {
		// Surface the error with a provider-aware hint.
		switch {
		case strings.Contains(err.Error(), "no GitHub token"):
			return nil, "", provider, fmt.Errorf("%w\n  hint: set GLVALET_GITHUB_TOKEN (or GLVALET_TOKEN)", err)
		case strings.Contains(err.Error(), "no token"):
			return nil, "", provider, fmt.Errorf("%w\n  hint: set token with `glab auth login` or GLVALET_TOKEN", err)
		}
		return nil, "", provider, err
	}
	if c.Provider != "" {
		provider = c.Provider
	}
	return c.Hosts, c.Host, provider, nil
}

// ─── Shared output helpers ────────────────────────────────────────────────────

var (
	colorOK   = color.New(color.FgGreen, color.Bold).SprintFunc()
	colorErr  = color.New(color.FgRed, color.Bold).SprintFunc()
	colorDim  = color.New(color.FgHiBlack).SprintFunc()
	colorInfo = color.New(color.FgCyan).SprintFunc()
)

func ok(format string, a ...any)   { fmt.Printf(colorOK("✓ ")+format+"\n", a...) }
func fail(format string, a ...any) { fmt.Fprintf(os.Stderr, colorErr("✗ ")+format+"\n", a...) }
func info(format string, a ...any) { fmt.Printf(colorInfo("→ ")+format+"\n", a...) }
