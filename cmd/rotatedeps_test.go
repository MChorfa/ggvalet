package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/MChorfa/ggvalet/internal/rotation"
)

// ─── fakes ───────────────────────────────────────────────────────────────────

type fakeRotateStore struct {
	last     time.Time
	found    bool
	lastErr  error
	setHost  string
	setAt    time.Time
	setCtxOK bool
	setErr   error
}

type ctxKey string

const probeCtxKey ctxKey = "probe"

func (s *fakeRotateStore) SetLastRotated(ctx context.Context, host string, at time.Time) error {
	s.setHost, s.setAt = host, at
	s.setCtxOK = ctx.Value(probeCtxKey) == "yes"
	return s.setErr
}

func (s *fakeRotateStore) LastRotated(ctx context.Context, host string) (time.Time, bool, error) {
	return s.last, s.found, s.lastErr
}

type fakeJournal struct{ entries []journal.Entry }

func (j *fakeJournal) Record(e journal.Entry) error {
	j.entries = append(j.entries, e)
	return nil
}

const (
	testHost      = "gitlab.example.com"
	testOldToken  = "glpat-OLD-0000000000"
	testNewSecret = "glpat-NEW-1111111111"
)

// wiringWithAPI builds a rotateWiring for testHost around the given API.
func wiringWithAPI(api rotateAPI) (*rotateWiring, *fakeRotateStore, *fakeJournal) {
	st := &fakeRotateStore{}
	j := &fakeJournal{}
	return &rotateWiring{
		api: api,
		hosts: map[string]*config.HostConfig{
			testHost: {Token: testOldToken, APIProtocol: "https"},
		},
		store: st,
		jrnl:  j,
		warn:  func(string, ...any) {},
	}, st, j
}

// ─── redaction ───────────────────────────────────────────────────────────────

func TestRedactSecrets_ReplacesEveryOccurrence(t *testing.T) {
	err := fmt.Errorf("GET https://h/x?private_token=%s failed for %s", testNewSecret, testNewSecret)
	got := redactSecrets(err, testNewSecret)
	if strings.Contains(got.Error(), testNewSecret) {
		t.Fatalf("secret survived redaction: %q", got)
	}
	if !strings.Contains(got.Error(), "[redacted]") {
		t.Fatalf("want a redaction marker, got %q", got)
	}
}

func TestRedactSecrets_PassesNilAndEmptySecretsThrough(t *testing.T) {
	if got := redactSecrets(nil, testNewSecret); got != nil {
		t.Fatalf("redactSecrets(nil) = %v", got)
	}
	orig := errors.New("plain failure")
	// An empty secret must not turn into a match on every position.
	if got := redactSecrets(orig, ""); got.Error() != "plain failure" {
		t.Fatalf("empty secret mangled the message: %q", got)
	}
}

// ─── the leak hazard ─────────────────────────────────────────────────────────

// leakyAPI puts whatever token it is handed straight into its error, which is
// the failure mode this wiring exists to contain: the state machine wraps this
// error and the wrap reaches stderr.
func leakyAPI() rotateAPI {
	return rotateAPI{
		getSelf: func(_ context.Context, baseURL, token string, _ bool) (*rotation.TokenInfo, error) {
			return nil, fmt.Errorf("GET %s: rejected token %s", baseURL, token)
		},
		rotate: func(_ context.Context, baseURL, token, _ string, _ bool) (*rotation.TokenInfo, string, error) {
			return nil, "", fmt.Errorf("POST %s: rejected token %s", baseURL, token)
		},
	}
}

func TestRotateDeps_VerifyTokenNeverLeaksTheNewSecret(t *testing.T) {
	w, _, _ := wiringWithAPI(leakyAPI())
	err := w.deps(context.Background()).VerifyToken(context.Background(), testHost, testNewSecret)
	if err == nil {
		t.Fatal("VerifyToken should fail")
	}
	if strings.Contains(err.Error(), testNewSecret) {
		t.Fatalf("the new secret leaked into the error: %q", err)
	}
	if strings.Contains(err.Error(), testOldToken) {
		t.Fatalf("the configured token leaked into the error: %q", err)
	}
	if !strings.Contains(err.Error(), testHost) {
		t.Fatalf("the error must name the host: %q", err)
	}
}

