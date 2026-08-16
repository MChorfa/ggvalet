package rotation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Phase names the step a rotation reached. They are ordered: a Result carrying
// a phase past PhaseRotate means the remote token has already been replaced.
const (
	PhasePreflight = "preflight"
	PhaseSkipped   = "skipped"
	PhaseRotate    = "rotate"
	PhaseEscrow    = "escrow"
	PhaseVerify    = "verify"
	PhaseCommit    = "commit"
	PhaseJournal   = "journal"
	PhaseStore     = "store"
	PhaseDone      = "done"
)

// probePlaceholder is written into a throwaway copy of config.yml during
// preflight to prove the host's token line exists and can be rewritten. It
// never touches the real config.
const probePlaceholder = "ggvalet-preflight-probe"

// Deps are the side effects the state machine needs, injected as function
// fields so that every phase can be failed in a test without a network.
type Deps struct {
	Now         func() time.Time
	GetSelf     func(ctx context.Context, host string) (*TokenInfo, error)
	Rotate      func(ctx context.Context, host, expiresAt string) (*TokenInfo, string, error)
	VerifyToken func(ctx context.Context, host, token string) error
	SetToken    func(path, host, token string) error
	Digest      func(path string) (string, error)
	Journal     func(host string, tokenID int, ok bool) error
	Store       func(host string, at time.Time) error

	// LastRotated reports when host was last rotated. Optional: a nil
	// LastRotated makes every host due.
	LastRotated func(host string) (time.Time, bool, error)

	// Warn surfaces non-fatal conditions (a corrupt escrow file, say) to the
	// operator. Optional; nil discards them.
	Warn func(format string, args ...any)
}

// Options configures a single rotation run.
type Options struct {
	Force       bool
	EscrowDir   string
	ConfigPath  string
	ExpiryDays  int
	CadenceDays int
}

// Result reports how far a rotation got. Rotated is the field that matters
// operationally: once it is true, the old token is gone from GitLab.
type Result struct {
	Host       string
	Phase      string
	Rotated    bool
	NewTokenID int
}

// String renders a Result for an operator. It carries token identity, never a
// token value.
func (r *Result) String() string {
	if !r.Rotated {
		return fmt.Sprintf("%s: %s", r.Host, r.Phase)
	}
	return fmt.Sprintf("%s: %s (new token id %d)", r.Host, r.Phase, r.NewTokenID)
}

// Rotator drives the rotation state machine.
type Rotator struct{ Deps Deps }

func (r *Rotator) warn(format string, args ...any) {
	if r.Deps.Warn != nil {
		r.Deps.Warn(format, args...)
	}
}

