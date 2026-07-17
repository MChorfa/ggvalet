package provider_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ckodex/gitlabvalet/internal/provider"
	"github.com/ckodex/gitlabvalet/internal/provider/gitlab"
)

// ─── sentinel ─────────────────────────────────────────────────────────────────

// TestErrUnsupported_IsSentinel verifies that ErrUnsupported satisfies
// errors.Is both directly and when wrapped.
func TestErrUnsupported_IsSentinel(t *testing.T) {
	t.Parallel()

	if !errors.Is(provider.ErrUnsupported, provider.ErrUnsupported) {
		t.Error("errors.Is(ErrUnsupported, ErrUnsupported) = false; want true")
	}

	wrapped := fmt.Errorf("op: %w", provider.ErrUnsupported)
	if !errors.Is(wrapped, provider.ErrUnsupported) {
		t.Error("errors.Is(wrapped ErrUnsupported) = false; want true")
	}
}

// ─── kind string values ───────────────────────────────────────────────────────

// TestKind_StringValues verifies the literal string values for each Kind
// constant — P6 depends on these being stable.
func TestKind_StringValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind provider.Kind
		want string
	}{
		{provider.KindGitLab, "gitlab"},
		{provider.KindGitHub, "github"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			if string(tc.kind) != tc.want {
				t.Errorf("Kind value = %q; want %q", tc.kind, tc.want)
			}
		})
	}
}

// ─── compile-time interface satisfaction ─────────────────────────────────────

// TestProviderInterface_CompileTime proves the GitLab provider satisfies the
// host-neutral provider.Provider interface without needing a stub.
func TestProviderInterface_CompileTime(t *testing.T) {
	t.Parallel()

	// Compile-time check: this line fails to build if Provider interface changes.
	var _ provider.Provider = (*gitlab.GitLab)(nil)
}
