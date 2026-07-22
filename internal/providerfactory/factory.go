// Package providerfactory selects a provider.Provider implementation based on GLVALET_PROVIDER.
package providerfactory

import (
	"fmt"
	"os"
	"strings"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/MChorfa/ggvalet/internal/provider/github"
	"github.com/MChorfa/ggvalet/internal/provider/gitlab"
)

// NewFromConfig constructs a provider.Provider based on cfg.Provider, falling
// back to the GLVALET_PROVIDER environment variable.
// "" or "gitlab" → GitLab provider.
// "github" → GitHub provider (propagates github.ErrFeatureDisabled if disabled).
// Any other value → error with helpful message.
func NewFromConfig(cfg *config.Config) (provider.Provider, error) {
	v := strings.TrimSpace(strings.ToLower(cfg.Provider))
	if v == "" {
		v = strings.TrimSpace(strings.ToLower(os.Getenv("GLVALET_PROVIDER")))
	}

	switch v {
	case "", "gitlab":
		return gitlab.New(cfg)
	case "github":
		return github.New(cfg)
	default:
		return nil, fmt.Errorf("providerfactory: unknown provider=%q (want gitlab|github)", v)
	}
}
