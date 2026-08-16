package rotation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newSecret is the value the fake Rotate hands back. It is the thing the whole
// state machine exists to not lose, so tests assert on it by identity.
const newSecret = "rotated-fake-secret-0001"

const testHost = "gitlab.example.com"

type callLog struct {
	rotateCalls int
	setCalls    int
	journalOK   []bool
	stored      []time.Time
	warnings    []string
}

// newTestRotator builds a Rotator whose every phase can be made to fail on
// demand, with no network and no real GitLab. failAt is one of
// "preflight", "rotate", "verify", "commit", "journal", "store", or "" for none.
func newTestRotator(t *testing.T, failAt string) (*Rotator, *callLog, string, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(cfgPath, []byte(twoHostFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	escrowDir := filepath.Join(dir, "escrow")

	log := &callLog{}
	fail := func(phase string) error {
		if phase == failAt {
			return errors.New("injected failure at " + phase)
		}
		return nil
	}
	r := &Rotator{Deps: Deps{
		Now: func() time.Time { return time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC) },
		GetSelf: func(ctx context.Context, host string) (*TokenInfo, error) {
			if err := fail("preflight"); err != nil {
				return nil, err
			}
			return &TokenInfo{ID: 51629, Scopes: []string{"api", "self_rotate"}, Active: true}, nil
		},
		Rotate: func(ctx context.Context, host, expiresAt string) (*TokenInfo, string, error) {
			log.rotateCalls++
			if err := fail("rotate"); err != nil {
				return nil, "", err
			}
			return &TokenInfo{ID: 51630, ExpiresAt: expiresAt, Scopes: []string{"api", "self_rotate"}}, newSecret, nil
		},
		VerifyToken: func(ctx context.Context, host, token string) error { return fail("verify") },
		SetToken: func(path, host, token string) error {
			log.setCalls++
			if err := fail("commit"); err != nil {
				return err
			}
			return SetHostToken(path, host, token)
		},
		Digest: FileDigest,
		Journal: func(host string, tokenID int, ok bool) error {
			if err := fail("journal"); err != nil {
				return err
			}
			log.journalOK = append(log.journalOK, ok)
			return nil
		},
		Store: func(host string, at time.Time) error {
			if err := fail("store"); err != nil {
				return err
			}
			log.stored = append(log.stored, at)
			return nil
		},
		Warn: func(format string, args ...any) { log.warnings = append(log.warnings, format) },
	}}
	return r, log, cfgPath, escrowDir
}

func testOptions(escrowDir, cfgPath string) Options {
	return Options{EscrowDir: escrowDir, ConfigPath: cfgPath, ExpiryDays: 65, Force: true}
}

func mustList(t *testing.T, dir string) []*Escrow {
	t.Helper()
	e, skipped, err := ListEscrows(dir)
	if err != nil {
		t.Fatalf("ListEscrows: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("unexpected unparseable escrows: %v", skipped)
	}
	return e
}

// After any failure past the rotate call, the secret must be on disk and a
// healthy recovery run must converge to a committed config.
func TestRotate_RecoversFromEveryPostRotateFailure(t *testing.T) {
	for _, phase := range []string{"verify", "commit", "journal", "store"} {
		t.Run(phase, func(t *testing.T) {
			r, _, cfgPath, escrowDir := newTestRotator(t, phase)

			res, err := r.Rotate(context.Background(), testHost, testOptions(escrowDir, cfgPath))
			if err == nil {
				t.Fatalf("expected failure at %s", phase)
			}
			if res.Phase != phase {
				t.Errorf("Result.Phase = %q, want %q", res.Phase, phase)
			}
			if !res.Rotated {
				t.Error("Result.Rotated must be true once the remote token has been replaced")
			}
			escrows := mustList(t, escrowDir)
			if len(escrows) != 1 {
				t.Fatalf("secret not escrowed: got %d records", len(escrows))
			}
			if escrows[0].NewToken != newSecret {
				t.Fatal("escrow lost the secret")
			}
			if escrows[0].NewTokenID != 51630 || escrows[0].OldTokenID != 51629 {
				t.Errorf("escrow token ids wrong: old=%d new=%d", escrows[0].OldTokenID, escrows[0].NewTokenID)
			}

			// A clean rotator (nothing failing) must finish the job.
			healthy, log, _, _ := newTestRotator(t, "")
			results, err := healthy.RecoverIn(context.Background(), escrowDir, cfgPath)
			if err != nil {
				t.Fatalf("RecoverIn: %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("expected 1 recovery, got %d", len(results))
			}
			if results[0].Phase != PhaseDone {
				t.Errorf("recovery phase = %q, want %q", results[0].Phase, PhaseDone)
			}
			if log.rotateCalls != 0 {
				t.Errorf("recovery must not rotate again, got %d rotate calls", log.rotateCalls)
			}
			b, err := os.ReadFile(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), "token: "+newSecret) {
				t.Errorf("config not committed after recovery:\n%s", b)
			}
			if left := mustList(t, escrowDir); len(left) != 0 {
				t.Errorf("escrow not cleaned up: %d left", len(left))
			}
		})
	}
}

// Preflight failures must not create an escrow, must not call rotate, and must
// not touch config.
func TestRotate_PreflightFailureIsANoOp(t *testing.T) {
	r, log, cfgPath, escrowDir := newTestRotator(t, "preflight")
	before, _ := os.ReadFile(cfgPath)

	res, err := r.Rotate(context.Background(), testHost, testOptions(escrowDir, cfgPath))
	if err == nil {
		t.Fatal("expected preflight failure")
	}
	if res.Rotated {
		t.Error("preflight failure must not report Rotated")
	}
	if log.rotateCalls != 0 {
		t.Errorf("preflight failure must not call rotate, got %d", log.rotateCalls)
	}
	if e := mustList(t, escrowDir); len(e) != 0 {
		t.Errorf("preflight must not escrow, got %d", len(e))
	}
	after, _ := os.ReadFile(cfgPath)
	if string(before) != string(after) {
		t.Error("preflight must not modify config")
	}
}

// A token without self_rotate must be rejected before the irreversible call.
func TestRotate_MissingScopeRejectedBeforeRotate(t *testing.T) {
	r, log, cfgPath, escrowDir := newTestRotator(t, "")
	r.Deps.GetSelf = func(ctx context.Context, host string) (*TokenInfo, error) {
		return &TokenInfo{ID: 1, Scopes: []string{"api"}, Active: true}, nil
	}

	_, err := r.Rotate(context.Background(), testHost, testOptions(escrowDir, cfgPath))
	if err == nil || !strings.Contains(err.Error(), "self_rotate") {
		t.Fatalf("want self_rotate scope error, got %v", err)
	}
	if log.rotateCalls != 0 {
		t.Errorf("must not rotate a token lacking the scope, got %d calls", log.rotateCalls)
	}
}

func TestRotate_InactiveOrRevokedTokenRejected(t *testing.T) {
	for name, info := range map[string]*TokenInfo{
		"inactive": {ID: 1, Scopes: []string{"api", "self_rotate"}, Active: false},
		"revoked":  {ID: 1, Scopes: []string{"api", "self_rotate"}, Active: true, Revoked: true},
	} {
		t.Run(name, func(t *testing.T) {
			r, log, cfgPath, escrowDir := newTestRotator(t, "")
			r.Deps.GetSelf = func(ctx context.Context, host string) (*TokenInfo, error) { return info, nil }
			if _, err := r.Rotate(context.Background(), testHost, testOptions(escrowDir, cfgPath)); err == nil {
				t.Fatal("expected rejection")
			}
			if log.rotateCalls != 0 {
				t.Errorf("must not rotate, got %d calls", log.rotateCalls)
			}
		})
	}
}

// A config whose directory cannot be written must be caught before rotating,
// because SetHostToken commits by writing a sibling temp file and renaming.
func TestRotate_PreflightRejectsUnwritableConfigDir(t *testing.T) {
	r, log, cfgPath, escrowDir := newTestRotator(t, "")
	cfgDir := filepath.Dir(cfgPath)
	if err := os.Chmod(cfgDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cfgDir, 0o700) })

	if _, err := r.Rotate(context.Background(), testHost, testOptions(escrowDir, cfgPath)); err == nil {
		t.Fatal("expected preflight to reject an unwritable config directory")
	}
	if log.rotateCalls != 0 {
		t.Errorf("must not rotate when the config cannot be committed, got %d calls", log.rotateCalls)
	}
}

