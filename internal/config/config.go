// Package config loads runtime configuration for ggvalet.
//
// Host selection order (first wins):
//  1. --host flag
//  2. GLVALET_HOST env var
//  3. top-level "host:" field in glab config (~/.config/glab-cli/config.yml)
//     or default login in tea config (~/.config/tea/config.yml)
//  4. GLVALET_GITLAB_URL / GLVALET_GITEA_URL env var (stripped of scheme → hostname)
//  5. First host alphabetically in the hosts map
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ─── Types ────────────────────────────────────────────────────────────────────

// HostConfig mirrors the per-host block in glab's config.yml exactly.
// Unknown fields are silently ignored (container_registry_domains, etc.).
// The Provider field is not serialized; it records which provider a host
// belongs to (gitlab or gitea) after loading.
type HostConfig struct {
	Token       string `yaml:"token"`
	User        string `yaml:"user"`
	GitProtocol string `yaml:"git_protocol"`
	APIProtocol string `yaml:"api_protocol"`
	// api_host may contain a path segment: "sc01-trt.thales-systems.ca/gitlab"
	APIHost string `yaml:"api_host"`
	// glab stores this as the string "true" / "false", not a YAML boolean.
	SkipTLSVerify string `yaml:"skip_tls_verify"`
	// Provider records which provider this host belongs to (gitlab or gitea).
	Provider string `yaml:"-"`
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

	// Provider names the selected provider implementation: "gitlab", "github", or "gitea".
	// Empty defaults to gitlab. Set from GLVALET_PROVIDER.
	Provider string

	// GitHubURL is the GitHub API base URL. Empty means use api.github.com.
	// Set to a GitHub Enterprise Server URL (e.g. "https://github.example.com/api/v3")
	// when targeting an enterprise instance. Read from GLVALET_GITHUB_URL.
	GitHubURL string
	// GitHubToken is the personal access token for the GitHub provider.
	// Read from GLVALET_GITHUB_TOKEN. Falls back to Token when empty.
	GitHubToken string

	// GiteaURL is the Gitea API base URL. Set from tea login config when the
	// active provider is "gitea". Falls back to GLVALET_GITEA_URL.
	GiteaURL string

	JournalPath    string
	CachePath      string
	StatePath      string
	DefaultProject string
	DefaultGroup   string

	// All hosts loaded from the active provider's config (glab or tea).
	// Used by `ggvalet hosts` and cross-instance commands like report push.
	Hosts map[string]*HostConfig
}

// ─── Options ──────────────────────────────────────────────────────────────────

// Options carries values set by CLI flags before Load is called.
type Options struct {
	HostFlag string // value of --host / -H flag; empty if not provided
}

// Provider names used by GLVALET_PROVIDER and Config.Provider.
const (
	providerGitLab  = "gitlab"
	providerGitHub  = "github"
	providerGitea   = "gitea"
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

// Load resolves Config from glab/tea config + env vars + flags.
func Load(opts Options) (*Config, error) {
	envToken := os.Getenv("GLVALET_TOKEN")
	provider := providerFromEnv()
	if provider == "" {
		provider = providerGitLab
	}

	switch provider {
	case providerGitHub:
		hosts, _, _ := loadGlabHosts()
		if hosts == nil {
			hosts = make(map[string]*HostConfig)
		}
		return loadGitHubConfig(hosts, envToken)
	case providerGitea:
		return loadGiteaConfig(opts, envToken)
	default:
		return loadGitLabConfig(opts, envToken)
	}
}

// loadGitLabConfig builds a Config for the GitLab provider.
func loadGitLabConfig(opts Options, envToken string) (*Config, error) {
	hosts, glabDefault, err := loadGlabHosts()
	if err != nil {
		hosts = make(map[string]*HostConfig)
	}

	// Merge a token-only env override so it appears in Hosts.
	envURL := os.Getenv("GLVALET_GITLAB_URL")
	if envToken != "" && envURL != "" {
		key := stripScheme(envURL)
		if _, exists := hosts[key]; !exists {
			hosts[key] = &HostConfig{Token: envToken, APIProtocol: "https", Provider: providerGitLab}
		}
	}

	if len(hosts) == 0 {
		return nil, fmt.Errorf(
			"no GitLab hosts found — run `glab auth login` or set GLVALET_TOKEN + GLVALET_GITLAB_URL",
		)
	}

	active := firstNonEmpty(
		opts.HostFlag,
		os.Getenv("GLVALET_HOST"),
		glabDefault,
		stripScheme(envURL),
	)
	if active == "" {
		active = sortedKeys(hosts)[0]
	}

	hcfg, ok := hosts[active]
	if !ok {
		return nil, fmt.Errorf(
			"host %q not in glab config — available: %s\n"+
				"  hint: run `glab auth login --hostname %s`",
			active, strings.Join(sortedKeys(hosts), ", "), active,
		)
	}

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
		Provider:       providerGitLab,
		JournalPath:    journalPath(),
		CachePath:      cachePath(),
		StatePath:      statePath(),
		DefaultProject: os.Getenv("GLVALET_DEFAULT_PROJECT"),
		DefaultGroup:   os.Getenv("GLVALET_DEFAULT_GROUP"),
		Hosts:          hosts,
	}, nil
}

