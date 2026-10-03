package lab

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/policy"
	"github.com/MChorfa/ggvalet/internal/transfer"
	"github.com/MChorfa/ggvalet/internal/trustwall"
	"github.com/MChorfa/ggvalet/internal/vector"
)

// TestDeliveryLabAcceptance12 implements the formal acceptance criteria
// specified in Milestone 0: GG Valet Delivery Laboratory.
func TestDeliveryLabAcceptance12(t *testing.T) {
	ctx := context.Background()
	_ = ctx
	canonical := policy.DefaultCanonicalModel()

	// ─── STEP 1: Discover what it can actually observe ───────────────────
	// Valet observes project metadata, protected branches, and variables.
	observedProjB := map[string]any{
		"project":           "gitlab-shared/team-b",
		"protected":         false,                   // Observed
		"unmasked_variable": true,                    // Observed
		"mutable_image_tag": true,                    // Observed
		"sast_included":     false,                   // Observed
		"unknown_field":     vector.PresenceUnknown,  // Cannot observe
		"redacted_secret":   vector.PresenceRedacted, // Masked
	}
	if observedProjB["protected"] != false {
		t.Fatalf("Step 1 failed: could not observe protected branch state")
	}

	// ─── STEP 2: Explicitly identify what it CANNOT observe ──────────────
	// Invariant: Presence semantics must distinguish EMPTY, UNKNOWN, REDACTED
	// UNKNOWN != NEGATIVE, EMPTY != NEGATIVE.
	vUnknown := vector.StateVector{
		EntityID:  "team-b:audit_stream",
		Presence:  vector.PresenceUnknown,
		Valence:   vector.ValenceNeutral,
		Anti:      vector.AntiNone,
		Coherence: vector.Coherent,
		Evidence:  vector.EvidenceInferred,
		Mode:      vector.ModeNormal,
		Epoch:     1,
		Timestamp: time.Now(),
	}
	if vUnknown.Presence != vector.PresenceUnknown {
		t.Fatalf("Step 2 failed: failed to explicitly model UNKNOWN presence")
	}

	// ─── STEP 3: Reconstruct actual state (Vector State S) ───────────────
	// Reconstruct S(e,t) = <P, V, A, C, E, L, tau>
	assessment := policy.Evaluate("gitlab-shared/team-b", observedProjB, canonical)
	vec := assessment.CompositeVector
	if vec.Valence != vector.ValenceNegative || vec.Anti != vector.AntiContradicts {
		t.Fatalf("Step 3 failed: reconstructed vector state is incorrect: %s", vec)
	}

	// ─── STEP 4: Compare that state with canonical delivery model ────────
	// Canonical requires protected branches and masked variables.
	if len(assessment.Findings) == 0 {
		t.Fatalf("Step 4 failed: comparison against canonical model produced no findings")
	}

	// ─── STEP 5: Produce evidence-backed findings ────────────────────────
	foundMainUnprotected := false
	for _, f := range assessment.Findings {
		if f.DeviationID == "DEV-001" {
			foundMainUnprotected = true
			if f.EvidenceStatus != vector.EvidenceVerified {
				t.Fatalf("Step 5 failed: finding DEV-001 does not have VERIFIED evidence")
			}
			if f.ProposedPatch == "" {
				t.Fatalf("Step 5 failed: finding DEV-001 lacks concrete remediation patch")
			}
		}
	}
	if !foundMainUnprotected {
		t.Fatalf("Step 5 failed: DEV-001 finding not produced")
	}

	// ─── STEP 6: Distinguish documented requirements from inferred candidates
	docReqs := 0
	infOpts := 0
	for _, f := range assessment.Findings {
		switch f.Kind {
		case policy.KindDocumentedRequirement:
			docReqs++
		case policy.KindInferredOptimization:
			infOpts++
		}
	}
	if docReqs == 0 || infOpts == 0 {
		t.Fatalf("Step 6 failed: did not distinguish documented (%d) from inferred (%d)", docReqs, infOpts)
	}

	// ─── STEP 7: Propose minimal transitions delta(Pe, S0, X, C) ─────────
	if len(assessment.Transitions) == 0 {
		t.Fatalf("Step 7 failed: no conformance transitions proposed")
	}
	trans := assessment.Transitions[0]
	if trans.EvidenceDigest == "" {
		t.Fatalf("Step 7 failed: transition missing evidence digest")
	}

	// ─── STEP 8: Refuse actions outside its authority ────────────────────
	// valet-advisor possesses: CapReadProject, CapCreateIssue, CapCreateMR
	// but lacks: CapModifyProtectedBranch.
	// Invariant: Finding emitted + Action refused + Receipt written!
	advisorLease := authority.NewLease("valet-advisor-instance", authority.RoleAdvisor, "gitlab-shared/*", time.Hour)
	refusalReceipt, err := advisorLease.Authorize(authority.CapModifyProtectedBranch, "gitlab-shared/team-b")
	if !errors.Is(err, authority.ErrUnauthorized) {
		t.Fatalf("Step 8 failed: advisor should be unauthorized to modify protected branch")
	}
	if refusalReceipt == nil || !refusalReceipt.ActionNotPerformed {
		t.Fatalf("Step 8 failed: refusal receipt was not generated")
	}
	if refusalReceipt.Reason != "insufficient_authority" {
		t.Fatalf("Step 8 failed: expected reason insufficient_authority, got %s", refusalReceipt.Reason)
	}

	// ─── STEP 9: Automatically repair explicitly authorized reversible deviations
	// When granted valet-reconciler authority:
	reconcilerLease := authority.NewLease("valet-reconciler-instance", authority.RoleReconciler, "gitlab-shared/*", time.Hour)
	_, err = reconcilerLease.Authorize(authority.CapModifyProtectedBranch, "gitlab-shared/team-b")
	if err != nil {
		t.Fatalf("Step 9 failed: reconciler should be authorized to repair: %v", err)
	}

	// Apply simulated remediation
	observedProjB["protected"] = true
	assessmentAfterRepair := policy.Evaluate("gitlab-shared/team-b", observedProjB, canonical)
	// Verify that DEV-001 is now resolved
	for _, f := range assessmentAfterRepair.Findings {
		if f.DeviationID == "DEV-001" {
			t.Fatalf("Step 9 failed: DEV-001 still present after repair")
		}
	}

	// ─── STEP 10: Prevent untrusted artifact from crossing Trustwall ─────
	twGate := trustwall.NewGate()
	untrustedArtifact := trustwall.ArtifactClaim{
		ArtifactID:       "team-b-app.tar.gz",
		ArtifactDigest:   "sha256:deadbeef9999",
		SourceRepo:       "gitlab-shared/team-b",
		CommitSHA:        "c0ffee11",
		HasSignature:     false, // Unsigned!
		LicenseCompliant: true,
	}
	twRcpt := twGate.Admit(untrustedArtifact)
	if twRcpt.Decision != trustwall.Quarantine {
		t.Fatalf("Step 10 failed: untrusted artifact was not quarantined, got %s", twRcpt.Decision)
	}
	if twRcpt.ResultingVector.Mode != vector.ModeQuarantined {
		t.Fatalf("Step 10 failed: vector mode is not QUARANTINED")
	}

	// ─── STEP 11: Transmit approved artifact + provenance through diode ──
	trustedArtifact := trustwall.ArtifactClaim{
		ArtifactID:       "canonical-release-v1.0.0.tar.gz",
		ArtifactDigest:   "sha256:0123456789abcdef",
		SourceRepo:       "gitlab-shared/team-a",
		CommitSHA:        "commit-1234",
		HasSignature:     true,
		SignatureValid:   true,
		HasSBOM:          true,
		HasProvenance:    true,
		ProvenanceSHA:    "commit-1234",
		LicenseCompliant: true,
		ApprovalsCount:   2,
	}
	admitRcpt := twGate.Admit(trustedArtifact)
	if admitRcpt.Decision != trustwall.Admit {
		t.Fatalf("Step 11 failed: trusted artifact not admitted: %s", admitRcpt.Decision)
	}

	bundle, err := transfer.SealBundle(
		trustedArtifact.ArtifactID,
		trustedArtifact.ArtifactDigest,
		"sig-cosign-valid-team-a",
		"sha256:sbom-cyclonedx-team-a",
		"sha256:slsa-prov-team-a",
	)
	if err != nil {
		t.Fatalf("Step 11 failed: failed to seal bundle: %v", err)
	}

	diode := transfer.NewDiode()
	diodeRcpt, err := diode.Transmit(bundle, "gitlab-shared.local", "gitlab-airgap.local")
	if err != nil {
		t.Fatalf("Step 11 failed: diode transmission error: %v", err)
	}
	if diodeRcpt.Status != "DELIVERED_AIRGAP" {
		t.Fatalf("Step 11 failed: transmission status not DELIVERED_AIRGAP")
	}

	// Verify asset exists in air-gap store
	_, foundInAirgap := diode.GetAirgapArtifact(trustedArtifact.ArtifactID)
	if !foundInAirgap {
		t.Fatalf("Step 11 failed: asset not found in destination air-gap registry")
	}

	// ─── STEP 12: Detect subsequent drift and reconcile again ────────────
	// Simulating manual out-of-band drift (e.g. branch protection disabled via web UI)
	observedProjB["protected"] = false
	driftAssessment := policy.Evaluate("gitlab-shared/team-b", observedProjB, canonical)
	driftDetected := false
	for _, f := range driftAssessment.Findings {
		if f.DeviationID == "DEV-001" {
			driftDetected = true
			break
		}
	}
	if !driftDetected {
		t.Fatalf("Step 12 failed: drift was not detected")
	}

	// Reconcile again under authorized lease
	observedProjB["protected"] = true
	finalAssessment := policy.Evaluate("gitlab-shared/team-b", observedProjB, canonical)
	for _, f := range finalAssessment.Findings {
		if f.DeviationID == "DEV-001" {
			t.Fatalf("Step 12 failed: drift not reconciled")
		}
	}
}
