package cmd

import (
	"path/filepath"
	"testing"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/policy"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/stretchr/testify/require"
)

func TestRulesCmd_Lifecycle(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.db")
	st, err := state.Open(dbPath)
	require.NoError(t, err)
	defer st.Close()

	origCfg := cfg
	cfg = &config.Config{
		StatePath: dbPath,
	}
	defer func() { cfg = origCfg }()

	// 1. Discover a candidate rule
	discCmd := rulesCmd()
	discCmd.SetArgs([]string{"discover", "--pattern", "Clean tree test", "--runs", "50", "--matches", "45"})
	err = discCmd.Execute()
	require.NoError(t, err)

	// List rules to get the newly discovered rule ID
	rules, err := st.ListCandidateRules(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, rules)
	ruleID := rules[0].ID

	// 2. List candidate rules
	listCmd := rulesCmd()
	listCmd.SetArgs([]string{"list"})
	err = listCmd.Execute()
	require.NoError(t, err)

	// List candidate rules with --json
	listJSON := rulesCmd()
	listJSON.SetArgs([]string{"list", "--json"})
	err = listJSON.Execute()
	require.NoError(t, err)

	// 3. Simulate candidate rule
	simCmd := rulesCmd()
	simCmd.SetArgs([]string{"simulate", "--id", ruleID})
	err = simCmd.Execute()
	require.NoError(t, err)

	// Verify simulated stage
	simRule, err := st.GetCandidateRule(t.Context(), ruleID)
	require.NoError(t, err)
	require.Equal(t, policy.StageSimulated, simRule.Stage)

	// 4. Promote candidate rule to SHADOW
	promShadow := rulesCmd()
	promShadow.SetArgs([]string{"promote", "--id", ruleID, "--stage", "SHADOW", "--approver", "sec-lead"})
	err = promShadow.Execute()
	require.NoError(t, err)

	// 5. Promote candidate rule to WARN
	promWarn := rulesCmd()
	promWarn.SetArgs([]string{"promote", "--id", ruleID, "--stage", "WARN"})
	err = promWarn.Execute()
	require.NoError(t, err)

	// 6. Promote candidate rule to ENFORCE
	promEnforce := rulesCmd()
	promEnforce.SetArgs([]string{"promote", "--id", ruleID, "--stage", "ENFORCE"})
	err = promEnforce.Execute()
	require.NoError(t, err)

	finalRule, err := st.GetCandidateRule(t.Context(), ruleID)
	require.NoError(t, err)
	require.Equal(t, policy.StageEnforce, finalRule.Stage)
}
