// Package rotation implements credential rotation for configured hosts.
package rotation

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/MChorfa/ggvalet/internal/config"
)

const (
	DefaultCadenceDays   = 30
	DefaultPATExpiryDays = 65
)

type Profile struct {
	Credentials []string `yaml:"credentials"`
}

type Defaults struct {
	CadenceDays   int `yaml:"cadence_days"`
	PATExpiryDays int `yaml:"pat_expiry_days"`
}

type Config struct {
	Version  int                `yaml:"version"`
	Defaults Defaults           `yaml:"defaults"`
	Hosts    map[string]Profile `yaml:"hosts"`
}

// Uses reports whether host declares the named credential type.
func (c *Config) Uses(host, cred string) bool {
	p, ok := c.Hosts[host]
	if !ok {
		return false
	}
	for _, got := range p.Credentials {
		if got == cred {
			return true
		}
	}
	return false
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("rotation config parse: %w", err)
	}
	if c.Defaults.CadenceDays == 0 {
		c.Defaults.CadenceDays = DefaultCadenceDays
	}
	if c.Defaults.PATExpiryDays == 0 {
		c.Defaults.PATExpiryDays = DefaultPATExpiryDays
	}
	return &c, nil
}

// Propose builds a starting profile from observed hosts. Every host gets pat;
// ssh is never assumed, because a host that fails to authenticate cannot be
// probed to discover whether it uses SSH.
func Propose(hosts map[string]*config.HostConfig) *Config {
	c := &Config{
		Version:  1,
		Defaults: Defaults{CadenceDays: DefaultCadenceDays, PATExpiryDays: DefaultPATExpiryDays},
		Hosts:    map[string]Profile{},
	}
	names := make([]string, 0, len(hosts))
	for h := range hosts {
		names = append(names, h)
	}
	sort.Strings(names)
	for _, h := range names {
		c.Hosts[h] = Profile{Credentials: []string{"pat"}}
	}
	return c
}

func Save(path string, c *Config) error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFile(path, b, 0o600)
}

func writeFile(path string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, mode)
}
