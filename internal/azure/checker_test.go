package azure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tf-preflight/tf-preflight/internal/model"
)

func TestIsLocationAvailable(t *testing.T) {
	locations := &locationCatalog{
		known: map[string]struct{}{
			"west europe": {},
			"westeurope":  {},
		},
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
			name: "virtual network",
			candidate: model.Candidate{
				ResourceType:   "azurerm_virtual_network",
				SubscriptionID: "sub-123",
				ResourceGroup:  "rg-net",
				Name:           "vnet-app",
			},
			want: "/subscriptions/sub-123/resourceGroups/rg-net/providers/Microsoft.Network/virtualNetworks/vnet-app",
		},
		{
			name: "subnet",
			candidate: model.Candidate{
				ResourceType:   "azurerm_subnet",
				SubscriptionID: "sub-123",
				ResourceGroup:  "rg-net",
				VirtualNetwork: "vnet-app",
				Name:           "subnet-app",
			},
			want: "/subscriptions/sub-123/resourceGroups/rg-net/providers/Microsoft.Network/virtualNetworks/vnet-app/subnets/subnet-app",
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
			name: "traffic manager azure endpoint",
			candidate: model.Candidate{
				ResourceType:          "azurerm_traffic_manager_azure_endpoint",
				SubscriptionID:        "sub-123",
				ResourceGroup:         "rg-net",
				TrafficManagerProfile: "tm-profile",
				Name:                  "endpoint-app",
			},
			want: "/subscriptions/sub-123/resourceGroups/rg-net/providers/Microsoft.Network/trafficManagerProfiles/tm-profile/AzureEndpoints/endpoint-app",
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

func TestResolveNamespace_SupportsNetworkResourceCoverage(t *testing.T) {
	for _, resourceType := range []string{"azurerm_virtual_network", "azurerm_subnet", "azurerm_traffic_manager_azure_endpoint"} {
		meta, ok := ResolveNamespace(resourceType)
		if !ok {
			t.Fatalf("expected %s to be mapped", resourceType)
		}
		if meta.Namespace != "Microsoft.Network" {
			t.Fatalf("expected %s namespace to be Microsoft.Network, got %s", resourceType, meta.Namespace)
		}
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

func TestRunChecks_ReturnsErrorWhenTokenMissing(t *testing.T) {
	_, err := RunChecks(context.Background(), nil, NewAzureClient(""), "sub-123", "error", nil)
	if err == nil {
		t.Fatal("expected missing token error")
	}
	if !strings.Contains(err.Error(), "no azure token available") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunChecks_ReturnsErrorWhenSubscriptionCannotBeResolved(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := RunChecks(context.Background(), nil, NewAzureClient("token"), "", "error", nil)
	if err == nil {
		t.Fatal("expected subscription resolution error")
	}
	if !strings.Contains(err.Error(), "subscription could not be resolved") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveSubscriptionFromCLI_EmptyOutputFails(t *testing.T) {
	binDir := t.TempDir()
	azPath := filepath.Join(binDir, "az")
	if err := os.WriteFile(azPath, []byte("#!/bin/sh\nset -eu\nprintf ''\n"), 0o755); err != nil {
		t.Fatalf("unable to write az stub: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := ResolveSubscriptionFromCLI()
	if err == nil {
		t.Fatal("expected empty subscription output to fail")
	}
	if !strings.Contains(err.Error(), "empty subscription id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunChecks_SurfacesProviderLookupFailure(t *testing.T) {
	client := newAzureTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/locations"):
			writeJSON(w, `{"value":[{"name":"westeurope","displayName":"West Europe"}]}`)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/resourceGroups/rg-test"):
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/providers/Microsoft.Resources"):
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	})

	findings, err := RunChecks(context.Background(), []model.Candidate{{
		Address:        "azurerm_resource_group.rg",
		ResourceType:   "azurerm_resource_group",
		Mode:           "managed",
		Action:         "create",
		SubscriptionID: "sub-123",
		Name:           "rg-test",
		Location:       "westeurope",
	}}, client, "sub-123", "error", nil)
	if err != nil {
		t.Fatalf("unexpected RunChecks error: %v", err)
	}

	assertFindingCodes(t, findings, []string{"PROVIDER_QUERY_FAILED"})
}

func TestRunChecks_SurfacesLocationLookupFailure(t *testing.T) {
	client := newAzureTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/locations"):
			w.WriteHeader(http.StatusInternalServerError)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/resourceGroups/rg-test"):
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/providers/Microsoft.Resources"):
			writeJSON(w, `{"registrationState":"Registered"}`)
		default:
			http.NotFound(w, r)
		}
	})

	findings, err := RunChecks(context.Background(), []model.Candidate{{
		Address:        "azurerm_resource_group.rg",
		ResourceType:   "azurerm_resource_group",
		Mode:           "managed",
		Action:         "create",
		SubscriptionID: "sub-123",
		Name:           "rg-test",
		Location:       "westeurope",
	}}, client, "sub-123", "error", nil)
	if err != nil {
		t.Fatalf("unexpected RunChecks error: %v", err)
	}

	if !hasFindingCode(findings, "SUBSCRIPTION_LOCATIONS") {
		t.Fatalf("expected SUBSCRIPTION_LOCATIONS, got %+v", findings)
	}
	if hasFindingCode(findings, "INVALID_LOCATION") {
		t.Fatalf("did not expect INVALID_LOCATION when location lookup failed, got %+v", findings)
	}
}

func TestRunChecks_SurfacesQuotaLookupFailure(t *testing.T) {
	client := newAzureTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/locations"):
			writeJSON(w, `{"value":[{"name":"westeurope","displayName":"West Europe"}]}`)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/resourceGroups/rg-app/providers/Microsoft.Web/serverFarms/asp-01"):
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.EscapedPath(), "/subscriptions/sub-123/providers/Microsoft.Web/locations/West%20Europe/usages"):
			w.WriteHeader(http.StatusInternalServerError)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/providers/Microsoft.Web"):
			writeJSON(w, `{"registrationState":"Registered"}`)
		default:
			http.NotFound(w, r)
		}
	})

	findings, err := RunChecks(context.Background(), []model.Candidate{{
		Address:        "azurerm_service_plan.asp",
		ResourceType:   "azurerm_service_plan",
		Mode:           "managed",
		Action:         "create",
		SubscriptionID: "sub-123",
		ResourceGroup:  "rg-app",
		Name:           "asp-01",
		Location:       "westeurope",
	}}, client, "sub-123", "error", nil)
	if err != nil {
		t.Fatalf("unexpected RunChecks error: %v", err)
	}

	assertFindingCodes(t, findings, []string{"QUOTA_UNKNOWN"})
}

