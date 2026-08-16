package observed

import "testing"

func TestBaseTransport_VerifiesByDefault(t *testing.T) {
	if tr := BaseTransport(false); tr.TLSClientConfig != nil {
		t.Error("TLS verification must be on unless skip_tls_verify is set")
	}
	if tr := BaseTransport(true); tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("skip_tls_verify must be honoured when requested")
	}
}
