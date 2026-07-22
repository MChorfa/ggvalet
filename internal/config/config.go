// Package config loads runtime configuration for ggvalet.
//
// Host selection order (first wins):
//  1. --host flag
//  2. GLVALET_HOST env var
//  3. top-level "host:" field in glab config (~/.config/glab-cli/config.yml)
//  4. GLVALET_GITLAB_URL env var (stripped of scheme → hostname)
//  5. First host alphabetically in the hosts map
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ─── Types ────────────────────────────────────────────────────────────────────

// HostConfig mirrors the per-host block in glab's config.yml exactly.
// Unknown fields are silently ignored (container_registry_domains, etc.).
type HostConfig struct {
	Token       string `yaml:"token"`
	User        string `yaml:"user"`
	GitProtocol string `yaml:"git_protocol"`
	APIProtocol string `yaml:"api_protocol"`
	// api_host may contain a path segment: "sc01-trt.thales-systems.ca/gitlab"
	APIHost string `yaml:"api_host"`
	// glab stores this as the string "true" / "false", not a YAML boolean.
	SkipTLSVerify string `yaml:"skip_tls_verify"`
}

// SkipTLS returns true when skip_tls_verify is set to "true" (case-insensitive).
func (h *HostConfig) SkipTLS() bool {
	return strings.EqualFold(strings.TrimSpace(h.SkipTLSVerify), "true")
}

// APIURL returns the base URL for this host's REST API.
// api_host is used verbatim when set (handles path-bearing values like
// "sc01-trt.thales-systems.ca/gitlab" → "https://sc01-trt.thales-systems.ca/gitlab").
func (h *HostConfig) APIURL(hostname string) string {
	proto := "https"
	if strings.EqualFold(h.APIProtocol, "http") {
		proto = "http"
	}
	target := hostname
	if h.APIHost != "" {
		target = h.APIHost
	}
	// Ensure no double-scheme if target already contains "://"
	if strings.Contains(target, "://") {
		return target
	}
	return proto + "://" + target
}

// glabFileConfig is the exact shape of ~/.config/glab-cli/config.yml.
type glabFileConfig struct {
	// Top-level "host:" is the user-selected default instance in glab.
	DefaultHost string                 `yaml:"host"`
	Hosts       map[string]*HostConfig `yaml:"hosts"`
}

// Config is the resolved runtime configuration passed to the client.
type Config struct {
	Host      string // active hostname key, e.g. "gitlab.thalesdigital.io"
	GitLabURL string // derived API base URL
	Token     string
	User      string
	SkipTLS   bool // from active host's skip_tls_verify

	// Provider names the selected provider implementation: "gitlab" or "github".
	// Empty defaults to gitlab. Set from GLVALET_PROVIDER.
	Provider string

	// GitHubURL is the GitHub API base URL. Empty means use api.github.com.
	// Set to a GitHub Enterprise Server URL (e.g. "https://github.example.com/api/v3")
	// when targeting an enterprise instance. Read from GLVALET_GITHUB_URL.
	GitHubURL string
	// GitHubToken is the personal access token for the GitHub provider.
	// Read from GLVALET_GITHUB_TOKEN. Falls back to Token when empty.
	GitHubToken string

	JournalPath    string
	CachePath      string
	StatePath      string
	DefaultProject string
	DefaultGroup   string

	// All hosts loaded from glab config — used by `ggvalet hosts` and flag validation.
	Hosts map[string]*HostConfig
}

// ─── Options ──────────────────────────────────────────────────────────────────

// Options carries values set by CLI flags before Load is called.
type Options struct {
	HostFlag string // value of --host / -H flag; empty if not provided
}

// Provider names used by GLVALET_PROVIDER and Config.Provider.
const (
	providerGitLab = "gitlab"
	providerGitHub = "github"
)

// providerFromEnv returns the trimmed, lower-cased GLVALET_PROVIDER value.
// Empty defaults are handled by callers; this only canonicalizes.
func providerFromEnv() string {
	return strings.TrimSpace(strings.ToLower(os.Getenv("GLVALET_PROVIDER")))
}