// A host absent from config.yml has nowhere to receive the new token; catching
// that before rotating avoids stranding a credential in escrow.
func TestRotate_PreflightRejectsHostMissingFromConfig(t *testing.T) {
	r, log, cfgPath, escrowDir := newTestRotator(t, "")
	if _, err := r.Rotate(context.Background(), "absent.example.org", testOptions(escrowDir, cfgPath)); err == nil {
		t.Fatal("expected preflight to reject a host with no token line")
	}
	if log.rotateCalls != 0 {
		t.Errorf("must not rotate, got %d calls", log.rotateCalls)
	}
	// The probe must leave nothing behind.
	entries, err := os.ReadDir(filepath.Dir(cfgPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range entries {
		if en.Name() != "config.yml" && en.Name() != "escrow" {
			t.Errorf("preflight probe left %s behind", en.Name())
		}
	}
}

// An uncommitted escrow means a previous run died mid-rotation. Rotating again
// would revoke the token the operator has not yet recovered.
func TestRotate_ExistingEscrowBlocksSecondRotation(t *testing.T) {
	r, log, cfgPath, escrowDir := newTestRotator(t, "commit")
	opt := testOptions(escrowDir, cfgPath)
	if _, err := r.Rotate(context.Background(), testHost, opt); err == nil {
		t.Fatal("expected the commit failure")
	}
	before := log.rotateCalls

	_, err := r.Rotate(context.Background(), testHost, opt)
	if err == nil || !strings.Contains(err.Error(), "--recover") {
		t.Fatalf("want an error pointing at recovery, got %v", err)
	}
	if log.rotateCalls != before {
		t.Errorf("second attempt must not rotate again: %d → %d", before, log.rotateCalls)
	}
}

func TestRotate_HappyPathCommitsAndCleansUp(t *testing.T) {
	r, log, cfgPath, escrowDir := newTestRotator(t, "")

	res, err := r.Rotate(context.Background(), testHost, testOptions(escrowDir, cfgPath))
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if res.Phase != PhaseDone || !res.Rotated || res.NewTokenID != 51630 {
		t.Errorf("unexpected result %+v", res)
	}
	b, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(b), "token: "+newSecret) {
		t.Errorf("config not committed:\n%s", b)
	}
	// The other host's token must be untouched.
	if !strings.Contains(string(b), "sc01-trt.example.ca") {
		t.Error("unrelated host block lost")
	}
	if left := mustList(t, escrowDir); len(left) != 0 {
		t.Errorf("escrow must be deleted after a verified commit, %d left", len(left))
	}
	if len(log.journalOK) != 1 || !log.journalOK[0] {
		t.Errorf("journal not recorded ok: %v", log.journalOK)
	}
	if len(log.stored) != 1 {
		t.Errorf("last-rotated not stored: %v", log.stored)
	}
}

// The expiry date handed to GitLab must come from Deps.Now plus ExpiryDays, so
// that it is deterministic and testable rather than wall-clock dependent.
func TestRotate_ExpiryDerivedFromNowAndExpiryDays(t *testing.T) {
	r, _, cfgPath, escrowDir := newTestRotator(t, "")
	var got string
	r.Deps.Rotate = func(ctx context.Context, host, expiresAt string) (*TokenInfo, string, error) {
		got = expiresAt
		return &TokenInfo{ID: 51630, ExpiresAt: expiresAt}, newSecret, nil
	}
	if _, err := r.Rotate(context.Background(), testHost, testOptions(escrowDir, cfgPath)); err != nil {
		t.Fatal(err)
	}
	if want := "2026-11-05"; got != want {
		t.Errorf("expires_at = %q, want %q", got, want)
	}
}

func TestRotate_ZeroExpiryDaysUsesDefault(t *testing.T) {
	r, _, cfgPath, escrowDir := newTestRotator(t, "")
	var got string
	r.Deps.Rotate = func(ctx context.Context, host, expiresAt string) (*TokenInfo, string, error) {
		got = expiresAt
		return &TokenInfo{ID: 51630, ExpiresAt: expiresAt}, newSecret, nil
	}
	opt := testOptions(escrowDir, cfgPath)
	opt.ExpiryDays = 0
	if _, err := r.Rotate(context.Background(), testHost, opt); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC).AddDate(0, 0, DefaultPATExpiryDays).Format("2006-01-02")
	if got != want {
		t.Errorf("expires_at = %q, want %q", got, want)
	}
}

