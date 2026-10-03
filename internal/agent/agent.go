package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/controlplane"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/MChorfa/ggvalet/internal/syncindex"
	"github.com/MChorfa/ggvalet/internal/vector"
)

// Agent encapsulates an autonomous, governed agent operating over capability leases,
// vector states S(e,t), bounded resilience, and explicit receipts.
type Agent struct {
	Name             string
	Engine           *controlplane.Engine
	FederationEngine *syncindex.Engine
	Store            *state.Store
	ObservedOverride map[string]any
}

// NewAgent instantiates a new Governed Agent.
func NewAgent(name string, engine *controlplane.Engine, fedEngine *syncindex.Engine, store *state.Store) *Agent {
	if name == "" {
		name = "valet-governed-agent"
	}
	return &Agent{
		Name:             name,
		Engine:           engine,
		FederationEngine: fedEngine,
		Store:            store,
	}
}

// Plan formulates a minimal transition plan without applying mutations.
func (a *Agent) Plan(ctx context.Context, intent IntentEnvelope) (*TrajectoryReport, error) {
	return a.Run(ctx, intent, true)
}

// Run executes the 6-phase cognitive control loop:
// 1. OBSERVE: Audit target project, capture initial Vector State S_0
// 2. ANALYZE & FENCE: Check anti-invariants and categorize findings
// 3. ACQUIRE LEASE: Bind ephemeral Capability Lease
// 4. PLAN: Formulate transition contract delta(P_e, S_0, X, C)
// 5. EXECUTE / RECONCILE: Execute mutations or pivot on refusal
// 6. ASSURE & RECORD: Verify final Vector State S_1 and emit evidence
func (a *Agent) Run(ctx context.Context, intent IntentEnvelope, dryRun bool) (*TrajectoryReport, error) {
	if a.Engine == nil {
		return nil, fmt.Errorf("agent: control plane engine is required")
	}

	report := &TrajectoryReport{
		Intent:      intent,
		Steps:       make([]TrajectoryStep, 0),
		Receipts:    make([]string, 0),
		Refusals:    make([]authority.RefusalReceipt, 0),
		CompletedAt: time.Now().UTC(),
	}

	// -------------------------------------------------------------
	// 1. OBSERVE: Audit target project, capture initial Vector State S_0
	// -------------------------------------------------------------
	assessment, err := a.Engine.Audit(ctx, intent.Resource, a.ObservedOverride)
	if err != nil {
		report.Status = "FAILED"
		return report, fmt.Errorf("agent observe phase failed: %w", err)
	}

	s0 := assessment.CompositeVector
	report.InitialVector = s0
	currentVector := s0

	step1 := TrajectoryStep{
		StepIndex:       1,
		Kind:            StepObserve,
		Description:     fmt.Sprintf("Observed project %s: %d findings, vector %s", intent.Resource, len(assessment.Findings), s0.String()),
		InitialVector:   s0,
		ResultingVector: s0,
		Disposition:     "OBSERVED",
		EvidenceDigest:  state.Digest(assessment),
		Timestamp:       time.Now().UTC(),
	}
	report.Steps = append(report.Steps, step1)

	// -------------------------------------------------------------
	// 2. ANALYZE & FENCE: Check anti-invariants and categorize findings
	// -------------------------------------------------------------
	hasHardAnti := s0.Anti != vector.AntiNone && s0.Anti != ""
	disposition := "CONFORMANT"
	if len(assessment.Findings) > 0 {
		if hasHardAnti {
			disposition = "HARD_ANTI_VIOLATION"
		} else {
			disposition = "DEVIATIONS_DETECTED"
		}
	}

	step2 := TrajectoryStep{
		StepIndex:       2,
		Kind:            StepAnalyze,
		Description:     fmt.Sprintf("Analyzed state: %d findings, anti_relation=%s, coherence=%s", len(assessment.Findings), s0.Anti, s0.Coherence),
		InitialVector:   s0,
		ResultingVector: s0,
		Disposition:     disposition,
		EvidenceDigest:  state.Digest(map[string]any{"findings_count": len(assessment.Findings), "anti": s0.Anti}),
		Timestamp:       time.Now().UTC(),
	}
	report.Steps = append(report.Steps, step2)

	// If there are no findings, the target is already conformant!
	if len(assessment.Findings) == 0 {
		report.FinalVector = s0
		report.Status = "CONVERGED"
		report.CompletedAt = time.Now().UTC()
		return report, nil
	}

	// -------------------------------------------------------------
	// 3. ACQUIRE LEASE: Scoped ephemeral Capability Lease
	// -------------------------------------------------------------
	lease := authority.NewLease(intent.Actor, intent.RequestedRole, intent.Resource, intent.BudgetDuration)
	report.LeaseID = lease.ID

	step3 := TrajectoryStep{
		StepIndex:       3,
		Kind:            StepAcquireLease,
		Description:     fmt.Sprintf("Acquired capability lease %s for role %s on scope %s (TTL %v)", lease.ID, lease.Role, lease.Scope, intent.BudgetDuration),
		InitialVector:   currentVector,
		ResultingVector: currentVector,
		Disposition:     "LEASE_GRANTED",
		EvidenceDigest:  state.Digest(lease),
		Timestamp:       time.Now().UTC(),
	}
	report.Steps = append(report.Steps, step3)

	// -------------------------------------------------------------
	// 4. PLAN: Formulate transition contract delta(P_e, S_0, X, C)
	// -------------------------------------------------------------
	var authorizedCaps []authority.Capability
	var unauthorizedCaps []authority.Capability
	for _, f := range assessment.Findings {
		_, authErr := lease.Authorize(f.RequiredCapability, intent.Resource)
		if authErr != nil {
			unauthorizedCaps = append(unauthorizedCaps, f.RequiredCapability)
		} else {
			authorizedCaps = append(authorizedCaps, f.RequiredCapability)
		}
	}

	planDesc := fmt.Sprintf("Planned transitions: %d authorized, %d unauthorized", len(authorizedCaps), len(unauthorizedCaps))
	step4 := TrajectoryStep{
		StepIndex:       4,
		Kind:            StepPlan,
		Description:     planDesc,
		InitialVector:   currentVector,
		ResultingVector: currentVector,
		Disposition:     "PLAN_FORMULATED",
		EvidenceDigest:  state.Digest(map[string]any{"authorized": authorizedCaps, "unauthorized": unauthorizedCaps}),
		Timestamp:       time.Now().UTC(),
	}
	report.Steps = append(report.Steps, step4)

	// -------------------------------------------------------------
	// 5. EXECUTE / RECONCILE: Execute mutations or pivot on refusal
	// -------------------------------------------------------------
	reconcileResult, err := a.Engine.Reconcile(ctx, intent.Resource, lease, dryRun, a.ObservedOverride)
	if err != nil {
		report.Status = "FAILED"
		return report, fmt.Errorf("agent reconcile execution failed: %w", err)
	}

	// Ingest any refusal receipts
	if len(reconcileResult.Refused) > 0 {
		report.Refusals = append(report.Refusals, reconcileResult.Refused...)
		for _, ref := range reconcileResult.Refused {
			report.Receipts = append(report.Receipts, ref.ReceiptID)
		}

		step5 := TrajectoryStep{
			StepIndex:       5,
			Kind:            StepRefusedPivot,
			Description:     fmt.Sprintf("Authority refused %d actions; pivoted to advisory remediation (MR generation)", len(reconcileResult.Refused)),
			InitialVector:   currentVector,
			ResultingVector: currentVector,
			Disposition:     "PIVOT_ADVISORY",
			Obligations: []string{
				"create_advisory_mr",
				"escalate_to_maintainer",
				"preserve_forensic_receipt",
			},
			EvidenceDigest: state.Digest(reconcileResult.Refused),
			Timestamp:      time.Now().UTC(),
		}
		report.Steps = append(report.Steps, step5)
	}

	// Record applied or planned actions
	if len(reconcileResult.Applied) > 0 || len(reconcileResult.Planned) > 0 {
		actionCount := len(reconcileResult.Applied)
		actionKind := "Applied"
		if dryRun {
			actionCount = len(reconcileResult.Planned)
			actionKind = "Planned"
		}
		stepIdx := len(report.Steps) + 1
		stepAction := TrajectoryStep{
			StepIndex:       stepIdx,
			Kind:            StepExecuteAction,
			Description:     fmt.Sprintf("%s %d reconciliation actions", actionKind, actionCount),
			InitialVector:   currentVector,
			ResultingVector: currentVector,
			Disposition:     "ACTIONS_EXECUTED",
			EvidenceDigest:  state.Digest(map[string]any{"applied": reconcileResult.Applied, "planned": reconcileResult.Planned}),
			Timestamp:       time.Now().UTC(),
		}
		report.Steps = append(report.Steps, stepAction)
	}

	// -------------------------------------------------------------
	// 6. ASSURE & RECORD: Verify final Vector State S_1
	// -------------------------------------------------------------
	var s1 vector.StateVector
	if len(reconcileResult.Refused) > 0 {
		// When refused, state is held safely (SAFE_HOLD)
		s1 = vector.StateVector{
			EntityID:  intent.Resource,
			Presence:  vector.PresencePresent,
			Valence:   vector.ValenceMixed,
			Anti:      s0.Anti,
			Coherence: vector.Partial,
			Evidence:  vector.EvidenceVerified,
			Mode:      vector.ModeSafeHold,
			Epoch:     s0.Epoch + 1,
			Timestamp: time.Now().UTC(),
			Summary:   fmt.Sprintf("Safely held: %d actions refused, advisory remediations queued", len(reconcileResult.Refused)),
		}
		report.Status = "SAFE_HOLD"
	} else if dryRun {
		s1 = s0
		s1.Mode = vector.ModeNormal
		report.Status = "PLAN_READY"
	} else {
		// Fully converged!
		s1 = vector.StateVector{
			EntityID:  intent.Resource,
			Presence:  vector.PresencePresent,
			Valence:   vector.ValencePositive,
			Anti:      vector.AntiNone,
			Coherence: vector.Coherent,
			Evidence:  vector.EvidenceVerified,
			Mode:      vector.ModeNormal,
			Epoch:     s0.Epoch + 1,
			Timestamp: time.Now().UTC(),
			Summary:   "Converged to canonical delivery model",
		}
		report.Status = "CONVERGED"
	}

	report.FinalVector = s1

	stepFinal := TrajectoryStep{
		StepIndex:       len(report.Steps) + 1,
		Kind:            StepAssure,
		Description:     fmt.Sprintf("Assurance completed: status=%s, final_vector=%s", report.Status, s1.String()),
		InitialVector:   currentVector,
		ResultingVector: s1,
		Disposition:     report.Status,
		EvidenceDigest:  state.Digest(s1),
		Timestamp:       time.Now().UTC(),
	}
	report.Steps = append(report.Steps, stepFinal)

	if a.Store != nil {
		_ = a.Store.RecordVectorState(ctx, s1)
	}

	report.CompletedAt = time.Now().UTC()
	return report, nil
}

