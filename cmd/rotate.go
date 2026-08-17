package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/MChorfa/ggvalet/internal/rotation"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/spf13/cobra"
)

func rotateCmd() *cobra.Command {
	var check, force, doRecover bool
	c := &cobra.Command{
		Use:   "rotate",
		Short: "Rotate personal access tokens on configured hosts",
		Long: `Rotate personal access tokens on the hosts listed in ~/.ggvalet/rotation.yaml.

Rotation uses GitLab's personal_access_tokens/self/rotate endpoint, which
revokes the old token and returns its replacement exactly once. The replacement
is written to a 0600 escrow file and fsynced before anything else is attempted,
so an interrupted run is recoverable:

  ggvalet rotate --recover

An escrow file on disk always means "rotated but not yet fully committed". The
command refuses to rotate a host that still has one.

Note on the audit trail: recovery replays the commit, journal and state writes,
and those writes are not idempotent. A run killed between the journal write and
the escrow delete will, on --recover, append a second rotate entry for the same
token. Duplicate entries in the journal are expected after a recovery; the
token id tells you they are the same rotation.`,
		Example: `  ggvalet rotate --check      # report what is due; exit 1 if anything is
  ggvalet rotate              # rotate every host that is due
  ggvalet rotate --force      # ignore the cadence
  ggvalet rotate --recover    # commit a secret left in escrow`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRotate(cmd.Context(), rotateOptions{check: check, force: force, doRecover: doRecover})
		},
	}
	c.Flags().BoolVar(&check, "check", false, "report what would happen; exit 1 if anything is due or broken")
	c.Flags().BoolVar(&force, "force", false, "ignore cadence")
	c.Flags().BoolVar(&doRecover, "recover", false, "replay an uncommitted escrow")
	return c
}

// ─── paths ───────────────────────────────────────────────────────────────────

func ggvaletHome() string {
	if v := os.Getenv("GLVALET_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ggvalet")
}

func rotationConfigPath() string { return filepath.Join(ggvaletHome(), "rotation.yaml") }
func escrowDir() string          { return filepath.Join(ggvaletHome(), "escrow") }

// glabConfigPath is the file the new token is written into — resolved by the
// same search order config.Load used to read it.
func glabConfigPath() string { return config.GlabConfigPath() }

// ─── run ─────────────────────────────────────────────────────────────────────

type rotateOptions struct{ check, force, doRecover bool }

// rotateRunner holds one run's collaborators. Output goes through explicit
// writers so the operator-facing text is testable without a terminal.
type rotateRunner struct {
	rot         *rotation.Rotator
	rc          *rotation.Config
	jrnl        journalRecorder
	escrowDir   string
	configPath  string
	now         func() time.Time
	lastRotated func(host string) (time.Time, bool, error)
	out         io.Writer
	errOut      io.Writer
}

func runRotate(ctx context.Context, o rotateOptions) error {
	hosts, _, _, err := loadHostsForDisplay()
	if err != nil {
		return err
	}

	rcPath := rotationConfigPath()
	rc, err := rotation.Load(rcPath)
	if os.IsNotExist(err) {
		// A rotation profile is a statement about which credentials a host
		// uses; guessing it and acting on the guess would rotate tokens the
		// operator never listed. Propose, then stop.
		if err := rotation.Save(rcPath, rotation.Propose(hosts)); err != nil {
			return err
		}
		return fmt.Errorf("wrote a proposed rotation profile to %s — review it, then re-run", rcPath)
	}
	if err != nil {
		return err
	}

	statePath, journalPath, err := rotatePaths()
	if err != nil {
		return err
	}
	st, err := state.Open(statePath)
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}
	defer st.Close()
	jrnl, err := journal.Open(journalPath)
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}

	w := &rotateWiring{
		api:   liveRotateAPI(),
		hosts: hosts,
		store: st,
		jrnl:  jrnl,
		warn:  func(format string, args ...any) { fmt.Fprintf(os.Stderr, "! "+format+"\n", args...) },
	}
	rr := &rotateRunner{
		rot:        &rotation.Rotator{Deps: w.deps(ctx)},
		rc:         rc,
		jrnl:       jrnl,
		escrowDir:  escrowDir(),
		configPath: glabConfigPath(),
		now:        time.Now,
		lastRotated: func(host string) (time.Time, bool, error) {
			return st.LastRotated(ctx, host)
		},
		out:    os.Stdout,
		errOut: os.Stderr,
	}

	switch {
	case o.doRecover:
		return rr.recoverAll(ctx)
	case o.check:
		return rr.checkHosts(o.force)
	default:
		return rr.rotateHosts(ctx, o.force)
	}
}