func TestRunChecks_SurfacesUnsupportedExistenceMapping(t *testing.T) {
	client := newAzureTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/locations"):
			writeJSON(w, `{"value":[{"name":"westeurope","displayName":"West Europe"}]}`)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/providers/Microsoft.Sql/locations/westeurope/usages"):
			writeJSON(w, `{"value":[{"name":{"value":"databases"},"currentValue":1,"limit":10}]}`)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/providers/Microsoft.Sql"):
			writeJSON(w, `{"registrationState":"Registered"}`)
		default:
			http.NotFound(w, r)
		}
	})

	findings, err := RunChecks(context.Background(), []model.Candidate{{
		Address:        "azurerm_mssql_database.db",
		ResourceType:   "azurerm_mssql_database",
		Mode:           "managed",
		Action:         "create",
		SubscriptionID: "sub-123",
		ResourceGroup:  "rg-db",
		Name:           "db-01",
		Location:       "westeurope",
	}}, client, "sub-123", "error", nil)
	if err != nil {
		t.Fatalf("unexpected RunChecks error: %v", err)
	}

	assertFindingCodes(t, findings, []string{"RESOURCE_EXISTS_CHECK_UNSUPPORTED"})
}

func TestRunChecks_SurfacesIncompleteExistenceMapping(t *testing.T) {
	client := newAzureTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/locations"):
			writeJSON(w, `{"value":[{"name":"westeurope","displayName":"West Europe"}]}`)
		case strings.HasPrefix(r.URL.EscapedPath(), "/subscriptions/sub-123/providers/Microsoft.Web/locations/West%20Europe/usages"):
			writeJSON(w, `{"value":[{"name":{"value":"sites"},"currentValue":1,"limit":10}]}`)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/providers/Microsoft.Web"):
			writeJSON(w, `{"registrationState":"Registered"}`)
		default:
			http.NotFound(w, r)
		}
	})

	findings, err := RunChecks(context.Background(), []model.Candidate{{
		Address:        "azurerm_service_plan.asp",
		ResourceType:   "azurerm_service_plan",
		Mode:           "managed",
		Action:         "create",
		SubscriptionID: "sub-123",
		Name:           "asp-01",
		Location:       "westeurope",
	}}, client, "sub-123", "error", nil)
	if err != nil {
		t.Fatalf("unexpected RunChecks error: %v", err)
	}

	assertFindingCodes(t, findings, []string{"RESOURCE_EXISTS_CHECK_INCOMPLETE"})
}

