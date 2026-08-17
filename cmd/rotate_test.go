package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/MChorfa/ggvalet/internal/rotation"
)

// ─── command surface ─────────────────────────────────────────────────────────

func TestRotateCmd_Registered(t *testing.T) {
	c := rotateCmd()
	if c.Use != "rotate" {
		t.Fatalf("Use = %q", c.Use)
	}
	for _, f := range []string{"check", "force", "recover"} {
		if c.Flags().Lookup(f) == nil {
			t.Errorf("missing --%s flag", f)
		}
	}
}

func TestRotate_IsNotHostNeutral(t *testing.T) {
	for _, p := range hostNeutralPrefixes {
		if strings.HasPrefix(p, "ggvalet rotate") || strings.HasPrefix(p, "ggvalet ssh") {
			t.Errorf("%q must not be host-neutral: it issues host-specific API calls", p)
		}
	}
}

// The guard blocks whatever is not on an allowlist, so `ggvalet rotate` is
// already refused under a non-GitLab provider. The explicit entry is what keeps
// that true if a host-neutral prefix is ever added above it.
func TestRotate_IsAnExplicitlyBlockedLeaf(t *testing.T) {
	for _, path := range []string{"ggvalet rotate", "ggvalet ssh audit"} {
		if !hostBlockedLeaves[path] {
			t.Errorf("%q should be in hostBlockedLeaves", path)
		}
	}
}

func TestGgvaletHome_HonoursTheOverride(t *testing.T) {
	t.Setenv("GLVALET_HOME", "/somewhere/else")
	if got := ggvaletHome(); got != "/somewhere/else" {
		t.Fatalf("ggvaletHome() = %q", got)
	}
	if got := rotationConfigPath(); got != "/somewhere/else/rotation.yaml" {
		t.Fatalf("rotationConfigPath() = %q", got)
	}
	if got := escrowDir(); got != "/somewhere/else/escrow" {
		t.Fatalf("escrowDir() = %q", got)
	}
}

func TestGgvaletHome_DefaultsUnderTheUserHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GLVALET_HOME", "")
	t.Setenv("HOME", home)
	if got := ggvaletHome(); got != filepath.Join(home, ".ggvalet") {
		t.Fatalf("ggvaletHome() = %q, want %q", got, filepath.Join(home, ".ggvalet"))
	}
}

// F4: the record phase is not idempotent, so a recovered rotation appends a
// second journal entry. That is only tolerable if an operator reading the
// journal is told; both help surfaces must say so.
func TestRotate_DuplicateJournalEntriesAreDocumented(t *testing.T) {
	if !strings.Contains(rotateCmd().Long, "second rotate entry") {
		t.Error("`ggvalet rotate --help` does not warn about duplicate journal entries")
	}
	if !strings.Contains(journalShowCmd().Long, "same token id") {
		t.Error("`ggvalet journal show --help` does not explain duplicate rotate entries")
	}
}

func TestRunRotate_ProposesAProfileAndRefusesToAct(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GLVALET_HOME", home)
	t.Setenv("GLVALET_TOKEN", "test-token")
	t.Setenv("GLVALET_GITLAB_URL", "https://gitlab.example.com")
	t.Setenv("GLVALET_PROVIDER", "")
	hostFlag = ""

	err := runRotate(context.Background(), rotateOptions{})
	if err == nil {
		t.Fatal("a first run must refuse to act on a profile the operator has not reviewed")
	}
	if !strings.Contains(err.Error(), filepath.Join(home, "rotation.yaml")) {
		t.Fatalf("the error must name the file to review: %v", err)
	}
	rc, lerr := rotation.Load(filepath.Join(home, "rotation.yaml"))
	if lerr != nil {
		t.Fatalf("the proposed profile should be on disk: %v", lerr)
	}
	if len(rc.Hosts) == 0 {
		t.Fatal("the proposed profile lists no hosts")
	}
}

