package trustwall

import (
	"testing"

	"github.com/MChorfa/ggvalet/internal/vector"
)

func TestTrustwallDecisions(t *testing.T) {
	gate := NewGate()

	// 1. Fully compliant artifact -> ADMIT
	goodClaim := ArtifactClaim{
		ArtifactID:       "pkg-clean-v1.0.0",
		ArtifactDigest:   "sha256:1111222233334444",
		SourceRepo:       "gitlab-shared/team-a",
		CommitSHA:        "a1b2c3d4",
		HasSignature:     true,
		SignatureValid:   true,
		HasSBOM:          true,
		HasProvenance:    true,
		ProvenanceSHA:    "a1b2c3d4",
		LicenseCompliant: true,
		ApprovalsCount:   2,
	}
	rcptGood := gate.Admit(goodClaim)
	if rcptGood.Decision != Admit {
		t.Fatalf("expected Admit, got %s violations: %v", rcptGood.Decision, rcptGood.Violations)
	}
	if !rcptGood.ResultingVector.IsConformant() {
		t.Errorf("admitted vector should be conformant")
	}

	// 2. Unsigned artifact -> QUARANTINE
	unsignedClaim := goodClaim
	unsignedClaim.ArtifactID = "pkg-unsigned"
	unsignedClaim.HasSignature = false
	rcptUnsigned := gate.Admit(unsignedClaim)
	if rcptUnsigned.Decision != Quarantine {
		t.Fatalf("expected Quarantine, got %s", rcptUnsigned.Decision)
	}
	if rcptUnsigned.ResultingVector.Mode != vector.ModeQuarantined {
		t.Errorf("expected ModeQuarantined, got %s", rcptUnsigned.ResultingVector.Mode)
	}

	// 3. Provenance mismatch -> QUARANTINE
	tamperedClaim := goodClaim
	tamperedClaim.ArtifactID = "pkg-tampered"
	tamperedClaim.ProvenanceSHA = "fake-sha-9999"
	rcptTampered := gate.Admit(tamperedClaim)
	if rcptTampered.Decision != Quarantine {
		t.Fatalf("expected Quarantine for provenance mismatch, got %s", rcptTampered.Decision)
	}

	// 4. Insufficient approvals (not quarantine, but DENY / safe hold)
	deniedClaim := goodClaim
	deniedClaim.ArtifactID = "pkg-single-approval"
	deniedClaim.ApprovalsCount = 1
	rcptDenied := gate.Admit(deniedClaim)
	if rcptDenied.Decision != Deny {
		t.Fatalf("expected Deny for missing approval, got %s", rcptDenied.Decision)
	}
	if rcptDenied.ResultingVector.Mode != vector.ModeSafeHold {
		t.Errorf("expected ModeSafeHold, got %s", rcptDenied.ResultingVector.Mode)
	}
}
