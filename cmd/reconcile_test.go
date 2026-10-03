package cmd

import (
	"path/filepath"
	"testing"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/stretchr/testify/require"
)

func TestReconcileCmd_RolesAndModes(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.db")
	st, err := state.Open(dbPath)
	require.NoError(t, err)
	defer st.Close()

	origCfg := cfg
	cfg = &config.Config{
		StatePath:      dbPath,
		DefaultProject: "gitlab-shared/team-b",
	}
	defer func() { cfg = origCfg }()

	// 1. Observer role (refusal receipts)
	obsCmd := reconcileCmd()
	obsCmd.SetArgs([]string{"-p", "gitlab-shared/team-b", "--role", "valet-observer"})
	err = obsCmd.Execute()
	require.NoError(t, err)

	// 2. Advisor role with remediate
	advCmd := reconcileCmd()
	advCmd.SetArgs([]string{"-p", "gitlab-shared/team-b", "--role", "valet-advisor", "--remediate"})
	err = advCmd.Execute()
	require.NoError(t, err)

	// 3. Reconciler role dry run
	recDry := reconcileCmd()
	recDry.SetArgs([]string{"-p", "gitlab-shared/team-b", "--role", "valet-reconciler", "--dry-run"})
	err = recDry.Execute()
	require.NoError(t, err)

	// 4. Reconciler live execution with yes
	recLive := reconcileCmd()
	recLive.SetArgs([]string{"-p", "gitlab-shared/team-b", "--role", "valet-reconciler", "--yes"})
	err = recLive.Execute()
	require.NoError(t, err)

	// 5. JSON output
	jsonCmd := reconcileCmd()
	jsonCmd.SetArgs([]string{"-p", "gitlab-shared/team-b", "--role", "valet-reconciler", "--json"})
	err = jsonCmd.Execute()
	require.NoError(t, err)
}