// Drives the whole assembly — profile, state db, journal, wiring — with a host
// that rotation.yaml lists and the glab config does not. Preflight fails before
// any remote call, which is what makes the test deterministic offline.
func TestRunRotate_ReportsAHostMissingFromTheGlabConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GLVALET_HOME", home)
	t.Setenv("GLVALET_STATE", filepath.Join(home, "state.db"))
	t.Setenv("GLVALET_JOURNAL", filepath.Join(home, "journal.jsonl"))
	t.Setenv("GLVALET_TOKEN", "test-token")
	t.Setenv("GLVALET_GITLAB_URL", "https://gitlab.example.com")
	t.Setenv("GLVALET_PROVIDER", "")
	hostFlag = ""
	saved := cfg
	cfg = nil
	t.Cleanup(func() { cfg = saved })

	if err := rotation.Save(filepath.Join(home, "rotation.yaml"), &rotation.Config{
		Version: 1,
		Hosts:   map[string]rotation.Profile{"gone.example.com": {Credentials: []string{"pat"}}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	silenceOutput(t)

	err := runRotate(context.Background(), rotateOptions{})
	if err == nil {
		t.Fatal("a host that cannot be resolved must surface as an error")
	}
	if !strings.Contains(err.Error(), "need attention") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─── runner fixture ──────────────────────────────────────────────────────────

const fixtureSecret = "glpat-NEW-2222222222"

// rotateFixture is one host's worth of real files (a glab config that can
// actually receive a token, an escrow dir) plus a fully faked Deps, so the
// state machine runs end to end without a network.
type rotateFixture struct {
	runner  *rotateRunner
	deps    *rotation.Deps
	jrnl    *fakeJournal
	out     *bytes.Buffer
	errOut  *bytes.Buffer
	cfgPath string
	escrow  string
}

// writeRotatableConfig writes a glab config whose host block can actually
// receive a token, which preflight proves by running the real writer over a
// copy of it.
func writeRotatableConfig(t *testing.T, dir string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, "config.yml")
	body := "host: " + testHost + "\nhosts:\n  " + testHost + ":\n    " +
		"token" + ": " + testOldToken + "\n    api_protocol: https\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return cfgPath
}

func newRotateFixture(t *testing.T) *rotateFixture {
	t.Helper()
	dir := t.TempDir()
	cfgPath := writeRotatableConfig(t, dir)

	j := &fakeJournal{}
	now := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	deps := &rotation.Deps{
		Now: func() time.Time { return now },
		GetSelf: func(context.Context, string) (*rotation.TokenInfo, error) {
			return &rotation.TokenInfo{ID: 1000, Active: true, Scopes: []string{"api", "self_rotate"}}, nil
		},
		Rotate: func(context.Context, string, string) (*rotation.TokenInfo, string, error) {
			return &rotation.TokenInfo{ID: 51630, Scopes: []string{"api", "self_rotate"}}, fixtureSecret, nil
		},
		VerifyToken: func(context.Context, string, string) error { return nil },
		SetToken:    rotation.SetHostToken,
		Digest:      rotation.FileDigest,
		Journal: func(host string, tokenID int, ok bool) error {
			out := journal.OutcomeOK
			if !ok {
				out = journal.OutcomeErr
			}
			return j.Record(journal.Entry{
				Host: host, Op: journal.OpRotate, Entity: journal.EntityToken,
				EntityID: tokenID, Outcome: out, Detail: "personal access token rotated",
			})
		},
		Store: func(string, time.Time) error { return nil },
	}

	f := &rotateFixture{
		deps: deps, jrnl: j,
		out: &bytes.Buffer{}, errOut: &bytes.Buffer{},
		cfgPath: cfgPath, escrow: filepath.Join(dir, "escrow"),
	}
	f.runner = &rotateRunner{
		rc: &rotation.Config{
			Defaults: rotation.Defaults{CadenceDays: 30, PATExpiryDays: 65},
			Hosts:    map[string]rotation.Profile{testHost: {Credentials: []string{"pat"}}},
		},
		jrnl:       j,
		escrowDir:  f.escrow,
		configPath: cfgPath,
		now:        func() time.Time { return now },
		out:        f.out,
		errOut:     f.errOut,
	}
	f.runner.rot = &rotation.Rotator{Deps: *deps}
	return f
}

// rebind re-reads deps into the rotator after a test has changed a field.
func (f *rotateFixture) rebind() { f.runner.rot = &rotation.Rotator{Deps: *f.deps} }

func (f *rotateFixture) printed() string { return f.out.String() + f.errOut.String() }

func (f *rotateFixture) assertNoSecret(t *testing.T) {
	t.Helper()
	if strings.Contains(f.printed(), fixtureSecret) {
		t.Fatalf("the new secret reached the terminal:\n%s", f.printed())
	}
	for _, e := range f.jrnl.entries {
		if strings.Contains(e.Detail, fixtureSecret) || strings.Contains(e.Title, fixtureSecret) {
			t.Fatalf("the new secret reached the journal: %+v", e)
		}
	}
}

// The fixture above installs a rotation.Deps literal, so it never exercises
// rotateWiring.deps — and therefore never exercises redactSecrets. This test
// builds the runner the way runRotate does, from a wiring whose API leaks the
// secret it is handed, so the composition at rotate.go's
// `Rotator{Deps: w.deps(ctx)}` is what is under test. Remove the redaction from
// VerifyToken and this fails; the fixture-based tests do not.
func TestRotateHosts_ComposedWiringStripsALeakedSecretFromTheOutput(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeRotatableConfig(t, dir)
	j := &fakeJournal{}

	w := &rotateWiring{
		api: rotateAPI{
			// Preflight authenticates with the configured token and succeeds;
			// verify authenticates with the new secret and leaks it.
			getSelf: func(_ context.Context, baseURL, token string, _ bool) (*rotation.TokenInfo, error) {
				if token == fixtureSecret {
					return nil, fmt.Errorf("GET %s: rejected token %s", baseURL, token)
				}
				return &rotation.TokenInfo{ID: 1000, Active: true, Scopes: []string{"api", "self_rotate"}}, nil
			},
			rotate: func(context.Context, string, string, string, bool) (*rotation.TokenInfo, string, error) {
				return &rotation.TokenInfo{ID: 51630}, fixtureSecret, nil
			},
		},
		hosts: map[string]*config.HostConfig{testHost: {Token: testOldToken, APIProtocol: "https"}},
		store: &fakeRotateStore{},
		jrnl:  j,
		warn:  func(string, ...any) {},
	}
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	rr := &rotateRunner{
		rot: &rotation.Rotator{Deps: w.deps(context.Background())},
		rc: &rotation.Config{
			Defaults: rotation.Defaults{CadenceDays: 30, PATExpiryDays: 65},
			Hosts:    map[string]rotation.Profile{testHost: {Credentials: []string{"pat"}}},
		},
		jrnl:       j,
		escrowDir:  filepath.Join(dir, "escrow"),
		configPath: cfgPath,
		now:        time.Now,
		out:        out,
		errOut:     errOut,
	}

	if err := rr.rotateHosts(context.Background(), false); err == nil {
		t.Fatal("the run should fail at verify")
	}
	printed := out.String() + errOut.String()
	if strings.Contains(printed, fixtureSecret) {
		t.Fatalf("the leaked secret reached the operator's terminal:\n%s", printed)
	}
	if !strings.Contains(printed, "[redacted]") {
		t.Fatalf("the leak should have been stripped by the wiring, not merely absent:\n%s", printed)
	}
	for _, e := range j.entries {
		if strings.Contains(e.Detail, fixtureSecret) {
			t.Fatalf("the leaked secret reached the journal: %+v", e)
		}
	}
}

// ─── outcomes an operator sees ───────────────────────────────────────────────

func TestRotateHosts_SuccessJournalsExactlyOnce(t *testing.T) {
	f := newRotateFixture(t)
	if err := f.runner.rotateHosts(context.Background(), false); err != nil {
		t.Fatalf("rotateHosts: %v", err)
	}
	if !strings.Contains(f.out.String(), "51630") || !strings.Contains(f.out.String(), testHost) {
		t.Fatalf("success line must name host and new token id:\n%s", f.out.String())
	}
	// The state machine journals the success; the boundary must not add a
	// second entry for the same rotation.
	if len(f.jrnl.entries) != 1 || f.jrnl.entries[0].Outcome != journal.OutcomeOK {
		t.Fatalf("journal = %+v, want exactly one ok entry", f.jrnl.entries)
	}
	f.assertNoSecret(t)
}

func TestRotateHosts_NotDueIsQuietAndNotAFailure(t *testing.T) {
	f := newRotateFixture(t)
	f.deps.LastRotated = func(string) (time.Time, bool, error) {
		return time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC), true, nil
	}
	f.rebind()
	if err := f.runner.rotateHosts(context.Background(), false); err != nil {
		t.Fatalf("a host that is not due is not a failure: %v", err)
	}
	if !strings.Contains(f.out.String(), "not due") {
		t.Fatalf("want a not-due line:\n%s", f.out.String())
	}
	if len(f.jrnl.entries) != 0 {
		t.Fatalf("a skipped host must not be journalled: %+v", f.jrnl.entries)
	}
}

func TestRotateHosts_PreflightFailureIsNotJournalled(t *testing.T) {
	f := newRotateFixture(t)
	f.deps.GetSelf = func(context.Context, string) (*rotation.TokenInfo, error) {
		return nil, errors.New("HTTP 401")
	}
	f.rebind()
	err := f.runner.rotateHosts(context.Background(), false)
	if err == nil {
		t.Fatal("a preflight failure must surface as an error")
	}
	if !strings.Contains(f.errOut.String(), testHost) {
		t.Fatalf("the failure line must name the host:\n%s", f.errOut.String())
	}
	if strings.Contains(f.printed(), "CRITICAL") {
		t.Fatalf("nothing was rotated; this is not the critical case:\n%s", f.printed())
	}
	// Nothing happened remotely, so there is no attempt to audit.
	if len(f.jrnl.entries) != 0 {
		t.Fatalf("preflight failure must not be journalled: %+v", f.jrnl.entries)
	}
}

// R8: the boundary journals a rotation that died after the remote call. The
// state machine cannot — the phase that failed may be its own journal write.
func TestRotateHosts_RotatedButNotCommittedIsLoudAndJournalled(t *testing.T) {
	f := newRotateFixture(t)
	f.deps.VerifyToken = func(context.Context, string, string) error {
		return errors.New("the new token for " + testHost + " was not accepted by the host")
	}
	f.rebind()

	err := f.runner.rotateHosts(context.Background(), false)
	if err == nil {
		t.Fatal("a half-finished rotation must surface as an error")
	}

	printed := f.printed()
	for _, want := range []string{"CRITICAL", testHost, "51630", "--recover", f.escrow} {
		if !strings.Contains(printed, want) {
			t.Errorf("operator output is missing %q:\n%s", want, printed)
		}
	}
	// The escrow is the only copy of the credential; it must still be there.
	escrows, _, lerr := rotation.ListEscrows(f.escrow)
	if lerr != nil || len(escrows) != 1 {
		t.Fatalf("want one escrow left behind, got %d (%v)", len(escrows), lerr)
	}

	if len(f.jrnl.entries) != 1 {
		t.Fatalf("want one journal entry for the failed rotation, got %+v", f.jrnl.entries)
	}
	e := f.jrnl.entries[0]
	if e.Outcome != journal.OutcomeErr || e.Op != journal.OpRotate || e.Entity != journal.EntityToken {
		t.Fatalf("entry = %+v", e)
	}
	if e.EntityID != 51630 {
		t.Fatalf("the entry must carry the new token id, got %d", e.EntityID)
	}
	if !strings.Contains(e.Detail, rotation.PhaseVerify) {
		t.Fatalf("the entry should name the phase that failed, got %q", e.Detail)
	}
	f.assertNoSecret(t)
}

// res.Rotated is set before the escrow write, so a failure of that write also
// reaches the CRITICAL block. The state machine hedges its own message for that
// phase ("may still be on disk"); the loudest line must not flatly contradict it
// two lines above the cause that carries the truth.
func TestPrintCritical_DoesNotAssertAnEscrowThatMayNotExist(t *testing.T) {
	f := newRotateFixture(t)
	f.runner.printCritical(
		&rotation.Result{Host: testHost, Phase: rotation.PhaseEscrow, Rotated: true, NewTokenID: 51630},
		errors.New("could not confirm the escrow write"))

	printed := f.errOut.String()
	if strings.Contains(printed, "is in escrow") {
		t.Fatalf("the escrow write is what failed; it must not be asserted:\n%s", printed)
	}
	for _, want := range []string{"may be in escrow", f.escrow, "before treating this host as lost"} {
		if !strings.Contains(printed, want) {
			t.Errorf("missing %q:\n%s", want, printed)
		}
	}

	// A later phase did write the escrow, and saying so plainly is what makes
	// the hedge above meaningful.
	f.errOut.Reset()
	f.runner.printCritical(
		&rotation.Result{Host: testHost, Phase: rotation.PhaseCommit, Rotated: true, NewTokenID: 51630},
		errors.New("boom"))
	if !strings.Contains(f.errOut.String(), "is in escrow") {
		t.Fatalf("a post-escrow failure should state the escrow exists:\n%s", f.errOut.String())
	}
}

// ─── --check ─────────────────────────────────────────────────────────────────

func TestCheckHosts_DueHostIsReportedAndExitsNonZero(t *testing.T) {
	f := newRotateFixture(t)
	f.runner.lastRotated = func(string) (time.Time, bool, error) { return time.Time{}, false, nil }
	err := f.runner.checkHosts(false)
	if err == nil {
		t.Fatal("--check must fail when a host is due")
	}
	if !strings.Contains(f.out.String(), "never rotated") {
		t.Fatalf("want the reason for dueness:\n%s", f.out.String())
	}
	// --check reports; it must not touch the remote or the config.
	if len(f.jrnl.entries) != 0 {
		t.Fatalf("--check journalled something: %+v", f.jrnl.entries)
	}
}

func TestCheckHosts_NotDueHostPasses(t *testing.T) {
	f := newRotateFixture(t)
	f.runner.lastRotated = func(string) (time.Time, bool, error) {
		return time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC), true, nil
	}
	if err := f.runner.checkHosts(false); err != nil {
		t.Fatalf("checkHosts: %v", err)
	}
	if !strings.Contains(f.out.String(), "next due") {
		t.Fatalf("want the next due date:\n%s", f.out.String())
	}
}

