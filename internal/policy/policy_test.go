package policy

import (
	"testing"

	"github.com/MChorfa/ggvalet/internal/vector"
)

func TestPolicyEvaluationAndFindings(t *testing.T) {
	model := DefaultCanonicalModel()

	// 1. Compliant project
	cleanObserved := map[string]any{
		"protected":         true,
		"unmasked_variable": false,
		"mutable_image_tag": false,
	}
	assessment := Evaluate("gitlab-shared/team-a", cleanObserved, model)
	if len(assessment.Findings) != 0 {
		t.Errorf("expected 0 findings for team-a, got %d", len(assessment.Findings))
	}
	if !assessment.CompositeVector.IsConformant() {
		t.Errorf("team-a should be conformant")
	}

	// 2. Adverse project (Team B)
	badObserved := map[string]any{
		"protected":         false, // DEV-001
		"unmasked_variable": true,  // DEV-003
		"mutable_image_tag": true,  // DEV-006
	}
	badAssessment := Evaluate("gitlab-shared/team-b", badObserved, model)
	if len(badAssessment.Findings) != 3 {
		t.Fatalf("expected 3 findings for team-b, got %d", len(badAssessment.Findings))
	}

	// Verify distinction between Documented Requirements and Inferred Optimizations
	reqCount := 0
	optCount := 0
	for _, f := range badAssessment.Findings {
		if f.Kind == KindDocumentedRequirement {
			reqCount++
		}
		if f.Kind == KindInferredOptimization {
			optCount++
		}
	}
	if reqCount != 2 {
		t.Errorf("expected 2 documented requirements, got %d", reqCount)
	}
	if optCount != 1 {
		t.Errorf("expected 1 inferred optimization, got %d", optCount)
	}

	// Check Anti dominance: Team B must be non-conformant
	if badAssessment.CompositeVector.IsConformant() {
		t.Errorf("team-b with anti violations MUST NOT be conformant")
	}
	if badAssessment.CompositeVector.Anti != vector.AntiContradicts {
		t.Errorf("expected AntiContradicts, got %s", badAssessment.CompositeVector.Anti)
	}
}

func TestCandidateRuleProgression(t *testing.T) {
	rule := DiscoverOptimizationCandidate("expensive_nightly_redundant_run", 100, 83)
	if rule.MatchFrequency != 0.83 {
		t.Errorf("expected frequency 0.83, got %f", rule.MatchFrequency)
	}
	if rule.Stage != StageDiscovered {
		t.Errorf("expected StageDiscovered, got %s", rule.Stage)
	}

	rule.Simulate()
	if rule.Stage != StageSimulated {
		t.Errorf("expected StageSimulated, got %s", rule.Stage)
	}

	err := rule.Promote(StageWarn, "alice-architect")
	if err != nil {
		t.Fatalf("expected promotion success, got %v", err)
	}
	if rule.Stage != StageWarn || rule.ApprovedBy != "alice-architect" {
		t.Errorf("promotion state mismatch: stage=%s approver=%s", rule.Stage, rule.ApprovedBy)
	}
}