// loadGitHubConfig builds a Config for the GitHub provider when no GitLab host
// is required. It still carries the Hosts map so `ggvalet hosts` can display any
// configured glab instances alongside the GitHub selection.
func loadGitHubConfig(hosts map[string]*HostConfig, fallbackToken string) (*Config, error) {
	githubToken := firstNonEmpty(os.Getenv("GLVALET_GITHUB_TOKEN"), fallbackToken)
	if githubToken == "" {
		return nil, fmt.Errorf(
			"no GitHub token — set GLVALET_GITHUB_TOKEN or GLVALET_TOKEN",
		)
	}

	githubURL := os.Getenv("GLVALET_GITHUB_URL")
	host := "github.com"
	if githubURL != "" {
		if u, err := url.Parse(githubURL); err == nil && u.Host != "" {
			host = u.Host
		}
	}

	return &Config{
		Host:        host,
		Token:       githubToken,
		User:        "",
		SkipTLS:     false,
		Provider:    providerGitHub,
		GitHubURL:   githubURL,
		GitHubToken: githubToken,

		JournalPath:    journalPath(),
		CachePath:      cachePath(),
		StatePath:      statePath(),
		DefaultProject: os.Getenv("GLVALET_DEFAULT_PROJECT"),
		DefaultGroup:   os.Getenv("GLVALET_DEFAULT_GROUP"),
		Hosts:          hosts,
	}, nil
}

// ─── Loader ───────────────────────────────────────────────────────────────────

// Load resolves Config from glab config + env vars + flags.
func Load(opts Options) (*Config, error) {
	hosts, glabDefault, err := loadGlabHosts()
	if err != nil {
		hosts = make(map[string]*HostConfig)
	}

	// Merge a token-only env override so it appears in Hosts.
	envURL := os.Getenv("GLVALET_GITLAB_URL")
	envToken := os.Getenv("GLVALET_TOKEN")
	if envToken != "" && envURL != "" {
		key := stripScheme(envURL)
		if _, exists := hosts[key]; !exists {
			hosts[key] = &HostConfig{Token: envToken, APIProtocol: "https"}
		}
	}

	provider := providerFromEnv()
	if provider == "" {
		provider = providerGitLab
	}

	// ── GitHub-only mode ───────────────────────────────────────────────────────
	// When the user explicitly selects the GitHub provider, we do not require a
	// GitLab host in glab config. Tokens and URLs come from GLVALET_GITHUB_*
	// env vars (falling back to GLVALET_TOKEN for backward compatibility).
	if provider == providerGitHub {
		return loadGitHubConfig(hosts, envToken)
	}

	if len(hosts) == 0 {
		return nil, fmt.Errorf(
			"no GitLab hosts found — run `glab auth login` or set GLVALET_TOKEN + GLVALET_GITLAB_URL",
		)
	}

	// ── Select active host (first match wins) ─────────────────────────────────
	active := firstNonEmpty(
		opts.HostFlag,             // 1. --host flag
		os.Getenv("GLVALET_HOST"), // 2. env override
		glabDefault,               // 3. glab top-level "host:" field
		stripScheme(envURL),       // 4. GLVALET_GITLAB_URL env
	)
	if active == "" {
		active = sortedKeys(hosts)[0] // 5. alphabetical fallback
	}

	hcfg, ok := hosts[active]
	if !ok {
		return nil, fmt.Errorf(
			"host %q not in glab config — available: %s\n"+
				"  hint: run `glab auth login --hostname %s`",
			active, strings.Join(sortedKeys(hosts), ", "), active,
		)
	}

	// Allow GLVALET_TOKEN to backfill a host whose token is empty in the config
	// (e.g. when the config file shows an empty token field).
	token := hcfg.Token
	if token == "" {
		token = envToken
	}
	if token == "" {
		return nil, fmt.Errorf(
			"no token for host %q\n"+
				"  hint: run `glab auth login --hostname %s` or set GLVALET_TOKEN",
			active, active,
		)
	}

	return &Config{
		Host:           active,
		GitLabURL:      hcfg.APIURL(active),
		Token:          token,
		User:           hcfg.User,
		SkipTLS:        hcfg.SkipTLS(),
		Provider:       provider,
		JournalPath:    journalPath(),
		CachePath:      cachePath(),
		StatePath:      statePath(),
		DefaultProject: os.Getenv("GLVALET_DEFAULT_PROJECT"),
		DefaultGroup:   os.Getenv("GLVALET_DEFAULT_GROUP"),
		Hosts:          hosts,
	}, nil
}

