package syncindex

import (
	"fmt"
	"time"

	"github.com/MChorfa/ggvalet/internal/vector"
)

type DriftStatus string

const (
	StatusInSync             DriftStatus = "IN_SYNC"
	StatusDestinationMissing DriftStatus = "DESTINATION_MISSING"
	StatusUpstreamDrift      DriftStatus = "UPSTREAM_DRIFT"
	StatusDownstreamDrift    DriftStatus = "DOWNSTREAM_DRIFT"
	StatusConflict           DriftStatus = "CONFLICT"
)

// EntitySnapshot represents the captured state of a synchronizable entity.
type EntitySnapshot struct {
	ID     int      `json:"id"`
	IID    int      `json:"iid"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels"`
	State  string   `json:"state"`
	WebURL string   `json:"web_url"`
	Digest string   `json:"digest"`
}

// NewEntitySnapshot calculates the content digest and creates an immutable snapshot.
func NewEntitySnapshot(id, iid int, title, body string, labels []string, state, webURL string) EntitySnapshot {
	digest := ComputeContentDigest(title, body, labels, state)
	return EntitySnapshot{
		ID:     id,
		IID:    iid,
		Title:  title,
		Body:   body,
		Labels: labels,
		State:  state,
		WebURL: webURL,
		Digest: digest,
	}
}

// DriftAnalysis records the outcome of comparing source, destination, and indexed baseline states.
type DriftAnalysis struct {
	EntityKey         string             `json:"entity_key"`
	EntityType        string             `json:"entity_type"`
	SrcHost           string             `json:"src_host"`
	SrcProject        string             `json:"src_project"`
	SrcIID            int                `json:"src_iid"`
	DstHost           string             `json:"dst_host"`
	DstProject        string             `json:"dst_project"`
	DstIID            int                `json:"dst_iid,omitempty"`
	Status            DriftStatus        `json:"status"`
	Vector            vector.StateVector `json:"vector_state"`
	SourceDigest      string             `json:"source_digest"`
	IndexDigest       string             `json:"index_digest,omitempty"`
	DestinationDigest string             `json:"destination_digest,omitempty"`
	Epoch             int                `json:"epoch"`
	RecommendedAction string             `json:"recommended_action"`
}

