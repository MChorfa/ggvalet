// Work Items are a newer GitLab feature, available via both REST and GraphQL.
// This file uses the REST endpoint /projects/:id/work_items.
// Requires GitLab 15.1+ with work_items feature flag enabled on your instance.
package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/ckodex/gitlabvalet/internal/journal"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// wiType maps friendly names to GitLab work item type IDs.
var wiType = map[string]string{
	"issue":     "ISSUE",
	"task":      "TASK",
	"objective": "OBJECTIVE",
	"keyresult": "KEY_RESULT",
	"kr":        "KEY_RESULT",
}

func workItemCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wi",
		Short: "Manage GitLab work items (tasks, objectives, key results)",
	}
	cmd.AddCommand(
		wiListCmd(),
		wiCreateCmd(),
		wiCloseCmd(),
	)
	return cmd
}

// ─── list ─────────────────────────────────────────────────────────────────────

type wiListResponse struct {
	WorkItems []struct {
		ID    int    `json:"id"`
		IID   int    `json:"iid"`
		Title string `json:"title"`
		State string `json:"state"`
		Type  struct {
			Name string `json:"name"`
		} `json:"work_item_type"`
		Assignees []struct {
			Username string `json:"username"`
		} `json:"assignees"`
		WebURL string `json:"web_url"`
	} `json:"work_items"`
}

func wiListCmd() *cobra.Command {
	var project, state, witype string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List work items in a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}

			url := fmt.Sprintf("%s/api/v4/projects/%s/work_items?state=%s&per_page=50",
				cfg.GitLabURL, urlEncode(project), state)
			if witype != "" {
				if mapped, ok := wiType[witype]; ok {
					url += "&work_item_type_name=" + mapped
				}
			}

			body, err := glGet(url)
			if err != nil {
				glClient.RecErr(journal.OpList, journal.EntityWorkItem, project, "", err.Error())
				return err
			}

			// GitLab returns a list directly for work_items endpoint
			var items []struct {
				ID    int    `json:"id"`
				IID   int    `json:"iid"`
				Title string `json:"title"`
				State string `json:"state"`
				Type  struct {
					Name string `json:"name"`
				} `json:"work_item_type"`
				Assignees []struct {
					Username string `json:"username"`
				} `json:"assignees"`
				WebURL string `json:"web_url"`
			}
			if err := json.Unmarshal(body, &items); err != nil {
				return fmt.Errorf("parse work items: %w", err)
			}

			glClient.Rec(journal.OpList, journal.EntityWorkItem, project, "", 0, 0,
				fmt.Sprintf("list %d work items (state=%s)", len(items), state), "")

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"IID", "Type", "Title", "State", "Assignee"})
			table.SetBorder(false)
			table.SetAutoWrapText(false)
			for _, wi := range items {
				assignee := ""
				if len(wi.Assignees) > 0 {
					assignee = wi.Assignees[0].Username
				}
				table.Append([]string{
					fmt.Sprintf("%d", wi.IID),
					wi.Type.Name,
					truncate(wi.Title, 50),
					wi.State,
					assignee,
				})
			}
			table.Render()
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().StringVar(&state, "state", "opened", "State: opened|closed|all")
	cmd.Flags().StringVar(&witype, "type", "", "Type: task|objective|kr|issue")
	return cmd
}

// ─── create ───────────────────────────────────────────────────────────────────

func wiCreateCmd() *cobra.Command {
	var project, title, wtype string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a work item (task, objective, key result)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if title == "" {
				return fmt.Errorf("--title required")
			}

			typeName := "TASK"
			if wtype != "" {
				if mapped, ok := wiType[wtype]; ok {
					typeName = mapped
				} else {
					typeName = wtype
				}
			}

			payload := map[string]any{
				"title":               title,
				"work_item_type_name": typeName,
			}
			body, err := glPost(fmt.Sprintf("%s/api/v4/projects/%s/work_items",
				cfg.GitLabURL, urlEncode(project)), payload)
			if err != nil {
				glClient.RecErr(journal.OpCreate, journal.EntityWorkItem, project, "", err.Error())
				return err
			}

			var wi struct {
				IID    int    `json:"iid"`
				WebURL string `json:"web_url"`
				Title  string `json:"title"`
			}
			if err := json.Unmarshal(body, &wi); err != nil {
				return fmt.Errorf("parse create response: %w", err)
			}

			glClient.Rec(journal.OpCreate, journal.EntityWorkItem, project, "", 0, wi.IID,
				wi.Title, wi.WebURL)
			ok("Work item #%d created (%s): %s", wi.IID, typeName, wi.WebURL)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().StringVarP(&title, "title", "t", "", "Title (required)")
	cmd.Flags().StringVar(&wtype, "type", "task", "Type: task|objective|kr|issue")
	return cmd
}

// ─── close ────────────────────────────────────────────────────────────────────

func wiCloseCmd() *cobra.Command {
	var project string
	var id int

	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close a work item",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if project == "" {
				return fmt.Errorf("--project required")
			}
			if id == 0 {
				return fmt.Errorf("--id required (work item ID)")
			}

			payload := map[string]any{
				"state_event": "close",
			}
			_, err := glPatch(fmt.Sprintf("%s/api/v4/projects/%s/work_items/%d",
				cfg.GitLabURL, urlEncode(project), id), payload)
			if err != nil {
				glClient.RecErr(journal.OpClose, journal.EntityWorkItem, project, "", err.Error())
				return err
			}

			glClient.Rec(journal.OpClose, journal.EntityWorkItem, project, "", id, 0, "work item", "")
			ok("Work item #%d closed", id)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project path or ID")
	cmd.Flags().IntVar(&id, "id", 0, "Work item ID")
	return cmd
}

// ─── HTTP helpers (uses token from cfg via closure) ───────────────────────────

func glGet(url string) ([]byte, error) {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("PRIVATE-TOKEN", cfg.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GET %s → %d: %s", url, resp.StatusCode, string(body))
	}
	return body, nil
}

func glPost(url string, payload any) ([]byte, error) {
	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	req.Header.Set("PRIVATE-TOKEN", cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("POST %s → %d: %s", url, resp.StatusCode, string(body))
	}
	return body, nil
}

func glPatch(url string, payload any) ([]byte, error) {
	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPatch, url, bytes.NewReader(data))
	req.Header.Set("PRIVATE-TOKEN", cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PATCH %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("PATCH %s → %d: %s", url, resp.StatusCode, string(body))
	}
	return body, nil
}

func urlEncode(s string) string {
	out := ""
	for _, c := range s {
		if c == '/' {
			out += "%2F"
		} else {
			out += string(c)
		}
	}
	return out
}
