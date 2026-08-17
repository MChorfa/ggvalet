package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/MChorfa/ggvalet/internal/rotation"
)

// rotateAPI is the remote surface a rotation needs, held as function fields so
// a test can exercise the wiring — in particular its redaction — without a
// network listener.
type rotateAPI struct {
	getSelf func(ctx context.Context, baseURL, token string, skipTLS bool) (*rotation.TokenInfo, error)
	rotate  func(ctx context.Context, baseURL, token, expiresAt string, skipTLS bool) (*rotation.TokenInfo, string, error)
}

func liveRotateAPI() rotateAPI {
	return rotateAPI{getSelf: rotation.GetSelfToken, rotate: rotation.RotateSelfToken}
}

// rotationStore is the slice of state.Store a rotation uses.
type rotationStore interface {
	SetLastRotated(ctx context.Context, host string, at time.Time) error
	LastRotated(ctx context.Context, host string) (time.Time, bool, error)
}

// journalRecorder is the slice of journal.Journal a rotation uses.
type journalRecorder interface {
	Record(e journal.Entry) error
}

// rotateWiring binds the rotation state machine's Deps to the real world: the
// glab config supplies each host's base URL, current token and TLS setting.
type rotateWiring struct {
	api   rotateAPI
	hosts map[string]*config.HostConfig
	store rotationStore
	jrnl  journalRecorder
	warn  func(format string, args ...any)
}

// hostConfig resolves a host named in rotation.yaml against the glab config.
// Both failures are operator-fixable and are named as such: rotation.yaml is
// edited by hand and drifts from the glab config.
func (w *rotateWiring) hostConfig(host string) (*config.HostConfig, error) {
	hc, ok := w.hosts[host]
	if !ok || hc == nil {
		return nil, fmt.Errorf("host %s is listed in rotation.yaml but not in the glab config", host)
	}
	if strings.TrimSpace(hc.Token) == "" {
		return nil, fmt.Errorf("host %s has no token in the glab config; run `glab auth login` first", host)
	}
	return hc, nil
}

// deps binds the wiring to one run's context.
//
// The state machine's Deps deliberately take no ctx (it keeps fault-injection
// fakes trivial), while the state API takes one first — so the run's ctx
// arrives here by closure.
func (w *rotateWiring) deps(ctx context.Context) rotation.Deps {
	return rotation.Deps{
		Now: time.Now,

		GetSelf: func(ctx context.Context, host string) (*rotation.TokenInfo, error) {
			hc, err := w.hostConfig(host)
			if err != nil {
				return nil, err
			}
			info, err := w.api.getSelf(ctx, hc.APIURL(host), hc.Token, hc.SkipTLS())
			return info, redactSecrets(err, hc.Token)
		},

		Rotate: func(ctx context.Context, host, expiresAt string) (*rotation.TokenInfo, string, error) {
			hc, err := w.hostConfig(host)
			if err != nil {
				return nil, "", err
			}
			info, secret, err := w.api.rotate(ctx, hc.APIURL(host), hc.Token, expiresAt, hc.SkipTLS())
			if err != nil {
				// The one call holding both values: it authenticates with the
				// configured token and mints the replacement. Redact both, even
				// though a failing rotate returns an empty secret today.
				return nil, "", redactSecrets(err, hc.Token, secret)
			}
			return info, secret, nil
		},

		// VerifyToken is the only Deps field handed the new secret, and the
		// state machine wraps whatever it returns — so a leak here reaches
		// stderr. Every error out of it goes through redaction.
		VerifyToken: func(ctx context.Context, host, token string) error {
			hc, err := w.hostConfig(host)
			if err != nil {
				return err
			}
			if _, err := w.api.getSelf(ctx, hc.APIURL(host), token, hc.SkipTLS()); err != nil {
				return redactSecrets(
					fmt.Errorf("the new token for %s was not accepted by the host: %w", host, err),
					token, hc.Token)
			}
			return nil
		},

		// SetHostToken is handed the new secret and its errors reach stderr
		// through the state machine's `commit %s: %w`. Every return it has today
		// carries a path or a host name only (traced at review time); the
		// wrapper is what keeps that true if one of them ever grows the value.
		SetToken: func(path, host, token string) error {
			return redactSecrets(rotation.SetHostToken(path, host, token), token)
		},
		// Digest takes a path and returns a hash — no secret crosses this seam,
		// so it is wired raw.
		Digest: rotation.FileDigest,

		Journal: func(host string, tokenID int, ok bool) error {
			e := journal.Entry{
				Host:     host,
				Op:       journal.OpRotate,
				Entity:   journal.EntityToken,
				EntityID: tokenID,
				Outcome:  journal.OutcomeOK,
				Detail:   "personal access token rotated",
			}
			if !ok {
				e.Outcome = journal.OutcomeErr
				e.Detail = "personal access token rotation failed"
			}
			return w.jrnl.Record(e)
		},

		Store: func(host string, at time.Time) error {
			return w.store.SetLastRotated(ctx, host, at)
		},
		LastRotated: func(host string) (time.Time, bool, error) {
			return w.store.LastRotated(ctx, host)
		},

		Warn: w.warn,
	}
}

// redactSecrets replaces every occurrence of the given secrets in err's message.
//
// This is the last point at which a token value can enter an error string: the
// rotation state machine wraps whatever these functions return, and that wrap
// is printed. The wrapping is deliberately flattened — a secret can sit
// anywhere in the chain, and nothing in this command matches on error identity.
func redactSecrets(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, s := range secrets {
		if s == "" {
			continue // ReplaceAll with "" would splice a marker between every rune
		}
		msg = strings.ReplaceAll(msg, s, "[redacted]")
	}
	if msg == err.Error() {
		return err
	}
	return errors.New(msg)
}
