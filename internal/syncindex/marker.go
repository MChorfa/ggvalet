package syncindex

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	MarkerVersion = "v1"
	MarkerPrefix  = "<!-- ggvalet-sync:v1 "
	MarkerSuffix  = " -->"
	LegacyPrefix  = "<!-- ggvalet-sync-src: "
	LegacySuffix  = " -->"
)

var (
	structuredRegex = regexp.MustCompile(`<!-- ggvalet-sync:v1\s+host=([^\s]+)\s+proj=([^\s]+)\s+entity=([^\s]+)\s+iid=(\d+)\s+digest=([^\s]+)\s+epoch=(\d+)\s*-->`)
	legacyRegex     = regexp.MustCompile(`<!-- ggvalet-sync-src:\s*([^\s]+)\s*-->`)
)

// SyncMarker represents cryptographic provenance and identity for cross-instance synchronization.
type SyncMarker struct {
	Version       string `json:"version"`
	SrcHost       string `json:"src_host"`
	SrcProject    string `json:"src_project"`
	EntityType    string `json:"entity_type"` // issue, epic, milestone
	SrcIID        int    `json:"src_iid"`
	ContentDigest string `json:"content_digest"`
	SyncEpoch     int    `json:"sync_epoch"`
	SrcURL        string `json:"src_url,omitempty"`
}

// ComputeContentDigest creates a deterministic hash over semantic fields, ignoring markers and cosmetic whitespace.
func ComputeContentDigest(title, body string, labels []string, state string) string {
	cleanBody := StripMarker(body)
	sortedLabels := append([]string(nil), labels...)
	sort.Strings(sortedLabels)
	labelsStr := strings.Join(sortedLabels, ",")

	canonical := fmt.Sprintf("title:%s|state:%s|labels:%s|body:%s",
		strings.TrimSpace(title),
		strings.TrimSpace(strings.ToLower(state)),
		labelsStr,
		strings.TrimSpace(cleanBody),
	)

	h := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(h[:])
}

// FormatMarker formats a SyncMarker into an HTML comment.
func FormatMarker(m SyncMarker) string {
	version := m.Version
	if version == "" {
		version = MarkerVersion
	}
	return fmt.Sprintf("<!-- ggvalet-sync:%s host=%s proj=%s entity=%s iid=%d digest=%s epoch=%d -->",
		version, m.SrcHost, m.SrcProject, m.EntityType, m.SrcIID, m.ContentDigest, m.SyncEpoch)
}

// EmbedMarker appends or replaces the structured sync marker in the description body.
func EmbedMarker(body string, m SyncMarker) string {
	clean := StripMarker(body)
	marker := FormatMarker(m)
	footer := fmt.Sprintf("\n\n---\n*Synced by [ggvalet](https://github.com/MChorfa/ggvalet)*  \n%s", marker)
	if strings.TrimSpace(clean) == "" {
		return marker
	}
	return strings.TrimRight(clean, "\r\n") + footer
}

// ExtractMarker extracts structured or legacy markers from description text.
func ExtractMarker(body string) (*SyncMarker, bool) {
	if matches := structuredRegex.FindStringSubmatch(body); len(matches) == 7 {
		iid, _ := strconv.Atoi(matches[4])
		epoch, _ := strconv.Atoi(matches[6])
		return &SyncMarker{
			Version:       MarkerVersion,
			SrcHost:       matches[1],
			SrcProject:    matches[2],
			EntityType:    matches[3],
			SrcIID:        iid,
			ContentDigest: matches[5],
			SyncEpoch:     epoch,
		}, true
	}

	if matches := legacyRegex.FindStringSubmatch(body); len(matches) == 2 {
		rawURL := matches[1]
		m := parseLegacyURL(rawURL)
		if m != nil {
			return m, true
		}
	}

	return nil, false
}

// StripMarker removes any structured or legacy sync marker comments from body.
func StripMarker(body string) string {
	res := structuredRegex.ReplaceAllString(body, "")
	res = legacyRegex.ReplaceAllString(res, "")
	// Also clean up trailing sync footer artifacts if present
	res = strings.ReplaceAll(res, "*Synced by [ggvalet](https://github.com/MChorfa/ggvalet)*", "")
	res = strings.TrimRight(res, " \t\r\n-")
	return res
}

func parseLegacyURL(raw string) *SyncMarker {
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	host := u.Host
	path := strings.Trim(u.Path, "/")
	// e.g. group/subgroup/project/-/issues/42
	parts := strings.Split(path, "/-/")
	if len(parts) != 2 {
		return nil
	}
	project := parts[0]
	entityParts := strings.Split(parts[1], "/")
	if len(entityParts) != 2 {
		return nil
	}
	entity := entityParts[0]
	if strings.HasSuffix(entity, "s") {
		entity = strings.TrimSuffix(entity, "s")
	}
	iid, err := strconv.Atoi(entityParts[1])
	if err != nil {
		return nil
	}

	return &SyncMarker{
		Version:       "legacy",
		SrcHost:       host,
		SrcProject:    project,
		EntityType:    entity,
		SrcIID:        iid,
		ContentDigest: "sha256:legacy",
		SyncEpoch:     1,
		SrcURL:        raw,
	}
}