// Without Force, a host rotated inside the cadence window is skipped — no
// remote call, no escrow.
func TestRotate_NotDueIsSkippedWithoutForce(t *testing.T) {
	r, log, cfgPath, escrowDir := newTestRotator(t, "")
	r.Deps.LastRotated = func(host string) (time.Time, bool, error) {
		return time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC), true, nil
	}
	opt := testOptions(escrowDir, cfgPath)
	opt.Force = false
	opt.CadenceDays = 30

	res, err := r.Rotate(context.Background(), testHost, opt)
	if err != nil {
		t.Fatalf("a not-due host is not an error: %v", err)
	}
	if res.Phase != PhaseSkipped || res.Rotated {
		t.Errorf("unexpected result %+v", res)
	}
	if log.rotateCalls != 0 {
		t.Errorf("must not rotate when not due, got %d", log.rotateCalls)
	}
}

func TestRotate_DueProceedsWithoutForce(t *testing.T) {
	r, log, cfgPath, escrowDir := newTestRotator(t, "")
	r.Deps.LastRotated = func(host string) (time.Time, bool, error) {
		return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), true, nil
	}
	opt := testOptions(escrowDir, cfgPath)
	opt.Force = false
	opt.CadenceDays = 30

	if _, err := r.Rotate(context.Background(), testHost, opt); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if log.rotateCalls != 1 {
		t.Errorf("expected one rotate, got %d", log.rotateCalls)
	}
}

