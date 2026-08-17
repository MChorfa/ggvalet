package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/MChorfa/ggvalet/internal/providerfactory"
	"github.com/MChorfa/ggvalet/internal/rotation"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check ggvalet configuration, local state, and API reachability",
		Long: `Diagnose the ggvalet environment.

Checks performed:
  1. Configuration loads (glab/tea config + env + flags).
  2. State database (~/.ggvalet/state.db) is writable.
  3. Journal file (~/.ggvalet/journal.jsonl) is writable.
  4. Provider builds (gitlab, github, or gitea).
  5. One lightweight API call confirms the token is accepted by the host.
  6. Every other configured GitLab host is probed the same way — a dead
     second host does not stay invisible behind a healthy default.
  7. If ~/.ggvalet/rotation.yaml declares a host's PAT, its token health is
     checked remotely: active/revoked, near expiry, and the self_rotate
     scope rotation needs. "ggvalet rotate --check" deliberately never
     contacts a host; this is where that gap is covered.
  8. Escrow directory (~/.ggvalet/escrow) is scanned for an uncommitted or
     corrupt escrow file — the sign a rotation died after the remote call.

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
	header := color.CyanString("── ggvalet doctor ──")
	fmt.Fprintln(os.Stderr, header)

	// 1. Configuration
	cfg, err := config.Load(config.Options{HostFlag: hostFlag})
	if err != nil {
		fail("config: %v", err)
		switch providerHint() {
		case "github":
			info("hint: set GLVALET_GITHUB_TOKEN (or GLVALET_TOKEN) and GLVALET_GITHUB_ENABLED=true")
		case "gitea":
			info("hint: run `tea login add` or set GLVALET_TOKEN + GLVALET_GITEA_URL")
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

	// 6. Every other configured host. --host narrows to one host, so there is
	// nothing left to check beyond what step 5 already did.
	if hostFlag == "" && !doctorProbeOtherHosts(cfg) {
		okChecks = false
	}

	// 7. Remote token health for hosts rotation.yaml declares a PAT for.
	// rotate --check deliberately stays local and never contacts a host
	// (that's what keeps --check safe to run anywhere); doctor is where an
	// operator learns a token is near expiry or has lost the self_rotate
	// scope rotation depends on.
	if !doctorTokenHealth() {
		okChecks = false
	}

	// 8. Escrow — local only, but the single most important thing doctor can
	// surface: an uncommitted escrow means GitLab already revoked the old
	// token and the replacement is sitting on disk, unrecovered.
	if !doctorEscrowHealth() {
		okChecks = false
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

// ─── every configured host ──────────────────────────────────────────────────

// doctorHostLines probes each configured host and returns one report line per
// host. A failing non-default host is reported, never skipped — the previous
// single-host probe is why a dead second host stayed invisible.
func doctorHostLines(hosts map[string]*config.HostConfig, probe func(host string) error) []string {
	names := make([]string, 0, len(hosts))
	for h := range hosts {
		names = append(names, h)
	}
	sort.Strings(names)

	lines := make([]string, 0, len(names))
	for _, h := range names {
		if err := probe(h); err != nil {
			lines = append(lines, fmt.Sprintf("✗ %s: %v", h, err))
			continue
		}
		lines = append(lines, fmt.Sprintf("✓ %s: reachable", h))
	}
	return lines
}

// doctorProbeOtherHosts probes every configured host besides cfg.Host — the
// one step 5 already covered — and prints a line per host. Returns false if
// any of them failed.
func doctorProbeOtherHosts(cfg *config.Config) bool {
	others := make(map[string]*config.HostConfig, len(cfg.Hosts))
	for host, hc := range cfg.Hosts {
		if host == cfg.Host {
			continue
		}
		others[host] = hc
	}
	if len(others) == 0 {
		return true
	}

	okAll := true
	probe := func(host string) error {
		hostCfg, err := config.ForHost(cfg.Hosts, host, cfg.JournalPath)
		if err != nil {
			return err
		}
		p, err := providerfactory.NewFromConfig(hostCfg)
		if err != nil {
			return redactSecrets(err, hostCfg.Token)
		}
		if err := probeProvider(context.Background(), p, host); err != nil {
			return redactSecrets(err, hostCfg.Token)
		}
		return nil
	}
	for _, line := range doctorHostLines(others, probe) {
		fmt.Println(line)
		if strings.HasPrefix(line, "✗") {
			okAll = false
		}
	}
	return okAll
}

// ─── rotation token health ──────────────────────────────────────────────────

// tokenHealthWarnWithin is how far ahead of expiry doctor starts warning.
// Derived, not hand-tuned: a token with less life left than one full rotation
// cycle (DefaultPATExpiryDays - DefaultCadenceDays) means at least one
// scheduled rotation has already been missed — which is exactly the failure
// doctor exists to catch, since a token that lost self_rotate fails the
// scheduled rotate silently and rotate --check never contacts a host to
// notice. Expressed as a derivation so it self-corrects if either constant
// changes, rather than as an independent magic duration.
//
// This does mean doctor can warn briefly around the moment a rotation is
// legitimately due but simply hasn't run yet. That is the safe direction to
// err in: a false warning costs a glance, a missed one costs a credential.
const tokenHealthWarnWithin = time.Duration(rotation.DefaultPATExpiryDays-rotation.DefaultCadenceDays) * 24 * time.Hour

// doctorGetSelfFunc matches rotation.GetSelfToken's signature so a test can
// inject a fake and never open a socket.
type doctorGetSelfFunc func(ctx context.Context, baseURL, token string, skipTLS bool) (*rotation.TokenInfo, error)

// patHostNames returns the hosts rc declares a PAT for, sorted so a run reads
// the same way twice. only restricts the result to a single host, mirroring
// `rotate --host`; "" means every declared host. An operator who scopes a
// rotation to one host and then asks doctor about it should get an answer
// about that host, not about all of them.
func patHostNames(rc *rotation.Config, only string) []string {
	var out []string
	for host := range rc.Hosts {
		if rc.Uses(host, "pat") && (only == "" || only == host) {
			out = append(out, host)
		}
	}
	sort.Strings(out)
	return out
}

// doctorTokenLines probes remote token health for every host in patHosts.
// A host missing from hosts, or with no token configured, is reported rather
// than skipped — rotation.yaml and the glab config are edited independently
// and drift.
func doctorTokenLines(ctx context.Context, hosts map[string]*config.HostConfig, patHosts []string, now time.Time, getSelf doctorGetSelfFunc) []string {
	lines := make([]string, 0, len(patHosts))
	for _, host := range patHosts {
		hc, ok := hosts[host]
		if !ok || hc == nil || strings.TrimSpace(hc.Token) == "" {
			lines = append(lines, fmt.Sprintf("✗ %s: rotation.yaml declares pat but the glab config has no token for this host", host))
			continue
		}
		info, err := getSelf(ctx, hc.APIURL(host), hc.Token, hc.SkipTLS())
		if err != nil {
			lines = append(lines, fmt.Sprintf("✗ %s: token check failed: %v", host, redactSecrets(err, hc.Token)))
			continue
		}
		lines = append(lines, doctorTokenLine(host, info, now))
	}
	return lines
}

// doctorTokenLine renders one host's token health: active/revoked, near
// expiry, and the self_rotate scope rotation needs. It carries token
// identity — the id — and never the value; GetSelfToken's response never
// carries one (only rotate's does).
func doctorTokenLine(host string, info *rotation.TokenInfo, now time.Time) string {
	var problems []string
	if info.Revoked {
		problems = append(problems, "revoked")
	} else if !info.Active {
		problems = append(problems, "inactive")
	}
	if !info.HasScope("self_rotate") {
		problems = append(problems, "missing self_rotate scope")
	}
	if exp, err := time.Parse("2006-01-02", info.ExpiresAt); err == nil && !exp.After(now.Add(tokenHealthWarnWithin)) {
		problems = append(problems, fmt.Sprintf("expires %s", exp.Format("2006-01-02")))
	}
	if len(problems) > 0 {
		return fmt.Sprintf("✗ %s: token %s (id %d)", host, strings.Join(problems, ", "), info.ID)
	}
	return fmt.Sprintf("✓ %s: token healthy (id %d, expires %s)", host, info.ID, info.ExpiresAt)
}

// doctorTokenHealth checks remote token health for every host rotation.yaml
// declares a PAT for. It is a no-op — not a failure — when rotation has not
// been configured yet, since a fresh install has nothing to check.
func doctorTokenHealth() bool {
	rc, err := rotation.Load(rotationConfigPath())
	if os.IsNotExist(err) {
		info("rotation: not configured (%s not found); skipping token health checks", rotationConfigPath())
		return true
	}
	if err != nil {
		fail("rotation config: %v", err)
		return false
	}

	patHosts := patHostNames(rc, hostFlag)
	if len(patHosts) == 0 {
		info("rotation: no host declares a PAT; skipping token health checks")
		return true
	}

	hosts, _, _, err := loadHostsForDisplay()
	if err != nil {
		fail("rotation: %v", err)
		return false
	}

	okAll := true
	for _, line := range doctorTokenLines(context.Background(), hosts, patHosts, time.Now(), rotation.GetSelfToken) {
		fmt.Println(line)
		if strings.HasPrefix(line, "✗") {
			okAll = false
		}
	}
	return okAll
}

// ─── escrow ──────────────────────────────────────────────────────────────────

// doctorEscrowLines reports every uncommitted or corrupt escrow file in dir.
// An uncommitted escrow means GitLab already revoked the old token and the
// replacement is sitting on disk — the single most urgent thing doctor can
// surface. Corrupt files are named here; ListEscrows's other caller only
// warns about them mid-rotation (Deps.Warn), which is easy to miss.
func doctorEscrowLines(dir string) ([]string, error) {
	escrows, skipped, err := rotation.ListEscrows(dir)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(escrows)+len(skipped))
	for _, e := range escrows {
		lines = append(lines, fmt.Sprintf("✗ %s: uncommitted escrow (new token id %d) — run `ggvalet rotate --recover`", e.Host, e.NewTokenID))
	}
	for _, name := range skipped {
		lines = append(lines, fmt.Sprintf("✗ escrow file %s could not be parsed; inspect it by hand", name))
	}
	return lines, nil
}

// doctorEscrowHealth is doctorEscrowLines against the real escrow directory,
// printed and reduced to a pass/fail. It never mutates anything.
func doctorEscrowHealth() bool {
	lines, err := doctorEscrowLines(escrowDir())
	if err != nil {
		fail("escrow: %v", err)
		return false
	}
	if len(lines) == 0 {
		ok("escrow: none pending")
		return true
	}
	for _, line := range lines {
		fmt.Println(line)
	}
	return false
}