func TestCheckHosts_UncommittedEscrowIsAProblem(t *testing.T) {
	f := newRotateFixture(t)
	f.runner.lastRotated = func(string) (time.Time, bool, error) {
		return time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC), true, nil
	}
	if _, err := rotation.WriteEscrow(f.escrow, &rotation.Escrow{
		Version: 1, Host: testHost, State: rotation.StateRotated,
		NewTokenID: 51630, NewToken: fixtureSecret, ConfigPath: f.cfgPath,
	}); err != nil {
		t.Fatalf("WriteEscrow: %v", err)
	}
	err := f.runner.checkHosts(false)
	if err == nil {
		t.Fatal("an uncommitted escrow must make --check fail: the host is half-rotated")
	}
	if !strings.Contains(f.printed(), "--recover") {
		t.Fatalf("want the remedy in the output:\n%s", f.printed())
	}
	f.assertNoSecret(t)
}

func TestCheckHosts_CorruptEscrowIsSurfacedNotSwallowed(t *testing.T) {
	f := newRotateFixture(t)
	f.runner.lastRotated = func(string) (time.Time, bool, error) {
		return time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC), true, nil
	}
	if err := os.MkdirAll(f.escrow, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	bad := filepath.Join(f.escrow, "gitlab.example.com-truncated.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := f.runner.checkHosts(false); err == nil {
		t.Fatal("an unparseable escrow may be holding a credential; --check must fail")
	}
	if !strings.Contains(f.printed(), "gitlab.example.com-truncated.json") {
		t.Fatalf("the filename must be named so it can be inspected:\n%s", f.printed())
	}
}

