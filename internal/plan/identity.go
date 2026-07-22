package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
)

var markerRegex = regexp.MustCompile(`<!-- ggvalet:plan-id=([a-f0-9]{64}) -->`)

// MilestoneIdentity returns a deterministic content hash for a milestone.
// Inputs: title (load-bearing), due_date (load-bearing for time-series milestones).
// Returns lowercase hex sha256.
func MilestoneIdentity(m Milestone) string {
	canonical := fmt.Sprintf("v1|milestone|%s|%s", m.Title, m.DueDate)
	hash := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(hash[:])
}

// EpicIdentity returns a deterministic content hash for an epic.
// Inputs: title.
func EpicIdentity(e Epic) string {
	canonical := fmt.Sprintf("v1|epic|%s", e.Title)
	hash := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(hash[:])
}

// IssueIdentity returns a deterministic content hash for an issue.
// Inputs: title, milestone (local plan ID), epic (local plan ID).
// Note: milestone+epic are PLAN-LOCAL IDs (e.g. "m-q4-2026-w7"), not remote IIDs.
// This is correct: the marker is computed from plan intent, not remote state.
func IssueIdentity(i Issue) string {
	canonical := fmt.Sprintf("v1|issue|%s|%s|%s", i.Title, i.Milestone, i.Epic)
	hash := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(hash[:])
}

// EmbedMarker appends an HTML-comment marker carrying the identity hash to body.
// If body already contains a ggvalet:plan-id marker, it is REPLACED (not duplicated).
// Format: "\n\n<!-- ggvalet:plan-id=<hash> -->\n"
func EmbedMarker(body, hash string) string {
	marker := fmt.Sprintf("<!-- ggvalet:plan-id=%s -->", hash)

	if markerRegex.MatchString(body) {
		body = markerRegex.ReplaceAllString(body, marker)
		return body
	}

	if body == "" {
		return fmt.Sprintf("%s\n", marker)
	}

	return fmt.Sprintf("%s\n\n%s\n", body, marker)
}

// ExtractMarker returns the embedded plan-id hash from body, if present.
// Returns ("", false) if no marker found.
func ExtractMarker(body string) (string, bool) {
	matches := markerRegex.FindStringSubmatch(body)
	if len(matches) < 2 {
		return "", false
	}
	return matches[1], true
}