// A host that has never been rotated is always due.
func TestRotate_NeverRotatedIsDue(t *testing.T) {
	r, log, cfgPath, escrowDir := newTestRotator(t, "")
	r.Deps.LastRotated = func(host string) (time.Time, bool, error) { return time.Time{}, false, nil }
	opt := testOptions(escrowDir, cfgPath)
	opt.Force = false
	opt.CadenceDays = 30

	if _, err := r.Rotate(context.Background(), testHost, opt); err != nil {
		t.Fatal(err)
	}
	if log.rotateCalls != 1 {
		t.Errorf("expected one rotate, got %d", log.rotateCalls)
	}
}

// If the config was edited between preflight and commit, refuse rather than
// clobber an operator's concurrent change. The secret stays in escrow.
func TestRotate_ConfigChangedSincePreflightRefusesToCommit(t *testing.T) {
	r, log, cfgPath, escrowDir := newTestRotator(t, "")
	r.Deps.VerifyToken = func(ctx context.Context, host, token string) error {
		return os.WriteFile(cfgPath, []byte(twoHostFixture+"\n# edited by someone else\n"), 0o600)
	}

	res, err := r.Rotate(context.Background(), testHost, testOptions(escrowDir, cfgPath))
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("want a refusal to merge, got %v", err)
	}
	if res.Phase != PhaseCommit {
		t.Errorf("Phase = %q, want %q", res.Phase, PhaseCommit)
	}
	if log.setCalls != 0 {
		t.Errorf("must not write config after an external edit, got %d writes", log.setCalls)
	}
	if e := mustList(t, escrowDir); len(e) != 1 {
		t.Fatalf("secret must remain escrowed, got %d", len(e))
	}
}

