package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func receiptCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "receipt", Short: "Inspect or export durable operation receipts"}
	cmd.AddCommand(receiptExportCmd())
	return cmd
}

func receiptExportCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{Use: "export", Short: "Export append-only receipts as JSONL", RunE: func(cmd *cobra.Command, args []string) error {
		w := cmd.OutOrStdout()
		if output != "" {
			f, err := os.OpenFile(output, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
			if err != nil {
				return fmt.Errorf("open receipt export: %w", err)
			}
			defer f.Close()
			w = f
		}
		return glClient.State.ExportJSONL(cmd.Context(), w)
	}}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Write JSONL to a file")
	return cmd
}
