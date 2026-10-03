package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/MChorfa/ggvalet/internal/trustwall"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func trustwallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "trustwall",
		Short:             "Manage Trustwall admission, quarantine, and promotion gates",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
	}
	cmd.AddCommand(
		trustwallVerifyCmd(),
	)
	return cmd
}

func trustwallVerifyCmd() *cobra.Command {
	var (
		artifactID string
		digest     string
		sourceRepo string
		commitSHA  string
		signed     bool
		hasSBOM    bool
		hasProv    bool
		provSHA    string
		approvals  int
		licenseOK  bool
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify artifact claims against Trustwall mandatory invariants",
		RunE: func(cmd *cobra.Command, args []string) error {
			if artifactID == "" {
				return fmt.Errorf("--artifact-id is required")
			}

			gate := trustwall.NewGate()
			claim := trustwall.ArtifactClaim{
				ArtifactID:       artifactID,
				ArtifactDigest:   digest,
				SourceRepo:       sourceRepo,
				CommitSHA:        commitSHA,
				HasSignature:     signed,
				SignatureValid:   signed,
				HasSBOM:          hasSBOM,
				HasProvenance:    hasProv,
				ProvenanceSHA:    provSHA,
				ApprovalsCount:   approvals,
				LicenseCompliant: licenseOK,
			}

			rcpt := gate.Admit(claim)

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(rcpt)
			}

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Property", "Value"})
			table.Append([]string{"Artifact ID", rcpt.ArtifactID})
			table.Append([]string{"Decision", string(rcpt.Decision)})
			table.Append([]string{"Vector State", rcpt.ResultingVector.String()})
			table.Append([]string{"Receipt ID", rcpt.ReceiptID})
			table.Append([]string{"Receipt Digest", rcpt.ReceiptDigest})
			table.Render()

			if len(rcpt.Violations) > 0 {
				fmt.Println("\nViolations:")
				for _, v := range rcpt.Violations {
					fmt.Printf("  ✗ %s\n", v)
				}
			}
			if len(rcpt.Obligations) > 0 {
				fmt.Println("\nObligations:")
				for _, o := range rcpt.Obligations {
					fmt.Printf("  • %s\n", o)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&artifactID, "artifact-id", "", "Unique artifact identifier")
	cmd.Flags().StringVar(&digest, "digest", "", "SHA-256 artifact digest")
	cmd.Flags().StringVar(&sourceRepo, "source", "", "Source repository path")
	cmd.Flags().StringVar(&commitSHA, "commit", "", "Git commit SHA")
	cmd.Flags().BoolVar(&signed, "signed", false, "Cosign signature verified")
	cmd.Flags().BoolVar(&hasSBOM, "sbom", false, "CycloneDX SBOM present")
	cmd.Flags().BoolVar(&hasProv, "provenance", false, "SLSA provenance present")
	cmd.Flags().StringVar(&provSHA, "provenance-sha", "", "Commit SHA attested in provenance")
	cmd.Flags().IntVar(&approvals, "approvals", 0, "Number of dual-control peer approvals")
	cmd.Flags().BoolVar(&licenseOK, "license-compliant", true, "Open source license compliance check")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output admission receipt as JSON")

	return cmd
}
