package syncindex

import (
	"testing"
)

func TestMarkerEmbeddingAndExtraction(t *testing.T) {
	origBody := "This is a feature description for cross-instance sync."
	digest := ComputeContentDigest("Feature Issue", origBody, []string{"security", "backend"}, "opened")

	marker := SyncMarker{
		Version:       MarkerVersion,
		SrcHost:       "gitlab.shared.local",
		SrcProject:    "cortaix/platform",
		EntityType:    "issue",
		SrcIID:        42,
		ContentDigest: digest,
		SyncEpoch:     1,
	}

	embedded := EmbedMarker(origBody, marker)
	extracted, ok := ExtractMarker(embedded)
	if !ok || extracted == nil {
		t.Fatalf("expected marker extraction to succeed, got %v", extracted)
	}

	if extracted.SrcHost != "gitlab.shared.local" {
		t.Errorf("expected src_host gitlab.shared.local, got %s", extracted.SrcHost)
	}
	if extracted.SrcProject != "cortaix/platform" {
		t.Errorf("expected src_project cortaix/platform, got %s", extracted.SrcProject)
	}
	if extracted.EntityType != "issue" {
		t.Errorf("expected entity_type issue, got %s", extracted.EntityType)
	}
	if extracted.SrcIID != 42 {
		t.Errorf("expected src_iid 42, got %d", extracted.SrcIID)
	}
	if extracted.ContentDigest != digest {
		t.Errorf("expected digest %s, got %s", digest, extracted.ContentDigest)
	}
	if extracted.SyncEpoch != 1 {
		t.Errorf("expected epoch 1, got %d", extracted.SyncEpoch)
	}

	// Verify StripMarker restores original body content
	clean := StripMarker(embedded)
	if clean != origBody {
		t.Errorf("expected clean body %q, got %q", origBody, clean)
	}

	// Verify ComputeContentDigest over embedded body equals digest over original body
	digestAfterEmbedding := ComputeContentDigest("Feature Issue", embedded, []string{"backend", "security"}, "opened")
	if digestAfterEmbedding != digest {
		t.Errorf("digest mismatch with marker present: %s vs %s", digestAfterEmbedding, digest)
	}
}

func TestLegacyMarkerExtraction(t *testing.T) {
	legacyBody := "Some description\n\n---\n*Synced by [ggvalet](https://github.com/MChorfa/ggvalet)*  \n<!-- ggvalet-sync-src: https://gitlab.thalesdigital.io/group/subgroup/project/-/issues/108 -->"
	extracted, ok := ExtractMarker(legacyBody)
	if !ok || extracted == nil {
		t.Fatalf("expected legacy marker extraction to succeed")
	}

	if extracted.SrcHost != "gitlab.thalesdigital.io" {
		t.Errorf("expected host gitlab.thalesdigital.io, got %s", extracted.SrcHost)
	}
	if extracted.SrcProject != "group/subgroup/project" {
		t.Errorf("expected project group/subgroup/project, got %s", extracted.SrcProject)
	}
	if extracted.EntityType != "issue" {
		t.Errorf("expected entity issue, got %s", extracted.EntityType)
	}
	if extracted.SrcIID != 108 {
		t.Errorf("expected iid 108, got %d", extracted.SrcIID)
	}
}
