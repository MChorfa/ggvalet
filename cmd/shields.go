// cmd/shields.go — two complementary features:
//
//  1. "badge" subcommand: generate shields.io badge markdown for a project's
//     health metrics (open issues, pipeline, milestone progress, label counts).
//
//  2. "chips" subcommand: print colored label chips to the terminal using
//     lipgloss — the in-terminal equivalent of GitLab's label shields.
//     Used internally by the TUI, but also callable standalone.
package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func shieldsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shields",
		Short: "Project health badges (shields.io markdown) and label chips",
	}
	cmd.AddCommand(shieldsBadgeCmd(), shieldsChipsCmd())
	return cmd
}

// ─── badge — shields.io markdown ─────────────────────────────────────────────

func shieldsBadgeCmd() *cobra.Command {
	var project, output string
	var endpointJSON bool

	cmd := &cobra.Command{
		Use:   "badge",
		Short: "Generate shields.io badge markdown for a project's README",
		Example: `  ggvalet shields badge --project group/project
  ggvalet shields badge --project group/project --output README-badges.md
  ggvalet shields badge --project group/project --endpoint-json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}

			ctx := cmd.Context()

			// Fetch project metadata via the host-neutral Provider surface.
			proj, err := glClient.Provider.GetProject(ctx, project)
			if err != nil {
				return fmt.Errorf("get project: %w", err)
			}

			// Open issues count
			openCount := proj.OpenIssuesCount

			// Latest pipeline status (best-effort; ignore ErrUnsupported)
			pipelineStatus := "unknown"
			pipelineColor := "lightgrey"
			pipelines, perr := glClient.Provider.ListPipelines(ctx, project, provider.ListPipelinesOptions{
				PerPage: 1,
			})
			if perr == nil && len(pipelines) > 0 {
				pipelineStatus = pipelines[0].Status
				switch pipelineStatus {
				case "success":
					pipelineColor = "brightgreen"
				case "failed":
					pipelineColor = "red"
				case "running":
					pipelineColor = "blue"
				case "pending":
					pipelineColor = "yellow"
				}
			}

			// Active milestone progress (best-effort; ignore ErrUnsupported)
			msText, msColor := "none", "lightgrey"
			milestones, merr := glClient.Provider.ListMilestones(ctx, project, provider.ListMilestonesOptions{
				State:   "active",
				PerPage: 1,
			})
			if merr == nil && len(milestones) > 0 {
				ms := milestones[0]
				msText = fmt.Sprintf("%s_unavailable", strings.ReplaceAll(ms.Title, " ", "_"))
				msColor = "lightgrey"
			}

			// Issue color
			issueColor := "brightgreen"
			if openCount > 20 {
				issueColor = "red"
			} else if openCount > 10 {
				issueColor = "orange"
			} else if openCount > 5 {
				issueColor = "yellow"
			}

			projectURL := proj.WebURL
			encodedProject := strings.ReplaceAll(project, "/", "%2F")
			baseURL := cfg.GitLabURL

			if endpointJSON {
				// shields.io endpoint format (serve this from a static host)
				ep := map[string]any{
					"schemaVersion": 1,
					"label":         "open issues",
					"message":       fmt.Sprintf("%d", openCount),
					"color":         issueColor,
				}
				data, _ := json.MarshalIndent(ep, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			// ── Markdown output ───────────────────────────────────────────────
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("<!-- ggvalet badges for %s -->\n\n", project))

			// Pipeline
			sb.WriteString(fmt.Sprintf(
				"[![Pipeline](%s/%s/badges/main/pipeline.svg)](%s/-/pipelines)\n",
				baseURL, encodedProject, projectURL))

			// Coverage (if available)
			sb.WriteString(fmt.Sprintf(
				"[![Coverage](%s/%s/badges/main/coverage.svg)](%s/-/pipelines)\n",
				baseURL, encodedProject, projectURL))

			// Open issues (shields.io static badge)
			sb.WriteString(fmt.Sprintf(
				"![Open Issues](https://img.shields.io/badge/issues-%d-%s?logo=gitlab)\n",
				openCount, issueColor))

			// Pipeline status (shields.io static badge)
			sb.WriteString(fmt.Sprintf(
				"![Pipeline](https://img.shields.io/badge/pipeline-%s-%s?logo=gitlab)\n",
				pipelineStatus, pipelineColor))

			// Milestone progress
			sb.WriteString(fmt.Sprintf(
				"![Milestone](https://img.shields.io/badge/milestone-%s-%s)\n",
				msText, msColor))

			// GitLab built-in latest release badge
			sb.WriteString(fmt.Sprintf(
				"[![Latest Release](%s/%s/-/badges/release.svg)](%s/-/releases)\n",
				baseURL, encodedProject, projectURL))

			result := sb.String()

			if output != "" {
				if err := os.WriteFile(output, []byte(result), 0o644); err != nil {
					return err
				}
				ok("Badges written to %s", output)
			} else {
				fmt.Print(result)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Write to file")
	cmd.Flags().BoolVar(&endpointJSON, "endpoint-json", false, "Output shields.io endpoint JSON instead of markdown")
	_ = http.DefaultClient // referenced to avoid import removal
	return cmd
}

// ─── chips — colored label chips in terminal ──────────────────────────────────

func shieldsChipsCmd() *cobra.Command {
	var project string
	var issueIID int

	cmd := &cobra.Command{
		Use:   "chips",
		Short: "Print colored label chips for an issue (or all labels in a project)",
		Example: `  ggvalet shields chips --project group/project              # all labels
  ggvalet shields chips --project group/project --iid 42    # labels on issue #42`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}

			if issueIID > 0 {
				// Labels on a specific issue
				iss, err := glClient.Provider.GetIssue(cmd.Context(), project, issueIID)
				if err != nil {
					return fmt.Errorf("get issue: %w", err)
				}

				fmt.Printf("Issue #%d: %s\n\n", iss.IID, iss.Title)
				if len(iss.Labels) == 0 {
					fmt.Println(colorDim("  (no labels)"))
					return nil
				}

				// Fetch project labels to get colors
				allLabels, err := glClient.Provider.ListLabels(cmd.Context(), project)
				if err != nil {
					return fmt.Errorf("list labels: %w", err)
				}
				colorMap := buildLabelColorMap(allLabels)

				chips := renderLabelChips(iss.Labels, colorMap)
				fmt.Println("  " + strings.Join(chips, "  "))
				fmt.Println()
				return nil
			}

			// All project labels as chips
			labels, err := glClient.Provider.ListLabels(cmd.Context(), project)
			if err != nil {
				return fmt.Errorf("list labels: %w", err)
			}

			fmt.Printf("Labels for %s:\n\n", project)
			for _, lbl := range labels {
				chip := renderChip(lbl.Name, lbl.Color)
				fmt.Printf("  %s  open:%d\n", chip, lbl.OpenIssuesCount)
			}
			fmt.Println()
			return nil
		},
	}

	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&issueIID, "iid", 0, "Show chips for a specific issue IID")
	return cmd
}