// rotatePaths resolves the state and journal locations, preferring the config
// the root command already loaded.
func rotatePaths() (statePath, journalPath string, err error) {
	if cfg != nil {
		return cfg.StatePath, cfg.JournalPath, nil
	}
	c, err := config.Load(config.Options{HostFlag: hostFlag})
	if err != nil {
		return "", "", err
	}
	return c.StatePath, c.JournalPath, nil
}

// patHosts returns the hosts declaring a PAT, sorted so a multi-host run reads
// the same way twice.
func (rr *rotateRunner) patHosts() []string {
	var out []string
	for host := range rr.rc.Hosts {
		if rr.rc.Uses(host, "pat") {
			out = append(out, host)
		}
	}
	sort.Strings(out)
	return out
}

func (rr *rotateRunner) rotateHosts(ctx context.Context, force bool) error {
	var problems int
	var criticals []*rotation.Result

	for _, host := range rr.patHosts() {
		res, err := rr.rot.Rotate(ctx, host, rotation.Options{
			Force:       force,
			EscrowDir:   rr.escrowDir,
			ConfigPath:  rr.configPath,
			ExpiryDays:  rr.rc.Defaults.PATExpiryDays,
			CadenceDays: rr.rc.Defaults.CadenceDays,
		})
		if err == nil {
			if res.Phase == rotation.PhaseSkipped {
				fmt.Fprintf(rr.out, "%s\n", colorDim("· "+host+" — not due"))
				continue
			}
			fmt.Fprintf(rr.out, "%s %s rotated (new token id %d)\n", colorOK("✓"), host, res.NewTokenID)
			continue
		}

		problems++
		if !res.Rotated {
			// Nothing was mutated remotely, so there is nothing to audit.
			fmt.Fprintf(rr.errOut, "%s %s: %v\n", colorErr("✗"), host, err)
			continue
		}
		criticals = append(criticals, res)
		rr.journalFailure(res)
		rr.printCritical(res, err)
	}

	// Reprint the half-finished rotations last: they are the one outcome that
	// must not scroll past inside a multi-host run.
	if len(criticals) > 0 {
		fmt.Fprintf(rr.errOut, "\n%s %d host(s) were rotated but not committed:\n",
			colorErr("CRITICAL"), len(criticals))
		for _, res := range criticals {
			fmt.Fprintf(rr.errOut, "    %s (new token id %d, stopped at %s)\n", res.Host, res.NewTokenID, res.Phase)
		}
		fmt.Fprintf(rr.errOut, "  run `ggvalet rotate --recover` before using these hosts again\n")
	}
	if problems > 0 {
		return fmt.Errorf("%d host(s) need attention", problems)
	}
	return nil
}

// printCritical reports the outcome that costs a credential if it is ignored:
// the old token is already revoked at the host and the replacement is not in
// the config.
//
// res.Rotated is set before the escrow write, so PhaseEscrow arrives here too —
// and for that phase the escrow may or may not exist, which is exactly why the
// state machine hedges its own message. Asserting the escrow there would talk
// an operator out of the directory listing that resolves it.
func (rr *rotateRunner) printCritical(res *rotation.Result, err error) {
	fmt.Fprintf(rr.errOut, "%s %s was rotated but not committed (stopped at %s)\n",
		colorErr("✗ CRITICAL:"), res.Host, res.Phase)
	if res.Phase == rotation.PhaseEscrow {
		fmt.Fprintf(rr.errOut, "    the old token is already revoked; the replacement (token id %d) may be in escrow\n", res.NewTokenID)
		fmt.Fprintf(rr.errOut, "    look for %s/%s-*.json before treating this host as lost\n", rr.escrowDir, res.Host)
	} else {
		fmt.Fprintf(rr.errOut, "    the old token is already revoked; the replacement (token id %d) is in escrow\n", res.NewTokenID)
		fmt.Fprintf(rr.errOut, "    escrow: %s\n", rr.escrowDir)
	}
	fmt.Fprintf(rr.errOut, "    run: ggvalet rotate --recover\n")
	fmt.Fprintf(rr.errOut, "    cause: %v\n", err)
}

