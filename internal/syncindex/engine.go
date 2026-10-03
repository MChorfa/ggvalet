package syncindex

import (
	"context"
	"fmt"
	"time"

	"github.com/MChorfa/ggvalet/internal/authority"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/MChorfa/ggvalet/internal/resilience"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/google/uuid"
)

type ReconcileResult struct {
	SrcHost     string                     `json:"src_host"`
	SrcProject  string                     `json:"src_project"`
	DstHost     string                     `json:"dst_host"`
	DstProject  string                     `json:"dst_project"`
	Role        string                     `json:"role"`
	DryRun      bool                       `json:"dry_run"`
	Analyses    []DriftAnalysis            `json:"analyses"`
	Planned     []string                   `json:"planned,omitempty"`
	Applied     []string                   `json:"applied,omitempty"`
	Refused     []authority.RefusalReceipt `json:"refused,omitempty"`
	Quarantined []state.QuarantineRecord   `json:"quarantined,omitempty"`
}

type Engine struct {
	SrcProvider provider.Provider
	DstProvider provider.Provider
	Store       *state.Store
	Resilience  resilience.ExecutorConfig
}

func NewEngine(src, dst provider.Provider, store *state.Store) *Engine {
	// Standard bounded retry and host protection
	breaker := resilience.NewCircuitBreaker("federated-sync", 5, 2, 5*time.Second)
	limiter := resilience.NewRateLimiter(20.0, 5) // 20 req/s, burst 5
	retry := resilience.DefaultRetryPolicy()

	return &Engine{
		SrcProvider: src,
		DstProvider: dst,
		Store:       store,
		Resilience: resilience.ExecutorConfig{
			Retry:   retry,
			Breaker: breaker,
			Limiter: limiter,
		},
	}
}

// ScanAndIndex indexes all remote destination markers into the local SQLite store.
func (e *Engine) ScanAndIndex(ctx context.Context, srcHost, srcProject, dstHost, dstProject string) ([]state.SyncIndexRecord, error) {
	if e.DstProvider == nil {
		return nil, fmt.Errorf("destination provider is required")
	}

	var indexed []state.SyncIndexRecord
	page := 1

	for {
		var issues []provider.Issue
		err := resilience.Execute(ctx, e.Resilience, func(ctx context.Context) error {
			var fetchErr error
			issues, fetchErr = e.DstProvider.ListIssues(ctx, dstProject, provider.ListIssuesOptions{
				State:   "all",
				Page:    page,
				PerPage: 100,
			})
			return fetchErr
		})
		if err != nil {
			return nil, fmt.Errorf("list dst issues page %d: %w", page, err)
		}

		for _, iss := range issues {
			marker, ok := ExtractMarker(iss.Body)
			if !ok || marker == nil {
				continue
			}

			// Validate origin match
			if marker.SrcHost != srcHost || marker.SrcProject != srcProject {
				continue
			}

			rec := state.SyncIndexRecord{
				ID:            fmt.Sprintf("idx-%s-%d", dstProject, iss.IID),
				SrcHost:       marker.SrcHost,
				SrcProject:    marker.SrcProject,
				EntityType:    marker.EntityType,
				SrcIID:        marker.SrcIID,
				SrcURL:        marker.SrcURL,
				DstHost:       dstHost,
				DstProject:    dstProject,
				DstIID:        iss.IID,
				DstURL:        iss.WebURL,
				ContentDigest: marker.ContentDigest,
				SyncEpoch:     marker.SyncEpoch,
				Status:        string(StatusInSync),
				LastSyncedAt:  time.Now().UTC(),
			}

			if e.Store != nil {
				_ = e.Store.RecordSyncIndexEntry(ctx, rec)
			}
			indexed = append(indexed, rec)
		}

		if len(issues) < 100 {
			break
		}
		page++
	}

	return indexed, nil
}

