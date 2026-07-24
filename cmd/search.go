// cmd/search.go — cross-instance full-text search across issues, epics, MRs.
// Runs concurrent searches against all configured hosts (or a subset via --host).
package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/MChorfa/ggvalet/internal/client"
	"github.com/MChorfa/ggvalet/internal/config"
	"github.com/MChorfa/ggvalet/internal/provider"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func searchCmd() *cobra.Command {
	var scope, project, group, searchHost string
	var allHosts bool

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Full-text search across issues, epics, MRs — optionally on all instances",
		Args:  cobra.MinimumNArgs(1),
		Example: `  ggvalet search "OIDC token"
  ggvalet search "JWT" --scope issues --project group/project
  ggvalet search "security" --all-hosts
  ggvalet search "sprint" --scope milestones`,
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]

			// Determine which hosts to search
			var hostsToSearch []string
			if allHosts {
				for h := range cfg.Hosts {
					hostsToSearch = append(hostsToSearch, h)
				}
			} else if searchHost != "" {
				hostsToSearch = []string{searchHost}
			} else {
				hostsToSearch = []string{cfg.Host}
			}

			info("Searching %q across %d instance(s)…", query, len(hostsToSearch))
			fmt.Println()

			// Concurrent search across hosts
			type result struct {
				host    string
				results []searchResult
				err     error
			}

			ch := make(chan result, len(hostsToSearch))
			var wg sync.WaitGroup

			for _, h := range hostsToSearch {
				wg.Add(1)
				go func(hostname string) {
					defer wg.Done()
					hCfg, err := config.ForHost(cfg.Hosts, hostname, cfg.JournalPath)
					if err != nil {
						ch <- result{host: hostname, err: err}
						return
					}
					c, err := client.New(hCfg)
					if err != nil {
						ch <- result{host: hostname, err: err}
						return
					}
					results, err := doSearch(c, hostname, query, scope, project, group)
					ch <- result{host: hostname, results: results, err: err}
				}(h)
			}

			go func() {
				wg.Wait()
				close(ch)
			}()

			// Collect and display
			totalFound := 0
			for r := range ch {
				if r.err != nil {
					fmt.Fprintf(os.Stderr, "%s %s: %v\n",
						colorErr("✗"), r.host, r.err)
					continue
				}
				if len(r.results) == 0 {
					continue
				}

				fmt.Printf("%s (%d)\n", colorInfo(shortHostname(r.host)), len(r.results))
				table := tablewriter.NewWriter(os.Stdout)
				table.SetHeader([]string{"Type", "IID", "Title", "State", "Project"})
				table.SetBorder(false)
				table.SetAutoWrapText(false)
				for _, sr := range r.results {
					table.Append([]string{
						sr.Type,
						sr.IID,
						truncate(sr.Title, 52),
						sr.State,
						truncate(sr.Project, 28),
					})
				}
				table.Render()
				fmt.Println()
				totalFound += len(r.results)
			}

			info("Total: %d results", totalFound)
			return nil
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "issues",
		"Search scope: issues | merge_requests | milestones | blobs | commits | notes")
	cmd.Flags().StringVar(&project, "project", "", "Scope to a specific project")
	cmd.Flags().StringVar(&group, "group", "", "Scope to a specific group")
	cmd.Flags().StringVar(&searchHost, "host", "", "Search on a specific host only")
	cmd.Flags().BoolVar(&allHosts, "all-hosts", false, "Search across all configured instances")
	return cmd
}

// ─── Search result ────────────────────────────────────────────────────────────

type searchResult struct {
	Type    string
	IID     string
	Title   string
	State   string
	Project string
	URL     string
}

// doSearch executes a search on one client instance using the REST search API.
func doSearch(c *client.Client, hostname, query, scope, project, group string) ([]searchResult, error) {
	switch scope {
	case "issues":
		return searchIssues(c, hostname, query, project)
	case "merge_requests", "mr", "mrs":
		return searchMRs(c, hostname, query, project)
	case "milestones":
		return searchMilestones(c, hostname, query, project)
	default:
		return searchIssues(c, hostname, query, project)
	}
}

func searchIssues(c *client.Client, hostname, query, project string) ([]searchResult, error) {
	ctx := context.Background()
	var results []searchResult

	if project != "" {
		issues, err := c.Provider.ListIssues(ctx, project, provider.ListIssuesOptions{
			Search:  query,
			PerPage: 25,
		})
		if err != nil {
			return nil, fmt.Errorf("could not search issues in %s: %w", project, err)
		}
		for _, iss := range issues {
			results = append(results, searchResult{
				Type:    "issue",
				IID:     fmt.Sprintf("#%d", iss.IID),
				Title:   iss.Title,
				State:   iss.State,
				Project: iss.Project,
				URL:     iss.WebURL,
			})
		}
	} else {
		// Cross-project search: ListMyIssues returns issues assigned to the
		// authenticated user (closest equivalent to GitLab's scope=all search).
		issues, err := c.Provider.ListMyIssues(ctx, provider.ListMyIssuesOptions{
			PerPage: 25,
		})
		if err != nil {
			return nil, fmt.Errorf("could not search issues across projects: %w", err)
		}
		for _, iss := range issues {
			if query != "" && !containsCI(iss.Title, query) && !containsCI(iss.Body, query) {
				continue
			}
			results = append(results, searchResult{
				Type:    "issue",
				IID:     fmt.Sprintf("#%d", iss.IID),
				Title:   iss.Title,
				State:   iss.State,
				Project: iss.Project,
				URL:     iss.WebURL,
			})
		}
	}
	return results, nil
}

func searchMRs(c *client.Client, hostname, query, project string) ([]searchResult, error) {
	if project == "" {
		project = cfg.DefaultProject
	}
	if project == "" {
		return nil, fmt.Errorf("a project is required to search merge requests — set --project or configure a default project")
	}

	mrs, err := c.Provider.ListMergeRequests(context.Background(), project, provider.ListMergeRequestsOptions{
		Search:  query,
		PerPage: 25,
	})
	if err != nil {
		return nil, fmt.Errorf("could not search merge requests in %s: %w", project, err)
	}

	var results []searchResult
	for _, mr := range mrs {
		results = append(results, searchResult{
			Type:    "mr",
			IID:     fmt.Sprintf("!%d", mr.IID),
			Title:   mr.Title,
			State:   mr.State,
			Project: project,
			URL:     mr.WebURL,
		})
	}
	return results, nil
}

func searchMilestones(c *client.Client, hostname, query, project string) ([]searchResult, error) {
	if project == "" {
		project = cfg.DefaultProject
	}
	if project == "" {
		return nil, fmt.Errorf("a project is required to search milestones — set --project or configure a default project")
	}

	mss, err := c.Provider.ListMilestones(context.Background(), project, provider.ListMilestonesOptions{
		Search:  query,
		PerPage: 25,
	})
	if err != nil {
		return nil, fmt.Errorf("could not search milestones in %s: %w", project, err)
	}

	var results []searchResult
	for _, m := range mss {
		results = append(results, searchResult{
			Type:    "milestone",
			IID:     fmt.Sprintf("%%%d", m.IID),
			Title:   m.Title,
			State:   m.State,
			Project: project,
			URL:     m.WebURL,
		})
	}
	return results, nil
}

// containsCI is a case-insensitive substring check.
func containsCI(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
