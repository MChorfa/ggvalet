package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/MChorfa/ggvalet/internal/rotation"
	"github.com/MChorfa/ggvalet/internal/sshaudit"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func sshCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "ssh",
		Short: "Inspect SSH key configuration",
	}
	c.AddCommand(sshAuditCmd())
	return c
}

func sshAuditCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "audit",
		Short: "Report drift between ssh_config, key files on disk, and rotation.yaml",
		Long: `Audit SSH key configuration for drift.

ggvalet never rotates SSH keys automatically: a new key has to be generated
and installed on the remote host by a human. This command only reports; it
never adds, replaces, or deletes a key, and it never touches ~/.ssh or any
remote host.

Checked:
  - every IdentityFile an ssh_config Host block references actually exists
    on disk (DANGLING_REF)
  - key filenames under ~/.ssh do not carry characters that make them
    unusable as ssh_config values, such as embedded whitespace (CORRUPT_NAME)
  - every host rotation.yaml marks as using ssh has a matching ssh_config
    entry

A host with no ssh_config entry and no "ssh" credential in rotation.yaml is
not a fault — not every host uses SSH. Exit is non-zero only when a finding
concerns a host ggvalet is responsible for (rotation.yaml lists "ssh" for
it) and that finding is not a plain match.`,
		Example: `  ggvalet ssh audit
  ggvalet ssh audit --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSSHAudit(os.Stdout, os.Stderr, asJSON)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "emit findings as JSON instead of a table")
	return c
}

// sshConfigPath resolves the ssh_config to audit, honouring the same kind of
// override rotate.go uses for GLVALET_HOME.
func sshConfigPath() string {
	if v := os.Getenv("GLVALET_SSH_CONFIG"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "config")
}

// sshKeyDir resolves the directory key files live in.
func sshKeyDir() string {
	if v := os.Getenv("GLVALET_SSH_DIR"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh")
}

func runSSHAudit(out, errOut io.Writer, asJSON bool) error {
	return sshAuditRun(out, errOut, sshConfigPath(), sshKeyDir(), rotationConfigPath(), asJSON)
}

// sshAuditRun is the testable body of `ggvalet ssh audit`: every path is a
// parameter, so a test can point it at a fixture without touching the real
// ~/.ssh or ~/.ggvalet.
func sshAuditRun(out, errOut io.Writer, configPath, keyDir, rotationPath string, asJSON bool) error {
	entries, err := sshaudit.ParseSSHConfig(configPath)
	if os.IsNotExist(err) {
		fmt.Fprintf(out, "%s\n", colorDim("no ssh_config at "+configPath+" — nothing to audit"))
		entries = nil
	} else if err != nil {
		return fmt.Errorf("reading ssh_config: %w", err)
	}

	rc, err := rotation.Load(rotationPath)
	managed := map[string]bool{}
	switch {
	case os.IsNotExist(err):
		fmt.Fprintf(errOut, "%s\n", colorDim("no rotation.yaml at "+rotationPath+" — no host is scoped as managed"))
	case err != nil:
		return fmt.Errorf("rotation config: %w", err)
	default:
		for host := range rc.Hosts {
			if rc.Uses(host, "ssh") {
				managed[host] = true
			}
		}
	}

	findings, err := sshaudit.Audit(entries, keyDir, managed)
	// A missing key directory is not a fault — a host with no SSH use at all
	// may never have created ~/.ssh. Audit has already appended every finding
	// it could derive from entries before hitting that error.
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading key directory: %w", err)
	}

	findings = append(findings, missingConfigFindings(entries, managed)...)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Host != findings[j].Host {
			return findings[i].Host < findings[j].Host
		}
		if findings[i].Class != findings[j].Class {
			return findings[i].Class < findings[j].Class
		}
		return findings[i].Path < findings[j].Path
	})

	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(findings); err != nil {
			return fmt.Errorf("encoding findings: %w", err)
		}
	} else {
		printSSHFindings(out, findings)
	}

	var problems int
	for _, f := range findings {
		if f.Managed && f.Class != sshaudit.ClassMatched {
			problems++
		}
	}
	if problems > 0 {
		return fmt.Errorf("%d managed SSH finding(s) need attention", problems)
	}
	return nil
}

// missingConfigFindings flags a managed host that rotation.yaml says uses ssh
// but that has no ssh_config Host block at all — the gap `patHosts` used to
// leave silent for PAT-only filtering, mirrored here for the ssh side: a
// declared-but-absent reference is still a dangling one.
func missingConfigFindings(entries []sshaudit.ConfigEntry, managed map[string]bool) []sshaudit.Finding {
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Host] = true
	}
	hosts := make([]string, 0, len(managed))
	for host := range managed {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)

	var out []sshaudit.Finding
	for _, host := range hosts {
		if seen[host] {
			continue
		}
		out = append(out, sshaudit.Finding{
			Class:   sshaudit.ClassDanglingRef,
			Host:    host,
			Detail:  "rotation.yaml declares ssh for this host but ssh_config has no matching Host block",
			Managed: true,
		})
	}
	return out
}

func printSSHFindings(out io.Writer, findings []sshaudit.Finding) {
	if len(findings) == 0 {
		fmt.Fprintf(out, "%s\n", colorOK("no SSH configuration found to audit"))
		return
	}
	table := tablewriter.NewWriter(out)
	table.SetHeader([]string{"Class", "Host", "Path", "Managed", "Detail"})
	table.SetBorder(false)
	table.SetAutoWrapText(false)
	for _, f := range findings {
		mark := ""
		if f.Managed {
			mark = "yes"
		}
		table.Append([]string{f.Class, f.Host, f.Path, mark, f.Detail})
	}
	table.Render()
}
