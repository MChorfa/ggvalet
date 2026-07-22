// Package cache provides a lightweight TTL-based disk cache for provider API responses.
// Each entry is a JSON file under ~/.ggvalet/cache/.
// Expired entries are pruned on read; flush with Cache.Flush().
package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Cache is a file-backed TTL store.
type Cache struct{ dir string }

type envelope struct {
	Data    json.RawMessage `json:"d"`
	Expires time.Time       `json:"e"`
	Key     string          `json:"k"` // human-readable key for debugging
}

// New opens (or creates) the cache directory.
func New(dir string) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("cache mkdir: %w", err)
	}
	return &Cache{dir: dir}, nil
}

// Get returns raw cached bytes for key if live. Returns (nil, false) on miss or expiry.
func (c *Cache) Get(key string) (json.RawMessage, bool) {
	data, err := os.ReadFile(c.path(key))
	if err != nil {
		return nil, false
	}
	var e envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, false
	}
	if time.Now().After(e.Expires) {
		_ = os.Remove(c.path(key))
		return nil, false
	}
	return e.Data, true
}

// Set serialises v and stores it under key with the given TTL.
// Cache failures are silently dropped — they must never break the main path.
func (c *Cache) Set(key string, v any, ttl time.Duration) {
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	e := envelope{Data: raw, Expires: time.Now().Add(ttl), Key: key}
	b, _ := json.Marshal(e)
	_ = os.WriteFile(c.path(key), b, 0o600)
}

// Unmarshal retrieves and deserialises cached data into dst.
// Returns false on miss, expiry, or unmarshal error.
func (c *Cache) Unmarshal(key string, dst any) bool {
	raw, ok := c.Get(key)
	if !ok {
		return false
	}
	return json.Unmarshal(raw, dst) == nil
}

// Flush removes all files in the cache directory.
func (c *Cache) Flush() int {
	entries, _ := os.ReadDir(c.dir)
	n := 0
	for _, e := range entries {
		if os.Remove(filepath.Join(c.dir, e.Name())) == nil {
			n++
		}
	}
	return n
}

// Stats returns counts of live and expired entries without removing them.
func (c *Cache) Stats() (live, expired int) {
	entries, _ := os.ReadDir(c.dir)
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(c.dir, e.Name()))
		var env envelope
		if json.Unmarshal(data, &env) != nil {
			continue
		}
		if time.Now().After(env.Expires) {
			expired++
		} else {
			live++
		}
	}
	return
}

// Key builds a canonical cache key from host + path + params.
func Key(host, path, params string) string {
	return fmt.Sprintf("%s|%s|%s", host, path, params)
}

func (c *Cache) path(key string) string {
	h := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, fmt.Sprintf("%x.json", h[:12]))
}
