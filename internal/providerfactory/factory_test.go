package providerfactory

import (
	"errors"
	"strings"
	"testing"

	"github.com/ckodex/gitlabvalet/internal/config"
	"github.com/ckodex/gitlabvalet/internal/provider"
	"github.com/ckodex/gitlabvalet/internal/provider/github"
)

func TestNewFromConfig_DefaultIsGitLab(t *testing.T) {
	t.Setenv("GLVALET_PROVIDER", "")

	cfg := &config.Config{
		Host:      "gitlab.example.com",
		GitLabURL: "https://gitlab.example.com",
		Token:     "test-token",
	}

	p, err := NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}

	if got := p.Kind(); got != provider.KindGitLab {
		t.Errorf("Kind() = %q, want %q", got, provider.KindGitLab)
	}
}

func TestNewFromConfig_ExplicitGitLab(t *testing.T) {
	t.Setenv("GLVALET_PROVIDER", "gitlab")

	cfg := &config.Config{
		Host:      "gitlab.example.com",
		GitLabURL: "https://gitlab.example.com",
		Token:     "test-token",
	}

	p, err := NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}

	if got := p.Kind(); got != provider.KindGitLab {
		t.Errorf("Kind() = %q, want %q", got, provider.KindGitLab)
	}
}

func TestNewFromConfig_GitHubEnabled(t *testing.T) {
	t.Setenv("GLVALET_PROVIDER", "github")
	t.Setenv("GLVALET_GITHUB_ENABLED", "true")

	cfg := &config.Config{
		Host:      "github.com",
		GitLabURL: "https://gitlab.example.com",
		Token:     "test-token",
	}

	p, err := NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}

	if got := p.Kind(); got != provider.KindGitHub {
		t.Errorf("Kind() = %q, want %q", got, provider.KindGitHub)
	}
}

func TestNewFromConfig_GitHubDisabled(t *testing.T) {
	t.Setenv("GLVALET_PROVIDER", "github")
	t.Setenv("GLVALET_GITHUB_ENABLED", "")

	cfg := &config.Config{
		Host:      "github.com",
		GitLabURL: "https://gitlab.example.com",
		Token:     "test-token",
	}

	_, err := NewFromConfig(cfg)
	if err == nil {
		t.Fatal("NewFromConfig: expected error, got nil")
	}

	if !errors.Is(err, github.ErrFeatureDisabled) {
		t.Errorf("errors.Is(err, github.ErrFeatureDisabled) = false, want true; err = %v", err)
	}
}

func TestNewFromConfig_UnknownProvider(t *testing.T) {
	t.Setenv("GLVALET_PROVIDER", "bitbucket")

	cfg := &config.Config{
		Host:      "bitbucket.example.com",
		GitLabURL: "https://gitlab.example.com",
		Token:     "test-token",
	}

	_, err := NewFromConfig(cfg)
	if err == nil {
		t.Fatal("NewFromConfig: expected error, got nil")
	}

	if got := err.Error(); !strings.Contains(got, "bitbucket") {
		t.Errorf("error message = %q, want to contain %q", got, "bitbucket")
	}
}