func TestDue_ForceAndUnreadableStateBothCountAsDue(t *testing.T) {
	f := newRotateFixture(t)
	f.runner.lastRotated = func(string) (time.Time, bool, error) {
		return time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC), true, nil
	}
	if due, reason := f.runner.due(testHost, true); !due || reason != "forced" {
		t.Fatalf("due(force) = %v, %q", due, reason)
	}

	f.runner.lastRotated = func(string) (time.Time, bool, error) {
		return time.Time{}, false, errors.New("database is locked")
	}
	// An unreadable state database is not evidence that a host is up to date.
	due, reason := f.runner.due(testHost, false)
	if !due || !strings.Contains(reason, "unreadable") {
		t.Fatalf("due(unreadable state) = %v, %q", due, reason)
	}

	f.runner.lastRotated = nil
	if due, _ := f.runner.due(testHost, false); !due {
		t.Fatal("with no state source at all, a host must read as due")
	}
}

// Rule 12: if the audit entry for a failed rotation cannot be written, that is
// itself something the operator has to be told.
func TestJournalFailure_SaysSoWhenItCannotRecord(t *testing.T) {
	f := newRotateFixture(t)
	f.runner.jrnl = brokenJournal{}
	f.deps.VerifyToken = func(context.Context, string, string) error { return errors.New("HTTP 401") }
	f.rebind()

	if err := f.runner.rotateHosts(context.Background(), false); err == nil {
		t.Fatal("the run should fail at verify")
	}
	printed := f.errOut.String()
	if !strings.Contains(printed, "could not be journalled") {
		t.Fatalf("a failed journal write must be reported:\n%s", printed)
	}
	// The line is indented to sit inside the CRITICAL block, so it has to be
	// printed inside it rather than above its header.
	if strings.Index(printed, "could not be journalled") < strings.Index(printed, "CRITICAL") {
		t.Fatalf("the journal-failure line precedes the block it is indented under:\n%s", printed)
	}
}