// loadGiteaConfig builds a Config for the Gitea provider from tea logins.
func loadGiteaConfig(opts Options, envToken string) (*Config, error) {
	hosts, teaDefault, err := loadTeaHosts()
	if err != nil {
		hosts = make(map[string]*HostConfig)
	}

	envURL := firstNonEmpty(os.Getenv("GLVALET_GITEA_URL"), os.Getenv("GLVALET_GITLAB_URL"))
	if envToken != "" && envURL != "" {
		key := stripScheme(envURL)
		if _, exists := hosts[key]; !exists {
			hosts[key] = &HostConfig{
				Token:       envToken,
				APIProtocol: "https",
				APIHost:     stripScheme(envURL),
				Provider:    providerGitea,
			}
		}
	}

	if len(hosts) == 0 {
		return nil, fmt.Errorf(
			"no Gitea logins found — run `tea login add` or set GLVALET_TOKEN + GLVALET_GITEA_URL",
		)
	}

	active := firstNonEmpty(
		opts.HostFlag,
		os.Getenv("GLVALET_HOST"),
		teaDefault,
		stripScheme(envURL),
	)
	if active == "" {
		active = sortedKeys(hosts)[0]
	}

	hcfg, ok := hosts[active]
	if !ok {
		return nil, fmt.Errorf(
			"host %q not in tea config — available: %s\n"+
				"  hint: run `tea login add --name %s`",
			active, strings.Join(sortedKeys(hosts), ", "), active,
		)
	}

	token := hcfg.Token
	if token == "" {
		token = envToken
	}
	if token == "" {
		return nil, fmt.Errorf(
			"no token for host %q\n"+
				"  hint: run `tea login add --name %s` or set GLVALET_TOKEN",
			active, active,
		)
	}

	return &Config{
		Host:           active,
		GiteaURL:       hcfg.APIURL(active),
		Token:          token,
		User:           hcfg.User,
		SkipTLS:        hcfg.SkipTLS(),
		Provider:       providerGitea,
		JournalPath:    journalPath(),
		CachePath:      cachePath(),
		StatePath:      statePath(),
		DefaultProject: os.Getenv("GLVALET_DEFAULT_PROJECT"),
		DefaultGroup:   os.Getenv("GLVALET_DEFAULT_GROUP"),
		Hosts:          hosts,
	}, nil
}

