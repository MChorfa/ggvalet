package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTransferCmd_PackageAndDiode(t *testing.T) {
	// 1. Package command human-readable
	pkgCmd := transferCmd()
	pkgCmd.SetArgs([]string{
		"package",
		"--artifact-id", "demo-pkg",
		"--digest", "sha256:1111222233334444",
		"--signature", "sig-valid",
		"--sbom", "sha256:5555666677778888",
		"--provenance", "sha256:9999aaaabbbbcccc",
	})
	err := pkgCmd.Execute()
	require.NoError(t, err)

	// Package command JSON
	pkgJSON := transferCmd()
	pkgJSON.SetArgs([]string{
		"package",
		"--artifact-id", "demo-pkg",
		"--digest", "sha256:1111222233334444",
		"--signature", "sig-valid",
		"--sbom", "sha256:5555666677778888",
		"--provenance", "sha256:9999aaaabbbbcccc",
		"--json",
	})
	err = pkgJSON.Execute()
	require.NoError(t, err)

	// 2. Diode command human-readable
	diodeCmd := transferCmd()
	diodeCmd.SetArgs([]string{
		"diode",
		"--artifact-id", "demo-pkg",
		"--digest", "sha256:1111222233334444",
		"--signature", "sig-valid",
		"--sbom", "sha256:5555666677778888",
		"--provenance", "sha256:9999aaaabbbbcccc",
		"--src", "gitlab-shared.local",
		"--dest", "gitlab-airgap.local",
	})
	err = diodeCmd.Execute()
	require.NoError(t, err)

	// Diode command JSON
	diodeJSON := transferCmd()
	diodeJSON.SetArgs([]string{
		"diode",
		"--artifact-id", "demo-pkg",
		"--digest", "sha256:1111222233334444",
		"--signature", "sig-valid",
		"--sbom", "sha256:5555666677778888",
		"--provenance", "sha256:9999aaaabbbbcccc",
		"--json",
	})
	err = diodeJSON.Execute()
	require.NoError(t, err)
}