type brokenJournal struct{}

func (brokenJournal) Record(journal.Entry) error { return errors.New("disk full") }

// ─── --recover ───────────────────────────────────────────────────────────────

func TestRecoverAll_CommitsAnEscrowedSecret(t *testing.T) {
	f := newRotateFixture(t)
	if _, err := rotation.WriteEscrow(f.escrow, &rotation.Escrow{
		Version: 1, Host: testHost, State: rotation.StateRotated,
		NewTokenID: 51630, NewToken: fixtureSecret, ConfigPath: f.cfgPath,
	}); err != nil {
		t.Fatalf("WriteEscrow: %v", err)
	}
	if err := f.runner.recoverAll(context.Background()); err != nil {
		t.Fatalf("recoverAll: %v", err)
	}
	body, err := os.ReadFile(f.cfgPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(body), fixtureSecret) {
		t.Fatal("recovery did not commit the escrowed secret to the config")
	}
	escrows, _, _ := rotation.ListEscrows(f.escrow)
	if len(escrows) != 0 {
		t.Fatalf("a committed escrow must be retired, %d left", len(escrows))
	}
	f.assertNoSecret(t)
}

func TestRecoverAll_NothingToRecoverSaysSo(t *testing.T) {
	f := newRotateFixture(t)
	if err := f.runner.recoverAll(context.Background()); err != nil {
		t.Fatalf("recoverAll: %v", err)
	}
	if !strings.Contains(f.out.String(), "nothing to recover") {
		t.Fatalf("want an explicit nothing-to-do line:\n%s", f.out.String())
	}
}

