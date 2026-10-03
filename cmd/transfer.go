package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/MChorfa/ggvalet/internal/transfer"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func transferCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "transfer",
		Short:             "Manage sealed air-gap bundle packaging and diode transmission",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
	}
	cmd.AddCommand(
		transferPackageCmd(),
		transferDiodeCmd(),
	)
	return cmd
}

func transferPackageCmd() *cobra.Command {
	var (
		artifactID string
		digest     string
		signature  string
		sbomDigest string
		provDigest string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "package",
		Short: "Seal an admitted artifact into an immutable air-gap evidence bundle",
		RunE: func(cmd *cobra.Command, args []string) error {
			if artifactID == "" || digest == "" || signature == "" || sbomDigest == "" || provDigest == "" {
				return fmt.Errorf("all parameters (--artifact-id, --digest, --signature, --sbom, --provenance) are required")
			}

			bundle, err := transfer.SealBundle(artifactID, digest, signature, sbomDigest, provDigest)
			if err != nil {
				return fmt.Errorf("seal bundle: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(bundle)
			}

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Property", "Value"})
			table.Append([]string{"Bundle ID", bundle.BundleID})
			table.Append([]string{"Artifact ID", bundle.ArtifactID})
			table.Append([]string{"Artifact Digest", bundle.ArtifactDigest})
			table.Append([]string{"Merkle Root", bundle.MerkleRoot})
			table.Append([]string{"Manifest Digest", bundle.ManifestDigest})
			table.Render()
			fmt.Println("\nEvidence bundle sealed and ready for diode transmission.")
			return nil
		},
	}

	cmd.Flags().StringVar(&artifactID, "artifact-id", "", "Artifact identifier")
	cmd.Flags().StringVar(&digest, "digest", "", "Artifact SHA-256 digest")
	cmd.Flags().StringVar(&signature, "signature", "", "Cosign cryptographic signature")
	cmd.Flags().StringVar(&sbomDigest, "sbom", "", "CycloneDX SBOM digest")
	cmd.Flags().StringVar(&provDigest, "provenance", "", "SLSA provenance digest")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output bundle details as JSON")

	return cmd
}

func transferDiodeCmd() *cobra.Command {
	var (
		artifactID string
		digest     string
		signature  string
		sbomDigest string
		provDigest string
		srcDomain  string
		destDomain string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "diode",
		Short: "Transmit sealed bundle through hardware diode into air-gap registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			if srcDomain == "" {
				srcDomain = "gitlab-shared.local"
			}
			if destDomain == "" {
				destDomain = "gitlab-airgap.local"
			}

			bundle, err := transfer.SealBundle(artifactID, digest, signature, sbomDigest, provDigest)
			if err != nil {
				return fmt.Errorf("seal bundle: %w", err)
			}

			diode := transfer.NewDiode()
			rcpt, err := diode.Transmit(bundle, srcDomain, destDomain)
			if err != nil {
				return fmt.Errorf("diode transmission failed: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(rcpt)
			}

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Property", "Value"})
			table.Append([]string{"Receipt ID", rcpt.ReceiptID})
			table.Append([]string{"Bundle ID", rcpt.BundleID})
			table.Append([]string{"Source Domain", rcpt.SourceDomain})
			table.Append([]string{"Destination Domain", rcpt.TargetDomain})
			table.Append([]string{"Status", rcpt.Status})
			table.Append([]string{"Merkle Root", rcpt.MerkleRoot})
			table.Render()
			fmt.Printf("\n✓ Successfully transmitted to isolated destination %s.\n", destDomain)
			return nil
		},
	}

	cmd.Flags().StringVar(&artifactID, "artifact-id", "demo-pkg", "Artifact identifier")
	cmd.Flags().StringVar(&digest, "digest", "sha256:1111222233334444", "Artifact SHA-256 digest")
	cmd.Flags().StringVar(&signature, "signature", "sig-valid", "Cosign cryptographic signature")
	cmd.Flags().StringVar(&sbomDigest, "sbom", "sha256:5555666677778888", "CycloneDX SBOM digest")
	cmd.Flags().StringVar(&provDigest, "provenance", "sha256:9999aaaabbbbcccc", "SLSA provenance digest")
	cmd.Flags().StringVar(&srcDomain, "src", "gitlab-shared.local", "Source domain")
	cmd.Flags().StringVar(&destDomain, "dest", "gitlab-airgap.local", "Destination airgap domain")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output diode receipt as JSON")

	return cmd
}
