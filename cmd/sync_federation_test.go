package cmd

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/stretchr/testify/require"
)

func TestSyncFederationCmd_Suite(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.db")
	st, err := state.Open(dbPath)
	require.NoError(t, err)
	defer st.Close()

	origCfg := cfg
	cfg = &config.Config{
		StatePath:      dbPath,
		Host:           "gitlab-shared.local",
		DefaultProject: "gitlab-shared/team-b",
	}
	defer func() { cfg = origCfg }()

	// Seed a test sync index entry
	err = st.RecordSyncIndexEntry(t.Context(), state.SyncIndexRecord{
		ID:            "idx-1",
		SrcHost:       "gitlab-shared.local",
		SrcProject:    "gitlab-shared/team-b",
		EntityType:    "issue",
		SrcIID:        42,
		SrcURL:        "https://gitlab-shared.local/team-b/-/issues/42",
		DstHost:       "gitlab-dedicated.local",
		DstProject:    "gitlab-dedicated/team-b",
		DstIID:        101,
		DstURL:        "https://gitlab-dedicated.local/team-b/-/issues/101",
		ContentDigest: "sha256:fedcba9876543210",
		SyncEpoch:     1,
		Status:        "SYNCED",
		LastSyncedAt:  time.Now().UTC(),
	})
	require.NoError(t, err)

	// Seed a test quarantine record
	err = st.RecordQuarantine(t.Context(), state.QuarantineRecord{
		ID:               "quar-test-1",
		EntityKey:        "issue:42",
		EntityType:       "issue",
		SrcHost:          "gitlab-shared.local",
		SrcProject:       "gitlab-shared/team-b",
		SrcIID:           42,
		DstHost:          "gitlab-dedicated.local",
		DstProject:       "gitlab-dedicated/team-b",
		DstIID:           101,
		SrcSnapshot:      `{"title":"issue 42"}`,
		DstSnapshot:      `{"title":"issue 101 modified"}`,
		BaselineDigest:   "sha256:base",
		QuarantineReason: "Both sides modified concurrently",
		ResolutionStatus: "QUARANTINED",
		QuarantinedAt:    time.Now().UTC(),
	})
	require.NoError(t, err)

	// 1. sync index scan
	cmdScan := syncCmd()
	cmdScan.SetArgs([]string{"index", "scan"})
	err = cmdScan.Execute()
	require.NoError(t, err)

	// 2. sync index list (human-readable and json)
	cmdList := syncCmd()
	cmdList.SetArgs([]string{"index", "list"})
	err = cmdList.Execute()
	require.NoError(t, err)

	cmdListJSON := syncCmd()
	cmdListJSON.SetArgs([]string{"index", "list", "--json"})
	err = cmdListJSON.Execute()
	require.NoError(t, err)

	// 3. sync drift (human-readable and json)
	cmdDrift := syncCmd()
	cmdDrift.SetArgs([]string{
		"drift",
		"--src-host", "gitlab-shared.local",
		"--src-project", "gitlab-shared/team-b",
		"--dst-host", "gitlab-dedicated.local",
		"--dst-project", "gitlab-dedicated/team-b",
	})
	err = cmdDrift.Execute()
	require.NoError(t, err)

	cmdDriftJSON := syncCmd()
	cmdDriftJSON.SetArgs([]string{
		"drift",
		"--src-host", "gitlab-shared.local",
		"--src-project", "gitlab-shared/team-b",
		"--dst-host", "gitlab-dedicated.local",
		"--dst-project", "gitlab-dedicated/team-b",
		"--json",
	})
	err = cmdDriftJSON.Execute()
	require.NoError(t, err)

	// 4. sync reconcile (dry-run and live)
	cmdRecDry := syncCmd()
	cmdRecDry.SetArgs([]string{
		"reconcile",
		"--src-host", "gitlab-shared.local",
		"--src-project", "gitlab-shared/team-b",
		"--dst-host", "gitlab-dedicated.local",
		"--dst-project", "gitlab-dedicated/team-b",
		"--dry-run",
	})
	err = cmdRecDry.Execute()
	require.NoError(t, err)

	cmdRecLive := syncCmd()
	cmdRecLive.SetArgs([]string{
		"reconcile",
		"--src-host", "gitlab-shared.local",
		"--src-project", "gitlab-shared/team-b",
		"--dst-host", "gitlab-dedicated.local",
		"--dst-project", "gitlab-dedicated/team-b",
		"--yes",
	})
	err = cmdRecLive.Execute()
	require.NoError(t, err)

	// 5. sync quarantine list & inspect
	cmdQList := syncCmd()
	cmdQList.SetArgs([]string{"quarantine", "list"})
	err = cmdQList.Execute()
	require.NoError(t, err)

	cmdQListJSON := syncCmd()
	cmdQListJSON.SetArgs([]string{"quarantine", "list", "--json"})
	err = cmdQListJSON.Execute()
	require.NoError(t, err)

	cmdQInspect := syncCmd()
	cmdQInspect.SetArgs([]string{"quarantine", "inspect", "--id", "quar-test-1"})
	err = cmdQInspect.Execute()
	require.NoError(t, err)
}
