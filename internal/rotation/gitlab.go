package rotation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MChorfa/ggvalet/internal/observed"
)

// TokenInfo describes a GitLab personal access token as returned by the
// self and self/rotate endpoints.
type TokenInfo struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at"`
	Active    bool     `json:"active"`
	Revoked   bool     `json:"revoked"`
	Token     string   `json:"token"` // only populated by the rotate response
}

// HasScope reports whether the token carries the named scope.
func (t *TokenInfo) HasScope(s string) bool {
	for _, got := range t.Scopes {
		if got == s {
			return true
		}
	}
	return false
}

func httpClient(skipTLS bool) *http.Client {
	// Single TLS-skip call site lives in internal/observed — see Step 0.
	return &http.Client{Timeout: 30 * time.Second, Transport: observed.BaseTransport(skipTLS)}
}

func doJSON(ctx context.Context, method, endpoint, token string, skipTLS bool) (*TokenInfo, error) {
	return doJSONWithClient(ctx, method, endpoint, token, httpClient(skipTLS))
}

// doJSONWithClient takes an *http.Client directly so tests can inject a fake
// RoundTripper and assert request shape without a real network listener.
func doJSONWithClient(ctx context.Context, method, endpoint, token string, hc *http.Client) (*TokenInfo, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s %s: HTTP %d", method, endpoint, resp.StatusCode)
	}
	var info TokenInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	return &info, nil
}

func apiBase(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/api/v4"
}

// GetSelfToken fetches metadata for the token that authenticates the request.
func GetSelfToken(ctx context.Context, baseURL, token string, skipTLS bool) (*TokenInfo, error) {
	return doJSON(ctx, http.MethodGet, apiBase(baseURL)+"/personal_access_tokens/self", token, skipTLS)
}

// RotateSelfToken revokes the current token server-side and returns its
// replacement. The secret is returned exactly once — the caller must persist
// it before doing anything else.
func RotateSelfToken(ctx context.Context, baseURL, token, expiresAt string, skipTLS bool) (*TokenInfo, string, error) {
	endpoint := apiBase(baseURL) + "/personal_access_tokens/self/rotate"
	if expiresAt != "" {
		endpoint += "?" + url.Values{"expires_at": {expiresAt}}.Encode()
	}
	info, err := doJSON(ctx, http.MethodPost, endpoint, token, skipTLS)
	if err != nil {
		return nil, "", err
	}
	if info.Token == "" {
		return nil, "", fmt.Errorf("rotate succeeded but response carried no token")
	}
	return info, info.Token, nil
}