// ForHost constructs a Config for a specific hostname from an already-loaded
// hosts map. Used by sync commands that need two independent clients.
func ForHost(hosts map[string]*HostConfig, hostname, journalPath string) (*Config, error) {
	hcfg, ok := hosts[hostname]
	if !ok {
		return nil, fmt.Errorf(
			"host %q not in glab config — available: %s",
			hostname, strings.Join(sortedKeys(hosts), ", "),
		)
	}
	token := hcfg.Token
	if token == "" {
		token = os.Getenv("GLVALET_TOKEN")
	}
	if token == "" {
		return nil, fmt.Errorf("no token for host %q — run `glab auth login --hostname %s`",
			hostname, hostname)
	}
	return &Config{
		Host:        hostname,
		GitLabURL:   hcfg.APIURL(hostname),
		Token:       token,
		User:        hcfg.User,
		SkipTLS:     hcfg.SkipTLS(),
		Provider:    providerGitLab,
		JournalPath: journalPath,
		CachePath:   cachePath(),
		StatePath:   statePath(),
		Hosts:       hosts,
	}, nil
}

// ─── glab config parser ───────────────────────────────────────────────────────

// loadGlabHosts reads every glab config location in priority order, merges all
// host entries (first-wins), and returns the top-level default hostname.
func loadGlabHosts() (map[string]*HostConfig, string, error) {
	merged := make(map[string]*HostConfig)
	defaultHost := ""
	var lastErr error

	for _, path := range glabConfigPaths() {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			lastErr = err
			continue
		}

		var fc glabFileConfig
		if err := yaml.Unmarshal(data, &fc); err != nil {
			lastErr = fmt.Errorf("parse %s: %w", path, err)
			continue
		}

		// Capture default host from first file that declares it.
		if defaultHost == "" && fc.DefaultHost != "" {
			defaultHost = fc.DefaultHost
		}

		for hostname, hc := range fc.Hosts {
			if _, exists := merged[hostname]; !exists {
				merged[hostname] = hc
			}
		}
	}

	// Legacy: older glab versions stored hosts in a separate hosts.yml.
	for _, dir := range glabConfigDirs() {
		data, err := os.ReadFile(filepath.Join(dir, "hosts.yml"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			continue
		}
		var hostsOnly map[string]*HostConfig
		if err := yaml.Unmarshal(data, &hostsOnly); err != nil {
			continue
		}
		for hostname, hc := range hostsOnly {
			if _, exists := merged[hostname]; !exists {
				merged[hostname] = hc
			}
		}
	}

	if len(merged) == 0 && lastErr != nil {
		return nil, "", lastErr
	}
	return merged, defaultHost, nil
}

// glabConfigPaths returns candidate config.yml paths in priority order.
func glabConfigPaths() []string {
	var paths []string
	for _, dir := range glabConfigDirs() {
		paths = append(paths, filepath.Join(dir, "config.yml"))
	}
	return paths
}

// glabConfigDirs returns directories to search, in priority order.
func glabConfigDirs() []string {
	var dirs []string

	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		dirs = append(dirs, filepath.Join(xdg, "glab-cli"))
	}
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("AppData"); appData != "" {
			dirs = append(dirs, filepath.Join(appData, "glab-cli"))
		}
	}

	if home, _ := os.UserHomeDir(); home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".config", "glab-cli"), // Linux/macOS default
			filepath.Join(home, ".glab-cli"),           // older glab
		)
	}

	// Escape hatch: GLVALET_GLAB_CONFIG points to the config file directly.
	if custom := os.Getenv("GLVALET_GLAB_CONFIG"); custom != "" {
		dirs = append([]string{filepath.Dir(custom)}, dirs...)
	}

	return dirs
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func journalPath() string {
	if p := os.Getenv("GLVALET_JOURNAL"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ggvalet", "journal.jsonl")
}

func cachePath() string {
	if p := os.Getenv("GLVALET_CACHE"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ggvalet", "cache")
}

func statePath() string {
	if p := os.Getenv("GLVALET_STATE"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ggvalet", "state.db")
}

// stripScheme removes "https://" or "http://" prefix.
func stripScheme(u string) string {
	for _, pfx := range []string{"https://", "http://"} {
		if strings.HasPrefix(u, pfx) {
			return u[len(pfx):]
		}
	}
	return u
}

// firstNonEmpty returns the first non-empty string from the list.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func sortedKeys(m map[string]*HostConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
