// Package journal provides an append-only JSONL activity ledger.
// Every GitLab operation performed through gitlabvalet is recorded here so the
// operator can generate time-boxed manager reports without touching the API again.
package journal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ─── Entry ───────────────────────────────────────────────────────────────────

// Op describes the kind of mutation performed.
type Op string

const (
	OpCreate  Op = "create"
	OpUpdate  Op = "update"
	OpClose   Op = "close"
	OpReopen  Op = "reopen"
	OpComment Op = "comment"
	OpList    Op = "list"
	OpAssign  Op = "assign"
	OpDelete  Op = "delete"
)

// Entity describes the GitLab resource type.
type Entity string

const (
	EntityIssue     Entity = "issue"
	EntityEpic      Entity = "epic"
	EntityMilestone Entity = "milestone"
	EntityWorkItem  Entity = "workitem"
	EntityMR        Entity = "mr"
	EntityLabel     Entity = "label"
	EntityNote      Entity = "note"
)

// Outcome is the terminal state of the operation.
type Outcome string

const (
	OutcomeOK  Outcome = "ok"
	OutcomeErr Outcome = "err"
)

// Entry is one immutable journal record.
type Entry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"ts"`
	Host      string    `json:"host"` // e.g. "gitlab.thalesdigital.io"
	Op        Op        `json:"op"`
	Entity    Entity    `json:"entity"`
	Project   string    `json:"project,omitempty"`
	Group     string    `json:"group,omitempty"`
	EntityID  int       `json:"entity_id,omitempty"`
	IID       int       `json:"iid,omitempty"`
	Title     string    `json:"title,omitempty"`
	URL       string    `json:"url,omitempty"`
	Outcome   Outcome   `json:"outcome"`
	Detail    string    `json:"detail,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
}

// ─── Journal ─────────────────────────────────────────────────────────────────

// Journal is the append-only ledger backed by a JSONL file.
type Journal struct {
	path string
}

// Open opens (or creates) the journal at the given path.
func Open(path string) (*Journal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("journal mkdir: %w", err)
	}
	return &Journal{path: path}, nil
}

// Record appends e to the ledger. Thread-safe via O_APPEND.
func (j *Journal) Record(e Entry) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}

	f, err := os.OpenFile(j.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("journal open: %w", err)
	}
	defer f.Close()

	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("journal marshal: %w", err)
	}
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

// ─── Query ────────────────────────────────────────────────────────────────────

// Filter controls which entries are returned by Query.
type Filter struct {
	Since    time.Time
	Until    time.Time
	Host     string // empty = all hosts
	Ops      []Op
	Entities []Entity
	Project  string
	Group    string
	Outcome  Outcome // empty = all
}

// Query reads all entries from the ledger and returns those matching f.
func (j *Journal) Query(f Filter) ([]Entry, error) {
	file, err := os.Open(j.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("journal open for read: %w", err)
	}
	defer file.Close()

	var results []Entry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // skip malformed lines
		}
		if !matches(e, f) {
			continue
		}
		results = append(results, e)
	}
	return results, scanner.Err()
}

func matches(e Entry, f Filter) bool {
	if !f.Since.IsZero() && e.Timestamp.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && e.Timestamp.After(f.Until) {
		return false
	}
	if f.Outcome != "" && e.Outcome != f.Outcome {
		return false
	}
	if f.Host != "" && e.Host != f.Host {
		return false
	}
	if f.Project != "" && e.Project != f.Project {
		return false
	}
	if f.Group != "" && e.Group != f.Group {
		return false
	}
	if len(f.Ops) > 0 && !containsOp(f.Ops, e.Op) {
		return false
	}
	if len(f.Entities) > 0 && !containsEntity(f.Entities, e.Entity) {
		return false
	}
	return true
}

func containsOp(set []Op, v Op) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

func containsEntity(set []Entity, v Entity) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// ─── Stats ────────────────────────────────────────────────────────────────────

// Stats returns a breakdown of entries for display.
type Stats struct {
	Total    int
	ByHost   map[string]int
	ByEntity map[Entity]int
	ByOp     map[Op]int
	Errors   int
}

func ComputeStats(entries []Entry) Stats {
	s := Stats{
		ByHost:   make(map[string]int),
		ByEntity: make(map[Entity]int),
		ByOp:     make(map[Op]int),
	}
	for _, e := range entries {
		s.Total++
		s.ByHost[e.Host]++
		s.ByEntity[e.Entity]++
		s.ByOp[e.Op]++
		if e.Outcome == OutcomeErr {
			s.Errors++
		}
	}
	return s
}
