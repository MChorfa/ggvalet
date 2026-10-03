package policy

import (
	"fmt"
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/vector"
	"github.com/google/uuid"
)

type FindingKind string

const (
	KindDocumentedRequirement FindingKind = "DOCUMENTED_REQUIREMENT"
	KindInferredOptimization  FindingKind = "INFERRED_OPTIMIZATION"
)

type Finding struct {
	ID                 string                `json:"id"`
	Kind               FindingKind           `json:"kind"`
	DeviationID        string                `json:"deviation_id,omitempty"`
	Category           string                `json:"category"`
	Project            string                `json:"project"`
	Title              string                `json:"title"`
	Description        string                `json:"description"`
	ObservedState      map[string]any        `json:"observed_state"`
	CanonicalTarget    map[string]any        `json:"canonical_target"`
	EvidenceStatus     vector.EvidenceStatus `json:"evidence_status"`
	StateVector        vector.StateVector    `json:"state_vector"`
	RecommendedAction  string                `json:"recommended_action"`
	ProposedPatch      string                `json:"proposed_patch,omitempty"`
	Reversible         bool                  `json:"reversible"`
	RequiredCapability authority.Capability  `json:"required_capability"`
}

type Assessment struct {
	Project         string                         `json:"project"`
	EvaluatedAt     time.Time                      `json:"evaluated_at"`
	CanonicalModel  CanonicalModel                 `json:"canonical_model"`
	CompositeVector vector.StateVector             `json:"composite_vector"`
	Findings        []Finding                      `json:"findings"`
	Transitions     []vector.ConformanceTransition `json:"transitions"`
}

