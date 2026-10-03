package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrTamperedBundle = errors.New("diode: bundle manifest digest verification failed")
	ErrReverseFlow    = errors.New("diode: reverse data transmission strictly prohibited by hardware diode")
)

type DiodeReceipt struct {
	ReceiptID     string    `json:"receipt_id"`
	BundleID      string    `json:"bundle_id"`
	ArtifactID    string    `json:"artifact_id"`
	SourceDomain  string    `json:"source_domain"`
	TargetDomain  string    `json:"target_domain"`
	TransferredAt time.Time `json:"transferred_at"`
	Status        string    `json:"status"`
	MerkleRoot    string    `json:"merkle_root"`
}

// Diode simulates an immutable, hardware-enforced one-way data transfer mechanism.
type Diode struct {
	mu          sync.Mutex
	airgapStore map[string]*EvidenceBundle
	receipts    []DiodeReceipt
}

func NewDiode() *Diode {
	return &Diode{
		airgapStore: make(map[string]*EvidenceBundle),
	}
}

// Transmit validates and transfers a sealed evidence bundle into the airgap registry.
func (d *Diode) Transmit(bundle *EvidenceBundle, srcDomain, destDomain string) (*DiodeReceipt, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Invariant: Unidirectional transfer only
	if srcDomain == "gitlab-airgap.local" {
		return nil, ErrReverseFlow
	}

	// Verify integrity
	checkBundle := *bundle
	checkBundle.ManifestDigest = ""
	// Re-verify Merkle Root
	leafA := sha256.Sum256([]byte(bundle.ArtifactDigest))
	leafS := sha256.Sum256([]byte(bundle.Signature))
	leafB := sha256.Sum256([]byte(bundle.SBOMDigest))
	leafP := sha256.Sum256([]byte(bundle.ProvenanceDigest))
	node1 := sha256.Sum256(append(leafA[:], leafS[:]...))
	node2 := sha256.Sum256(append(leafB[:], leafP[:]...))
	root := sha256.Sum256(append(node1[:], node2[:]...))
	expectedMerkle := hex.EncodeToString(root[:])

	if bundle.MerkleRoot != expectedMerkle {
		return nil, fmt.Errorf("%w: merkle root mismatch", ErrTamperedBundle)
	}

	// Store in air-gap repository
	d.airgapStore[bundle.ArtifactID] = bundle

	rcpt := DiodeReceipt{
		ReceiptID:     "rcpt-diode-" + uuid.NewString(),
		BundleID:      bundle.BundleID,
		ArtifactID:    bundle.ArtifactID,
		SourceDomain:  srcDomain,
		TargetDomain:  destDomain,
		TransferredAt: time.Now().UTC(),
		Status:        "DELIVERED_AIRGAP",
		MerkleRoot:    bundle.MerkleRoot,
	}
	d.receipts = append(d.receipts, rcpt)
	return &rcpt, nil
}

// GetAirgapArtifact queries the airgap side to verify receipt of the asset.
func (d *Diode) GetAirgapArtifact(artifactID string) (*EvidenceBundle, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	b, ok := d.airgapStore[artifactID]
	return b, ok
}
