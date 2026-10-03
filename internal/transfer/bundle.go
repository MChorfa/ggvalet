package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// EvidenceBundle models a sealed, immutable transfer package for air-gap distribution.
type EvidenceBundle struct {
	BundleID         string    `json:"bundle_id"`
	ArtifactID       string    `json:"artifact_id"`
	ArtifactDigest   string    `json:"artifact_digest"`
	Signature        string    `json:"signature"`
	SBOMDigest       string    `json:"sbom_digest"`
	ProvenanceDigest string    `json:"provenance_digest"`
	MerkleRoot       string    `json:"merkle_root"`
	PackagedAt       time.Time `json:"packaged_at"`
	ManifestDigest   string    `json:"manifest_digest"`
}

// SealBundle constructs an air-gap transfer package with cryptographic verification.
func SealBundle(artifactID, artifactDigest, signature, sbomDigest, provDigest string) (*EvidenceBundle, error) {
	if artifactDigest == "" || signature == "" || sbomDigest == "" || provDigest == "" {
		return nil, fmt.Errorf("transfer: all bundle evidence elements are mandatory")
	}

	now := time.Now().UTC()
	// Compute Merkle root of the 4 evidence leaves
	leafA := sha256.Sum256([]byte(artifactDigest))
	leafS := sha256.Sum256([]byte(signature))
	leafB := sha256.Sum256([]byte(sbomDigest))
	leafP := sha256.Sum256([]byte(provDigest))

	// Intermediate nodes
	node1 := sha256.Sum256(append(leafA[:], leafS[:]...))
	node2 := sha256.Sum256(append(leafB[:], leafP[:]...))
	root := sha256.Sum256(append(node1[:], node2[:]...))
	merkleRoot := hex.EncodeToString(root[:])

	bundle := &EvidenceBundle{
		BundleID:         "bundle-" + uuid.NewString(),
		ArtifactID:       artifactID,
		ArtifactDigest:   artifactDigest,
		Signature:        signature,
		SBOMDigest:       sbomDigest,
		ProvenanceDigest: provDigest,
		MerkleRoot:       merkleRoot,
		PackagedAt:       now,
	}

	raw, _ := json.Marshal(bundle)
	manifestHash := sha256.Sum256(raw)
	bundle.ManifestDigest = hex.EncodeToString(manifestHash[:])

	return bundle, nil
}
