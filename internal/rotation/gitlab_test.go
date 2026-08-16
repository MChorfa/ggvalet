package rotation

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeRoundTripper answers requests in-process, without opening a socket —
// this sandbox denies bind(2), which breaks httptest.NewServer (see the
// httptest-backed tests below). It lets doJSONWithClient's request-shaping
// logic (path, headers, query params, decode) be validated even where a real
// listener cannot be opened.
type fakeRoundTripper struct {
	gotReq *http.Request
	status int
	body   string
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
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
	var gotExpiry, gotAuth, gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("PRIVATE-TOKEN")
		gotExpiry = r.URL.Query().Get("expires_at")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":51630,"name":"t","scopes":["api","self_rotate"],
			"expires_at":"2026-10-20","active":true,"revoked":false,"token":"glpat-NEW"}`))
	}))
	defer srv.Close()

	info, secret, err := RotateSelfToken(context.Background(), srv.URL, "glpat-OLD", "2026-10-20", false)
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
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotExpiry != "2026-10-20" {
		t.Errorf("expires_at not sent, got %q", gotExpiry)
	}
	if gotAuth != "glpat-OLD" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if gotPath != "/api/v4/personal_access_tokens/self/rotate" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestRotateSelfToken_OmitsExpiryQueryWhenEmpty(t *testing.T) {
	var sawExpiryKey bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawExpiryKey = r.URL.Query()["expires_at"]
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":1,"token":"glpat-NEW"}`))
	}))
	defer srv.Close()

	if _, _, err := RotateSelfToken(context.Background(), srv.URL, "glpat-OLD", "", false); err != nil {
		t.Fatalf("RotateSelfToken: %v", err)
	}
	if sawExpiryKey {
		t.Error("expires_at query param sent despite empty expiresAt")
	}
}

func TestRotateSelfToken_ErrorsWhenResponseCarriesNoToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":1,"name":"t"}`))
	}))
	defer srv.Close()

	if _, _, err := RotateSelfToken(context.Background(), srv.URL, "glpat-OLD", "", false); err == nil {
		t.Fatal("expected error when rotate response carries no token")
	}
}

func TestRotateSelfToken_ErrorsOnMalformedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	if _, _, err := RotateSelfToken(context.Background(), srv.URL, "glpat-OLD", "", false); err == nil {
		t.Fatal("expected decode error on malformed body")
	}
}

func TestGetSelfToken_SurfacesUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"401 Unauthorized"}`))
	}))
	defer srv.Close()

	if _, err := GetSelfToken(context.Background(), srv.URL, "bad", false); err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestGetSelfToken_ReturnsMetadataOnSuccess(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("PRIVATE-TOKEN")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":51630,"name":"t","scopes":["api"],"expires_at":"2026-10-20","active":true,"revoked":false}`))
	}))
	defer srv.Close()

	info, err := GetSelfToken(context.Background(), srv.URL, "glpat-CUR", false)
	if err != nil {
		t.Fatalf("GetSelfToken: %v", err)
	}
	if info.ID != 51630 || info.ExpiresAt != "2026-10-20" || !info.Active || info.Revoked {
		t.Errorf("unexpected info: %+v", info)
	}
	if gotAuth != "glpat-CUR" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if gotPath != "/api/v4/personal_access_tokens/self" {
		t.Errorf("path = %q", gotPath)
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GetSelfToken(ctx, srv.URL, "tok", false); err == nil {
		t.Fatal("expected error on canceled context")
	} else if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("unexpected error: %v", err)
	}
}