// journalFailure records a rotation that died after the remote call.
//
// This belongs at the boundary, not in the state machine: the machine cannot
// journal its own failure without recursing when the failing phase is the
// journal write. The entry carries the phase and the new token's id — never the
// error text, which is assembled from wrapped dependencies, and never a value.
func (rr *rotateRunner) journalFailure(res *rotation.Result) {
	if rr.jrnl == nil {
		return
	}
	err := rr.jrnl.Record(journal.Entry{
		Host:     res.Host,
		Op:       journal.OpRotate,
		Entity:   journal.EntityToken,
		EntityID: res.NewTokenID,
		Outcome:  journal.OutcomeErr,
		Detail:   fmt.Sprintf("rotation failed at phase %s; secret held in escrow", res.Phase),
	})
	if err != nil {
		fmt.Fprintf(rr.errOut, "    (the failure could not be journalled: %v)\n", err)
	}
}

func (rr *rotateRunner) checkHosts(force bool) error {
	var problems int

	escrows, skipped, err := rotation.ListEscrows(rr.escrowDir)
	if err != nil {
		return fmt.Errorf("reading escrow dir: %w", err)
	}
	for _, e := range escrows {
		fmt.Fprintf(rr.out, "%s %-34s uncommitted escrow (new token id %d) — run `ggvalet rotate --recover`\n",
			colorErr("✗"), e.Host, e.NewTokenID)
		problems++
	}
	for _, name := range skipped {
		fmt.Fprintf(rr.errOut, "%s escrow file %s could not be parsed; inspect it by hand\n", colorErr("✗"), name)
		problems++
	}

	for _, host := range rr.patHosts() {
		due, reason := rr.due(host, force)
		mark := colorDim("·")
		if due {
			mark = colorInfo("→")
			problems++
		}
		fmt.Fprintf(rr.out, "%s %-34s due=%-5v %s\n", mark, host, due, reason)
	}

	if problems > 0 {
		return fmt.Errorf("%d host(s) need attention", problems)
	}
	return nil
}

// due mirrors the state machine's cadence rule for reporting only. The state
// machine re-decides at rotation time; this never gates a rotation, so the two
// disagreeing costs a misleading line, not a wrong action.
func (rr *rotateRunner) due(host string, force bool) (bool, string) {
	if force {
		return true, "forced"
	}
	if rr.lastRotated == nil {
		return true, "no rotation state"
	}
	last, ok, err := rr.lastRotated(host)
	if err != nil {
		// An unreadable state DB is not "not due" — say so and count it.
		return true, fmt.Sprintf("rotation state unreadable: %v", err)
	}
	if !ok {
		return true, "never rotated"
	}
	cadence := rr.rc.Defaults.CadenceDays
	if cadence <= 0 {
		cadence = rotation.DefaultCadenceDays
	}
	next := last.AddDate(0, 0, cadence)
	if rr.now().Before(next) {
		return false, fmt.Sprintf("next due %s", next.Format("2006-01-02"))
	}
	return true, fmt.Sprintf("last rotated %s, cadence %dd", last.Format("2006-01-02"), cadence)
}

func (rr *rotateRunner) recoverAll(ctx context.Context) error {
	results, err := rr.rot.RecoverIn(ctx, rr.escrowDir, rr.configPath)
	for _, res := range results {
		fmt.Fprintf(rr.out, "%s %s recovered (new token id %d)\n", colorOK("✓"), res.Host, res.NewTokenID)
	}
	if err != nil {
		return fmt.Errorf("recovery incomplete; the failed hosts keep their escrow files: %w", err)
	}
	if len(results) == 0 {
		fmt.Fprintf(rr.out, "%s\n", colorDim("nothing to recover"))
	}
	return nil
}
