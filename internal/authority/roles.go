package authority

// RoleName identifies standard pre-defined Valet authority roles.
type RoleName string

const (
	RoleObserver   RoleName = "valet-observer"
	RoleAdvisor    RoleName = "valet-advisor"
	RoleReconciler RoleName = "valet-reconciler"
	RolePromoter   RoleName = "valet-promoter"
	RoleTrustwall  RoleName = "valet-trustwall"
	RoleAdminTest  RoleName = "valet-admin-test"
)

// DefaultCapabilities returns the set of capabilities granted to a role.
func DefaultCapabilities(role RoleName) []Capability {
	switch role {
	case RoleObserver:
		return []Capability{
			CapReadProject,
			CapReadPipeline,
			CapReadArtifact,
			CapReadPolicy,
		}
	case RoleAdvisor:
		return []Capability{
			CapReadProject,
			CapReadPipeline,
			CapReadArtifact,
			CapReadPolicy,
			CapCreateIssue,
			CapCreateMR,
		}
	case RoleReconciler:
		return []Capability{
			CapReadProject,
			CapReadPipeline,
			CapReadArtifact,
			CapReadPolicy,
			CapCreateIssue,
			CapCreateMR,
			CapModifyConfig,
			CapModifyProtectedBranch,
			CapTriggerPipeline,
		}
	case RolePromoter:
		return []Capability{
			CapReadProject,
			CapReadArtifact,
			CapPromoteArtifact,
			CapSignRelease,
		}
	case RoleTrustwall:
		return []Capability{
			CapReadArtifact,
			CapReadPolicy,
			CapEvaluateTrustwall,
			CapAdmitArtifact,
			CapQuarantineArtifact,
		}
	case RoleAdminTest:
		return []Capability{
			CapReadProject,
			CapReadPipeline,
			CapReadArtifact,
			CapReadPolicy,
			CapCreateIssue,
			CapCreateMR,
			CapModifyConfig,
			CapModifyProtectedBranch,
			CapTriggerPipeline,
			CapPromoteArtifact,
			CapSignRelease,
			CapEvaluateTrustwall,
			CapAdmitArtifact,
			CapQuarantineArtifact,
			CapAdminInstance,
		}
	default:
		return nil
	}
}
