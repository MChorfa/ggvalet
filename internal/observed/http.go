package observed

import (
	"context"
	"fmt"
	"net/http"

	"github.com/MChorfa/ggvalet/internal/state"
)

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
