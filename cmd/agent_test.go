package cmd

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/stretchr/testify/require"
)

func TestAgentCmd_RunAndPlan(t *testing.T) {
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

	// 1. Plan
	planCmd := agentCmd()
	var planBuf bytes.Buffer
	planCmd.SetOut(&planBuf)
	planCmd.SetArgs([]string{"plan", "-p", "gitlab-shared/team-b", "--role", "valet-observer"})
	err = planCmd.Execute()
	require.NoError(t, err)

	// 2. Run under valet-observer (Refusal & SAFE_HOLD)
	runCmd := agentCmd()
	var runBuf bytes.Buffer
	runCmd.SetOut(&runBuf)
	runCmd.SetArgs([]string{"run", "-p", "gitlab-shared/team-b", "--role", "valet-observer", "--json"})
	err = runCmd.Execute()
	require.NoError(t, err)

	// 3. Run under valet-reconciler with --yes (Converge)
	convergeCmd := agentCmd()
	var convergeBuf bytes.Buffer
	convergeCmd.SetOut(&convergeBuf)
	convergeCmd.SetArgs([]string{"run", "-p", "gitlab-shared/team-b", "--role", "valet-reconciler", "--yes"})
	err = convergeCmd.Execute()
	require.NoError(t, err)

	// 4. Status
	statusCmd := agentCmd()
	var statusBuf bytes.Buffer
	statusCmd.SetOut(&statusBuf)
	statusCmd.SetArgs([]string{"status"})
	err = statusCmd.Execute()
	require.NoError(t, err)

	// 5. Status JSON
	statusJSONCmd := agentCmd()
	statusJSONCmd.SetArgs([]string{"status", "--json"})
	err = statusJSONCmd.Execute()
	require.NoError(t, err)
}