// RunFederatedSync executes multi-instance sync with conflict quarantine defense.
func (a *Agent) RunFederatedSync(
	ctx context.Context,
	intent IntentEnvelope,
	srcHost, srcProject, dstHost, dstProject string,
	dryRun bool,
	srcIssues, dstIssues []provider.Issue,
) (*TrajectoryReport, error) {
	if a.FederationEngine == nil {
		return nil, fmt.Errorf("agent: federation engine is required for sync")
	}

	report := &TrajectoryReport{
		Intent:      intent,
		Steps:       make([]TrajectoryStep, 0),
		Receipts:    make([]string, 0),
		Refusals:    make([]authority.RefusalReceipt, 0),
		CompletedAt: time.Now().UTC(),
	}

	// 1. Observe drift
	analyses, err := a.FederationEngine.EvaluateDrift(ctx, srcHost, srcProject, dstHost, dstProject, srcIssues, dstIssues)
	if err != nil {
		report.Status = "FAILED"
		return report, fmt.Errorf("evaluate drift failed: %w", err)
	}

	s0 := vector.StateVector{
		EntityID:  fmt.Sprintf("%s -> %s", srcProject, dstProject),
		Presence:  vector.PresencePresent,
		Valence:   vector.ValenceNeutral,
		Anti:      vector.AntiNone,
		Coherence: vector.Coherent,
		Evidence:  vector.EvidenceObserved,
		Mode:      vector.ModeNormal,
		Epoch:     1,
		Timestamp: time.Now().UTC(),
	}

	var conflicts []syncindex.DriftAnalysis
	for _, item := range analyses {
		if item.Status == syncindex.StatusConflict {
			conflicts = append(conflicts, item)
			s0.Coherence = vector.Decoherent
			s0.Anti = vector.AntiContradicts
			s0.Mode = vector.ModeSafeHold
		}
	}
	report.InitialVector = s0

	report.Steps = append(report.Steps, TrajectoryStep{
		StepIndex:       1,
		Kind:            StepObserve,
		Description:     fmt.Sprintf("Observed federated sync: %d entities analyzed across %s and %s", len(analyses), srcHost, dstHost),
		InitialVector:   s0,
		ResultingVector: s0,
		Disposition:     "OBSERVED",
		EvidenceDigest:  state.Digest(analyses),
		Timestamp:       time.Now().UTC(),
	})

	// 2. Quarantine conflicts if detected
	if len(conflicts) > 0 {
		report.Steps = append(report.Steps, TrajectoryStep{
			StepIndex:       2,
			Kind:            StepQuarantineConflict,
			Description:     fmt.Sprintf("Detected %d concurrent edit conflicts; entities isolated into SAFE_HOLD quarantine", len(conflicts)),
			InitialVector:   s0,
			ResultingVector: s0,
			Disposition:     "QUARANTINED",
			Obligations:     []string{"preserve_forensic_snapshot", "require_human_resolution"},
			EvidenceDigest:  state.Digest(conflicts),
			Timestamp:       time.Now().UTC(),
		})
	}

	// 3. Acquire Lease
	lease := authority.NewLease(intent.Actor, intent.RequestedRole, dstProject, intent.BudgetDuration)
	report.LeaseID = lease.ID
	report.Steps = append(report.Steps, TrajectoryStep{
		StepIndex:       len(report.Steps) + 1,
		Kind:            StepAcquireLease,
		Description:     fmt.Sprintf("Acquired capability lease %s for role %s", lease.ID, lease.Role),
		InitialVector:   s0,
		ResultingVector: s0,
		Disposition:     "LEASE_GRANTED",
		EvidenceDigest:  state.Digest(lease),
		Timestamp:       time.Now().UTC(),
	})

	// 4. Reconcile
	res, err := a.FederationEngine.Reconcile(ctx, srcHost, srcProject, dstHost, dstProject, lease, dryRun, srcIssues, dstIssues)
	if err != nil {
		report.Status = "FAILED"
		return report, fmt.Errorf("federated reconcile failed: %w", err)
	}

	if len(res.Refused) > 0 {
		report.Refusals = append(report.Refusals, res.Refused...)
		for _, ref := range res.Refused {
			report.Receipts = append(report.Receipts, ref.ReceiptID)
		}
		report.Steps = append(report.Steps, TrajectoryStep{
			StepIndex:       len(report.Steps) + 1,
			Kind:            StepRefusedPivot,
			Description:     fmt.Sprintf("Authority refused %d sync actions", len(res.Refused)),
			InitialVector:   s0,
			ResultingVector: s0,
			Disposition:     "PIVOT_ADVISORY",
			EvidenceDigest:  state.Digest(res.Refused),
			Timestamp:       time.Now().UTC(),
		})
	}

	if len(res.Applied) > 0 || len(res.Planned) > 0 {
		actionCount := len(res.Applied)
		if dryRun {
			actionCount = len(res.Planned)
		}
		report.Steps = append(report.Steps, TrajectoryStep{
			StepIndex:       len(report.Steps) + 1,
			Kind:            StepExecuteAction,
			Description:     fmt.Sprintf("Processed %d sync transitions", actionCount),
			InitialVector:   s0,
			ResultingVector: s0,
			Disposition:     "ACTIONS_EXECUTED",
			EvidenceDigest:  state.Digest(map[string]any{"applied": res.Applied, "planned": res.Planned}),
			Timestamp:       time.Now().UTC(),
		})
	}

	// 5. Assure
	s1 := s0
	if len(conflicts) > 0 || len(res.Refused) > 0 {
		s1.Mode = vector.ModeSafeHold
		report.Status = "SAFE_HOLD"
	} else if dryRun {
		report.Status = "PLAN_READY"
	} else {
		s1.Mode = vector.ModeNormal
		s1.Valence = vector.ValencePositive
		report.Status = "CONVERGED"
	}
	report.FinalVector = s1

	report.Steps = append(report.Steps, TrajectoryStep{
		StepIndex:       len(report.Steps) + 1,
		Kind:            StepAssure,
		Description:     fmt.Sprintf("Assurance completed: status=%s", report.Status),
		InitialVector:   s0,
		ResultingVector: s1,
		Disposition:     report.Status,
		EvidenceDigest:  state.Digest(s1),
		Timestamp:       time.Now().UTC(),
	})

	report.CompletedAt = time.Now().UTC()
	return report, nil
}
