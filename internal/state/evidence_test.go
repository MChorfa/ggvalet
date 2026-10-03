package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/policy"
	"github.com/MChorfa/ggvalet/internal/trustwall"
	"github.com/MChorfa/ggvalet/internal/vector"
)

func TestEvidenceStoreIntegration(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test_evidence.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	// 1. Record and query RefusalReceipt
	refusal := &authority.RefusalReceipt{
		ReceiptID:          "rcpt-refuse-001",
		ActionNotPerformed: true,
		Reason:             "insufficient_authority",
		RequiredCapability: authority.CapModifyProtectedBranch,
		AttemptedResource:  "gitlab-shared/team-b",
		SubjectRole:        "valet-observer",
		LeaseID:            "lease-obs-001",
		Timestamp:          time.Now().UTC(),
		EvidenceDigest:     "sha256:abcd1234abcd",
	}
	if err := store.RecordRefusalReceipt(ctx, refusal); err != nil {
		t.Fatalf("RecordRefusalReceipt failed: %v", err)
	}

	receipts, err := store.QueryRefusalReceipts(ctx)
	if err != nil {
		t.Fatalf("QueryRefusalReceipts failed: %v", err)
	}
	if len(receipts) != 1 || receipts[0].ReceiptID != "rcpt-refuse-001" {
		t.Fatalf("expected 1 receipt with ID rcpt-refuse-001, got %v", receipts)
	}
	if receipts[0].Reason != "insufficient_authority" {
		t.Errorf("expected reason insufficient_authority, got %s", receipts[0].Reason)
	}

	// 2. Record and query VectorState
	vec := vector.StateVector{
		EntityID:  "proj-101",
		Presence:  vector.PresencePresent,
		Valence:   vector.ValenceNegative,
		Anti:      vector.AntiContradicts,
		Coherence: vector.Decoherent,
		Evidence:  vector.EvidenceVerified,
		Mode:      vector.ModeDegraded,
		Epoch:     1,
		Timestamp: time.Now().UTC(),
	}
	if err := store.RecordVectorState(ctx, vec); err != nil {
		t.Fatalf("RecordVectorState failed: %v", err)
	}

	latest, err := store.GetLatestVectorState(ctx, "proj-101")
	if err != nil || latest == nil {
		t.Fatalf("GetLatestVectorState failed: %v", err)
	}
	if latest.Valence != vector.ValenceNegative || latest.Anti != vector.AntiContradicts {
		t.Errorf("vector mismatch: got %v", latest)
	}

	allVectors, err := store.ListVectorStates(ctx, 10)
	if err != nil || len(allVectors) == 0 {
		t.Fatalf("ListVectorStates failed: %v, len=%d", err, len(allVectors))
	}
	if allVectors[0].EntityID != "proj-101" {
		t.Errorf("expected proj-101, got %s", allVectors[0].EntityID)
	}

	// 3. Record Trustwall Receipt
	twRcpt := &trustwall.AdmissionReceipt{
		ReceiptID:     "rcpt-tw-001",
		ArtifactID:    "app-v1",
		Decision:      trustwall.Admit,
		Violations:    nil,
		Obligations:   []string{"audit"},
		ReceiptDigest: "sha256:deadbeef",
		EvaluatedAt:   time.Now().UTC(),
	}
	if err := store.RecordTrustwallReceipt(ctx, twRcpt); err != nil {
		t.Fatalf("RecordTrustwallReceipt failed: %v", err)
	}

	// 4. Candidate Rule Lifecycle
	cand := policy.DiscoverOptimizationCandidate("pattern-expensive-rerun", 50, 40)
	if err := store.RecordCandidateRule(ctx, cand); err != nil {
		t.Fatalf("RecordCandidateRule failed: %v", err)
	}

	rules, err := store.ListCandidateRules(ctx)
	if err != nil || len(rules) != 1 {
		t.Fatalf("ListCandidateRules failed: %v, len=%d", err, len(rules))
	}
	if rules[0].Stage != policy.StageDiscovered {
		t.Errorf("expected StageDiscovered, got %s", rules[0].Stage)
	}

	// 5. GetCandidateRule by ID
	fetched, err := store.GetCandidateRule(ctx, cand.ID)
	if err != nil || fetched == nil {
		t.Fatalf("GetCandidateRule failed: %v, fetched=%v", err, fetched)
	}
	if fetched.Title != cand.Title {
		t.Errorf("expected title %q, got %q", cand.Title, fetched.Title)
	}

	// Non-existent ID returns nil, nil
	missing, err := store.GetCandidateRule(ctx, "cand-missing")
	if err != nil || missing != nil {
		t.Fatalf("expected nil for missing rule, got err=%v, rule=%v", err, missing)
	}

	// Update and verify
	cand.Simulate()
	if err := cand.Promote(policy.StageShadow, "test-approver"); err != nil {
		t.Fatalf("promote failed: %v", err)
	}
	if err := store.RecordCandidateRule(ctx, cand); err != nil {
		t.Fatalf("update rule failed: %v", err)
	}
	updated, err := store.GetCandidateRule(ctx, cand.ID)
	if err != nil || updated.Stage != policy.StageShadow || updated.ApprovedBy != "test-approver" {
		t.Fatalf("expected StageShadow and test-approver, got %v", updated)
	}

	// 6. IntegrityCheck and EvidenceCensus
	if err := store.IntegrityCheck(ctx); err != nil {
		t.Fatalf("IntegrityCheck failed: %v", err)
	}
	census, err := store.EvidenceCensus(ctx)
	if err != nil {
		t.Fatalf("EvidenceCensus failed: %v", err)
	}
	if census["refusal_receipts"] != 1 || census["candidate_rules"] != 1 {
		t.Errorf("unexpected census counts: %v", census)
	}
}
