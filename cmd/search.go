// cmd/search.go — cross-instance full-text search across issues, epics, MRs.
// Runs concurrent searches against all configured hosts (or a subset via --host).
package cmd

import (
	"fmt"
	"os"
	"sync"

	"github.com/ckodex/gitlabvalet/internal/client"
	"github.com/ckodex/gitlabvalet/internal/config"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	gl "github.com/xanzy/go-gitlab"
)

func searchCmd() *cobra.Command {
	var scope, project, group, searchHost string
	var allHosts bool

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Full-text search across issues, epics, MRs — optionally on all instances",
		Args:  cobra.MinimumNArgs(1),
		Example: `  glv search "OIDC token"
  glv search "JWT" --scope issues --project group/project
  glv search "security" --all-hosts
  glv search "sprint" --scope milestones`,
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
	var results []searchResult
	if project != "" {
		issues, _, err := c.GL.Issues.ListProjectIssues(project, &gl.ListProjectIssuesOptions{
			Search:      gl.Ptr(query),
			ListOptions: gl.ListOptions{PerPage: 25},
		})
		if err != nil {
			return nil, err
		}
		for _, iss := range issues {
			proj := ""
			if iss.References != nil {
				proj = pathBefore(iss.References.Full, "#")
			}
			results = append(results, searchResult{
				Type:    "issue",
				IID:     fmt.Sprintf("#%d", iss.IID),
				Title:   iss.Title,
				State:   iss.State,
				Project: proj,
				URL:     iss.WebURL,
			})
		}
	} else {
		issues, _, err := c.GL.Issues.ListIssues(&gl.ListIssuesOptions{
			Search:      gl.Ptr(query),
			Scope:       gl.Ptr("all"),
			ListOptions: gl.ListOptions{PerPage: 25},
		})
		if err != nil {
			return nil, err
		}
		for _, iss := range issues {
			proj := ""
			if iss.References != nil {
				proj = pathBefore(iss.References.Full, "#")
			}
			results = append(results, searchResult{
				Type:    "issue",
				IID:     fmt.Sprintf("#%d", iss.IID),
				Title:   iss.Title,
				State:   iss.State,
				Project: proj,
				URL:     iss.WebURL,
			})
		}
	}
	return results, nil
}

func searchMRs(c *client.Client, hostname, query, project string) ([]searchResult, error) {
	var results []searchResult
	opts := &gl.ListProjectMergeRequestsOptions{
		Search:      gl.Ptr(query),
		ListOptions: gl.ListOptions{PerPage: 25},
	}
	if project == "" {
		project = cfg.DefaultProject
	}
	if project == "" {
		return nil, nil
	}
	mrs, _, err := c.GL.MergeRequests.ListProjectMergeRequests(project, opts)
	if err != nil {
		return nil, err
	}
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
	var results []searchResult
	if project == "" {
		project = cfg.DefaultProject
	}
	if project == "" {
		return nil, nil
	}
	ms, _, err := c.GL.Milestones.ListMilestones(project, &gl.ListMilestonesOptions{
		Search:      gl.Ptr(query),
		ListOptions: gl.ListOptions{PerPage: 25},
	})
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
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

// pathBefore returns the part of s before the first occurrence of sep.
func pathBefore(s, sep string) string {
	for i := 0; i < len(s)-len(sep)+1; i++ {
		if s[i:i+len(sep)] == sep {
			return s[:i]
		}
	}
	return s
}