// ForHost constructs a Config for a specific hostname from an already-loaded
// hosts map. Used by cross-instance commands (report push, sync) that need an
// independent client. The returned Config preserves the host's provider.
func ForHost(hosts map[string]*HostConfig, hostname, journalPath string) (*Config, error) {
	hcfg, ok := hosts[hostname]
	if !ok {
		return nil, fmt.Errorf(
			"host %q not in config — available: %s",
			hostname, strings.Join(sortedKeys(hosts), ", "),
		)
	}
	token := hcfg.Token
	if token == "" {
		token = os.Getenv("GLVALET_TOKEN")
	}
	if token == "" {
		return nil, fmt.Errorf("no token for host %q", hostname)
	}

	provider := hcfg.Provider
	if provider == "" {
		provider = providerGitLab
	}

	cfg := &Config{
		Host:        hostname,
		Token:       token,
		User:        hcfg.User,
		SkipTLS:     hcfg.SkipTLS(),
		Provider:    provider,
		JournalPath: journalPath,
		CachePath:   cachePath(),
		StatePath:   statePath(),
		Hosts:       hosts,
	}
	if provider == providerGitea {
		cfg.GiteaURL = hcfg.APIURL(hostname)
	} else {
		cfg.GitLabURL = hcfg.APIURL(hostname)
	}
	return cfg, nil
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
				hc.Provider = providerGitLab
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
				hc.Provider = providerGitLab
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

// ─── tea config parser ────────────────────────────────────────────────────────

// teaLogin mirrors the per-login block in tea's config.yml.
type teaLogin struct {
	Name     string `yaml:"name"`
	URL      string `yaml:"url"`
	Token    string `yaml:"token"`
	Default  bool   `yaml:"default"`
	User     string `yaml:"user"`
	Insecure bool   `yaml:"insecure"`
}

// teaFileConfig is the shape of ~/.config/tea/config.yml.
type teaFileConfig struct {
	Logins []teaLogin `yaml:"logins"`
}

// loadTeaHosts reads tea config locations in priority order, merges all
// logins (first-wins by name), and returns the default login name.
func loadTeaHosts() (map[string]*HostConfig, string, error) {
	merged := make(map[string]*HostConfig)
	defaultHost := ""
	var lastErr error

	for _, path := range teaConfigPaths() {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			lastErr = err
			continue
		}

		var fc teaFileConfig
		if err := yaml.Unmarshal(data, &fc); err != nil {
			lastErr = fmt.Errorf("parse %s: %w", path, err)
			continue
		}

		for _, login := range fc.Logins {
			if _, exists := merged[login.Name]; !exists {
				merged[login.Name] = teaLoginToHostConfig(&login)
				if defaultHost == "" && login.Default {
					defaultHost = login.Name
				}
			}
		}
	}

	if len(merged) == 0 && lastErr != nil {
		return nil, "", lastErr
	}
	return merged, defaultHost, nil
}

func teaLoginToHostConfig(l *teaLogin) *HostConfig {
	proto := "https"
	host := l.URL
	if u, err := url.Parse(l.URL); err == nil {
		if u.Scheme != "" {
			proto = u.Scheme
		}
		host = u.Host
		if u.Path != "" && u.Path != "/" {
			host = strings.TrimRight(host+u.Path, "/")
		}
	}
	return &HostConfig{
		Token:         l.Token,
		User:          l.User,
		APIProtocol:   proto,
		APIHost:       host,
		SkipTLSVerify: strconv.FormatBool(l.Insecure),
		Provider:      providerGitea,
	}
}

// teaConfigPaths returns candidate tea config.yml paths in priority order.
func teaConfigPaths() []string {
	var paths []string
	for _, dir := range teaConfigDirs() {
		paths = append(paths, filepath.Join(dir, "config.yml"))
	}
	return paths
}

// teaConfigDirs returns directories to search for tea config, in priority order.
func teaConfigDirs() []string {
	var dirs []string

	if custom := os.Getenv("GLVALET_TEA_CONFIG"); custom != "" {
		dirs = append(dirs, filepath.Dir(custom))
	}

	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		dirs = append(dirs, filepath.Join(xdg, "tea"))
	}
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("AppData"); appData != "" {
			dirs = append(dirs, filepath.Join(appData, "tea"))
		}
	}

	if home, _ := os.UserHomeDir(); home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".config", "tea"), // Linux/macOS default
			filepath.Join(home, ".tea"),           // older tea
		)
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
