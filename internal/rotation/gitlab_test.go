package rotation

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// fakeRoundTripper answers requests in-process, without opening a socket —
// this sandbox denies bind(2), so httptest.NewServer cannot be used here. It
// lets the request-shaping logic (path, headers, query params, decode) be
// validated against the real exported calls.
type fakeRoundTripper struct {
	gotReq *http.Request
	status int
	body   string
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Honour cancellation the way a real transport does, so context
	// propagation stays testable.
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	f.gotReq = req
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(f.body)),
	}, nil
}

// testBaseURL stands in for a GitLab instance. Nothing dials it.
const testBaseURL = "https://gitlab.example.com"

// serveFake routes the exported API calls through frt for the duration of the
// test, restoring the real client factory afterwards.
func serveFake(t *testing.T, frt *fakeRoundTripper) {
	t.Helper()
	prev := newHTTPClient
	newHTTPClient = func(bool) *http.Client { return &http.Client{Transport: frt} }
	t.Cleanup(func() { newHTTPClient = prev })
}

func TestDoJSONWithClient_FormsRequestAndDecodes(t *testing.T) {
	frt := &fakeRoundTripper{body: `{"id":51630,"name":"t","scopes":["api"],"expires_at":"2026-10-20","active":true,"revoked":false}`}
	hc := &http.Client{Transport: frt}

	info, err := doJSONWithClient(context.Background(), http.MethodGet,
		"https://gitlab.example.com/api/v4/personal_access_tokens/self", "glpat-CUR", hc)
	if err != nil {
		t.Fatalf("doJSONWithClient: %v", err)
	}
	if frt.gotReq.Method != http.MethodGet {
		t.Errorf("method = %q, want GET", frt.gotReq.Method)
	}
	if frt.gotReq.URL.Path != "/api/v4/personal_access_tokens/self" {
		t.Errorf("path = %q", frt.gotReq.URL.Path)
	}
	if got := frt.gotReq.Header.Get("PRIVATE-TOKEN"); got != "glpat-CUR" {
		t.Errorf("PRIVATE-TOKEN header = %q, want glpat-CUR", got)
	}
	if info.ID != 51630 || info.ExpiresAt != "2026-10-20" || !info.Active || info.Revoked {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestDoJSONWithClient_RotateSendsExpiryQueryParam(t *testing.T) {
	frt := &fakeRoundTripper{body: `{"id":51630,"token":"glpat-NEW"}`}
	hc := &http.Client{Transport: frt}

	endpoint := "https://gitlab.example.com/api/v4/personal_access_tokens/self/rotate?expires_at=2026-10-20"
	info, err := doJSONWithClient(context.Background(), http.MethodPost, endpoint, "glpat-OLD", hc)
	if err != nil {
		t.Fatalf("doJSONWithClient: %v", err)
	}
	if frt.gotReq.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", frt.gotReq.Method)
	}
	if frt.gotReq.URL.Path != "/api/v4/personal_access_tokens/self/rotate" {
		t.Errorf("path = %q", frt.gotReq.URL.Path)
	}
	if got := frt.gotReq.URL.Query().Get("expires_at"); got != "2026-10-20" {
		t.Errorf("expires_at = %q, want 2026-10-20", got)
	}
	if got := frt.gotReq.Header.Get("PRIVATE-TOKEN"); got != "glpat-OLD" {
		t.Errorf("PRIVATE-TOKEN header = %q, want glpat-OLD", got)
	}
	if info.Token != "glpat-NEW" {
		t.Errorf("token = %q, want glpat-NEW", info.Token)
	}
}

func TestDoJSONWithClient_NonSuccessStatusIsError(t *testing.T) {
	frt := &fakeRoundTripper{status: http.StatusUnauthorized, body: `{"message":"401 Unauthorized"}`}
	hc := &http.Client{Transport: frt}

	if _, err := doJSONWithClient(context.Background(), http.MethodGet, "https://gitlab.example.com/api/v4/personal_access_tokens/self", "bad", hc); err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestDoJSONWithClient_MalformedBodyIsDecodeError(t *testing.T) {
	frt := &fakeRoundTripper{body: `not-json`}
	hc := &http.Client{Transport: frt}

	if _, err := doJSONWithClient(context.Background(), http.MethodGet, "https://gitlab.example.com/api/v4/personal_access_tokens/self", "tok", hc); err == nil {
		t.Fatal("expected decode error on malformed body")
	}
}

func TestHTTPClient_SetsTimeoutAndTransport(t *testing.T) {
	hc := httpClient(true)
	if hc.Timeout != 30_000_000_000 {
		t.Errorf("timeout = %v, want 30s", hc.Timeout)
	}
	tr, ok := hc.Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("httpClient(true) did not wire skip-TLS transport from observed.BaseTransport")
	}
}

func TestRotateSelfToken_SendsExpiryAndReturnsSecret(t *testing.T) {
	frt := &fakeRoundTripper{body: `{"id":51630,"name":"t","scopes":["api","self_rotate"],
		"expires_at":"2026-10-20","active":true,"revoked":false,"token":"glpat-NEW"}`}
	serveFake(t, frt)

	info, secret, err := RotateSelfToken(context.Background(), testBaseURL, "glpat-OLD", "2026-10-20", false)
	if err != nil {
		t.Fatalf("RotateSelfToken: %v", err)
	}
	if secret != "glpat-NEW" {
		t.Errorf("secret = %q, want glpat-NEW", secret)
	}
	if info.ID != 51630 {
		t.Errorf("id = %d, want 51630", info.ID)
	}
	if !info.HasScope("self_rotate") {
		t.Errorf("scopes = %v, want self_rotate present", info.Scopes)
	}
	if frt.gotReq.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", frt.gotReq.Method)
	}
	if got := frt.gotReq.URL.Query().Get("expires_at"); got != "2026-10-20" {
		t.Errorf("expires_at not sent, got %q", got)
	}
	if got := frt.gotReq.Header.Get("PRIVATE-TOKEN"); got != "glpat-OLD" {
		t.Errorf("auth header = %q", got)
	}
	if frt.gotReq.URL.Path != "/api/v4/personal_access_tokens/self/rotate" {
		t.Errorf("path = %q", frt.gotReq.URL.Path)
	}
}

