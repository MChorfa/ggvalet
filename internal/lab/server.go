package lab

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Server is an in-memory high-fidelity GitLab v4 API server for local testing.
type Server struct {
	URL        string
	httpServer *httptest.Server
	mu         sync.RWMutex

	// In-memory data structures
	Projects          map[string]*projectRecord
	ProtectedBranches map[string][]map[string]any
	Variables         map[string][]map[string]any
	Pipelines         map[string][]map[string]any
	Jobs              map[string][]map[string]any
	Issues            map[string][]map[string]any
	MergeRequests     map[string][]map[string]any
	Artifacts         map[string][]byte
}

type projectRecord struct {
	ID            int    `json:"id"`
	Path          string `json:"path"`
	PathNamespace string `json:"path_with_namespace"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	Description   string `json:"description"`
	WebURL        string `json:"web_url"`
}

// NewServer spins up the high-fidelity GitLab lab server.
func NewServer() *Server {
	s := &Server{
		Projects:          make(map[string]*projectRecord),
		ProtectedBranches: make(map[string][]map[string]any),
		Variables:         make(map[string][]map[string]any),
		Pipelines:         make(map[string][]map[string]any),
		Jobs:              make(map[string][]map[string]any),
		Issues:            make(map[string][]map[string]any),
		MergeRequests:     make(map[string][]map[string]any),
		Artifacts:         make(map[string][]byte),
	}

	s.seedDefaults()
	s.httpServer = httptest.NewServer(s.handler())
	s.URL = s.httpServer.URL
	return s
}

// Close shuts down the server.
func (s *Server) Close() {
	if s.httpServer != nil {
		s.httpServer.Close()
	}
}

func (s *Server) seedDefaults() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Team A (Canonical)
	projA := "gitlab-shared/team-a"
	s.Projects[projA] = &projectRecord{
		ID:            101,
		Path:          "team-a",
		PathNamespace: projA,
		Name:          "Team A Canonical",
		DefaultBranch: "main",
		Description:   "Canonical team with v7 pipeline and strict branch protection",
		WebURL:        "http://gitlab-shared.local/gitlab-shared/team-a",
	}
	s.ProtectedBranches[projA] = []map[string]any{
		{
			"name":                "main",
			"push_access_levels":  []map[string]any{{"access_level": 0}},  // No one can push directly
			"merge_access_levels": []map[string]any{{"access_level": 40}}, // Maintainers merge
			"allow_force_push":    false,
		},
	}
	s.Variables[projA] = []map[string]any{
		{"key": "DEPLOY_KEY", "value": "secret-val-masked", "masked": true, "protected": true},
	}
	s.Pipelines[projA] = []map[string]any{
		{
			"id":         1001,
			"iid":        1,
			"project_id": 101,
			"status":     "success",
			"ref":        "main",
			"sha":        "a1b2c3d4e5f6",
			"web_url":    "http://gitlab-shared.local/gitlab-shared/team-a/-/pipelines/1001",
		},
	}

	// 2. Team B (Adverse / Seeded Deviations)
	projB := "gitlab-shared/team-b"
	s.Projects[projB] = &projectRecord{
		ID:            102,
		Path:          "team-b",
		PathNamespace: projB,
		Name:          "Team B Adverse",
		DefaultBranch: "main",
		Description:   "Adverse project with unprotected branch, unmasked tokens, stale templates",
		WebURL:        "http://gitlab-shared.local/gitlab-shared/team-b",
	}
	s.ProtectedBranches[projB] = []map[string]any{} // Empty: DEV-001 (unprotected main branch)
	s.Variables[projB] = []map[string]any{
		{"key": "DEPLOY_KEY", "value": "unmasked-raw-token-12345", "masked": false, "protected": false}, // DEV-003
	}
	s.Pipelines[projB] = []map[string]any{
		{
			"id":         2001,
			"iid":        1,
			"project_id": 102,
			"status":     "failed",
			"ref":        "main",
			"sha":        "b2c3d4e5f6a1",
			"web_url":    "http://gitlab-shared.local/gitlab-shared/team-b/-/pipelines/2001",
		},
	}

	// 3. Team C (Drifted)
	projC := "gitlab-shared/team-c"
	s.Projects[projC] = &projectRecord{
		ID:            103,
		Path:          "team-c",
		PathNamespace: projC,
		Name:          "Team C Drifted",
		DefaultBranch: "main",
		Description:   "Drifted project with deprecated runner tags and manual web UI drift",
		WebURL:        "http://gitlab-shared.local/gitlab-shared/team-c",
	}

	// 4. Controlled Project (Dedicated)
	projDed := "gitlab-dedicated/controlled-proj"
	s.Projects[projDed] = &projectRecord{
		ID:            201,
		Path:          "controlled-proj",
		PathNamespace: projDed,
		Name:          "Controlled Project",
		DefaultBranch: "main",
		Description:   "High-assurance project subject to dual-control Trustwall sign-off",
		WebURL:        "http://gitlab-dedicated.local/gitlab-dedicated/controlled-proj",
	}
}

func (s *Server) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		token := r.Header.Get("PRIVATE-TOKEN")
		method := r.Method
		path := r.URL.Path

		// Token Role Enforcement simulation:
		// - "token-observer": read-only (rejects POST, PUT, DELETE, PATCH with 403)
		// - "token-advisor": read + issues/MRs (rejects protected branch or config mutations with 403)
		// - "token-reconciler": allows mutations
		if token == "token-observer" && method != http.MethodGet {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":   "insufficient_scope",
				"message": "valet-observer token is read-only; mutation forbidden",
			})
			return
		}

		if token == "token-advisor" && method != http.MethodGet {
			// Advisor can only POST to issues or merge_requests
			if !strings.Contains(path, "/issues") && !strings.Contains(path, "/merge_requests") {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":   "insufficient_authority",
					"message": "valet-advisor token cannot mutate project configuration or protected branches directly",
				})
				return
			}
		}

		s.mu.Lock()
		defer s.mu.Unlock()

		// Route: /api/v4/projects
		if path == "/api/v4/projects" && method == http.MethodGet {
			var list []*projectRecord
			for _, p := range s.Projects {
				list = append(list, p)
			}
			_ = json.NewEncoder(w).Encode(list)
			return
		}

		// Route: /api/v4/projects/:id (or URL-encoded path)
		if strings.HasPrefix(path, "/api/v4/projects/") {
			sub := strings.TrimPrefix(path, "/api/v4/projects/")

			// Find project
			var targetProj *projectRecord
			var projKey string
			var subResource string

			for key, p := range s.Projects {
				if sub == key || sub == strconv.Itoa(p.ID) || sub == p.PathNamespace {
					targetProj = p
					projKey = key
					subResource = ""
					break
				}
				if strings.HasPrefix(sub, key+"/") {
					targetProj = p
					projKey = key
					subResource = strings.TrimPrefix(sub, key+"/")
					break
				}
				if strings.HasPrefix(sub, strconv.Itoa(p.ID)+"/") {
					targetProj = p
					projKey = key
					subResource = strings.TrimPrefix(sub, strconv.Itoa(p.ID)+"/")
					break
				}
			}

			if targetProj == nil {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": "404 Project Not Found"})
				return
			}

			// Sub-routes
			if subResource == "" {
				_ = json.NewEncoder(w).Encode(targetProj)
				return
			}

			// Protected branches
			if subResource == "protected_branches" {
				if method == http.MethodGet {
					branches := s.ProtectedBranches[projKey]
					if branches == nil {
						branches = []map[string]any{}
					}
					_ = json.NewEncoder(w).Encode(branches)
					return
				}
				if method == http.MethodPost {
					body, _ := io.ReadAll(r.Body)
					var req map[string]any
					_ = json.Unmarshal(body, &req)
					s.ProtectedBranches[projKey] = append(s.ProtectedBranches[projKey], req)
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(req)
					return
				}
			}

			// Variables
			if subResource == "variables" {
				if method == http.MethodGet {
					vars := s.Variables[projKey]
					if vars == nil {
						vars = []map[string]any{}
					}
					_ = json.NewEncoder(w).Encode(vars)
					return
				}
				if method == http.MethodPost || method == http.MethodPut {
					body, _ := io.ReadAll(r.Body)
					var req map[string]any
					_ = json.Unmarshal(body, &req)
					s.Variables[projKey] = append(s.Variables[projKey], req)
					_ = json.NewEncoder(w).Encode(req)
					return
				}
			}

			// Pipelines
			if subResource == "pipelines" {
				pipes := s.Pipelines[projKey]
				if pipes == nil {
					pipes = []map[string]any{}
				}
				_ = json.NewEncoder(w).Encode(pipes)
				return
			}

			// Issues
			if subResource == "issues" {
				if method == http.MethodGet {
					issues := s.Issues[projKey]
					if issues == nil {
						issues = []map[string]any{}
					}
					_ = json.NewEncoder(w).Encode(issues)
					return
				}
				if method == http.MethodPost {
					body, _ := io.ReadAll(r.Body)
					var req map[string]any
					_ = json.Unmarshal(body, &req)
					req["id"] = len(s.Issues[projKey]) + 1
					req["iid"] = len(s.Issues[projKey]) + 1
					req["created_at"] = time.Now().UTC().Format(time.RFC3339)
					s.Issues[projKey] = append(s.Issues[projKey], req)
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(req)
					return
				}
			}

			// Merge Requests
			if subResource == "merge_requests" {
				if method == http.MethodGet {
					mrs := s.MergeRequests[projKey]
					if mrs == nil {
						mrs = []map[string]any{}
					}
					_ = json.NewEncoder(w).Encode(mrs)
					return
				}
				if method == http.MethodPost {
					body, _ := io.ReadAll(r.Body)
					var req map[string]any
					_ = json.Unmarshal(body, &req)
					req["id"] = len(s.MergeRequests[projKey]) + 1
					req["iid"] = len(s.MergeRequests[projKey]) + 1
					req["created_at"] = time.Now().UTC().Format(time.RFC3339)
					s.MergeRequests[projKey] = append(s.MergeRequests[projKey], req)
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(req)
					return
				}
			}
		}

		// Fallback empty response
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	})
}
