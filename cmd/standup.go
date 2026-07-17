// cmd/standup.go — auto-generate a daily standup from the local journal.
//
// Yesterday section: all journal ops in the last N hours (default 24h, use 72h
// on Mondays to cover the weekend).
// Today section:     open issues assigned to the current user on the active host.
// Blockers:          issues with label "blocked" assigned to current user.
//
// Output targets: stdout (default), file, Slack webhook, Teams webhook.
package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ckodex/gitlabvalet/internal/journal"
	"github.com/spf13/cobra"
	gl "github.com/xanzy/go-gitlab"
)

func standupCmd() *cobra.Command {
	var since, output, slackURL, teamsURL string
	var pushIssue bool
	var dstProject string

	cmd := &cobra.Command{
		Use:   "standup",
		Short: "Generate a daily standup summary from the journal + open issues",
		Example: `  glv standup                        # print to stdout
  glv standup --since 72h             # Monday standup (covers weekend)
  glv standup --output standup.md     # write to file
  glv standup --slack https://hooks.slack.com/...
  glv standup --teams https://outlook.office.com/...`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dur, err := parseDuration(since)
			if err != nil {
				return fmt.Errorf("--since: %w", err)
			}
			sinceTime := time.Now().UTC().Add(-dur)

			// ── Yesterday: journal entries ────────────────────────────────────
			entries, err := glClient.Journal.Query(journal.Filter{
				Since: sinceTime,
				Ops:   []journal.Op{journal.OpCreate, journal.OpUpdate, journal.OpClose, journal.OpComment, journal.OpAssign},
			})
			if err != nil {
				return err
			}

			// ── Today: open issues assigned to me ─────────────────────────────
			myIssues, _, _ := glClient.GL.Issues.ListIssues(&gl.ListIssuesOptions{
				Scope:       gl.Ptr("assigned_to_me"),
				State:       gl.Ptr("opened"),
				ListOptions: gl.ListOptions{PerPage: 20},
			})

			// ── Blocked: open issues with "blocked" label ─────────────────────
			blockedLabel := gl.LabelOptions{"blocked"}
			blocked, _, _ := glClient.GL.Issues.ListIssues(&gl.ListIssuesOptions{
				Scope:       gl.Ptr("assigned_to_me"),
				State:       gl.Ptr("opened"),
				Labels:      &blockedLabel,
				ListOptions: gl.ListOptions{PerPage: 10},
			})

			// ── Render ────────────────────────────────────────────────────────
			var buf strings.Builder
			renderStandup(&buf, entries, myIssues, blocked, cfg.Host, cfg.User, sinceTime)
			text := buf.String()

			// ── Output ────────────────────────────────────────────────────────
			switch {
			case output != "":
				if err := os.WriteFile(output, []byte(text), 0o644); err != nil {
					return err
				}
				ok("Standup written to %s", output)

			case slackURL != "":
				if err := pushSlack(slackURL, text); err != nil {
					return fmt.Errorf("slack push: %w", err)
				}
				ok("Standup pushed to Slack")

			case teamsURL != "":
				if err := pushTeams(teamsURL, text); err != nil {
					return fmt.Errorf("teams push: %w", err)
				}
				ok("Standup pushed to Teams")

			case pushIssue:
				if dstProject == "" {
					dstProject = cfg.DefaultProject
				}
				if dstProject == "" {
					return fmt.Errorf("--dst-project required with --push-issue")
				}
				title := fmt.Sprintf("Standup — %s", time.Now().Format("2006-01-02 Mon"))
				iss, _, err := glClient.GL.Issues.CreateIssue(dstProject, &gl.CreateIssueOptions{
					Title:       gl.Ptr(title),
					Description: gl.Ptr(text),
					Labels:      &gl.LabelOptions{"standup"},
				})
				if err != nil {
					return fmt.Errorf("create standup issue: %w", err)
				}
				ok("Standup issue #%d: %s", iss.IID, iss.WebURL)

			default:
				fmt.Print(text)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&since, "since", "24h", "Journal lookback: 24h | 48h | 72h (use 72h on Mondays)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Write to file")
	cmd.Flags().StringVar(&slackURL, "slack", "", "Slack incoming webhook URL")
	cmd.Flags().StringVar(&teamsURL, "teams", "", "Microsoft Teams webhook URL")
	cmd.Flags().BoolVar(&pushIssue, "push-issue", false, "Create a standup issue on GitLab")
	cmd.Flags().StringVar(&dstProject, "dst-project", "", "Target project for --push-issue")
	return cmd
}

// ─── Renderer ─────────────────────────────────────────────────────────────────

func renderStandup(b *strings.Builder, entries []journal.Entry,
	today []*gl.Issue, blocked []*gl.Issue, host, user string, since time.Time) {

	day := time.Now().Format("Monday, January 02 2006")
	b.WriteString(fmt.Sprintf("## Standup — %s\n", day))
	if user != "" {
		b.WriteString(fmt.Sprintf("**%s** @ `%s`\n", user, host))
	}
	b.WriteString("\n")

	// ── Yesterday ────────────────────────────────────────────────────────────
	b.WriteString("### Yesterday\n")
	if len(entries) == 0 {
		b.WriteString("- _(no recorded activity)_\n")
	}
	// Group by host for multi-instance clarity
	byHost := make(map[string][]journal.Entry)
	for _, e := range entries {
		byHost[e.Host] = append(byHost[e.Host], e)
	}
	for _, h := range sortedHostKeys(byHost) {
		prefix := ""
		if len(byHost) > 1 {
			prefix = fmt.Sprintf("`%s` ", shortHostname(h))
		}
		for _, e := range byHost[h] {
			proj := e.Project
			if proj == "" {
				proj = e.Group
			}
			b.WriteString(fmt.Sprintf("- %s**[%s %s]** %s",
				prefix, e.Entity, e.Op,
				truncate(e.Title, 70)))
			if proj != "" {
				b.WriteString(fmt.Sprintf(" _(%s)_", proj))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	// ── Today ─────────────────────────────────────────────────────────────────
	b.WriteString("### Today\n")
	if len(today) == 0 {
		b.WriteString("- _(no open issues assigned to me)_\n")
	}
	for _, iss := range today {
		ms := ""
		if iss.Milestone != nil {
			ms = fmt.Sprintf(" [%s]", iss.Milestone.Title)
		}
		b.WriteString(fmt.Sprintf("- #%d %s%s\n", iss.IID, truncate(iss.Title, 70), ms))
	}
	b.WriteString("\n")

	// ── Blockers ──────────────────────────────────────────────────────────────
	b.WriteString("### Blockers\n")
	if len(blocked) == 0 {
		b.WriteString("_none_\n")
	} else {
		for _, iss := range blocked {
			b.WriteString(fmt.Sprintf("- #%d %s\n", iss.IID, iss.Title))
		}
	}
	b.WriteString("\n")
}

// ─── Push helpers ─────────────────────────────────────────────────────────────

func pushSlack(webhookURL, text string) error {
	payload := map[string]string{"text": text}
	data, _ := json.Marshal(payload)
	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(data)) //nolint:noctx
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack webhook returned %d", resp.StatusCode)
	}
	return nil
}

func pushTeams(webhookURL, text string) error {
	// Adaptive Card simple text payload
	payload := map[string]any{
		"type": "message",
		"attachments": []map[string]any{{
			"contentType": "application/vnd.microsoft.card.adaptive",
			"content": map[string]any{
				"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
				"type":    "AdaptiveCard",
				"version": "1.2",
				"body": []map[string]any{{
					"type": "TextBlock",
					"text": text,
					"wrap": true,
				}},
			},
		}},
	}
	data, _ := json.Marshal(payload)
	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(data)) //nolint:noctx
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("teams webhook returned %d", resp.StatusCode)
	}
	return nil
}

func sortedHostKeys(m map[string][]journal.Entry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// sort inline
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
