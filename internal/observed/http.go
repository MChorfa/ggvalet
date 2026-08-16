package observed

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/MChorfa/ggvalet/internal/state"
)

// BaseTransport returns the shared outbound transport. It is the ONLY place in
// this repository that may disable TLS verification, and it does so solely to
// mirror glab's per-host skip_tls_verify setting — ggvalet must not be stricter
// than the CLI whose config it reads, or it would fail on hosts glab can reach.
// Any new caller uses this function; nobody writes InsecureSkipVerify again.
func BaseTransport(skipTLS bool) *http.Transport {
	tr := &http.Transport{}
	if skipTLS {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // mirrors glab skip_tls_verify
	}
	return tr
}

type Transport struct {
	Base     http.RoundTripper
	Store    *state.Store
	Provider string
	Host     string
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	op := state.Operation{Provider: t.Provider, Host: t.Host, Action: req.Method,
		Resource: "http", Target: req.URL.Path,
		InputDigest: state.Digest(req.Method + " " + req.URL.Path)}
	id, err := t.Store.Begin(req.Context(), op)
	if err != nil {
		return nil, fmt.Errorf("persist HTTP intent: %w", err)
	}
	resp, callErr := base.RoundTrip(req)
	status, detail := state.StatusSucceeded, ""
	if callErr != nil {
		status, detail = state.StatusFailed, callErr.Error()
	} else if resp.StatusCode >= http.StatusBadRequest {
		status, detail = state.StatusFailed, resp.Status
	}
	if receiptErr := t.Store.Complete(context.WithoutCancel(req.Context()), id, status, detail); receiptErr != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, &ReceiptUncertainError{OperationID: id, Cause: receiptErr}
	}
	return resp, callErr
}