func TestRotateSelfToken_OmitsExpiryQueryWhenEmpty(t *testing.T) {
	frt := &fakeRoundTripper{body: `{"id":1,"token":"glpat-NEW"}`}
	serveFake(t, frt)

	if _, _, err := RotateSelfToken(context.Background(), testBaseURL, "glpat-OLD", "", false); err != nil {
		t.Fatalf("RotateSelfToken: %v", err)
	}
	if _, sawExpiryKey := frt.gotReq.URL.Query()["expires_at"]; sawExpiryKey {
		t.Error("expires_at query param sent despite empty expiresAt")
	}
}

func TestRotateSelfToken_ErrorsWhenResponseCarriesNoToken(t *testing.T) {
	serveFake(t, &fakeRoundTripper{body: `{"id":1,"name":"t"}`})

	if _, _, err := RotateSelfToken(context.Background(), testBaseURL, "glpat-OLD", "", false); err == nil {
		t.Fatal("expected error when rotate response carries no token")
	}
}

func TestRotateSelfToken_ErrorsOnMalformedBody(t *testing.T) {
	serveFake(t, &fakeRoundTripper{body: `not-json`})

	if _, _, err := RotateSelfToken(context.Background(), testBaseURL, "glpat-OLD", "", false); err == nil {
		t.Fatal("expected decode error on malformed body")
	}
}

func TestGetSelfToken_SurfacesUnauthorized(t *testing.T) {
	serveFake(t, &fakeRoundTripper{status: http.StatusUnauthorized, body: `{"message":"401 Unauthorized"}`})

	if _, err := GetSelfToken(context.Background(), testBaseURL, "bad", false); err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestGetSelfToken_ReturnsMetadataOnSuccess(t *testing.T) {
	frt := &fakeRoundTripper{body: `{"id":51630,"name":"t","scopes":["api"],"expires_at":"2026-10-20","active":true,"revoked":false}`}
	serveFake(t, frt)

	info, err := GetSelfToken(context.Background(), testBaseURL, "glpat-CUR", false)
	if err != nil {
		t.Fatalf("GetSelfToken: %v", err)
	}
	if info.ID != 51630 || info.ExpiresAt != "2026-10-20" || !info.Active || info.Revoked {
		t.Errorf("unexpected info: %+v", info)
	}
	if got := frt.gotReq.Header.Get("PRIVATE-TOKEN"); got != "glpat-CUR" {
		t.Errorf("auth header = %q", got)
	}
	if frt.gotReq.URL.Path != "/api/v4/personal_access_tokens/self" {
		t.Errorf("path = %q", frt.gotReq.URL.Path)
	}
}

func TestGetSelfToken_ErrorOnUnreachableHost(t *testing.T) {
	if _, err := GetSelfToken(context.Background(), "http://127.0.0.1:0", "tok", false); err == nil {
		t.Fatal("expected error dialing an unreachable host")
	}
}

func TestApiBase_TrimsTrailingSlash(t *testing.T) {
	if got := apiBase("https://gitlab.example.com/"); got != "https://gitlab.example.com/api/v4" {
		t.Errorf("apiBase = %q", got)
	}
	if got := apiBase("https://gitlab.example.com"); got != "https://gitlab.example.com/api/v4" {
		t.Errorf("apiBase = %q", got)
	}
}

func TestTokenInfo_HasScope(t *testing.T) {
	info := &TokenInfo{Scopes: []string{"api", "read_user"}}
	if !info.HasScope("api") {
		t.Error("expected HasScope(api) true")
	}
	if info.HasScope("write_repository") {
		t.Error("expected HasScope(write_repository) false")
	}
}

func TestDoJSON_ContextCanceled(t *testing.T) {
	serveFake(t, &fakeRoundTripper{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GetSelfToken(ctx, testBaseURL, "tok", false); err == nil {
		t.Fatal("expected error on canceled context")
	} else if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("unexpected error: %v", err)
	}
}
