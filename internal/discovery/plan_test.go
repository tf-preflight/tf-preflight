package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tf-preflight/tf-preflight/internal/model"
)

func TestCandidatesFromPlanMergesExactRootAddress(t *testing.T) {
	hcl := &HCLContext{
		Candidates: []model.Candidate{
			{
				Address:         "azurerm_service_plan.asp",
				ResourceType:    "azurerm_service_plan",
				Name:            "asp-static",
				Location:        "west europe",
				ResourceGroup:   "rg-static",
				RawRestrictions: map[string]any{"ip_restriction": []any{map[string]any{"name": "corp"}}},
			},
		},
	}

	cands, err := CandidatesFromPlan(planBlob(t, []map[string]any{
		{
			"address": "azurerm_service_plan.asp",
			"type":    "azurerm_service_plan",
			"name":    "asp",
			"mode":    "managed",
			"change": map[string]any{
				"actions": []string{"create"},
				"after": map[string]any{
					"sku": map[string]any{"name": "S1"},
				},
				"after_unknown": map[string]any{},
			},
		},
	}), hcl)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
	if got := cands[0].Source; got != "merged" {
		t.Fatalf("expected merged source, got %s", got)
	}
	if got := cands[0].Location; got != "west europe" {
		t.Fatalf("expected HCL location, got %s", got)
	}
	if got := cands[0].ResourceGroup; got != "rg-static" {
		t.Fatalf("expected HCL resource group, got %s", got)
	}
	if got := cands[0].Name; got != "asp-static" {
		t.Fatalf("expected HCL name, got %s", got)
	}
	if got := cands[0].Sku; got != "S1" {
		t.Fatalf("expected plan sku S1, got %s", got)
	}
	if len(cands[0].RawRestrictions) == 0 {
		t.Fatalf("expected HCL restrictions to merge, got %#v", cands[0].RawRestrictions)
	}
}

func TestCandidatesFromPlanMergesModuleQualifiedAddress(t *testing.T) {
	hcl := &HCLContext{
		Candidates: []model.Candidate{
			{
				Address:       "module.web.azurerm_service_plan.asp",
				ResourceType:  "azurerm_service_plan",
				Name:          "asp-web",
				Location:      "eastus2",
				ResourceGroup: "rg-web",
			},
		},
	}

	cands, err := CandidatesFromPlan(planBlob(t, []map[string]any{
		{
			"address": "module.web.azurerm_service_plan.asp",
			"type":    "azurerm_service_plan",
			"name":    "asp",
			"mode":    "managed",
			"change": map[string]any{
				"actions": []string{"create"},
				"after": map[string]any{
					"sku": map[string]any{"name": "P1v3"},
				},
				"after_unknown": map[string]any{},
			},
		},
	}), hcl)
	if err != nil {
		t.Fatal(err)
	}
	if got := cands[0].Source; got != "merged" {
		t.Fatalf("expected merged source, got %s", got)
	}
	if got := cands[0].Location; got != "eastus2" {
		t.Fatalf("expected exact module location, got %s", got)
	}
	if got := cands[0].ResourceGroup; got != "rg-web" {
		t.Fatalf("expected exact module resource group, got %s", got)
	}
}

func TestCandidatesFromPlanMatchesDuplicateTypeNameByExactModuleAddress(t *testing.T) {
	hcl := &HCLContext{
		Candidates: []model.Candidate{
			{
				Address:       "module.api.azurerm_service_plan.asp",
				ResourceType:  "azurerm_service_plan",
				Name:          "asp-shared",
				Location:      "eastus",
				ResourceGroup: "rg-api",
			},
			{
				Address:       "module.web.azurerm_service_plan.asp",
				ResourceType:  "azurerm_service_plan",
				Name:          "asp-shared",
				Location:      "westeurope",
				ResourceGroup: "rg-web",
			},
		},
	}

	cands, err := CandidatesFromPlan(planBlob(t, []map[string]any{
		{
			"address": "module.web.azurerm_service_plan.asp",
			"type":    "azurerm_service_plan",
			"name":    "asp",
			"mode":    "managed",
			"change": map[string]any{
				"actions": []string{"create"},
				"after": map[string]any{
					"sku": map[string]any{"name": "B1"},
				},
				"after_unknown": map[string]any{},
			},
		},
	}), hcl)
	if err != nil {
		t.Fatal(err)
	}
	if got := cands[0].Location; got != "westeurope" {
		t.Fatalf("expected module.web location, got %s", got)
	}
	if got := cands[0].ResourceGroup; got != "rg-web" {
		t.Fatalf("expected module.web resource group, got %s", got)
	}
}

