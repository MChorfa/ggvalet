// Package providerfactory selects a provider.Provider implementation based on GLVALET_PROVIDER.
package providerfactory

import (
	"fmt"
	"os"
	"strings"

	"github.com/ckodex/gitlabvalet/internal/config"
	"github.com/ckodex/gitlabvalet/internal/provider"
	"github.com/ckodex/gitlabvalet/internal/provider/github"
	"github.com/ckodex/gitlabvalet/internal/provider/gitlab"
)

// NewFromConfig constructs a provider.Provider based on GLVALET_PROVIDER.
// "" or "gitlab" → GitLab provider.
// "github" → GitHub provider (propagates github.ErrFeatureDisabled if disabled).
// Any other value → error with helpful message.
func NewFromConfig(cfg *config.Config) (provider.Provider, error) {
	v := os.Getenv("GLVALET_PROVIDER")
	v = strings.TrimSpace(strings.ToLower(v))

	switch v {
	case "", "gitlab":
		return gitlab.New(cfg)
	case "github":
		return github.New(cfg)
	default:
		return nil, fmt.Errorf("providerfactory: unknown GLVALET_PROVIDER=%q (want gitlab|github)", v)
	}
}
