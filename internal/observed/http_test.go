package observed

import (
	"net/http"
	"testing"
)

func TestBaseTransport_VerifiesByDefault(t *testing.T) {
	// http.DefaultTransport.Clone() already carries a non-nil TLSClientConfig
	// (ALPN protocols for HTTP/2), so "verifies by default" means
	// InsecureSkipVerify is false, not that TLSClientConfig is nil.
	if tr := BaseTransport(false); tr.TLSClientConfig != nil && tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("TLS verification must be on unless skip_tls_verify is set")
	}
	if tr := BaseTransport(true); tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("skip_tls_verify must be honoured when requested")
	}
}

// TestBaseTransport_PreservesDefaultTransportSemantics guards against
// regressing to a bare &http.Transport{}, which silently drops
// ProxyFromEnvironment and connection pooling — see the commit that added
// this test for the incident. Without ProxyFromEnvironment, calls to
// corporate GitLab hosts behind an HTTP proxy would fail outright.
func TestBaseTransport_PreservesDefaultTransportSemantics(t *testing.T) {
	for _, skipTLS := range []bool{false, true} {
		tr := BaseTransport(skipTLS)
		if tr.Proxy == nil {
			t.Errorf("skipTLS=%v: Proxy is nil, want ProxyFromEnvironment (or equivalent) from http.DefaultTransport", skipTLS)
		}
		if tr.MaxIdleConns == 0 {
			t.Errorf("skipTLS=%v: MaxIdleConns is 0, want http.DefaultTransport's pooling default", skipTLS)
		}
	}
}

// TestBaseTransport_ClonesIndependently ensures mutating the TLS config of
// one returned transport (as skipTLS=true does) cannot leak into another
// caller's transport via a shared pointer — http.Transport.Clone() deep-clones
// TLSClientConfig, but this pins that behavior against regression as a defect
// class (e.g. a future refactor that skips Clone() and reuses the shared
// http.DefaultTransport.TLSClientConfig pointer directly).
func TestBaseTransport_ClonesIndependently(t *testing.T) {
	insecure := BaseTransport(true)
	secure := BaseTransport(false)
	if secure.TLSClientConfig != nil && secure.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("skipTLS=false transport was mutated by a skipTLS=true call — TLSClientConfig is shared, not cloned")
	}
	if insecure.TLSClientConfig == http.DefaultTransport.(*http.Transport).TLSClientConfig {
		t.Fatal("BaseTransport(true) mutated the shared http.DefaultTransport.TLSClientConfig in place")
	}
}
