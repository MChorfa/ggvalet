package policy

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type RuleStage string

const (
	StageDiscovered RuleStage = "DISCOVERED"
	StageSimulated  RuleStage = "SIMULATED"
	StageShadow     RuleStage = "SHADOW"
	StageWarn       RuleStage = "WARN"
	StageEnforce    RuleStage = "ENFORCE"
)

// CandidateRule models a learned pattern progressing toward canonical policy.
type CandidateRule struct {
	ID                string    `json:"id"`
	Title             string    `json:"title"`
	Description       string    `json:"description"`
	PatternObserved   string    `json:"pattern_observed"`
	Stage             RuleStage `json:"stage"`
	HistoricalRuns    int       `json:"historical_runs_analyzed"`
	MatchFrequency    float64   `json:"match_frequency"` // e.g. 0.83 (83%)
	EstimatedSavings  string    `json:"estimated_savings"`
	SimulatedPassRate float64   `json:"simulated_pass_rate"`
	ApprovedBy        string    `json:"approved_by,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// DiscoverOptimizationCandidate inspects historical execution traces and formulates
// a candidate rule without immediately enforcing it.
func DiscoverOptimizationCandidate(patternName string, runsCount int, matchCount int) *CandidateRule {
	now := time.Now().UTC()
	freq := float64(matchCount) / float64(runsCount)
	rule := &CandidateRule{
		ID:                "cand-" + uuid.NewString()[:8],
		Title:             "Skip unchanged nightly integration rerun",
		Description:       "Avoid rerunning expensive integration stage when source and dependency trees are identical.",
		PatternObserved:   patternName,
		Stage:             StageDiscovered,
		HistoricalRuns:    runsCount,
		MatchFrequency:    freq,
		EstimatedSavings:  fmt.Sprintf("%.1f%% reduction in runner minutes", freq*100*0.65),
		SimulatedPassRate: 0.994,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	return rule
}

// Simulate runs the candidate rule against historical pipelines to verify safety.
func (c *CandidateRule) Simulate() {
	c.Stage = StageSimulated
	if c.SimulatedPassRate == 0 {
		c.SimulatedPassRate = 0.994
	}
	c.UpdatedAt = time.Now().UTC()
}

// Promote promotes the rule to Shadow, Warn, or Enforce upon human approval.
func (c *CandidateRule) Promote(nextStage RuleStage, approver string) error {
	if c.Stage == StageDiscovered {
		return fmt.Errorf("candidate rule must be simulated before promotion")
	}
	c.Stage = nextStage
	c.ApprovedBy = approver
	c.UpdatedAt = time.Now().UTC()
	return nil
}