func TestRunChecks_SupportsVirtualNetworkAndSubnet(t *testing.T) {
	client := newAzureTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/locations"):
			writeJSON(w, `{"value":[{"name":"westeurope","displayName":"West Europe"}]}`)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/resourceGroups/rg-net/providers/Microsoft.Network/virtualNetworks/vnet-app/subnets/subnet-app"):
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/resourceGroups/rg-net/providers/Microsoft.Network/virtualNetworks/vnet-app"):
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/providers/Microsoft.Network"):
			writeJSON(w, `{"registrationState":"Registered"}`)
		default:
			http.NotFound(w, r)
		}
	})

	findings, err := RunChecks(context.Background(), []model.Candidate{
		{
			Address:        "azurerm_virtual_network.vnet",
			ResourceType:   "azurerm_virtual_network",
			Mode:           "managed",
			Action:         "create",
			SubscriptionID: "sub-123",
			ResourceGroup:  "rg-net",
			Name:           "vnet-app",
			Location:       "westeurope",
		},
		{
			Address:        "azurerm_subnet.subnet",
			ResourceType:   "azurerm_subnet",
			Mode:           "managed",
			Action:         "create",
			SubscriptionID: "sub-123",
			ResourceGroup:  "rg-net",
			VirtualNetwork: "vnet-app",
			Name:           "subnet-app",
		},
	}, client, "sub-123", "error", nil)
	if err != nil {
		t.Fatalf("unexpected RunChecks error: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no degraded findings for supported vnet/subnet checks, got %+v", findings)
	}
}

func TestRunChecks_SupportsTrafficManagerProfileWithoutLocation(t *testing.T) {
	client := newAzureTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/locations"):
			writeJSON(w, `{"value":[{"name":"westeurope","displayName":"West Europe"}]}`)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/resourceGroups/rg-net/providers/Microsoft.Network/trafficManagerProfiles/tm-profile"):
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/providers/Microsoft.Network"):
			writeJSON(w, `{"registrationState":"Registered"}`)
		default:
			http.NotFound(w, r)
		}
	})

	findings, err := RunChecks(context.Background(), []model.Candidate{{
		Address:        "azurerm_traffic_manager_profile.tm",
		ResourceType:   "azurerm_traffic_manager_profile",
		Mode:           "managed",
		Action:         "create",
		SubscriptionID: "sub-123",
		ResourceGroup:  "rg-net",
		Name:           "tm-profile",
	}}, client, "sub-123", "error", nil)
	if err != nil {
		t.Fatalf("unexpected RunChecks error: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no degraded findings for supported traffic manager profile checks, got %+v", findings)
	}
}

func TestRunChecks_SupportsTrafficManagerAzureEndpoint(t *testing.T) {
	client := newAzureTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/locations"):
			writeJSON(w, `{"value":[{"name":"westeurope","displayName":"West Europe"}]}`)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/resourceGroups/rg-net/providers/Microsoft.Network/trafficManagerProfiles/tm-profile/AzureEndpoints/endpoint-app"):
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/providers/Microsoft.Network"):
			writeJSON(w, `{"registrationState":"Registered"}`)
		default:
			http.NotFound(w, r)
		}
	})

	findings, err := RunChecks(context.Background(), []model.Candidate{{
		Address:               "azurerm_traffic_manager_azure_endpoint.endpoint",
		ResourceType:          "azurerm_traffic_manager_azure_endpoint",
		Mode:                  "managed",
		Action:                "create",
		SubscriptionID:        "sub-123",
		ResourceGroup:         "rg-net",
		TrafficManagerProfile: "tm-profile",
		Name:                  "endpoint-app",
	}}, client, "sub-123", "error", nil)
	if err != nil {
		t.Fatalf("unexpected RunChecks error: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no degraded findings for supported traffic manager endpoint checks, got %+v", findings)
	}
}

func TestRunChecks_SurfacesIncompleteSubnetExistenceMapping(t *testing.T) {
	client := newAzureTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/locations"):
			writeJSON(w, `{"value":[{"name":"westeurope","displayName":"West Europe"}]}`)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/providers/Microsoft.Network"):
			writeJSON(w, `{"registrationState":"Registered"}`)
		default:
			http.NotFound(w, r)
		}
	})

	findings, err := RunChecks(context.Background(), []model.Candidate{{
		Address:        "azurerm_subnet.subnet",
		ResourceType:   "azurerm_subnet",
		Mode:           "managed",
		Action:         "create",
		SubscriptionID: "sub-123",
		ResourceGroup:  "rg-net",
		Name:           "subnet-app",
	}}, client, "sub-123", "error", nil)
	if err != nil {
		t.Fatalf("unexpected RunChecks error: %v", err)
	}

	assertFindingCodes(t, findings, []string{"RESOURCE_EXISTS_CHECK_INCOMPLETE"})
}

