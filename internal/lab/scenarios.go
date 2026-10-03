package lab

import (
	"fmt"
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/vector"
)

type DeviationCategory string

const (
	CatSecurity    DeviationCategory = "SECURITY"
	CatSupplyChain DeviationCategory = "SUPPLY_CHAIN"
	CatReliability DeviationCategory = "RELIABILITY"
	CatPerformance DeviationCategory = "PERFORMANCE"
	CatCost        DeviationCategory = "COST"
	CatConfig      DeviationCategory = "CONFIGURATION"
	CatTrust       DeviationCategory = "TRUST"
)

type DeviationFixture struct {
	ID             string               `json:"id"`
	Category       DeviationCategory    `json:"category"`
	Project        string               `json:"project"` // e.g. "gitlab-shared/team-b"
	Title          string               `json:"title"`
	Description    string               `json:"description"`
	ActualState    map[string]any       `json:"actual_state"`
	CanonicalState map[string]any       `json:"canonical_state"`
	Reversible     bool                 `json:"reversible"`
	RequiredCap    authority.Capability `json:"required_capability"`
	InitialVector  vector.StateVector   `json:"initial_vector"`
}

// GenerateDeviations returns all 30 seeded deviation fixtures.
func GenerateDeviations() []DeviationFixture {
	now := time.Now().UTC()
	makeVec := func(id string, anti vector.AntiRelation, mode vector.LifecycleMode) vector.StateVector {
		val := vector.ValenceNegative
		coh := vector.Partial
		if anti == vector.AntiContradicts || anti == vector.AntiAttacks {
			val = vector.ValenceNegative
			coh = vector.Decoherent
		}
		return vector.StateVector{
			EntityID:  id,
			Presence:  vector.PresencePresent,
			Valence:   val,
			Anti:      anti,
			Coherence: coh,
			Evidence:  vector.EvidenceVerified,
			Mode:      mode,
			Epoch:     1,
			Timestamp: now,
		}
	}

	return []DeviationFixture{
		// ─── SECURITY ────────────────────────────────────────────────────────
		{
			ID:          "DEV-001",
			Category:    CatSecurity,
			Project:     "gitlab-shared/team-b",
			Title:       "Unprotected main branch",
			Description: "Main branch allows force push and direct push by all developers without merge review.",
			ActualState: map[string]any{"protected": false, "allow_force_push": true},
			CanonicalState: map[string]any{
				"protected": true, "push_access_level": 0, "merge_access_level": 40,
			},
			Reversible:    true,
			RequiredCap:   authority.CapModifyProtectedBranch,
			InitialVector: makeVec("DEV-001", vector.AntiContradicts, vector.ModeNormal),
		},
		{
			ID:             "DEV-002",
			Category:       CatSecurity,
			Project:        "gitlab-shared/team-b",
			Title:          "Open push permissions to release branch",
			Description:    "Release branches permit direct commits bypassing CI/CD verification gates.",
			ActualState:    map[string]any{"release_push_allowed": true},
			CanonicalState: map[string]any{"release_push_allowed": false},
			Reversible:     true,
			RequiredCap:    authority.CapModifyProtectedBranch,
			InitialVector:  makeVec("DEV-002", vector.AntiContradicts, vector.ModeNormal),
		},
		{
			ID:             "DEV-003",
			Category:       CatSecurity,
			Project:        "gitlab-shared/team-b",
			Title:          "Unmasked CI variable containing API token",
			Description:    "Secret token deploy_key is marked unmasked and visible in pipeline job output.",
			ActualState:    map[string]any{"variable": "DEPLOY_KEY", "masked": false},
			CanonicalState: map[string]any{"variable": "DEPLOY_KEY", "masked": true},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-003", vector.AntiAttacks, vector.ModeNormal),
		},
		{
			ID:             "DEV-004",
			Category:       CatSecurity,
			Project:        "gitlab-shared/team-b",
			Title:          "Missing Secret Detection in pipeline",
			Description:    "CI pipeline does not include the canonical secret-detection template.",
			ActualState:    map[string]any{"secret_detection_included": false},
			CanonicalState: map[string]any{"secret_detection_included": true},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-004", vector.AntiContradicts, vector.ModeNormal),
		},
		{
			ID:             "DEV-005",
			Category:       CatSecurity,
			Project:        "gitlab-shared/team-b",
			Title:          "Missing SAST scanning stage",
			Description:    "Static application security testing stage is absent from .gitlab-ci.yml.",
			ActualState:    map[string]any{"sast_stage_included": false},
			CanonicalState: map[string]any{"sast_stage_included": true},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-005", vector.AntiContradicts, vector.ModeNormal),
		},
		{
			ID:             "DEV-015",
			Category:       CatSecurity,
			Project:        "gitlab-shared/team-c",
			Title:          "Hardcoded credentials in repository files",
			Description:    "Found committed AWS/GitLab access token in test fixtures.",
			ActualState:    map[string]any{"hardcoded_secret": true},
			CanonicalState: map[string]any{"hardcoded_secret": false},
			Reversible:     false, // requires human rotation / re-write history
			RequiredCap:    authority.CapCreateIssue,
			InitialVector:  makeVec("DEV-015", vector.AntiAttacks, vector.ModeSafeHold),
		},

		// ─── SUPPLY CHAIN ───────────────────────────────────────────────────
		{
			ID:          "DEV-006",
			Category:    CatSupplyChain,
			Project:     "gitlab-shared/team-b",
			Title:       "Mutable image tag 'latest' in CI configuration",
			Description: "Job image uses 'registry.example.com/build:latest' allowing silent drift.",
			ActualState: map[string]any{"image": "registry.example.com/build:latest"},
			CanonicalState: map[string]any{
				"image": "registry.example.com/build@sha256:d8e8f9a2b3c4...",
			},
			Reversible:    true,
			RequiredCap:   authority.CapModifyConfig,
			InitialVector: makeVec("DEV-006", vector.AntiNone, vector.ModeDegraded),
		},
		{
			ID:             "DEV-007",
			Category:       CatSupplyChain,
			Project:        "gitlab-shared/team-b",
			Title:          "Image pulled from untrusted public registry",
			Description:    "Container image is referenced from public docker.io instead of internal mirror.",
			ActualState:    map[string]any{"registry": "docker.io"},
			CanonicalState: map[string]any{"registry": "registry.local:5000/internal"},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-007", vector.AntiNone, vector.ModeDegraded),
		},
		{
			ID:             "DEV-008",
			Category:       CatSupplyChain,
			Project:        "gitlab-shared/team-b",
			Title:          "Missing Cosign signature verification",
			Description:    "Production deployment job does not verify Cosign image signature.",
			ActualState:    map[string]any{"cosign_verify": false},
			CanonicalState: map[string]any{"cosign_verify": true},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-008", vector.AntiContradicts, vector.ModeNormal),
		},
		{
			ID:             "DEV-009",
			Category:       CatSupplyChain,
			Project:        "gitlab-shared/team-b",
			Title:          "Missing CycloneDX SBOM generation",
			Description:    "Build pipeline does not generate software bill of materials.",
			ActualState:    map[string]any{"sbom_generated": false},
			CanonicalState: map[string]any{"sbom_generated": true},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-009", vector.AntiNone, vector.ModeNormal),
		},
		{
			ID:             "DEV-010",
			Category:       CatSupplyChain,
			Project:        "gitlab-dedicated/controlled-proj",
			Title:          "Provenance digest mismatch vs commit SHA",
			Description:    "Claimed SLSA provenance references commit SHA differing from signed git tree.",
			ActualState:    map[string]any{"provenance_sha": "abc1234", "git_sha": "def5678"},
			CanonicalState: map[string]any{"provenance_sha": "def5678", "git_sha": "def5678"},
			Reversible:     false,
			RequiredCap:    authority.CapEvaluateTrustwall,
			InitialVector:  makeVec("DEV-010", vector.AntiContradicts, vector.ModeQuarantined),
		},

		// ─── RELIABILITY ────────────────────────────────────────────────────
		{
			ID:             "DEV-011",
			Category:       CatReliability,
			Project:        "gitlab-shared/team-b",
			Title:          "Missing pipeline execution timeout",
			Description:    "Project timeout is left at default unbounded, allowing zombie job locks.",
			ActualState:    map[string]any{"timeout_minutes": 0},
			CanonicalState: map[string]any{"timeout_minutes": 60},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-011", vector.AntiNone, vector.ModeNormal),
		},
		{
			ID:             "DEV-012",
			Category:       CatReliability,
			Project:        "gitlab-shared/team-b",
			Title:          "Flaky test stage without retry backoff policy",
			Description:    "Integration test job fails sporadically and lacks bounded retry budget.",
			ActualState:    map[string]any{"retry_count": 0},
			CanonicalState: map[string]any{"retry_count": 2, "retry_when": "runner_system_failure"},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-012", vector.AntiNone, vector.ModeNormal),
		},
		{
			ID:             "DEV-013",
			Category:       CatReliability,
			Project:        "gitlab-shared/team-b",
			Title:          "Missing pipeline failure notification webhook",
			Description:    "No failure alerts configured on critical release pipelines.",
			ActualState:    map[string]any{"alert_webhook": ""},
			CanonicalState: map[string]any{"alert_webhook": "https://alerts.local/webhook"},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-013", vector.AntiNone, vector.ModeNormal),
		},
		{
			ID:             "DEV-014",
			Category:       CatReliability,
			Project:        "gitlab-shared/team-c",
			Title:          "Deprecated runner tag mapping causing queue stalls",
			Description:    "Jobs targeted tags 'legacy-x86' that have no active runner registered.",
			ActualState:    map[string]any{"tags": []string{"legacy-x86"}},
			CanonicalState: map[string]any{"tags": []string{"canonical-linux"}},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-014", vector.AntiNone, vector.ModeDegraded),
		},

		// ─── PERFORMANCE ────────────────────────────────────────────────────
		{
			ID:             "DEV-016",
			Category:       CatPerformance,
			Project:        "gitlab-shared/team-b",
			Title:          "Uncached package dependency downloads",
			Description:    "Go modules downloaded fresh on every build, adding 4 minutes to each run.",
			ActualState:    map[string]any{"cache_enabled": false},
			CanonicalState: map[string]any{"cache_enabled": true, "cache_key": "go-mod-${CI_COMMIT_REF_SLUG}"},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-016", vector.AntiNone, vector.ModeNormal),
		},
		{
			ID:             "DEV-017",
			Category:       CatPerformance,
			Project:        "gitlab-shared/team-b",
			Title:          "Redundant duplicate build jobs across stages",
			Description:    "Both 'build' and 'test' stages compile the identical binary without artifact sharing.",
			ActualState:    map[string]any{"redundant_build": true},
			CanonicalState: map[string]any{"redundant_build": false},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-017", vector.AntiNone, vector.ModeNormal),
		},
		{
			ID:             "DEV-018",
			Category:       CatPerformance,
			Project:        "gitlab-shared/team-c",
			Title:          "Bloated runner container image (>4GB)",
			Description:    "CI runner base container is 4.8GB uncompressed, causing container pull latency.",
			ActualState:    map[string]any{"image_size_mb": 4800},
			CanonicalState: map[string]any{"image_size_mb": 350},
			Reversible:     false,
			RequiredCap:    authority.CapCreateMR,
			InitialVector:  makeVec("DEV-018", vector.AntiNone, vector.ModeNormal),
		},
		{
			ID:             "DEV-019",
			Category:       CatPerformance,
			Project:        "gitlab-shared/team-b",
			Title:          "Sequential matrix builds where parallelization is viable",
			Description:    "Cross-platform tests run sequentially instead of utilizing GitLab matrix parallel.",
			ActualState:    map[string]any{"parallel_matrix": false},
			CanonicalState: map[string]any{"parallel_matrix": true},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-019", vector.AntiNone, vector.ModeNormal),
		},

		// ─── COST ───────────────────────────────────────────────────────────
		{
			ID:             "DEV-020",
			Category:       CatCost,
			Project:        "gitlab-shared/team-b",
			Title:          "Nightly full pipeline triggered when no commits occurred",
			Description:    "Expensive 2-hour integration suite runs every night without code changes.",
			ActualState:    map[string]any{"skip_empty_nightly": false},
			CanonicalState: map[string]any{"skip_empty_nightly": true},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-020", vector.AntiNone, vector.ModeNormal),
		},
		{
			ID:             "DEV-021",
			Category:       CatCost,
			Project:        "gitlab-shared/team-b",
			Title:          "Artifact retention set to 'never' on ephemeral intermediate jobs",
			Description:    "Debug dumps retained indefinitely, exhausting storage quota.",
			ActualState:    map[string]any{"expire_in": "never"},
			CanonicalState: map[string]any{"expire_in": "7 days"},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-021", vector.AntiNone, vector.ModeNormal),
		},
		{
			ID:             "DEV-022",
			Category:       CatCost,
			Project:        "gitlab-shared/team-c",
			Title:          "Idle dedicated runner provisioned without auto-scale",
			Description:    "High-spec GPU runner remains statically online 24/7 with <2% utilization.",
			ActualState:    map[string]any{"idle_minutes": 1400},
			CanonicalState: map[string]any{"auto_scale": true, "idle_shutdown_minutes": 15},
			Reversible:     false,
			RequiredCap:    authority.CapCreateIssue,
			InitialVector:  makeVec("DEV-022", vector.AntiNone, vector.ModeNormal),
		},

		// ─── CONFIGURATION ──────────────────────────────────────────────────
		{
			ID:             "DEV-023",
			Category:       CatConfig,
			Project:        "gitlab-shared/team-b",
			Title:          "Stale CI component version divergence",
			Description:    "Project imports canonical template v2; active canonical template is v7.",
			ActualState:    map[string]any{"template_version": "v2.1.0"},
			CanonicalState: map[string]any{"template_version": "v7.0.0"},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-023", vector.AntiNone, vector.ModeDegraded),
		},
		{
			ID:             "DEV-024",
			Category:       CatConfig,
			Project:        "gitlab-shared/team-b",
			Title:          "Missing canonical branch naming convention",
			Description:    "No push rules or regex configured to enforce 'feature/*', 'fix/*' naming.",
			ActualState:    map[string]any{"branch_regex": ""},
			CanonicalState: map[string]any{"branch_regex": `^(feature|fix|release)\/[a-z0-9._-]+$`},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-024", vector.AntiNone, vector.ModeNormal),
		},
		{
			ID:             "DEV-025",
			Category:       CatConfig,
			Project:        "gitlab-shared/team-b",
			Title:          "Deprecated 'only/except' syntax instead of 'rules'",
			Description:    ".gitlab-ci.yml uses legacy only/except keywords deprecated in GitLab 16+.",
			ActualState:    map[string]any{"uses_only_except": true},
			CanonicalState: map[string]any{"uses_only_except": false, "uses_rules": true},
			Reversible:     true,
			RequiredCap:    authority.CapModifyConfig,
			InitialVector:  makeVec("DEV-025", vector.AntiNone, vector.ModeNormal),
		},

		// ─── TRUST & TRUSTWALL ──────────────────────────────────────────────
		{
			ID:             "DEV-026",
			Category:       CatTrust,
			Project:        "gitlab-shared/team-b",
			Title:          "Untrusted artifact submitted to Trustwall promotion",
			Description:    "Binary tarball submitted for promotion lacks Cosign signature.",
			ActualState:    map[string]any{"artifact": "team-b-app.tar.gz", "signed": false},
			CanonicalState: map[string]any{"artifact": "team-b-app.tar.gz", "signed": true},
			Reversible:     false,
			RequiredCap:    authority.CapEvaluateTrustwall,
			InitialVector:  makeVec("DEV-026", vector.AntiContradicts, vector.ModeQuarantined),
		},
		{
			ID:             "DEV-027",
			Category:       CatTrust,
			Project:        "gitlab-shared/team-b",
			Title:          "Non-compliant GPLv3 license detected in commercial library",
			Description:    "SPDX license scanner identified AGPL/GPL violation in embedded dependency.",
			ActualState:    map[string]any{"license_compliant": false, "violating_license": "AGPL-3.0"},
			CanonicalState: map[string]any{"license_compliant": true},
			Reversible:     false,
			RequiredCap:    authority.CapEvaluateTrustwall,
			InitialVector:  makeVec("DEV-027", vector.AntiInvalidates, vector.ModeQuarantined),
		},
		{
			ID:             "DEV-028",
			Category:       CatTrust,
			Project:        "gitlab-dedicated/controlled-proj",
			Title:          "Artifact promotion attempted without dual-control sign-off",
			Description:    "Production release gate bypassed peer reviewer signature requirement.",
			ActualState:    map[string]any{"approvals_count": 1, "required_approvals": 2},
			CanonicalState: map[string]any{"approvals_count": 2, "required_approvals": 2},
			Reversible:     false,
			RequiredCap:    authority.CapPromoteArtifact,
			InitialVector:  makeVec("DEV-028", vector.AntiContradicts, vector.ModeSafeHold),
		},
		{
			ID:             "DEV-029",
			Category:       CatTrust,
			Project:        "gitlab-dedicated/controlled-proj",
			Title:          "Quarantined CVE vulnerability package included in release candidate",
			Description:    "Dependency tree contains OpenSSL version flagged under CRITICAL security bulletin.",
			ActualState:    map[string]any{"has_quarantined_cve": true, "cve": "CVE-2026-9999"},
			CanonicalState: map[string]any{"has_quarantined_cve": false},
			Reversible:     false,
			RequiredCap:    authority.CapEvaluateTrustwall,
			InitialVector:  makeVec("DEV-029", vector.AntiInvalidates, vector.ModeQuarantined),
		},
		{
			ID:             "DEV-030",
			Category:       CatTrust,
			Project:        "gitlab-shared/team-c",
			Title:          "State drift after manual web-UI override",
			Description:    "Protected branch rule was modified through Web UI directly, diverging from canonical GitOps declaration.",
			ActualState:    map[string]any{"drift_detected": true, "source": "web_ui"},
			CanonicalState: map[string]any{"drift_detected": false},
			Reversible:     true,
			RequiredCap:    authority.CapModifyProtectedBranch,
			InitialVector:  makeVec("DEV-030", vector.AntiNone, vector.ModeRecovering),
		},
	}
}

// DeviationsByCategory maps all 30 fixtures by category.
func DeviationsByCategory() map[DeviationCategory][]DeviationFixture {
	all := GenerateDeviations()
	out := make(map[DeviationCategory][]DeviationFixture)
	for _, d := range all {
		out[d.Category] = append(out[d.Category], d)
	}
	return out
}

// GetDeviationByID looks up a fixture by its identifier (e.g. "DEV-001").
func GetDeviationByID(id string) (DeviationFixture, error) {
	for _, d := range GenerateDeviations() {
		if d.ID == id {
			return d, nil
		}
	}
	return DeviationFixture{}, fmt.Errorf("deviation %q not found", id)
}
