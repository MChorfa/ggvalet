package transfer

import (
	"errors"
	"testing"
)

func TestDiodeTransferAndAirgapStorage(t *testing.T) {
	// 1. Seal a bundle
	bundle, err := SealBundle(
		"app-binary-v1.0.0",
		"sha256:aaaa1111",
		"sig-cosign-valid",
		"sha256:bbbb2222",
		"sha256:cccc3333",
	)
	if err != nil {
		t.Fatalf("SealBundle failed: %v", err)
	}
	if bundle.MerkleRoot == "" {
		t.Errorf("expected non-empty MerkleRoot")
	}

	diode := NewDiode()

	// 2. Normal forward transmission through diode: connected -> airgap
	rcpt, err := diode.Transmit(bundle, "gitlab-shared.local", "gitlab-airgap.local")
	if err != nil {
		t.Fatalf("Transmit failed: %v", err)
	}
	if rcpt.Status != "DELIVERED_AIRGAP" {
		t.Errorf("expected DELIVERED_AIRGAP, got %s", rcpt.Status)
	}

	// 3. Verify destination has received it
	destItem, found := diode.GetAirgapArtifact("app-binary-v1.0.0")
	if !found || destItem.ArtifactDigest != "sha256:aaaa1111" {
		t.Errorf("airgap destination failed to find transmitted artifact")
	}

	// 4. Reverse transmission attempt MUST fail (Hardware diode invariant)
	_, err = diode.Transmit(bundle, "gitlab-airgap.local", "gitlab-shared.local")
	if !errors.Is(err, ErrReverseFlow) {
		t.Errorf("expected ErrReverseFlow on reverse transmission, got %v", err)
	}

	// 5. Tampered bundle MUST fail
	tampered := *bundle
	tampered.Signature = "tampered-signature"
	_, err = diode.Transmit(&tampered, "gitlab-shared.local", "gitlab-airgap.local")
	if !errors.Is(err, ErrTamperedBundle) {
		t.Errorf("expected ErrTamperedBundle, got %v", err)
	}
}