// AnalyzeDrift evaluates source, destination, and recorded index digests to determine drift status and vector state.
func AnalyzeDrift(
	srcHost, srcProject, entityType string,
	src *EntitySnapshot,
	dst *EntitySnapshot,
	lastIndexDigest string,
	epoch int,
) DriftAnalysis {
	entityKey := fmt.Sprintf("%s:%s/%s#%d", entityType, srcHost, srcProject, src.IID)
	now := time.Now().UTC()

	// 1. Destination does not exist
	if dst == nil {
		vec := vector.StateVector{
			EntityID:  entityKey,
			Presence:  vector.PresenceEmpty,
			Valence:   vector.ValenceNegative,
			Anti:      vector.AntiNone,
			Coherence: vector.Partial,
			Evidence:  vector.EvidenceVerified,
			Mode:      vector.ModeDegraded,
			Epoch:     int64(epoch),
			Timestamp: now,
		}
		return DriftAnalysis{
			EntityKey:         entityKey,
			EntityType:        entityType,
			SrcHost:           srcHost,
			SrcProject:        srcProject,
			SrcIID:            src.IID,
			Status:            StatusDestinationMissing,
			Vector:            vec,
			SourceDigest:      src.Digest,
			IndexDigest:       lastIndexDigest,
			Epoch:             epoch,
			RecommendedAction: "Create entity on destination instance with initial synchronization marker",
		}
	}

	srcDigest := src.Digest
	dstDigest := dst.Digest

	// 2. Perfectly in sync
	if (lastIndexDigest != "" && srcDigest == lastIndexDigest && dstDigest == lastIndexDigest) ||
		(lastIndexDigest == "" && srcDigest == dstDigest) {
		vec := vector.StateVector{
			EntityID:  entityKey,
			Presence:  vector.PresencePresent,
			Valence:   vector.ValencePositive,
			Anti:      vector.AntiNone,
			Coherence: vector.Coherent,
			Evidence:  vector.EvidenceVerified,
			Mode:      vector.ModeNormal,
			Epoch:     int64(epoch),
			Timestamp: now,
		}
		return DriftAnalysis{
			EntityKey:         entityKey,
			EntityType:        entityType,
			SrcHost:           srcHost,
			SrcProject:        srcProject,
			SrcIID:            src.IID,
			DstIID:            dst.IID,
			Status:            StatusInSync,
			Vector:            vec,
			SourceDigest:      srcDigest,
			IndexDigest:       lastIndexDigest,
			DestinationDigest: dstDigest,
			Epoch:             epoch,
			RecommendedAction: "None (entity is synchronized and coherent)",
		}
	}

	// 3. Upstream drift (source updated, destination still matches baseline)
	if lastIndexDigest != "" && srcDigest != lastIndexDigest && dstDigest == lastIndexDigest {
		vec := vector.StateVector{
			EntityID:  entityKey,
			Presence:  vector.PresencePresent,
			Valence:   vector.ValenceNegative,
			Anti:      vector.AntiNone,
			Coherence: vector.Partial,
			Evidence:  vector.EvidenceVerified,
			Mode:      vector.ModeDegraded,
			Epoch:     int64(epoch),
			Timestamp: now,
		}
		return DriftAnalysis{
			EntityKey:         entityKey,
			EntityType:        entityType,
			SrcHost:           srcHost,
			SrcProject:        srcProject,
			SrcIID:            src.IID,
			DstIID:            dst.IID,
			Status:            StatusUpstreamDrift,
			Vector:            vec,
			SourceDigest:      srcDigest,
			IndexDigest:       lastIndexDigest,
			DestinationDigest: dstDigest,
			Epoch:             epoch,
			RecommendedAction: "Forward synchronize latest source changes to destination",
		}
	}

	// 4. Downstream drift (destination modified independently while source remained at baseline)
	if lastIndexDigest != "" && srcDigest == lastIndexDigest && dstDigest != lastIndexDigest {
		vec := vector.StateVector{
			EntityID:  entityKey,
			Presence:  vector.PresencePresent,
			Valence:   vector.ValenceMixed,
			Anti:      vector.AntiNone,
			Coherence: vector.Partial,
			Evidence:  vector.EvidenceVerified,
			Mode:      vector.ModeDegraded,
			Epoch:     int64(epoch),
			Timestamp: now,
		}
		return DriftAnalysis{
			EntityKey:         entityKey,
			EntityType:        entityType,
			SrcHost:           srcHost,
			SrcProject:        srcProject,
			SrcIID:            src.IID,
			DstIID:            dst.IID,
			Status:            StatusDownstreamDrift,
			Vector:            vec,
			SourceDigest:      srcDigest,
			IndexDigest:       lastIndexDigest,
			DestinationDigest: dstDigest,
			Epoch:             epoch,
			RecommendedAction: "Review local destination changes; adopt or overwrite with source authority",
		}
	}

	// 5. Conflict (both sides modified independently since baseline, or unindexed divergent state)
	vec := vector.StateVector{
		EntityID:  entityKey,
		Presence:  vector.PresencePresent,
		Valence:   vector.ValenceNegative,
		Anti:      vector.AntiContradicts,
		Coherence: vector.Decoherent,
		Evidence:  vector.EvidenceVerified,
		Mode:      vector.ModeDegraded,
		Epoch:     int64(epoch),
		Timestamp: now,
	}
	return DriftAnalysis{
		EntityKey:         entityKey,
		EntityType:        entityType,
		SrcHost:           srcHost,
		SrcProject:        srcProject,
		SrcIID:            src.IID,
		DstIID:            dst.IID,
		Status:            StatusConflict,
		Vector:            vec,
		SourceDigest:      srcDigest,
		IndexDigest:       lastIndexDigest,
		DestinationDigest: dstDigest,
		Epoch:             epoch,
		RecommendedAction: "SAFE_HOLD: Both endpoints have diverged independently; manual reconciliation or fork required",
	}
}