// Recovery is idempotent: a config that already carries the escrowed token is
// left alone, and the escrow is still retired.
func TestRecoverIn_SkipsWriteWhenConfigAlreadyHasToken(t *testing.T) {
	r, log, cfgPath, escrowDir := newTestRotator(t, "journal")
	if _, err := r.Rotate(context.Background(), testHost, testOptions(escrowDir, cfgPath)); err == nil {
		t.Fatal("expected the journal failure")
	}
	if log.setCalls != 1 {
		t.Fatalf("expected the first run to commit the config, got %d writes", log.setCalls)
	}

	healthy, hlog, _, _ := newTestRotator(t, "")
	results, err := healthy.RecoverIn(context.Background(), escrowDir, cfgPath)
	if err != nil {
		t.Fatalf("RecoverIn: %v", err)
	}
	if len(results) != 1 || results[0].Phase != PhaseDone {
		t.Fatalf("unexpected results %+v", results)
	}
	if hlog.setCalls != 0 {
		t.Errorf("config already had the token; want 0 writes, got %d", hlog.setCalls)
	}
	if left := mustList(t, escrowDir); len(left) != 0 {
		t.Errorf("escrow not retired: %d left", len(left))
	}
}

// A corrupt escrow file is never a hard failure — it is named, and the valid
// escrows still recover.
func TestRecoverIn_SurfacesCorruptEscrowsAndContinues(t *testing.T) {
	r, _, cfgPath, escrowDir := newTestRotator(t, "commit")
	if _, err := r.Rotate(context.Background(), testHost, testOptions(escrowDir, cfgPath)); err == nil {
		t.Fatal("expected the commit failure")
	}
	if err := os.WriteFile(filepath.Join(escrowDir, "corrupt.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	healthy, log, _, _ := newTestRotator(t, "")
	results, err := healthy.RecoverIn(context.Background(), escrowDir, cfgPath)
	if err != nil {
		t.Fatalf("RecoverIn must not fail on a corrupt sibling: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected the valid escrow to recover, got %d", len(results))
	}
	if len(log.warnings) == 0 {
		t.Error("corrupt escrow filename was never surfaced")
	}
	if _, err := os.Stat(filepath.Join(escrowDir, "corrupt.json")); err != nil {
		t.Error("a corrupt escrow must be left on disk for the operator, not deleted")
	}
}

func TestRecoverIn_EmptyDirIsANoOp(t *testing.T) {
	r, _, cfgPath, _ := newTestRotator(t, "")
	results, err := r.RecoverIn(context.Background(), filepath.Join(t.TempDir(), "none"), cfgPath)
	if err != nil {
		t.Fatalf("RecoverIn: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected no results, got %d", len(results))
	}
}

// An escrow written by an older run without a ConfigPath falls back to the
// path the caller supplies.
func TestRecoverIn_FallsBackToSuppliedConfigPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(cfgPath, []byte(twoHostFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	escrowDir := filepath.Join(dir, "escrow")
	digest, err := FileDigest(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteEscrow(escrowDir, &Escrow{
		Version: 1, Host: testHost, State: StateRotated,
		OldTokenID: 1, NewTokenID: 2, NewToken: newSecret,
		ConfigSHA256: digest,
	}); err != nil {
		t.Fatal(err)
	}

	r, _, _, _ := newTestRotator(t, "")
	results, err := r.RecoverIn(context.Background(), escrowDir, cfgPath)
	if err != nil {
		t.Fatalf("RecoverIn: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 recovery, got %d", len(results))
	}
	b, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(b), "token: "+newSecret) {
		t.Errorf("config not committed:\n%s", b)
	}
}

// One bad escrow must not strand the others.
func TestRecoverIn_ContinuesAfterAFailingEscrow(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(cfgPath, []byte(twoHostFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	escrowDir := filepath.Join(dir, "escrow")
	digest, err := FileDigest(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"absent.example.org", testHost} {
		if _, err := WriteEscrow(escrowDir, &Escrow{
			Version: 1, Host: host, State: StateRotated,
			OldTokenID: 1, NewTokenID: 2, NewToken: newSecret,
			ConfigPath: cfgPath, ConfigSHA256: digest,
		}); err != nil {
			t.Fatal(err)
		}
	}

	r, _, _, _ := newTestRotator(t, "")
	results, err := r.RecoverIn(context.Background(), escrowDir, cfgPath)
	if err == nil {
		t.Fatal("expected the unknown host to report an error")
	}
	if len(results) != 1 {
		t.Fatalf("the healthy escrow should still have recovered, got %d results", len(results))
	}
	left := mustList(t, escrowDir)
	if len(left) != 1 || left[0].Host != "absent.example.org" {
		t.Fatalf("only the failed escrow should remain, got %+v", left)
	}
}

// The escrow file is the only place the secret may be written. Nothing that
// reaches an operator, a log, or the journal may carry it.
func TestRotate_SecretNeverAppearsInErrorsOrResults(t *testing.T) {
	for _, phase := range []string{"verify", "commit", "journal", "store"} {
		t.Run(phase, func(t *testing.T) {
			r, log, cfgPath, escrowDir := newTestRotator(t, phase)
			res, err := r.Rotate(context.Background(), testHost, testOptions(escrowDir, cfgPath))
			if err == nil {
				t.Fatal("expected failure")
			}
			if strings.Contains(err.Error(), newSecret) {
				t.Error("error message leaked the token")
			}
			if strings.Contains(res.String(), newSecret) {
				t.Error("Result leaked the token")
			}
			for _, w := range log.warnings {
				if strings.Contains(w, newSecret) {
					t.Error("warning leaked the token")
				}
			}
		})
	}
}

// Losing the escrow write is the one unrecoverable outcome; it must be loud.
func TestRotate_EscrowWriteFailureIsReportedAsCritical(t *testing.T) {
	r, _, cfgPath, _ := newTestRotator(t, "")
	// A readable but unwritable escrow directory: the preflight listing still
	// works, and the escrow write is what fails.
	blocked := filepath.Join(t.TempDir(), "escrow")
	if err := os.Mkdir(blocked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o700) })

	res, err := r.Rotate(context.Background(), testHost, testOptions(blocked, cfgPath))
	if err == nil {
		t.Fatal("expected the escrow write to fail")
	}
	if !strings.Contains(err.Error(), "CRITICAL") {
		t.Errorf("an unescrowed rotation must be flagged CRITICAL, got %v", err)
	}
	if strings.Contains(err.Error(), newSecret) {
		t.Error("the critical error leaked the token")
	}
	if res.Phase != PhaseEscrow {
		t.Errorf("Phase = %q, want %q", res.Phase, PhaseEscrow)
	}
}

// Warn is optional; a zero-valued Deps must not panic.
func TestRotator_NilWarnIsSafe(t *testing.T) {
	r, _, cfgPath, escrowDir := newTestRotator(t, "commit")
	r.Deps.Warn = nil
	if _, err := r.Rotate(context.Background(), testHost, testOptions(escrowDir, cfgPath)); err == nil {
		t.Fatal("expected the commit failure")
	}
	if err := os.WriteFile(filepath.Join(escrowDir, "corrupt.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	healthy, _, _, _ := newTestRotator(t, "")
	healthy.Deps.Warn = nil
	if _, err := healthy.RecoverIn(context.Background(), escrowDir, cfgPath); err != nil {
		t.Fatalf("RecoverIn: %v", err)
	}
}

func TestResult_StringIsHumanReadable(t *testing.T) {
	res := &Result{Host: testHost, Phase: PhaseDone, Rotated: true, NewTokenID: 51630}
	got := res.String()
	for _, want := range []string{testHost, PhaseDone, "51630"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, missing %q", got, want)
		}
	}
}