// Rotate runs the phases for one host.
//
// The ordering is the entire safety argument. GitLab's self/rotate endpoint
// revokes the old token and hands back the replacement exactly once, so the
// secret is written to escrow — durably, fsynced — before anything else is
// attempted. Every phase after that is recoverable from the escrow file;
// everything before it mutates nothing.
func (r *Rotator) Rotate(ctx context.Context, host string, opt Options) (*Result, error) {
	res := &Result{Host: host, Phase: PhasePreflight}

	// ── Phase 0: preflight. Mutates nothing. ──────────────────────────────
	existing, skipped, err := ListEscrows(opt.EscrowDir)
	if err != nil {
		return res, fmt.Errorf("preflight %s: reading escrow dir: %w", host, err)
	}
	for _, name := range skipped {
		r.warn("escrow file %s could not be parsed and was skipped", name)
	}
	for _, e := range existing {
		if e.Host == host {
			return res, fmt.Errorf("uncommitted escrow for %s at %s; run `ggvalet rotate --recover` first", host, e.Path)
		}
	}

	due, err := r.isDue(host, opt)
	if err != nil {
		return res, fmt.Errorf("preflight %s: %w", host, err)
	}
	if !due {
		res.Phase = PhaseSkipped
		return res, nil
	}

	info, err := r.Deps.GetSelf(ctx, host)
	if err != nil {
		return res, fmt.Errorf("preflight %s: %w", host, err)
	}
	if !info.Active || info.Revoked {
		return res, fmt.Errorf("preflight %s: token %d is inactive or revoked", host, info.ID)
	}
	if !info.HasScope("self_rotate") {
		return res, fmt.Errorf("preflight %s: token %d lacks the self_rotate scope", host, info.ID)
	}
	// Prove the config can actually receive the new token before making one.
	// A failure here costs nothing; the same failure after the rotate call
	// costs the credential.
	if err := probeConfigWritable(opt.ConfigPath, host); err != nil {
		return res, fmt.Errorf("preflight %s: %w", host, err)
	}
	digest, err := r.Deps.Digest(opt.ConfigPath)
	if err != nil {
		return res, fmt.Errorf("preflight %s: %w", host, err)
	}

	// ── Phase 1: rotate, then escrow. Nothing happens in between. ─────────
	expiryDays := opt.ExpiryDays
	if expiryDays <= 0 {
		expiryDays = DefaultPATExpiryDays
	}
	now := r.Deps.Now()
	expiresAt := now.AddDate(0, 0, expiryDays).Format("2006-01-02")

	res.Phase = PhaseRotate
	newInfo, secret, err := r.Deps.Rotate(ctx, host, expiresAt)
	if err != nil {
		return res, fmt.Errorf("rotate %s: %w", host, err)
	}
	res.NewTokenID = newInfo.ID
	res.Rotated = true

	res.Phase = PhaseEscrow
	esc := &Escrow{
		Version:    1,
		Host:       host,
		State:      StateRotated,
		RotatedAt:  now.UTC().Format(time.RFC3339),
		OldTokenID: info.ID,
		NewTokenID: newInfo.ID,
		NewToken:   secret,
		ExpiresAt:  newInfo.ExpiresAt,
		Scopes:     newInfo.Scopes,
		ConfigPath: opt.ConfigPath,
		// The digest of config.yml as it stood at preflight, so the commit
		// phase can tell an unmodified config from one edited underneath us.
		ConfigSHA256: digest,
	}
	if _, err := WriteEscrow(opt.EscrowDir, esc); err != nil {
		// The old token is revoked and the replacement may exist only in
		// memory. "May": several of WriteEscrow's failure returns happen after
		// the file is created and fsynced, with only the directory entry
		// unconfirmed, and in those the secret is sitting on disk. Telling an
		// operator it is lost would talk them out of the recovery that would
		// have worked, so point them at the directory first. No secret in the
		// message — this is the line most likely to reach a scrollback.
		return res, fmt.Errorf(
			"CRITICAL: rotated %s (new token id %d) but could not confirm the escrow write. "+
				"The replacement may still be on disk: look for %s/%s-*.json and run "+
				"`ggvalet rotate --recover` before treating this host as lost: %w",
			host, newInfo.ID, opt.EscrowDir, host, err)
	}

	return r.finish(ctx, esc, res)
}

