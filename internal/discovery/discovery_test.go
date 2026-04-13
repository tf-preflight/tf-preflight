package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDirectoryExtractsIntent(t *testing.T) {
	dir := t.TempDir()
	content := `
provider "azurerm" {
  subscription_id = "11111111-2222-3333-4444-555555555555"
  features {}
}

locals {
  rg_name = "preflight-rg"
}

resource "azurerm_resource_group" "rg" {
  name     = local.rg_name
  location = "west europe"
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := ParseDirectory(dir)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if ctx.Subscription != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("subscription mismatch: %s", ctx.Subscription)
	}
	if len(ctx.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(ctx.Candidates))
	}
	if ctx.Candidates[0].ResourceType != "azurerm_resource_group" {
		t.Fatalf("unexpected type: %s", ctx.Candidates[0].ResourceType)
	}
}

func TestParseDirectoryResolvesVarInLocal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "var.tfvars"), []byte(`region = "west europe"`), 0o644); err != nil {
		t.Fatal(err)
	}
	content := `
variable "region" {
  type    = string
  default = "eastus"
}

locals {
  rg_name = format("rg-%s", var.region)
}

resource "azurerm_resource_group" "rg" {
  name     = local.rg_name
  location = var.region
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, err := ParseDirectory(dir)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if got := ctx.Candidates[0].Location; got != "eastus" {
		t.Fatalf("expected location eastus, got %s", got)
	}
}

func TestParseDirectoryExtractsSubnetVirtualNetworkName(t *testing.T) {
	dir := t.TempDir()
	content := `
resource "azurerm_subnet" "app" {
  name                 = "subnet-app"
  resource_group_name  = "rg-app"
  virtual_network_name = "vnet-app"
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := ParseDirectory(dir)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(ctx.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(ctx.Candidates))
	}
	if got := ctx.Candidates[0].VirtualNetwork; got != "vnet-app" {
		t.Fatalf("expected virtual network vnet-app, got %s", got)
	}
}

func TestParseDirectoryExtractsTrafficManagerEndpointProfileID(t *testing.T) {
	dir := t.TempDir()
	content := `
resource "azurerm_traffic_manager_azure_endpoint" "app" {
  name       = "tm-endpoint-app"
  profile_id = "/subscriptions/sub-123/resourceGroups/rg-net/providers/Microsoft.Network/trafficManagerProfiles/tm-profile"
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := ParseDirectory(dir)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(ctx.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(ctx.Candidates))
	}
	if got := ctx.Candidates[0].ResourceGroup; got != "rg-net" {
		t.Fatalf("expected resource group rg-net, got %s", got)
	}
	if got := ctx.Candidates[0].TrafficManagerProfile; got != "tm-profile" {
		t.Fatalf("expected traffic manager profile tm-profile, got %s", got)
	}
}

func TestParseDirectoryExtractsSQLAndKeyVaultReferences(t *testing.T) {
	dir := t.TempDir()
	content := `
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

resource "azurerm_mssql_server" "sql" {
  name                         = "sql-01"
  location                     = "westeurope"
  resource_group_name          = "rg-db"
  version                      = "12.0"
  administrator_login          = "sqladmin"
  administrator_login_password = "Password123!"
}

resource "azurerm_mssql_database" "db" {
  name      = "db-01"
  server_id = azurerm_mssql_server.sql.id
  sku_name  = "GP_S_Gen5_2"
}

resource "azurerm_key_vault_secret" "secret" {
  name         = "app-secret"
  key_vault_id = azurerm_key_vault.kv.id
  value        = "super-secret"
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := ParseDirectory(dir)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	db, ok := ctx.CandidateMap["azurerm_mssql_database.db"]
	if !ok {
		t.Fatalf("expected mssql database candidate, got %#v", ctx.CandidateMap)
	}
	if got := db.ServerID; got != "/subscriptions/sub-123/resourceGroups/rg-db/providers/Microsoft.Sql/servers/sql-01" {
		t.Fatalf("expected resolved server ID, got %s", got)
	}
	if got := db.Sku; got != "GP_S_Gen5_2" {
		t.Fatalf("expected sku GP_S_Gen5_2, got %s", got)
	}

	secret, ok := ctx.CandidateMap["azurerm_key_vault_secret.secret"]
	if !ok {
		t.Fatalf("expected key vault secret candidate, got %#v", ctx.CandidateMap)
	}
	if got := secret.KeyVaultID; got != "/subscriptions/sub-123/resourceGroups/rg-sec/providers/Microsoft.KeyVault/vaults/kv-app" {
		t.Fatalf("expected resolved key vault ID, got %s", got)
	}
}

func TestParseDirectoryExtractsFrontDoorReferences(t *testing.T) {
	dir := t.TempDir()
	content := `
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
  name                     = "fd-origin-group"
  cdn_frontdoor_profile_id = azurerm_cdn_frontdoor_profile.fd.id
}

resource "azurerm_cdn_frontdoor_origin" "origin" {
  name                          = "fd-origin"
  cdn_frontdoor_origin_group_id = azurerm_cdn_frontdoor_origin_group.group.id
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
`
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := ParseDirectory(dir)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	endpoint, ok := ctx.CandidateMap["azurerm_cdn_frontdoor_endpoint.endpoint"]
	if !ok {
		t.Fatalf("expected front door endpoint candidate, got %#v", ctx.CandidateMap)
	}
	if got := endpoint.ResourceGroup; got != "rg-edge" {
		t.Fatalf("expected endpoint resource group rg-edge, got %s", got)
	}
	if got := endpoint.FrontDoorProfile; got != "fd-profile" {
		t.Fatalf("expected endpoint profile fd-profile, got %s", got)
	}

	origin, ok := ctx.CandidateMap["azurerm_cdn_frontdoor_origin.origin"]
	if !ok {
		t.Fatalf("expected front door origin candidate, got %#v", ctx.CandidateMap)
	}
	if got := origin.ResourceGroup; got != "rg-edge" {
		t.Fatalf("expected origin resource group rg-edge, got %s", got)
	}
	if got := origin.FrontDoorProfile; got != "fd-profile" {
		t.Fatalf("expected origin profile fd-profile, got %s", got)
	}
	if got := origin.FrontDoorOriginGroup; got != "fd-origin-group" {
		t.Fatalf("expected origin group fd-origin-group, got %s", got)
	}

	route, ok := ctx.CandidateMap["azurerm_cdn_frontdoor_route.route"]
	if !ok {
		t.Fatalf("expected front door route candidate, got %#v", ctx.CandidateMap)
	}
	if got := route.ResourceGroup; got != "rg-edge" {
		t.Fatalf("expected route resource group rg-edge, got %s", got)
	}
	if got := route.FrontDoorProfile; got != "fd-profile" {
		t.Fatalf("expected route profile fd-profile, got %s", got)
	}
	if got := route.FrontDoorEndpoint; got != "fd-endpoint" {
		t.Fatalf("expected route endpoint fd-endpoint, got %s", got)
	}
	if got := route.FrontDoorOriginGroup; got != "fd-origin-group" {
		t.Fatalf("expected route origin group fd-origin-group, got %s", got)
	}
}