func TestCandidatesFromPlanLeavesAmbiguousNormalizedFallbackUnmerged(t *testing.T) {
	hcl := &HCLContext{
		Candidates: []model.Candidate{
			{
				Address:      `module.web["blue"].azurerm_service_plan.asp`,
				ResourceType: "azurerm_service_plan",
				Name:         "asp-blue",
				Location:     "eastus",
			},
			{
				Address:      `module.web["green"].azurerm_service_plan.asp`,
				ResourceType: "azurerm_service_plan",
				Name:         "asp-green",
				Location:     "westeurope",
			},
		},
	}

	cands, err := CandidatesFromPlan(planBlob(t, []map[string]any{
		{
			"address": `module.web["red"].azurerm_service_plan.asp`,
			"type":    "azurerm_service_plan",
			"name":    "asp",
			"mode":    "managed",
			"change": map[string]any{
				"actions": []string{"create"},
				"after": map[string]any{
					"name":     "asp-red",
					"location": "centralus",
				},
				"after_unknown": map[string]any{},
			},
		},
	}), hcl)
	if err != nil {
		t.Fatal(err)
	}
	if got := cands[0].Source; got != "plan" {
		t.Fatalf("expected plan-only source, got %s", got)
	}
	if got := cands[0].Location; got != "centralus" {
		t.Fatalf("expected plan location to remain, got %s", got)
	}
	assertCandidateWarningContains(t, cands[0], "multiple HCL resources matched normalized plan address")
}

func TestCandidatesFromPlanLeavesUnmatchedPlanResourceUnmerged(t *testing.T) {
	hcl := &HCLContext{
		Candidates: []model.Candidate{
			{
				Address:      "azurerm_service_plan.asp",
				ResourceType: "azurerm_service_plan",
				Name:         "asp-root",
				Location:     "westeurope",
			},
		},
	}

	cands, err := CandidatesFromPlan(planBlob(t, []map[string]any{
		{
			"address": "module.jobs.azurerm_service_plan.asp",
			"type":    "azurerm_service_plan",
			"name":    "asp",
			"mode":    "managed",
			"change": map[string]any{
				"actions": []string{"create"},
				"after": map[string]any{
					"name":     "asp-jobs",
					"location": "uksouth",
				},
				"after_unknown": map[string]any{},
			},
		},
	}), hcl)
	if err != nil {
		t.Fatal(err)
	}
	if got := cands[0].Source; got != "plan" {
		t.Fatalf("expected plan-only source, got %s", got)
	}
	if got := cands[0].Name; got != "asp-jobs" {
		t.Fatalf("expected plan name to remain, got %s", got)
	}
	if got := cands[0].Location; got != "uksouth" {
		t.Fatalf("expected plan location to remain, got %s", got)
	}
	assertCandidateWarningContains(t, cands[0], "no matching HCL resource found for plan address")
}

func TestCandidatesFromPlanMergesSubnetVirtualNetworkName(t *testing.T) {
	hcl := &HCLContext{
		Candidates: []model.Candidate{
			{
				Address:        "azurerm_subnet.app",
				ResourceType:   "azurerm_subnet",
				Name:           "subnet-app",
				ResourceGroup:  "rg-app",
				VirtualNetwork: "vnet-app",
			},
		},
	}

	cands, err := CandidatesFromPlan(planBlob(t, []map[string]any{
		{
			"address": "azurerm_subnet.app",
			"type":    "azurerm_subnet",
			"name":    "app",
			"mode":    "managed",
			"change": map[string]any{
				"actions": []string{"create"},
				"after": map[string]any{
					"name": "subnet-app",
				},
				"after_unknown": map[string]any{},
			},
		},
	}), hcl)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
	if got := cands[0].VirtualNetwork; got != "vnet-app" {
		t.Fatalf("expected merged virtual network name, got %s", got)
	}
	if got := cands[0].ResourceGroup; got != "rg-app" {
		t.Fatalf("expected merged resource group, got %s", got)
	}
}

