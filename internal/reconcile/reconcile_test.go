package reconcile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tf-preflight/tf-preflight/internal/azure"
	"github.com/tf-preflight/tf-preflight/internal/model"
)

func TestFilterCandidates_OnlyManagedCreates(t *testing.T) {
	candidates := []model.Candidate{
		{Address: "keep", Action: "create", Mode: "managed"},
		{Address: "also-keep", Action: "create"},
		{Address: "skip-update", Action: "update", Mode: "managed"},
		{Address: "skip-replace", Action: "replace", Mode: "managed"},
		{Address: "skip-data", Action: "create", Mode: "data"},
	}

	filtered := FilterCandidates(candidates)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 reconcile candidates, got %d", len(filtered))
	}
	if filtered[0].Address != "keep" || filtered[1].Address != "also-keep" {
		t.Fatalf("unexpected filtered candidates: %+v", filtered)
	}
}

func TestRun_ImportRequiredWhenResourceExists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/resourceGroups/rg-app/providers/Microsoft.Web/sites/app-01"):
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := azure.NewAzureClient("token")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()

	result, err := Run(context.Background(), []model.Candidate{
		{
			Address:        "azurerm_windows_web_app.app",
			ResourceType:   "azurerm_windows_web_app",
			Mode:           "managed",
			Action:         "create",
			SubscriptionID: "sub-123",
			ResourceGroup:  "rg-app",
			Name:           "app-01",
		},
	}, client, "sub-123", "/tmp/project", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.EvaluatedCandidates != 1 {
		t.Fatalf("expected 1 evaluated candidate, got %d", result.EvaluatedCandidates)
	}
	if len(result.Recommendations) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(result.Recommendations))
	}
	if result.Recommendations[0].ImportID != "/subscriptions/sub-123/resourceGroups/rg-app/providers/Microsoft.Web/sites/app-01" {
		t.Fatalf("unexpected import id: %s", result.Recommendations[0].ImportID)
	}
	if !strings.Contains(result.Recommendations[0].Command, "terraform import") {
		t.Fatalf("expected import command, got %q", result.Recommendations[0].Command)
	}
	if len(result.Findings) != 1 || result.Findings[0].Code != "IMPORT_REQUIRED" {
		t.Fatalf("expected IMPORT_REQUIRED finding, got %+v", result.Findings)
	}
}

func TestRun_IncompleteCandidateWarns(t *testing.T) {
	client := azure.NewAzureClient("token")

	result, err := Run(context.Background(), []model.Candidate{
		{
			Address:        "azurerm_service_plan.asp",
			ResourceType:   "azurerm_service_plan",
			Mode:           "managed",
			Action:         "create",
			SubscriptionID: "sub-123",
			Name:           "asp-01",
		},
	}, client, "sub-123", "/tmp/project", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Recommendations) != 0 {
		t.Fatalf("expected no recommendations, got %d", len(result.Recommendations))
	}
	if len(result.Findings) != 1 || result.Findings[0].Code != "IMPORT_CANDIDATE_INCOMPLETE" {
		t.Fatalf("expected IMPORT_CANDIDATE_INCOMPLETE, got %+v", result.Findings)
	}
}

func TestRun_UnsupportedTypeWarns(t *testing.T) {
	client := azure.NewAzureClient("token")

	result, err := Run(context.Background(), []model.Candidate{
		{
			Address:        "azurerm_mssql_database.db",
			ResourceType:   "azurerm_mssql_database",
			Mode:           "managed",
			Action:         "create",
			SubscriptionID: "sub-123",
			ResourceGroup:  "rg-db",
			Name:           "db-01",
		},
	}, client, "sub-123", "/tmp/project", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Code != "IMPORT_ID_UNSUPPORTED" {
		t.Fatalf("expected IMPORT_ID_UNSUPPORTED, got %+v", result.Findings)
	}
}
