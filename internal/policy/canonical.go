package policy

// CanonicalModel holds the non-negotiable delivery standards.
type CanonicalModel struct {
	Version            string   `json:"version"`
	RequiredStages     []string `json:"required_stages"`
	RequiredTemplates  []string `json:"required_templates"`
	EnforceBranchRules bool     `json:"enforce_branch_rules"`
	AllowedRegistries  []string `json:"allowed_registries"`
	RequireSBOM        bool     `json:"require_sbom"`
	RequireCosign      bool     `json:"require_cosign"`
	MaxTimeoutMinutes  int      `json:"max_timeout_minutes"`
}

// DefaultCanonicalModel returns the v7 Golden Standard for enterprise delivery.
func DefaultCanonicalModel() CanonicalModel {
	return CanonicalModel{
		Version: "v7.0.0",
		RequiredStages: []string{
			"build",
			"test",
			"security-sast",
			"security-secrets",
			"package",
			"sign-attest",
		},
		RequiredTemplates: []string{
			"Jobs/Secret-Detection.gitlab-ci.yml",
			"Jobs/SAST.gitlab-ci.yml",
			"Jobs/Dependency-Scanning.gitlab-ci.yml",
		},
		EnforceBranchRules: true,
		AllowedRegistries: []string{
			"registry.local:5000",
			"registry.internal.corp",
		},
		RequireSBOM:       true,
		RequireCosign:     true,
		MaxTimeoutMinutes: 60,
	}
}