func TestCandidatesFromPlanParsesTrafficManagerEndpointProfileID(t *testing.T) {
	cands, err := CandidatesFromPlan(planBlob(t, []map[string]any{
		{
			"address": "azurerm_traffic_manager_azure_endpoint.app",
			"type":    "azurerm_traffic_manager_azure_endpoint",
			"name":    "app",
			"mode":    "managed",
			"change": map[string]any{
				"actions": []string{"create"},
				"after": map[string]any{
					"name":       "tm-endpoint-app",
					"profile_id": "/subscriptions/sub-123/resourceGroups/rg-net/providers/Microsoft.Network/trafficManagerProfiles/tm-profile",
				},
				"after_unknown": map[string]any{},
			},
		},
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
	if got := cands[0].ResourceGroup; got != "rg-net" {
		t.Fatalf("expected parsed resource group rg-net, got %s", got)
	}
	if got := cands[0].TrafficManagerProfile; got != "tm-profile" {
		t.Fatalf("expected parsed traffic manager profile tm-profile, got %s", got)
	}
}

func TestCandidatesFromPlanParsesSQLAndKeyVaultFields(t *testing.T) {
	cands, err := CandidatesFromPlan(planBlob(t, []map[string]any{
		{
			"address": "azurerm_mssql_database.db",
			"type":    "azurerm_mssql_database",
			"name":    "db",
			"mode":    "managed",
			"change": map[string]any{
				"actions": []string{"create"},
				"after": map[string]any{
					"name":      "db-01",
					"server_id": "/subscriptions/sub-123/resourceGroups/rg-db/providers/Microsoft.Sql/servers/sql-01",
					"sku_name":  "GP_S_Gen5_2",
				},
				"after_unknown": map[string]any{},
			},
		},
		{
			"address": "azurerm_key_vault_secret.secret",
			"type":    "azurerm_key_vault_secret",
			"name":    "secret",
			"mode":    "managed",
			"change": map[string]any{
				"actions": []string{"create"},
				"after": map[string]any{
					"name":         "app-secret",
					"key_vault_id": "/subscriptions/sub-123/resourceGroups/rg-sec/providers/Microsoft.KeyVault/vaults/kv-app",
				},
				"after_unknown": map[string]any{},
			},
		},
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(cands))
	}
	if got := cands[0].ServerID; got != "/subscriptions/sub-123/resourceGroups/rg-db/providers/Microsoft.Sql/servers/sql-01" {
		t.Fatalf("expected parsed server ID, got %s", got)
	}
	if got := cands[0].Sku; got != "GP_S_Gen5_2" {
		t.Fatalf("expected parsed sku_name, got %s", got)
	}
	if got := cands[1].KeyVaultID; got != "/subscriptions/sub-123/resourceGroups/rg-sec/providers/Microsoft.KeyVault/vaults/kv-app" {
		t.Fatalf("expected parsed key vault ID, got %s", got)
	}
}

func TestCandidatesFromPlanMergesKeyVaultIDFromParsedHCL(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`
provider "azurerm" {
  subscription_id = "sub-123"
  features {}
}

resource "azurerm_key_vault" "kv" {
  name                = "kv-app"
  location            = "westeurope"
  resource_group_name = "rg-sec"
  tenant_id           = "00000000-0000-0000-0000-000000000000"
  sku_name            = "standard"
}

resource "azurerm_key_vault_secret" "secret" {
  name         = "app-secret"
  key_vault_id = azurerm_key_vault.kv.id
  value        = "super-secret"
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	hcl, err := ParseDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}

	cands, err := CandidatesFromPlan(planBlob(t, []map[string]any{
		{
			"address": "azurerm_key_vault_secret.secret",
			"type":    "azurerm_key_vault_secret",
			"name":    "secret",
			"mode":    "managed",
			"change": map[string]any{
				"actions": []string{"create"},
				"after": map[string]any{
					"name": "app-secret",
				},
				"after_unknown": map[string]any{},
			},
		},
	}), hcl)
	if err != nil {
		t.Fatal(err)
	}
	if got := cands[0].Source; got != "merged" {
		t.Fatalf("expected merged source, got %s", got)
	}
	if got := cands[0].KeyVaultID; got != "/subscriptions/sub-123/resourceGroups/rg-sec/providers/Microsoft.KeyVault/vaults/kv-app" {
		t.Fatalf("expected merged key vault ID, got %s", got)
	}
}

func TestCandidatesFromPlanParsesFrontDoorParentMetadataFromIDs(t *testing.T) {
	cands, err := CandidatesFromPlan(planBlob(t, []map[string]any{
		{
			"address": "azurerm_cdn_frontdoor_origin.origin",
			"type":    "azurerm_cdn_frontdoor_origin",
			"name":    "origin",
			"mode":    "managed",
			"change": map[string]any{
				"actions": []string{"create"},
				"after": map[string]any{
					"name":                          "fd-origin",
					"cdn_frontdoor_origin_group_id": "/subscriptions/sub-123/resourceGroups/rg-edge/providers/Microsoft.Cdn/profiles/fd-profile/originGroups/fd-group",
				},
				"after_unknown": map[string]any{},
			},
		},
		{
			"address": "azurerm_cdn_frontdoor_route.route",
			"type":    "azurerm_cdn_frontdoor_route",
			"name":    "route",
			"mode":    "managed",
			"change": map[string]any{
				"actions": []string{"create"},
				"after": map[string]any{
					"name":                          "fd-route",
					"cdn_frontdoor_endpoint_id":     "/subscriptions/sub-123/resourceGroups/rg-edge/providers/Microsoft.Cdn/profiles/fd-profile/afdEndpoints/fd-endpoint",
					"cdn_frontdoor_origin_group_id": "/subscriptions/sub-123/resourceGroups/rg-edge/providers/Microsoft.Cdn/profiles/fd-profile/originGroups/fd-group",
				},
				"after_unknown": map[string]any{},
			},
		},
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(cands))
	}
	if got := cands[0].ResourceGroup; got != "rg-edge" {
		t.Fatalf("expected origin resource group rg-edge, got %s", got)
	}
	if got := cands[0].FrontDoorProfile; got != "fd-profile" {
		t.Fatalf("expected origin profile fd-profile, got %s", got)
	}
	if got := cands[0].FrontDoorOriginGroup; got != "fd-group" {
		t.Fatalf("expected origin group fd-group, got %s", got)
	}
	if got := cands[1].FrontDoorProfile; got != "fd-profile" {
		t.Fatalf("expected route profile fd-profile, got %s", got)
	}
	if got := cands[1].FrontDoorEndpoint; got != "fd-endpoint" {
		t.Fatalf("expected route endpoint fd-endpoint, got %s", got)
	}
	if got := cands[1].FrontDoorOriginGroup; got != "fd-group" {
		t.Fatalf("expected route origin group fd-group, got %s", got)
	}
}

func TestCandidatesFromPlanMergesFrontDoorRouteMetadataFromParsedHCL(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`
provider "azurerm" {
  subscription_id = "sub-123"
  features {}
}

resource "azurerm_cdn_frontdoor_profile" "fd" {
  name                = "fd-profile"
  resource_group_name = "rg-edge"
  sku_name            = "Standard_AzureFrontDoor"
}

resource "azurerm_cdn_frontdoor_endpoint" "endpoint" {
  name                     = "fd-endpoint"
  cdn_frontdoor_profile_id = azurerm_cdn_frontdoor_profile.fd.id
}

resource "azurerm_cdn_frontdoor_origin_group" "group" {
  name                     = "fd-group"
  cdn_frontdoor_profile_id = azurerm_cdn_frontdoor_profile.fd.id
}

resource "azurerm_cdn_frontdoor_route" "route" {
  name                          = "fd-route"
  cdn_frontdoor_endpoint_id     = azurerm_cdn_frontdoor_endpoint.endpoint.id
  cdn_frontdoor_origin_group_id = azurerm_cdn_frontdoor_origin_group.group.id
  supported_protocols           = ["Http", "Https"]
  patterns_to_match             = ["/*"]
  forwarding_protocol           = "MatchRequest"
  link_to_default_domain        = true
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	hcl, err := ParseDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}

	cands, err := CandidatesFromPlan(planBlob(t, []map[string]any{
		{
			"address": "azurerm_cdn_frontdoor_route.route",
			"type":    "azurerm_cdn_frontdoor_route",
			"name":    "route",
			"mode":    "managed",
			"change": map[string]any{
				"actions": []string{"create"},
				"after": map[string]any{
					"name": "fd-route",
				},
				"after_unknown": map[string]any{},
			},
		},
	}), hcl)
	if err != nil {
		t.Fatal(err)
	}
	if got := cands[0].Source; got != "merged" {
		t.Fatalf("expected merged source, got %s", got)
	}
	if got := cands[0].ResourceGroup; got != "rg-edge" {
		t.Fatalf("expected merged route resource group rg-edge, got %s", got)
	}
	if got := cands[0].FrontDoorProfile; got != "fd-profile" {
		t.Fatalf("expected merged route profile fd-profile, got %s", got)
	}
	if got := cands[0].FrontDoorEndpoint; got != "fd-endpoint" {
		t.Fatalf("expected merged route endpoint fd-endpoint, got %s", got)
	}
	if got := cands[0].FrontDoorOriginGroup; got != "fd-group" {
		t.Fatalf("expected merged route origin group fd-group, got %s", got)
	}
}

func TestCandidatesFromPlanMergesForEachModuleInstanceFromParsedHCL(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "modules", "web")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`
locals {
  apps = {
    blue = "rg-blue"
  }
}

module "web" {
  for_each            = local.apps
  source              = "./modules/web"
  resource_group_name = each.value
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "main.tf"), []byte(`
variable "resource_group_name" {}

resource "azurerm_service_plan" "asp" {
  name                = "asp-web"
  location            = "westeurope"
  resource_group_name = var.resource_group_name
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	hcl, err := ParseDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}

	cands, err := CandidatesFromPlan(planBlob(t, []map[string]any{
		{
			"address": `module.web["blue"].azurerm_service_plan.asp`,
			"type":    "azurerm_service_plan",
			"name":    "asp",
			"mode":    "managed",
			"change": map[string]any{
				"actions": []string{"create"},
				"after": map[string]any{
					"sku": map[string]any{"name": "B1"},
				},
				"after_unknown": map[string]any{},
			},
		},
	}), hcl)
	if err != nil {
		t.Fatal(err)
	}
	if got := cands[0].Source; got != "merged" {
		t.Fatalf("expected merged source, got %s", got)
	}
	if got := cands[0].ResourceGroup; got != "rg-blue" {
		t.Fatalf("expected merged resource group rg-blue, got %s", got)
	}
}

func TestCandidatesFromPlanMergesTrafficManagerEndpointMetadataFromParsedModuleHCL(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "modules", "tm")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`
provider "azurerm" {
  subscription_id = "sub-123"
  features {}
}

module "tm" {
  source              = "./modules/tm"
  resource_group_name = "rg-net"
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "main.tf"), []byte(`
variable "resource_group_name" {}

resource "azurerm_traffic_manager_profile" "profile" {
  name                = "tm-profile"
  resource_group_name = var.resource_group_name
}

resource "azurerm_traffic_manager_azure_endpoint" "endpoint" {
  name       = "endpoint-app"
  profile_id = azurerm_traffic_manager_profile.profile.id
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	hcl, err := ParseDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}

	cands, err := CandidatesFromPlan(planBlob(t, []map[string]any{
		{
			"address": "module.tm.azurerm_traffic_manager_azure_endpoint.endpoint",
			"type":    "azurerm_traffic_manager_azure_endpoint",
			"name":    "endpoint",
			"mode":    "managed",
			"change": map[string]any{
				"actions": []string{"create"},
				"after": map[string]any{
					"name": "endpoint-app",
				},
				"after_unknown": map[string]any{},
			},
		},
	}), hcl)
	if err != nil {
		t.Fatal(err)
	}
	if got := cands[0].Source; got != "merged" {
		t.Fatalf("expected merged source, got %s", got)
	}
	if got := cands[0].ResourceGroup; got != "rg-net" {
		t.Fatalf("expected merged resource group rg-net, got %s", got)
	}
	if got := cands[0].TrafficManagerProfile; got != "tm-profile" {
		t.Fatalf("expected merged traffic manager profile tm-profile, got %s", got)
	}
}

func TestCandidatesFromPlanMergesIndexedModuleOutputDerivedTrafficManagerMetadata(t *testing.T) {
	dir := t.TempDir()

	resourceGroupDir := filepath.Join(dir, "modules", "resource_group")
	if err := os.MkdirAll(resourceGroupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourceGroupDir, "main.tf"), []byte(`
resource "azurerm_resource_group" "rg" {
  name     = var.rg_name
  location = var.rg_location
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourceGroupDir, "variables.tf"), []byte(`
variable "rg_name" {}
variable "rg_location" {}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourceGroupDir, "outputs.tf"), []byte(`
output "rg_details" {
  value = {
    name     = azurerm_resource_group.rg.name
    location = azurerm_resource_group.rg.location
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	trafficManagerDir := filepath.Join(dir, "modules", "traffic_manager")
	if err := os.MkdirAll(trafficManagerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trafficManagerDir, "main.tf"), []byte(`
resource "azurerm_traffic_manager_profile" "tm_profile" {
  name                = var.name
  resource_group_name = var.resource_group_name
}

resource "azurerm_traffic_manager_azure_endpoint" "tm_endpoint" {
  for_each = var.endpoints

  name       = each.value.name
  profile_id = azurerm_traffic_manager_profile.tm_profile.id
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trafficManagerDir, "variables.tf"), []byte(`
variable "name" {}
variable "resource_group_name" {}
variable "endpoints" {}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`
module "resource_group" {
  for_each = var.resource_groups
  source   = "./modules/resource_group"

  rg_name     = each.value.name
  rg_location = each.value.location
}

module "traffic_manager" {
  source              = "./modules/traffic_manager"
  name                = var.traffic_manager.name
  resource_group_name = module.resource_group[var.traffic_manager.rg_key].rg_details.name
  endpoints = {
    app1 = {
      name = "endpoint-app1"
    }
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "variables.tf"), []byte(`
variable "resource_groups" {}
variable "traffic_manager" {}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "terraform.tfvars"), []byte(`
resource_groups = {
  rg3 = {
    name     = "rg-net"
    location = "West Europe"
  }
}

traffic_manager = {
  name   = "tm-profile"
  rg_key = "rg3"
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	hcl, err := ParseDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}

	cands, err := CandidatesFromPlan(planBlob(t, []map[string]any{
		{
			"address": `module.traffic_manager.azurerm_traffic_manager_azure_endpoint.tm_endpoint["app1"]`,
			"type":    "azurerm_traffic_manager_azure_endpoint",
			"name":    "tm_endpoint",
			"mode":    "managed",
			"change": map[string]any{
				"actions": []string{"create"},
				"after": map[string]any{
					"name": "endpoint-app1",
				},
				"after_unknown": map[string]any{},
			},
		},
	}), hcl)
	if err != nil {
		t.Fatal(err)
	}
	if got := cands[0].Source; got != "merged" {
		t.Fatalf("expected merged source, got %s", got)
	}
	if got := cands[0].ResourceGroup; got != "rg-net" {
		t.Fatalf("expected merged resource group rg-net, got %s", got)
	}
	if got := cands[0].TrafficManagerProfile; got != "tm-profile" {
		t.Fatalf("expected merged traffic manager profile tm-profile, got %s", got)
	}
}

func planBlob(t *testing.T, changes []map[string]any) []byte {
	t.Helper()

	blob, err := json.Marshal(map[string]any{
		"resource_changes": changes,
	})
	if err != nil {
		t.Fatalf("marshal plan json: %v", err)
	}
	return blob
}

func assertCandidateWarningContains(t *testing.T, candidate model.Candidate, substring string) {
	t.Helper()

	for _, warning := range candidate.Warnings {
		if strings.Contains(warning, substring) {
			return
		}
	}
	t.Fatalf("expected warning containing %q, got %#v", substring, candidate.Warnings)
}