func TestRecoverAll_FailureKeepsTheEscrowAndReports(t *testing.T) {
	f := newRotateFixture(t)
	f.deps.VerifyToken = func(context.Context, string, string) error { return errors.New("HTTP 401") }
	f.rebind()
	if _, err := rotation.WriteEscrow(f.escrow, &rotation.Escrow{
		Version: 1, Host: testHost, State: rotation.StateRotated,
		NewTokenID: 51630, NewToken: fixtureSecret, ConfigPath: f.cfgPath,
	}); err != nil {
		t.Fatalf("WriteEscrow: %v", err)
	}
	if err := f.runner.recoverAll(context.Background()); err == nil {
		t.Fatal("a failed recovery must surface as an error")
	}
	escrows, _, _ := rotation.ListEscrows(f.escrow)
	if len(escrows) != 1 {
		t.Fatalf("a failed recovery must keep its escrow, %d left", len(escrows))
	}
	f.assertNoSecret(t)
}

// ─── host selection ──────────────────────────────────────────────────────────

func TestRotateRunner_OnlyHostsDeclaringPAT(t *testing.T) {
	f := newRotateFixture(t)
	f.runner.rc.Hosts["ssh-only.example.com"] = rotation.Profile{Credentials: []string{"ssh"}}
	got := f.runner.patHosts()
	if len(got) != 1 || got[0] != testHost {
		t.Fatalf("patHosts() = %v, want just the pat host", got)
	}
}