func TestRotateDeps_GetSelfAndRotateNeverLeakTheConfiguredToken(t *testing.T) {
	w, _, _ := wiringWithAPI(leakyAPI())
	d := w.deps(context.Background())

	_, err := d.GetSelf(context.Background(), testHost)
	if err == nil || strings.Contains(err.Error(), testOldToken) {
		t.Fatalf("GetSelf leaked or did not fail: %v", err)
	}
	_, _, err = d.Rotate(context.Background(), testHost, "2026-12-31")
	if err == nil || strings.Contains(err.Error(), testOldToken) {
		t.Fatalf("Rotate leaked or did not fail: %v", err)
	}
}

func TestRotateDeps_VerifyTokenAuthenticatesWithTheNewSecret(t *testing.T) {
	var seen string
	w, _, _ := wiringWithAPI(rotateAPI{
		getSelf: func(_ context.Context, _, token string, _ bool) (*rotation.TokenInfo, error) {
			seen = token
			return &rotation.TokenInfo{ID: 7, Active: true}, nil
		},
	})
	if err := w.deps(context.Background()).VerifyToken(context.Background(), testHost, testNewSecret); err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	// Verifying with the old token would prove nothing: it is already revoked.
	if seen != testNewSecret {
		t.Fatalf("VerifyToken authenticated with %q, want the new secret", seen)
	}
}

// ─── the rest of the wiring ──────────────────────────────────────────────────

func TestRotateDeps_UnknownOrTokenlessHostIsNamed(t *testing.T) {
	w, _, _ := wiringWithAPI(rotateAPI{})
	w.hosts["notoken.example.com"] = &config.HostConfig{}
	d := w.deps(context.Background())

	if _, err := d.GetSelf(context.Background(), "absent.example.com"); err == nil ||
		!strings.Contains(err.Error(), "absent.example.com") {
		t.Fatalf("want an error naming the absent host, got %v", err)
	}
	if _, err := d.GetSelf(context.Background(), "notoken.example.com"); err == nil ||
		!strings.Contains(err.Error(), "no token") {
		t.Fatalf("want an error about the missing token, got %v", err)
	}
}

func TestRotateDeps_JournalRecordsIdentityNotValue(t *testing.T) {
	w, _, j := wiringWithAPI(rotateAPI{})
	if err := w.deps(context.Background()).Journal(testHost, 4242, true); err != nil {
		t.Fatalf("Journal: %v", err)
	}
	if len(j.entries) != 1 {
		t.Fatalf("recorded %d entries, want 1", len(j.entries))
	}
	e := j.entries[0]
	if e.Op != journal.OpRotate || e.Entity != journal.EntityToken {
		t.Fatalf("entry = %s/%s, want rotate/token", e.Op, e.Entity)
	}
	if e.Host != testHost || e.EntityID != 4242 || e.Outcome != journal.OutcomeOK {
		t.Fatalf("entry = %+v", e)
	}
	if strings.Contains(e.Detail, testNewSecret) || strings.Contains(e.Detail, testOldToken) {
		t.Fatalf("journal detail carries a token: %q", e.Detail)
	}
}

func TestRotateDeps_JournalMarksAFailedRotation(t *testing.T) {
	w, _, j := wiringWithAPI(rotateAPI{})
	if err := w.deps(context.Background()).Journal(testHost, 9, false); err != nil {
		t.Fatalf("Journal: %v", err)
	}
	if j.entries[0].Outcome != journal.OutcomeErr {
		t.Fatalf("outcome = %q, want err", j.entries[0].Outcome)
	}
}

func TestRotateDeps_StoreAndLastRotatedCloseOverTheRunContext(t *testing.T) {
	w, st, _ := wiringWithAPI(rotateAPI{})
	when := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	st.last, st.found = when.AddDate(0, 0, -40), true

	ctx := context.WithValue(context.Background(), probeCtxKey, "yes")
	d := w.deps(ctx)

	if err := d.Store(testHost, when); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if st.setHost != testHost || !st.setAt.Equal(when) {
		t.Fatalf("Store passed %q/%v", st.setHost, st.setAt)
	}
	// The state API takes ctx first and Deps.Store does not, so the run's ctx
	// has to arrive by closure. If it did not, cancellation would never reach
	// the database.
	if !st.setCtxOK {
		t.Fatal("Store did not pass the run's context through to the state store")
	}
	last, ok, err := d.LastRotated(testHost)
	if err != nil || !ok || !last.Equal(when.AddDate(0, 0, -40)) {
		t.Fatalf("LastRotated = %v, %v, %v", last, ok, err)
	}
}
