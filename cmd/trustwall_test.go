package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrustwallCmd_Verify(t *testing.T) {
	// 1. Admitted claim
	admitCmd := trustwallCmd()
	admitCmd.SetArgs([]string{
		"verify",
		"--artifact-id", "core-binary",
		"--digest", "sha256:1234567890abcdef",
		"--source", "git@gitlab:org/repo.git",
		"--commit", "a1b2c3d4e5",
		"--signed",
		"--sbom",
		"--provenance",
		"--provenance-sha", "a1b2c3d4e5",
		"--approvals", "2",
		"--license-compliant",
	})
	err := admitCmd.Execute()
	require.NoError(t, err)

	// 2. Denied claim (no signature, no sbom, no approvals)
	denyCmd := trustwallCmd()
	denyCmd.SetArgs([]string{
		"verify",
		"--artifact-id", "untrusted-binary",
		"--digest", "sha256:deadbeef",
		"--source", "git@gitlab:org/repo.git",
		"--commit", "f0e1d2c3b4",
	})
	err = denyCmd.Execute()
	require.NoError(t, err)

	// 3. JSON output
	jsonCmd := trustwallCmd()
	jsonCmd.SetArgs([]string{
		"verify",
		"--artifact-id", "core-binary",
		"--digest", "sha256:1234567890abcdef",
		"--signed",
		"--json",
	})
	err = jsonCmd.Execute()
	require.NoError(t, err)
}
