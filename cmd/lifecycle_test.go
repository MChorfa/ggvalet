package cmd

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/stretchr/testify/require"
)

func TestLifecycleCmd_OnboardAndOffboard(t *testing.T) {
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

	// 1. Onboard user
	onboardUser := lifecycleCmd()
	var userBuf bytes.Buffer
	onboardUser.SetOut(&userBuf)
	onboardUser.SetArgs([]string{"onboard", "user", "-i", "alice.developer", "--role", "valet-observer"})
	err = onboardUser.Execute()
	require.NoError(t, err)

	// 2. Onboard project JSON
	onboardProject := lifecycleCmd()
	var projBuf bytes.Buffer
	onboardProject.SetOut(&projBuf)
	onboardProject.SetArgs([]string{"onboard", "project", "-i", "cortaix/space-defense", "--json"})
	err = onboardProject.Execute()
	require.NoError(t, err)

	// 3. Status
	statusCmd := lifecycleCmd()
	var statusBuf bytes.Buffer
	statusCmd.SetOut(&statusBuf)
	statusCmd.SetArgs([]string{"status"})
	err = statusCmd.Execute()
	require.NoError(t, err)

	// 4. Offboard user
	offboardUser := lifecycleCmd()
	var offBuf bytes.Buffer
	offboardUser.SetOut(&offBuf)
	offboardUser.SetArgs([]string{"offboard", "user", "-i", "alice.developer", "--reason", "Departed project"})
	err = offboardUser.Execute()
	require.NoError(t, err)

	// 5. Offboard project JSON
	offboardProject := lifecycleCmd()
	offboardProject.SetArgs([]string{"offboard", "project", "-i", "cortaix/space-defense", "--json"})
	err = offboardProject.Execute()
	require.NoError(t, err)
}