// ─── Chip rendering (lipgloss) ────────────────────────────────────────────────

// RenderLabelChips returns styled terminal chips for each label name.
// Used by the TUI and the chips subcommand.
func RenderLabelChips(labels []string, colorMap map[string]string) []string {
	return renderLabelChips(labels, colorMap)
}

func renderLabelChips(labels []string, colorMap map[string]string) []string {
	chips := make([]string, 0, len(labels))
	for _, name := range labels {
		hex := colorMap[name]
		chips = append(chips, renderChip(name, hex))
	}
	return chips
}

func renderChip(name, hexColor string) string {
	if hexColor == "" {
		hexColor = "#6e6e6e"
	}
	// Choose contrasting text color (white or black) based on luminance
	textColor := "#ffffff"
	if isLightColor(hexColor) {
		textColor = "#000000"
	}
	style := lipgloss.NewStyle().
		Background(lipgloss.Color(hexColor)).
		Foreground(lipgloss.Color(textColor)).
		PaddingLeft(1).
		PaddingRight(1).
		Bold(true)
	return style.Render(name)
}

func buildLabelColorMap(labels []provider.Label) map[string]string {
	m := make(map[string]string, len(labels))
	for _, l := range labels {
		m[l.Name] = l.Color
	}
	return m
}

// isLightColor returns true if the hex color has high luminance (prefers dark text).
func isLightColor(hex string) bool {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) < 6 {
		return false
	}
	var r, g, b int
	fmt.Sscanf(hex[:2], "%x", &r)
	fmt.Sscanf(hex[2:4], "%x", &g)
	fmt.Sscanf(hex[4:6], "%x", &b)
	// Perceived luminance formula
	luminance := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
	return luminance > 160
}