func TestRunChecks_AppServiceQuotaUsesCanonicalLocationDisplayName(t *testing.T) {
	quotaCalls := 0
	client := newAzureTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/locations"):
			writeJSON(w, `{"value":[{"name":"westeurope","displayName":"West Europe"}]}`)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/resourceGroups/rg-app/providers/Microsoft.Web/serverFarms/asp-01"):
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/resourceGroups/rg-app/providers/Microsoft.Web/sites/app-win"):
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.EscapedPath(), "/subscriptions/sub-123/providers/Microsoft.Web/locations/West%20Europe/usages"):
			quotaCalls++
			writeJSON(w, `{"value":[{"name":{"value":"sites"},"currentValue":1,"limit":10}]}`)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/providers/Microsoft.Web"):
			writeJSON(w, `{"registrationState":"Registered"}`)
		default:
			http.NotFound(w, r)
		}
	})

	findings, err := RunChecks(context.Background(), []model.Candidate{
		{
			Address:        "azurerm_service_plan.asp",
			ResourceType:   "azurerm_service_plan",
			Mode:           "managed",
			Action:         "create",
			SubscriptionID: "sub-123",
			ResourceGroup:  "rg-app",
			Name:           "asp-01",
			Location:       "westeurope",
		},
		{
			Address:        "azurerm_windows_web_app.app",
			ResourceType:   "azurerm_windows_web_app",
			Mode:           "managed",
			Action:         "create",
			SubscriptionID: "sub-123",
			ResourceGroup:  "rg-app",
			Name:           "app-win",
			Location:       "westeurope",
		},
	}, client, "sub-123", "error", nil)
	if err != nil {
		t.Fatalf("unexpected RunChecks error: %v", err)
	}
	if quotaCalls != 2 {
		t.Fatalf("expected 2 Microsoft.Web quota calls using display location, got %d", quotaCalls)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no degraded findings for supported App Service quota checks, got %+v", findings)
	}
}

func TestRunChecks_MixedResultsRemainDeterministic(t *testing.T) {
	client := newAzureTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/locations"):
			writeJSON(w, `{"value":[{"name":"westeurope","displayName":"West Europe"}]}`)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/resourceGroups/rg-test"):
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/resourceGroups/rg-app/providers/Microsoft.Web/serverFarms/asp-01"):
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.EscapedPath(), "/subscriptions/sub-123/providers/Microsoft.Web/locations/West%20Europe/usages"):
			w.WriteHeader(http.StatusInternalServerError)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/providers/Microsoft.Resources"):
			writeJSON(w, `{"registrationState":"NotRegistered"}`)
		case strings.HasPrefix(r.URL.Path, "/subscriptions/sub-123/providers/Microsoft.Web"):
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	})

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
		{
			Address:        "azurerm_service_plan.asp",
			ResourceType:   "azurerm_service_plan",
			Mode:           "managed",
			Action:         "create",
			SubscriptionID: "sub-123",
			ResourceGroup:  "rg-app",
			Name:           "asp-01",
			Location:       "westeurope",
		},
		{
			Address:      "custom_resource.example",
			ResourceType: "custom_resource_type",
			Mode:         "managed",
			Action:       "create",
		},
	}, client, "sub-123", "error", nil)
	if err != nil {
		t.Fatalf("unexpected RunChecks error: %v", err)
	}

	assertFindingCodes(t, findings, []string{
		"RESOURCE_EXISTS",
		"QUOTA_UNKNOWN",
		"UNSUPPORTED_RESOURCE_TYPE",
		"PROVIDER_NOT_REGISTERED",
		"PROVIDER_QUERY_FAILED",
	})
}

func newAzureTestClient(t *testing.T, handler http.HandlerFunc) *AzureClient {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := NewAzureClient("token")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	return client
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func hasFindingCode(findings []model.Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func assertFindingCodes(t *testing.T, findings []model.Finding, want []string) {
	t.Helper()

	got := make([]string, 0, len(findings))
	for _, finding := range findings {
		got = append(got, finding.Code)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected finding codes: got %v want %v", got, want)
	}
}
