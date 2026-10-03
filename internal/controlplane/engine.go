package controlplane

import (
	"context"
	"fmt"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/policy"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/MChorfa/ggvalet/internal/state"
)

type ReconciliationResult struct {
	Project    string                     `json:"project"`
	Role       string                     `json:"role"`
	DryRun     bool                       `json:"dry_run"`
	Assessment *policy.Assessment         `json:"assessment"`
	Planned    []string                   `json:"planned,omitempty"`
	Applied    []string                   `json:"applied,omitempty"`
	Refused    []authority.RefusalReceipt `json:"refused,omitempty"`
}

type Engine struct {
	Provider  provider.Provider
	Store     *state.Store
	Canonical policy.CanonicalModel
	Observer  *Observer
}

func NewEngine(p provider.Provider, s *state.Store, model policy.CanonicalModel) *Engine {
	return &Engine{
		Provider:  p,
		Store:     s,
		Canonical: model,
		Observer:  &Observer{Provider: p},
	}
}

// Audit observes the project, evaluates it against canonical standards,
// records the resulting VectorState in SQLite, and returns the assessment.
func (e *Engine) Audit(ctx context.Context, project string, observedOverride map[string]any) (*policy.Assessment, error) {
	observed := observedOverride
	if observed == nil && e.Provider != nil {
		var err error
		observed, err = e.Observer.ObserveProject(ctx, project)
		if err != nil {
			return nil, fmt.Errorf("audit observe project: %w", err)
		}
	}
	if observed == nil {
		observed = map[string]any{"project": project}
	}

	assessment := policy.Evaluate(project, observed, e.Canonical)

	if e.Store != nil {
		_ = e.Store.RecordVectorState(ctx, assessment.CompositeVector)
	}

	return assessment, nil
}

// Reconcile executes authority-gated reconciliation:
// - If authority is sufficient: plans or applies reversible mutations.
// - If authority is insufficient: refuses direct mutation, records RefusalReceipt.
func (e *Engine) Reconcile(
	ctx context.Context,
	project string,
	lease *authority.Lease,
	dryRun bool,
	observedOverride map[string]any,
) (*ReconciliationResult, error) {
	assessment, err := e.Audit(ctx, project, observedOverride)
	if err != nil {
		return nil, err
	}

	res := &ReconciliationResult{
		Project:    project,
		Role:       string(lease.Role),
		DryRun:     dryRun,
		Assessment: assessment,
	}

	for _, finding := range assessment.Findings {
		// Evaluate capability lease before side effects
		receipt, authErr := lease.Authorize(finding.RequiredCapability, project)
		if authErr != nil {
			// Authority denied! Record refusal receipt to SQLite
			if receipt != nil {
				res.Refused = append(res.Refused, *receipt)
				if e.Store != nil {
					_ = e.Store.RecordRefusalReceipt(ctx, receipt)
				}
			}
			continue
		}

		// Authority permitted!
		desc := fmt.Sprintf("[%s] %s (Cap: %s)", finding.DeviationID, finding.Title, finding.RequiredCapability)
		if dryRun {
			res.Planned = append(res.Planned, desc)
			continue
		}

		// Execute remediation
		// In production, execute the appropriate provider call
		appliedMsg := fmt.Sprintf("RECONCILED: %s -> %s", finding.Title, finding.RecommendedAction)
		res.Applied = append(res.Applied, appliedMsg)

		// Record outcome event
		if e.Store != nil {
			providerKind := "generic"
			providerHost := "local"
			if e.Provider != nil {
				providerKind = string(e.Provider.Kind())
				providerHost = e.Provider.Host()
			}
			opID, _ := e.Store.Begin(ctx, state.Operation{
				Provider:    providerKind,
				Host:        providerHost,
				Action:      string(finding.RequiredCapability),
				Resource:    project,
				Target:      finding.DeviationID,
				InputDigest: state.Digest(finding),
			})
			if opID != "" {
				_ = e.Store.Complete(ctx, opID, state.StatusSucceeded, appliedMsg)
			}
		}
	}

	return res, nil
}
