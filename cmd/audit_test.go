package cmd

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/state"
	"github.com/stretchr/testify/require"
)

func TestAuditCmd(t *testing.T) {
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

	// 1. Audit human-readable table output
	cmd1 := auditCmd()
	var buf1 bytes.Buffer
	cmd1.SetOut(&buf1)
	cmd1.SetArgs([]string{"--project", "gitlab-shared/team-b"})
	err = cmd1.Execute()
	require.NoError(t, err)

	// 2. Audit JSON output
	cmd2 := auditCmd()
	var buf2 bytes.Buffer
	cmd2.SetOut(&buf2)
	cmd2.SetArgs([]string{"--project", "gitlab-shared/team-b", "--json"})
	err = cmd2.Execute()
	require.NoError(t, err)
}