// Evaluate evaluates a project's observed properties against the canonical model.
func Evaluate(project string, observed map[string]any, model CanonicalModel) *Assessment {
	now := time.Now().UTC()
	var findings []Finding
	var transitions []vector.ConformanceTransition

	// Track overall vector state
	hasAnti := false
	valence := vector.ValencePositive
	coherence := vector.Coherent
	mode := vector.ModeNormal

	// Rule 1: Protected branch
	if isProtected, ok := observed["protected"].(bool); ok && !isProtected {
		s0 := vector.StateVector{
			EntityID:  project + ":branch_protection",
			Presence:  vector.PresencePresent,
			Valence:   vector.ValenceNegative,
			Anti:      vector.AntiContradicts,
			Coherence: vector.Decoherent,
			Evidence:  vector.EvidenceVerified,
			Mode:      vector.ModeNormal,
			Epoch:     1,
			Timestamp: now,
		}
		s1 := s0
		s1.Valence = vector.ValencePositive
		s1.Anti = vector.AntiNone
		s1.Coherence = vector.Coherent

		trans := vector.NewTransition(
			"canonical-branch-protection",
			"sha256-pol-branch",
			s0,
			vector.Stimulus{Type: "RECONCILE_PROTECTED_BRANCH", Target: project + ":main", OccurredAt: now},
			map[string]string{"project": project},
			vector.DispDeny, // currently non-conformant
			[]string{"enforce_maintainer_only_merge"},
			s1,
			[]string{"on_branch_push_rule_drift"},
		)
		transitions = append(transitions, *trans)

		findings = append(findings, Finding{
			ID:                 "FND-" + uuid.NewString()[:8],
			Kind:               KindDocumentedRequirement,
			DeviationID:        "DEV-001",
			Category:           "SECURITY",
			Project:            project,
			Title:              "Main branch unprotected",
			Description:        "Branch protection is missing; direct push allowed.",
			ObservedState:      map[string]any{"protected": false},
			CanonicalTarget:    map[string]any{"protected": true, "push_access_level": 0, "merge_access_level": 40},
			EvidenceStatus:     vector.EvidenceVerified,
			StateVector:        s0,
			RecommendedAction:  "Configure protected branch rules for main to require MR and maintainer approval",
			ProposedPatch:      "POST /api/v4/projects/" + project + "/protected_branches name=main&push_access_level=0&merge_access_level=40",
			Reversible:         true,
			RequiredCapability: authority.CapModifyProtectedBranch,
		})
		hasAnti = true
		valence = vector.ValenceNegative
		coherence = vector.Decoherent
	}

	// Rule 2: Unmasked variable token
	if hasUnmasked, ok := observed["unmasked_variable"].(bool); ok && hasUnmasked {
		s0 := vector.StateVector{
			EntityID:  project + ":unmasked_variable",
			Presence:  vector.PresencePresent,
			Valence:   vector.ValenceNegative,
			Anti:      vector.AntiAttacks,
			Coherence: vector.Decoherent,
			Evidence:  vector.EvidenceVerified,
			Mode:      vector.ModeNormal,
			Epoch:     1,
			Timestamp: now,
		}
		findings = append(findings, Finding{
			ID:                 "FND-" + uuid.NewString()[:8],
			Kind:               KindDocumentedRequirement,
			DeviationID:        "DEV-003",
			Category:           "SECURITY",
			Project:            project,
			Title:              "Unmasked API token in CI variable",
			Description:        "Variable DEPLOY_KEY is unmasked and risks leaking in job logs.",
			ObservedState:      map[string]any{"variable": "DEPLOY_KEY", "masked": false},
			CanonicalTarget:    map[string]any{"variable": "DEPLOY_KEY", "masked": true},
			EvidenceStatus:     vector.EvidenceVerified,
			StateVector:        s0,
			RecommendedAction:  "Mask the DEPLOY_KEY variable in project settings",
			ProposedPatch:      "PUT /api/v4/projects/" + project + "/variables/DEPLOY_KEY masked=true",
			Reversible:         true,
			RequiredCapability: authority.CapModifyConfig,
		})
		hasAnti = true
		valence = vector.ValenceNegative
	}

	// Rule 3: Mutable image tag (Inferred Optimization / Supply Chain)
	if hasMutableTag, ok := observed["mutable_image_tag"].(bool); ok && hasMutableTag {
		s0 := vector.StateVector{
			EntityID:  project + ":mutable_image_tag",
			Presence:  vector.PresencePresent,
			Valence:   vector.ValenceNegative,
			Anti:      vector.AntiNone,
			Coherence: vector.Partial,
			Evidence:  vector.EvidenceObserved,
			Mode:      vector.ModeDegraded,
			Epoch:     1,
			Timestamp: now,
		}
		findings = append(findings, Finding{
			ID:                 "FND-" + uuid.NewString()[:8],
			Kind:               KindInferredOptimization,
			DeviationID:        "DEV-006",
			Category:           "SUPPLY_CHAIN",
			Project:            project,
			Title:              "Mutable container image tag :latest",
			Description:        "CI jobs reference image:latest which introduces non-deterministic builds.",
			ObservedState:      map[string]any{"image": "image:latest"},
			CanonicalTarget:    map[string]any{"image": "image@sha256:..."},
			EvidenceStatus:     vector.EvidenceObserved,
			StateVector:        s0,
			RecommendedAction:  "Pin container image by immutable SHA256 digest in .gitlab-ci.yml",
			ProposedPatch:      "sed -i 's/:latest/@sha256:d8e8f9.../g' .gitlab-ci.yml",
			Reversible:         true,
			RequiredCapability: authority.CapCreateMR,
		})
		if valence == vector.ValencePositive {
			valence = vector.ValenceNeutral
		}
	}

	antiRel := vector.AntiNone
	if hasAnti {
		antiRel = vector.AntiContradicts
		mode = vector.ModeDegraded
	}

	compVec := vector.StateVector{
		EntityID:  project,
		Presence:  vector.PresencePresent,
		Valence:   valence,
		Anti:      antiRel,
		Coherence: coherence,
		Evidence:  vector.EvidenceVerified,
		Mode:      mode,
		Epoch:     1,
		Timestamp: now,
		Summary:   fmt.Sprintf("Evaluated against Canonical %s: %d findings", model.Version, len(findings)),
	}

	return &Assessment{
		Project:         project,
		EvaluatedAt:     now,
		CanonicalModel:  model,
		CompositeVector: compVec,
		Findings:        findings,
		Transitions:     transitions,
	}
}
