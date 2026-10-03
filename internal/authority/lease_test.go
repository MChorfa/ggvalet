package authority

import (
	"errors"
	"testing"
	"time"
)

func TestLeaseAuthorizations(t *testing.T) {
	// 1. Observer: can read project, cannot modify protected branch.
	obsLease := NewLease("valet-daemon", RoleObserver, "gitlab-shared/*", time.Hour)
	receipt, err := obsLease.Authorize(CapReadProject, "gitlab-shared/team-a")
	if err != nil || receipt != nil {
		t.Fatalf("expected read permitted, got err=%v receipt=%v", err, receipt)
	}

	receipt, err = obsLease.Authorize(CapModifyProtectedBranch, "gitlab-shared/team-b")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
	if receipt == nil {
		t.Fatalf("expected non-nil RefusalReceipt")
	}
	if !receipt.ActionNotPerformed {
		t.Errorf("receipt.ActionNotPerformed should be true")
	}
	if receipt.Reason != "insufficient_authority" {
		t.Errorf("expected reason insufficient_authority, got %s", receipt.Reason)
	}
	if receipt.RequiredCapability != CapModifyProtectedBranch {
		t.Errorf("expected required cap %s, got %s", CapModifyProtectedBranch, receipt.RequiredCapability)
	}
	if receipt.EvidenceDigest == "" {
		t.Errorf("expected non-empty EvidenceDigest")
	}

	// 2. Advisor: can read & propose MR, cannot direct modify config.
	advLease := NewLease("valet-daemon", RoleAdvisor, "*", time.Hour)
	if _, err := advLease.Authorize(CapCreateMR, "gitlab-shared/team-b"); err != nil {
		t.Errorf("expected CapCreateMR allowed for advisor, got %v", err)
	}
	receipt, err = advLease.Authorize(CapModifyConfig, "gitlab-shared/team-b")
	if !errors.Is(err, ErrUnauthorized) || receipt == nil {
		t.Errorf("expected CapModifyConfig refused for advisor")
	}

	// 3. Reconciler: can modify config, cannot promote artifact.
	recLease := NewLease("valet-daemon", RoleReconciler, "*", time.Hour)
	if _, err := recLease.Authorize(CapModifyConfig, "gitlab-shared/team-b"); err != nil {
		t.Errorf("expected CapModifyConfig allowed for reconciler, got %v", err)
	}
	receipt, err = recLease.Authorize(CapPromoteArtifact, "gitlab-shared/team-b")
	if !errors.Is(err, ErrUnauthorized) || receipt == nil {
		t.Errorf("expected CapPromoteArtifact refused for reconciler")
	}

	// 4. Scope limit: scoped to team-a cannot touch team-b.
	scopedLease := NewLease("valet-daemon", RoleReconciler, "gitlab-shared/team-a/*", time.Hour)
	receipt, err = scopedLease.Authorize(CapModifyConfig, "gitlab-shared/team-b/repo")
	if !errors.Is(err, ErrScopeExceeded) || receipt == nil {
		t.Errorf("expected ErrScopeExceeded, got %v", err)
	}
	if receipt.Reason != "scope_exceeded" {
		t.Errorf("expected reason scope_exceeded, got %s", receipt.Reason)
	}

	// 5. Expired lease.
	expiredLease := NewLease("valet-daemon", RoleAdminTest, "*", -time.Minute)
	receipt, err = expiredLease.Authorize(CapReadProject, "gitlab-shared/team-a")
	if !errors.Is(err, ErrLeaseExpired) || receipt == nil {
		t.Errorf("expected ErrLeaseExpired, got %v", err)
	}
	if receipt.Reason != "lease_expired" {
		t.Errorf("expected reason lease_expired, got %s", receipt.Reason)
	}
}
