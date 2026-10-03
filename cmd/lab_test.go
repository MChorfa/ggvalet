package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLabCmd_Suite(t *testing.T) {
	// 1. lab up
	upCmd := labCmd()
	upCmd.SetArgs([]string{"up"})
	err := upCmd.Execute()
	require.NoError(t, err)

	// 2. lab seed
	seedCmd := labCmd()
	seedCmd.SetArgs([]string{"seed"})
	err = seedCmd.Execute()
	require.NoError(t, err)

	// lab seed --json
	seedJSON := labCmd()
	seedJSON.SetArgs([]string{"seed", "--json"})
	err = seedJSON.Execute()
	require.NoError(t, err)

	// 3. lab status
	statCmd := labCmd()
	statCmd.SetArgs([]string{"status"})
	err = statCmd.Execute()
	require.NoError(t, err)

	// 4. lab down
	downCmd := labCmd()
	downCmd.SetArgs([]string{"down"})
	err = downCmd.Execute()
	require.NoError(t, err)
}
