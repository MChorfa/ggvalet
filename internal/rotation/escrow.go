package rotation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	StateRotated  = "ROTATED"
	StateVerified = "VERIFIED"
)

type Escrow struct {
	Version      int      `json:"version"`
	Host         string   `json:"host"`
	State        string   `json:"state"`
	RotatedAt    string   `json:"rotated_at"`
	OldTokenID   int      `json:"old_token_id"`
	NewTokenID   int      `json:"new_token_id"`
	NewToken     string   `json:"new_token"`
	ExpiresAt    string   `json:"expires_at"`
	Scopes       []string `json:"scopes"`
	ConfigPath   string   `json:"config_path"`
	ConfigSHA256 string   `json:"config_sha256_before"`

	Path string `json:"-"` // filled by ListEscrows/LoadEscrow
}

// WriteEscrow persists e and fsyncs both the file and its parent directory.
// Durability here is the whole point: GitLab has already revoked the old token
// by the time this is called, so an unsynced write means a lost credential.
func WriteEscrow(dir string, e *Escrow) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if e.RotatedAt == "" {
		e.RotatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%d.json", e.Host, time.Now().UTC().UnixNano())
	path := filepath.Join(dir, name)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	// fsync the directory so the new entry itself survives a crash.
	d, err := os.Open(dir)
	if err != nil {
		return "", err
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return "", err
	}
	e.Path = path
	return path, nil
}

// LoadEscrow reads and parses an escrow file.
func LoadEscrow(path string) (*Escrow, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var e Escrow
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("escrow parse %s: %w", path, err)
	}
	e.Path = path
	return &e, nil
}

// ListEscrows returns all escrow files in dir, or nil if the directory doesn't exist.
func ListEscrows(dir string) ([]*Escrow, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Escrow
	for _, en := range entries {
		if en.IsDir() || filepath.Ext(en.Name()) != ".json" {
			continue
		}
		e, err := LoadEscrow(filepath.Join(dir, en.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// DeleteEscrow removes an escrow file.
func DeleteEscrow(path string) error { return os.Remove(path) }
