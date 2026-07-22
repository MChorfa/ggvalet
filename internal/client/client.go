// Package client wraps go-gitlab with automatic journaling and a TTL cache.
package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/ckodex/gitlabvalet/internal/cache"
	"github.com/ckodex/gitlabvalet/internal/config"
	"github.com/ckodex/gitlabvalet/internal/journal"
	"github.com/ckodex/gitlabvalet/internal/observed"
	"github.com/ckodex/gitlabvalet/internal/provider"
	"github.com/ckodex/gitlabvalet/internal/providerfactory"
	"github.com/ckodex/gitlabvalet/internal/state"
	gl "github.com/xanzy/go-gitlab"
)

// Client bundles the GitLab SDK, journal, and cache.
type Client struct {
	GL       *gl.Client
	Provider provider.Provider // host-neutral provider interface (P5)
	Journal  *journal.Journal
	Cache    *cache.Cache
	State    *state.Store
	cfg      *config.Config
}

// New creates a Client from cfg. skip_tls_verify is respected per-host.
func New(cfg *config.Config) (*Client, error) {
	j, err := journal.Open(cfg.JournalPath)
	if err != nil {
		return nil, err
	}

	c, err := cache.New(cfg.CachePath)
	if err != nil {
		// non-fatal — degrade gracefully
		c = nil
	}

	statePath := cfg.StatePath
	if statePath == "" {
		statePath = filepath.Join(filepath.Dir(cfg.JournalPath), "state.db")
	}
	stateStore, err := state.Open(statePath)
	if err != nil {
		return nil, err
	}
	if err := stateStore.ImportLegacy(context.Background(), cfg.JournalPath); err != nil {
		stateStore.Close()
		return nil, fmt.Errorf("client: import legacy journal: %w", err)
	}

	// Select the provider implementation from cfg.Provider / GLVALET_PROVIDER
	// (gitlab|github); defaults to GitLab. This must happen before the raw
	// GitLab SDK is constructed so we can skip it entirely when GitHub is active.
	prov, err := providerfactory.NewFromConfig(cfg)
	if err != nil {
		stateStore.Close()
		return nil, fmt.Errorf("client: build provider: %w", err)
	}
	prov = observed.NewProvider(prov, stateStore, cfg.Host)

	var glc *gl.Client
	if cfg.Provider != "github" {
		baseTransport := http.DefaultTransport.(*http.Transport).Clone()
		if cfg.SkipTLS {
			baseTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
		}
		httpClient := &http.Client{Transport: &observed.Transport{
			Base: baseTransport, Store: stateStore, Provider: "gitlab", Host: cfg.Host,
		}}

		glc, err = gl.NewClient(cfg.Token,
			gl.WithBaseURL(cfg.GitLabURL),
			gl.WithHTTPClient(httpClient),
		)
		if err != nil {
			stateStore.Close()
			return nil, fmt.Errorf("gitlab client init: %w", err)
		}
	}

	return &Client{GL: glc, Provider: prov, Journal: j, Cache: c, State: stateStore, cfg: cfg}, nil
}

func (c *Client) Close() error { return c.State.Close() }

// Host returns the active hostname.
func (c *Client) Host() string { return c.cfg.Host }

// CacheKey builds a namespaced cache key for this host.
func (c *Client) CacheKey(path, params string) string {
	return cache.Key(c.cfg.Host, path, params)
}

// ─── Journal helpers ──────────────────────────────────────────────────────────

func (c *Client) Rec(op journal.Op, entity journal.Entity, project, group string,
	entityID, iid int, title, url string, tags ...string) error {
	return c.Journal.Record(journal.Entry{
		Host: c.cfg.Host, Op: op, Entity: entity,
		Project: project, Group: group,
		EntityID: entityID, IID: iid,
		Title: title, URL: url,
		Outcome: journal.OutcomeOK, Tags: tags,
	})
}

func (c *Client) RecErr(op journal.Op, entity journal.Entity, project, group, detail string) error {
	return c.Journal.Record(journal.Entry{
		Host: c.cfg.Host, Op: op, Entity: entity,
		Project: project, Group: group,
		Outcome: journal.OutcomeErr, Detail: detail,
	})
}