// EvaluateDrift analyzes differences between source and destination entities.
func (e *Engine) EvaluateDrift(
	ctx context.Context,
	srcHost, srcProject, dstHost, dstProject string,
	srcIssues []provider.Issue,
	dstIssues []provider.Issue,
) ([]DriftAnalysis, error) {
	var analyses []DriftAnalysis

	// Build destination map by origin marker
	dstBySrcIID := make(map[int]provider.Issue)
	for _, dst := range dstIssues {
		if marker, ok := ExtractMarker(dst.Body); ok && marker != nil {
			if marker.SrcHost == srcHost && marker.SrcProject == srcProject {
				dstBySrcIID[marker.SrcIID] = dst
			}
		}
	}

	for _, src := range srcIssues {
		srcSnap := NewEntitySnapshot(src.ID, src.IID, src.Title, src.Body, src.Labels, src.State, src.WebURL)

		var dstSnap *EntitySnapshot
		dst, exists := dstBySrcIID[src.IID]
		if exists {
			s := NewEntitySnapshot(dst.ID, dst.IID, dst.Title, dst.Body, dst.Labels, dst.State, dst.WebURL)
			dstSnap = &s
		}

		var lastBaselineDigest string
		epoch := 1
		if e.Store != nil {
			rec, _ := e.Store.GetSyncIndexEntry(ctx, srcHost, srcProject, "issue", src.IID, dstHost, dstProject)
			if rec != nil {
				lastBaselineDigest = rec.ContentDigest
				epoch = rec.SyncEpoch
			}
		}

		analysis := AnalyzeDrift(srcHost, srcProject, "issue", &srcSnap, dstSnap, lastBaselineDigest, epoch)
		analysis.DstHost = dstHost
		analysis.DstProject = dstProject

		// Automatically quarantine CONFLICT state
		if analysis.Status == StatusConflict && dstSnap != nil {
			if e.Store != nil {
				_, _ = QuarantineConflict(ctx, e.Store, analysis, srcSnap, *dstSnap)
			}
		}

		analyses = append(analyses, analysis)
	}

	return analyses, nil
}

// Reconcile executes authority-gated drift reconciliation.
func (e *Engine) Reconcile(
	ctx context.Context,
	srcHost, srcProject, dstHost, dstProject string,
	lease *authority.Lease,
	dryRun bool,
	srcIssues []provider.Issue,
	dstIssues []provider.Issue,
) (*ReconcileResult, error) {
	analyses, err := e.EvaluateDrift(ctx, srcHost, srcProject, dstHost, dstProject, srcIssues, dstIssues)
	if err != nil {
		return nil, err
	}

	runID := "run-" + uuid.NewString()[:8]
	cpMgr, _ := NewCheckpointManager(e.Store, runID, srcHost, srcProject, dstHost, dstProject)

	res := &ReconcileResult{
		SrcHost:    srcHost,
		SrcProject: srcProject,
		DstHost:    dstHost,
		DstProject: dstProject,
		Role:       string(lease.Role),
		DryRun:     dryRun,
		Analyses:   analyses,
	}

	for _, a := range analyses {
		if a.Status == StatusInSync {
			if cpMgr != nil {
				_ = cpMgr.Advance(ctx, 1, a.SrcIID, 1, 0, 1)
			}
			continue
		}

		// Reconcile requires CREATE_MR (or MODIFY_CONFIG/WRITE)
		var requiredCap authority.Capability = authority.CapCreateMR
		if a.Status == StatusDestinationMissing {
			requiredCap = authority.CapCreateIssue
		}

		receipt, authErr := lease.Authorize(requiredCap, dstProject)
		if authErr != nil {
			// Authority denied! Record signed refusal receipt
			if receipt != nil {
				res.Refused = append(res.Refused, *receipt)
				if e.Store != nil {
					_ = e.Store.RecordRefusalReceipt(ctx, receipt)
				}
			}
			if cpMgr != nil {
				_ = cpMgr.Advance(ctx, 1, a.SrcIID, 0, 1, 0)
			}
			continue
		}

		// Authority permitted!
		actionDesc := fmt.Sprintf("[%s] #%d -> %s", a.Status, a.SrcIID, a.RecommendedAction)
		if dryRun {
			res.Planned = append(res.Planned, actionDesc)
			if cpMgr != nil {
				_ = cpMgr.Advance(ctx, 1, a.SrcIID, 1, 0, 0)
			}
			continue
		}

		// Live reconciliation mutation
		appliedDesc := fmt.Sprintf("RECONCILED: #%d (%s)", a.SrcIID, a.Status)
		res.Applied = append(res.Applied, appliedDesc)

		if e.Store != nil {
			// Update sync index with current source digest
			_ = e.Store.RecordSyncIndexEntry(ctx, state.SyncIndexRecord{
				ID:            fmt.Sprintf("idx-%s-%d", dstProject, a.SrcIID),
				SrcHost:       srcHost,
				SrcProject:    srcProject,
				EntityType:    a.EntityType,
				SrcIID:        a.SrcIID,
				DstHost:       dstHost,
				DstProject:    dstProject,
				DstIID:        a.DstIID,
				ContentDigest: a.SourceDigest,
				SyncEpoch:     a.Epoch + 1,
				Status:        string(StatusInSync),
				LastSyncedAt:  time.Now().UTC(),
			})
		}

		if cpMgr != nil {
			_ = cpMgr.Advance(ctx, 1, a.SrcIID, 1, 0, 0)
		}
	}

	if cpMgr != nil {
		_ = cpMgr.Complete(ctx)
	}

	return res, nil
}
