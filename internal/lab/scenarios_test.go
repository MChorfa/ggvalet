package lab

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestDeviationsMatrix(t *testing.T) {
	deviations := GenerateDeviations()
	if len(deviations) != 30 {
		t.Fatalf("expected 30 deviations, got %d", len(deviations))
	}

	byCat := DeviationsByCategory()
	categories := []DeviationCategory{
		CatSecurity, CatSupplyChain, CatReliability, CatPerformance,
		CatCost, CatConfig, CatTrust,
	}

	for _, cat := range categories {
		list, ok := byCat[cat]
		if !ok || len(list) == 0 {
			t.Errorf("category %s has no deviations", cat)
		}
	}

	// Verify specific deviations
	dev1, err := GetDeviationByID("DEV-001")
	if err != nil {
		t.Fatalf("DEV-001 lookup error: %v", err)
	}
	if dev1.Category != CatSecurity {
		t.Errorf("expected DEV-001 category SECURITY, got %s", dev1.Category)
	}
	if !dev1.Reversible {
		t.Errorf("DEV-001 should be reversible")
	}

	dev10, err := GetDeviationByID("DEV-010")
	if err != nil {
		t.Fatalf("DEV-010 lookup error: %v", err)
	}
	if dev10.Category != CatSupplyChain {
		t.Errorf("expected DEV-010 category SUPPLY_CHAIN, got %s", dev10.Category)
	}
}

func TestLabServerPermissions(t *testing.T) {
	srv := NewServer()
	defer srv.Close()

	client := srv.httpServer.Client()

	// 1. Observer: GET is allowed
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v4/projects", nil)
	req.Header.Set("PRIVATE-TOKEN", "token-observer")
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected GET OK for observer, got status %d err %v", resp.StatusCode, err)
	}

	// 2. Observer: POST is refused (403 Forbidden)
	body, _ := json.Marshal(map[string]any{"name": "main"})
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/api/v4/projects/gitlab-shared%2Fteam-b/protected_branches", bytes.NewReader(body))
	req.Header.Set("PRIVATE-TOKEN", "token-observer")
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected POST 403 Forbidden for observer, got status %d err %v", resp.StatusCode, err)
	}

	// 3. Advisor: POST to issues is allowed
	issueBody, _ := json.Marshal(map[string]any{"title": "Adopt canonical pipeline", "description": "Please adopt v7"})
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/api/v4/projects/gitlab-shared%2Fteam-b/issues", bytes.NewReader(issueBody))
	req.Header.Set("PRIVATE-TOKEN", "token-advisor")
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected POST issue 201 Created for advisor, got status %d err %v", resp.StatusCode, err)
	}

	// 4. Advisor: POST to protected_branches is refused (403 Forbidden)
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/api/v4/projects/gitlab-shared%2Fteam-b/protected_branches", bytes.NewReader(body))
	req.Header.Set("PRIVATE-TOKEN", "token-advisor")
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected POST protected_branches 403 Forbidden for advisor, got status %d err %v", resp.StatusCode, err)
	}

	// 5. Reconciler: POST to protected_branches is allowed (201 Created)
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/api/v4/projects/gitlab-shared%2Fteam-b/protected_branches", bytes.NewReader(body))
	req.Header.Set("PRIVATE-TOKEN", "token-reconciler")
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected POST protected_branches 201 Created for reconciler, got status %d err %v", resp.StatusCode, err)
	}
}