// finish runs verify → commit → record against an escrowed secret. Rotate and
// RecoverIn share it, which is what makes recovery a replay rather than a
// second code path.
func (r *Rotator) finish(ctx context.Context, esc *Escrow, res *Result) (*Result, error) {
	res.Phase = PhaseVerify
	if err := r.Deps.VerifyToken(ctx, esc.Host, esc.NewToken); err != nil {
		return res, fmt.Errorf("verify %s: %w", esc.Host, err)
	}

	res.Phase = PhaseCommit
	committed, err := configHasToken(esc.ConfigPath, esc.Host, esc.NewToken)
	if err != nil {
		return res, fmt.Errorf("commit %s: %w", esc.Host, err)
	}
	if !committed {
		current, err := r.Deps.Digest(esc.ConfigPath)
		if err != nil {
			return res, fmt.Errorf("commit %s: %w", esc.Host, err)
		}
		if esc.ConfigSHA256 != "" && current != esc.ConfigSHA256 {
			return res, fmt.Errorf(
				"commit %s: config has changed since the rotation started; refusing to overwrite it. "+
					"The new token is held at %s", esc.Host, esc.Path)
		}
		if err := r.Deps.SetToken(esc.ConfigPath, esc.Host, esc.NewToken); err != nil {
			return res, fmt.Errorf("commit %s: %w", esc.Host, err)
		}
	}

	// ── Phase 3: record. Only now is the escrow retired. ──────────────────
	res.Phase = PhaseJournal
	if err := r.Deps.Journal(esc.Host, esc.NewTokenID, true); err != nil {
		return res, fmt.Errorf("journal %s: %w", esc.Host, err)
	}
	res.Phase = PhaseStore
	if err := r.Deps.Store(esc.Host, r.Deps.Now()); err != nil {
		return res, fmt.Errorf("state %s: %w", esc.Host, err)
	}
	if esc.Path != "" {
		if err := DeleteEscrow(esc.Path); err != nil {
			return res, fmt.Errorf("escrow cleanup %s: %w", esc.Host, err)
		}
	}
	res.Phase = PhaseDone
	return res, nil
}

// RecoverIn replays every uncommitted escrow found in dir. One escrow failing
// does not stop the others; the failures keep their escrow files.
func (r *Rotator) RecoverIn(ctx context.Context, dir, configPath string) ([]*Result, error) {
	escrows, skipped, err := ListEscrows(dir)
	if err != nil {
		return nil, err
	}
	for _, name := range skipped {
		r.warn("escrow file %s could not be parsed and was left in place; inspect it by hand", name)
	}

	var out []*Result
	var firstErr error
	for _, e := range escrows {
		if e.ConfigPath == "" {
			e.ConfigPath = configPath
		}
		res, err := r.finish(ctx, e, &Result{Host: e.Host, Rotated: true, NewTokenID: e.NewTokenID})
		if err != nil {
			r.warn("recovery of %s failed at %s: %v", e.Host, res.Phase, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out = append(out, res)
	}
	return out, firstErr
}

// isDue reports whether host should be rotated now. Force and a nil
// LastRotated both mean yes.
func (r *Rotator) isDue(host string, opt Options) (bool, error) {
	if opt.Force || r.Deps.LastRotated == nil {
		return true, nil
	}
	last, ok, err := r.Deps.LastRotated(host)
	if err != nil {
		return false, err
	}
	if !ok {
		return true, nil // never rotated
	}
	cadence := opt.CadenceDays
	if cadence <= 0 {
		cadence = DefaultCadenceDays
	}
	return !r.Deps.Now().Before(last.AddDate(0, 0, cadence)), nil
}

// configHasToken reports whether host's own `token:` line in path already
// carries token. It is how recovery stays idempotent after a commit that
// succeeded before a later phase failed.
//
// The check is scoped to the host's block on purpose. A whole-file match would
// return true for the same value sitting under a different host, which would
// skip the commit, delete the escrow, and destroy the only copy of the secret.
func configHasToken(path, host, token string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	idx, _, value := findHostToken(strings.Split(string(b), "\n"), host)
	return idx >= 0 && value == token, nil
}

// probeConfigWritable proves, without touching the real config, that the token
// line for host exists and that the commit phase would succeed: it copies the
// config to a sibling temp file and runs the real writer against the copy.
// Using the real writer is deliberate — a reimplementation here would be free
// to drift from the one that actually commits.
func probeConfigWritable(path, host string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ggvalet-preflight-*")
	if err != nil {
		return fmt.Errorf("config directory %s is not writable: %w", dir, err)
	}
	probe := tmp.Name()
	defer os.Remove(probe)

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := SetHostToken(probe, host, probePlaceholder); err != nil {
		return fmt.Errorf("config cannot receive a new token: %w", err)
	}
	return nil
}
