package syncindex

import (
	"testing"

	"github.com/MChorfa/ggvalet/internal/vector"
)

func TestAnalyzeDriftConditions(t *testing.T) {
	src := NewEntitySnapshot(1, 10, "Base Title", "Base description", []string{"sec"}, "opened", "http://src/10")
	baselineDigest := src.Digest

	// 1. Destination Missing
	d1 := AnalyzeDrift("host-a", "proj-a", "issue", &src, nil, "", 1)
	if d1.Status != StatusDestinationMissing {
		t.Errorf("expected StatusDestinationMissing, got %s", d1.Status)
	}
	if d1.Vector.Presence != vector.PresenceEmpty || d1.Vector.Valence != vector.ValenceNegative {
		t.Errorf("unexpected vector for missing: %v", d1.Vector)
	}

	// 2. In Sync
	dstInSync := NewEntitySnapshot(2, 20, "Base Title", "Base description", []string{"sec"}, "opened", "http://dst/20")
	d2 := AnalyzeDrift("host-a", "proj-a", "issue", &src, &dstInSync, baselineDigest, 1)
	if d2.Status != StatusInSync {
		t.Errorf("expected StatusInSync, got %s", d2.Status)
	}
	if d2.Vector.Valence != vector.ValencePositive || d2.Vector.Coherence != vector.Coherent {
		t.Errorf("unexpected vector for in sync: %v", d2.Vector)
	}

	// 3. Upstream Drift (src updated, dst matches old baseline)
	srcUpdated := NewEntitySnapshot(1, 10, "Base Title Updated", "Base description", []string{"sec"}, "opened", "http://src/10")
	d3 := AnalyzeDrift("host-a", "proj-a", "issue", &srcUpdated, &dstInSync, baselineDigest, 2)
	if d3.Status != StatusUpstreamDrift {
		t.Errorf("expected StatusUpstreamDrift, got %s", d3.Status)
	}
	if d3.Vector.Coherence != vector.Partial {
		t.Errorf("unexpected coherence: %v", d3.Vector.Coherence)
	}

	// 4. Downstream Drift (dst updated, src matches old baseline)
	dstUpdated := NewEntitySnapshot(2, 20, "Base Title", "Base description with local additions", []string{"sec"}, "opened", "http://dst/20")
	d4 := AnalyzeDrift("host-a", "proj-a", "issue", &src, &dstUpdated, baselineDigest, 2)
	if d4.Status != StatusDownstreamDrift {
		t.Errorf("expected StatusDownstreamDrift, got %s", d4.Status)
	}
	if d4.Vector.Valence != vector.ValenceMixed {
		t.Errorf("unexpected valence: %v", d4.Vector.Valence)
	}

	// 5. Conflict (both diverged from baseline)
	d5 := AnalyzeDrift("host-a", "proj-a", "issue", &srcUpdated, &dstUpdated, baselineDigest, 3)
	if d5.Status != StatusConflict {
		t.Errorf("expected StatusConflict, got %s", d5.Status)
	}
	if d5.Vector.Anti != vector.AntiContradicts || d5.Vector.Coherence != vector.Decoherent {
		t.Errorf("unexpected anti/coherence for conflict: %v", d5.Vector)
	}
}