// An ssh-only host is correctly out of scope for `ggvalet rotate` — but
// patHosts() used to drop it with no message at all, leaving an operator who
// configured the host unable to tell "audited, nothing to do" from "your
// config was ignored". Both `rotate` and `rotate --check` must say something.
func TestRotateHosts_SSHOnlyHostIsNotedNotSilentlyDropped(t *testing.T) {
	f := newRotateFixture(t)
	f.runner.rc.Hosts["ssh-only.example.com"] = rotation.Profile{Credentials: []string{"ssh"}}

	if err := f.runner.rotateHosts(context.Background(), false); err != nil {
		t.Fatalf("rotateHosts: %v", err)
	}
	if !strings.Contains(f.out.String(), "ssh-only.example.com") {
		t.Fatalf("the ssh-only host must be mentioned, not silently dropped:\n%s", f.out.String())
	}
	if !strings.Contains(f.out.String(), "ggvalet ssh audit") {
		t.Fatalf("the note should point at the command that covers ssh hosts:\n%s", f.out.String())
	}
}

func TestCheckHosts_SSHOnlyHostIsNotedNotSilentlyDropped(t *testing.T) {
	f := newRotateFixture(t)
	f.runner.rc.Hosts["ssh-only.example.com"] = rotation.Profile{Credentials: []string{"ssh"}}
	f.runner.lastRotated = func(string) (time.Time, bool, error) {
		return time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC), true, nil
	}

	if err := f.runner.checkHosts(false); err != nil {
		t.Fatalf("checkHosts: %v", err)
	}
	if !strings.Contains(f.out.String(), "ssh-only.example.com") {
		t.Fatalf("the ssh-only host must be mentioned, not silently dropped:\n%s", f.out.String())
	}
}

// docs/rotation.md makes `rotate --force --host <one-host>` the rollout gate:
// one watched rotation before the schedule is loaded. Until this, --host was
// accepted and ignored, so following the runbook exactly rotated every PAT
// host during the step designed to have the smallest possible blast radius.
func TestRotateHosts_HostFlagRestrictsTheRunToThatHost(t *testing.T) {
	f := newRotateFixture(t)
	f.runner.rc.Hosts["other.example.com"] = rotation.Profile{Credentials: []string{"pat"}}
	f.runner.only = testHost

	got := f.runner.patHosts()
	if len(got) != 1 || got[0] != testHost {
		t.Fatalf("patHosts() = %v, want exactly [%s] — --host must not widen to every host", got, testHost)
	}
}

func TestRotateHosts_UnscopedRunCoversEveryPATHost(t *testing.T) {
	f := newRotateFixture(t)
	f.runner.rc.Hosts["other.example.com"] = rotation.Profile{Credentials: []string{"pat"}}

	if got := f.runner.patHosts(); len(got) != 2 {
		t.Fatalf("patHosts() = %v, want both hosts when --host is unset", got)
	}
}

// A typo in the one flag that limits blast radius must not read as a healthy
// no-op run.
func TestRotateHosts_UnknownHostFlagIsAnError(t *testing.T) {
	f := newRotateFixture(t)
	f.runner.only = "not-declared.example.com"

	err := f.runner.rotateHosts(context.Background(), false)
	if err == nil {
		t.Fatal("a --host naming an undeclared host must fail, not silently rotate nothing")
	}
	if !strings.Contains(err.Error(), "not-declared.example.com") {
		t.Fatalf("the error must name the host that was not found: %v", err)
	}
	if !strings.Contains(err.Error(), testHost) {
		t.Fatalf("the error should list the hosts that ARE declared, to catch a typo: %v", err)
	}
}

func TestCheckHosts_UnknownHostFlagIsAnError(t *testing.T) {
	f := newRotateFixture(t)
	f.runner.only = "not-declared.example.com"

	if err := f.runner.checkHosts(false); err == nil {
		t.Fatal("--check must reject an undeclared --host too")
	}
}

func TestRotateRunner_SSHOnlyHosts(t *testing.T) {
	f := newRotateFixture(t)
	f.runner.rc.Hosts["ssh-only.example.com"] = rotation.Profile{Credentials: []string{"ssh"}}
	f.runner.rc.Hosts["both.example.com"] = rotation.Profile{Credentials: []string{"pat", "ssh"}}
	got := f.runner.sshOnlyHosts()
	if len(got) != 1 || got[0] != "ssh-only.example.com" {
		t.Fatalf("sshOnlyHosts() = %v, want just the ssh-only host", got)
	}
}
