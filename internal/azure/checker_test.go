package azure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tf-preflight/tf-preflight/internal/model"
)

func TestIsLocationAvailable(t *testing.T) {
	locations := map[string]struct{}{
		"west europe": {},
		"westeurope":  {},
	}
	if !isLocationAvailable(locations, "West Europe") {
		t.Fatalf("expected location to be available")
	}
}

func TestIsQuotaExceeded(t *testing.T) {
	items := []usageResponseItem{
		{Name: struct {
			Value string `json:"value"`
		}{Value: "sites"}, CurrentValue: 10, Limit: 10},
	}
	exceeded, metric := isQuotaExceeded(items, []string{"sites"})
	if !exceeded {
		t.Fatalf("expected exceeded")
	}
	if metric != "sites" {
		t.Fatalf("unexpected metric %s", metric)
	}
}

func TestBuildImportID_SupportedResourceTypes(t *testing.T) {
	tests := []struct {
		name      string
		candidate model.Candidate
		want      string
	}{
		{
			name: "resource group",
			candidate: model.Candidate{
				ResourceType:   "azurerm_resource_group",
				SubscriptionID: "sub-123",
				Name:           "rg-app",
			},
			want: "/subscriptions/sub-123/resourceGroups/rg-app",
		},
		{
			name: "service plan",
			candidate: model.Candidate{
				ResourceType:   "azurerm_service_plan",
				SubscriptionID: "sub-123",
				ResourceGroup:  "rg-app",
				Name:           "asp-01",
			},
			want: "/subscriptions/sub-123/resourceGroups/rg-app/providers/Microsoft.Web/serverFarms/asp-01",
		},
		{
			name: "windows web app",
			candidate: model.Candidate{
				ResourceType:   "azurerm_windows_web_app",
				SubscriptionID: "sub-123",
				ResourceGroup:  "rg-app",
				Name:           "app-win",
			},
			want: "/subscriptions/sub-123/resourceGroups/rg-app/providers/Microsoft.Web/sites/app-win",
		},
		{
			name: "linux web app",
			candidate: model.Candidate{
				ResourceType:   "azurerm_linux_web_app",
				SubscriptionID: "sub-123",
				ResourceGroup:  "rg-app",
				Name:           "app-linux",
			},
			want: "/subscriptions/sub-123/resourceGroups/rg-app/providers/Microsoft.Web/sites/app-linux",
		},
		{
			name: "traffic manager",
			candidate: model.Candidate{
				ResourceType:   "azurerm_traffic_manager_profile",
				SubscriptionID: "sub-123",
				ResourceGroup:  "rg-net",
				Name:           "tm-profile",
			},
			want: "/subscriptions/sub-123/resourceGroups/rg-net/providers/Microsoft.Network/trafficManagerProfiles/tm-profile",
		},
		{
			name: "mssql server",
			candidate: model.Candidate{
				ResourceType:   "azurerm_mssql_server",
				SubscriptionID: "sub-123",
				ResourceGroup:  "rg-db",
				Name:           "sql-01",
			},
			want: "/subscriptions/sub-123/resourceGroups/rg-db/providers/Microsoft.Sql/servers/sql-01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, missing, ok := BuildImportID(tt.candidate)
			if !ok {
				t.Fatalf("expected import ID support")
			}
			if len(missing) != 0 {
				t.Fatalf("expected no missing fields, got %v", missing)
			}
			if got != tt.want {
				t.Fatalf("unexpected import ID: got %q want %q", got, tt.want)
			}
		})
	}
}

func TestProbePath_StatusHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/exists":
			w.WriteHeader(http.StatusOK)
		case "/missing":
			http.NotFound(w, r)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := NewAzureClient("token")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()

	status, err := client.ProbePath(context.Background(), "GET", "/exists")
	if err != nil || status != http.StatusOK {
		t.Fatalf("expected 200 without error, got status=%d err=%v", status, err)
	}

	status, err = client.ProbePath(context.Background(), "GET", "/missing")
	if err != nil || status != http.StatusNotFound {
		t.Fatalf("expected 404 without error, got status=%d err=%v", status, err)
	}

	status, err = client.ProbePath(context.Background(), "GET", "/broken")
	if err != nil || status != http.StatusInternalServerError {
		t.Fatalf("expected 500 without error, got status=%d err=%v", status, err)
	}
}

func TestRunChecks_SurfacesExistenceProbeFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/locations"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"value":[{"name":"westeurope","displayName":"West Europe"}]}`))
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/providers/Microsoft.Resources"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"registrationState":"Registered"}`))
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/resourceGroups/rg-test"):
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewAzureClient("token")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()

	findings, err := RunChecks(context.Background(), []model.Candidate{
		{
			Address:        "azurerm_resource_group.rg",
			ResourceType:   "azurerm_resource_group",
			Mode:           "managed",
			Action:         "create",
			SubscriptionID: "sub-123",
			Name:           "rg-test",
			Location:       "westeurope",
		},
	}, client, "sub-123", "error", nil)
	if err != nil {
		t.Fatalf("unexpected RunChecks error: %v", err)
	}

	found := false
	for _, finding := range findings {
		if finding.Code == "RESOURCE_EXISTS_CHECK_FAILED" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected RESOURCE_EXISTS_CHECK_FAILED, got %+v", findings)
	}
}
