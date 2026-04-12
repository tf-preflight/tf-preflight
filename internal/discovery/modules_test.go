package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDirectoryValidatesLocalModuleImports(t *testing.T) {
	dir := t.TempDir()
	modulesDir := filepath.Join(dir, "modules", "web")
	if err := os.MkdirAll(modulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modulesDir, "main.tf"), []byte(`resource "azurerm_resource_group" "rg" {}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`
module "web" {
  source = "./modules/web"
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := ParseDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(ctx.ModuleImports); got != 1 {
		t.Fatalf("expected 1 module import, got %d", got)
	}
	if ctx.ModuleImports[0].SourceKind != "local" {
		t.Fatalf("expected source kind local, got %q", ctx.ModuleImports[0].SourceKind)
	}
	if ctx.ModuleImports[0].ResolvedPath == "" {
		t.Fatal("expected resolved path")
	}
	if len(ctx.Findings) != 0 {
		t.Fatalf("unexpected findings: %#v", ctx.Findings)
	}
}

func TestParseDirectoryFlagsMissingLocalModule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`
module "db" {
  source = "./modules/missing"
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := ParseDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(ctx.Findings); got == 0 {
		t.Fatalf("expected findings")
	}
	found := false
	for _, f := range ctx.Findings {
		if f.Code == "MODULE_SOURCE_NOT_FOUND" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected MODULE_SOURCE_NOT_FOUND, got %#v", ctx.Findings)
	}
}

func TestParseDirectoryFlagsUnusedModuleDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "modules", "unused"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "modules", "unused", "main.tf"), []byte(`resource "azurerm_resource_group" "rg" {}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`
module "web" {
  source = "./modules/web"
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "modules", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "modules", "web", "main.tf"), []byte(`resource "azurerm_resource_group" "rg" {}`), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := ParseDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range ctx.Findings {
		if f.Code == "MODULE_UNUSED_DIR" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected MODULE_UNUSED_DIR warning, got %#v", ctx.Findings)
	}
}

func TestParseDirectoryDiscoversLocalModuleCandidatesWithInputs(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "modules", "web")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
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
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`
module "web" {
  source              = "./modules/web"
  resource_group_name = "rg-web"
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := ParseDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}

	candidate, ok := ctx.CandidateMap["module.web.azurerm_service_plan.asp"]
	if !ok {
		t.Fatalf("expected module-prefixed service plan candidate, got %#v", ctx.CandidateMap)
	}
	if got := candidate.ResourceGroup; got != "rg-web" {
		t.Fatalf("expected merged module input resource group rg-web, got %s", got)
	}
}

func TestParseDirectoryExpandsStaticForEachModuleInstances(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "modules", "web")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
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
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`
locals {
  apps = {
    blue  = "rg-blue"
    green = "rg-green"
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

	ctx, err := ParseDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}

	blue, ok := ctx.CandidateMap[`module.web["blue"].azurerm_service_plan.asp`]
	if !ok {
		t.Fatalf("expected blue module instance candidate, got %#v", ctx.CandidateMap)
	}
	if got := blue.ResourceGroup; got != "rg-blue" {
		t.Fatalf("expected blue resource group rg-blue, got %s", got)
	}

	green, ok := ctx.CandidateMap[`module.web["green"].azurerm_service_plan.asp`]
	if !ok {
		t.Fatalf("expected green module instance candidate, got %#v", ctx.CandidateMap)
	}
	if got := green.ResourceGroup; got != "rg-green" {
		t.Fatalf("expected green resource group rg-green, got %s", got)
	}
}

func TestParseDirectoryDerivesTrafficManagerEndpointMetadataFromModuleReference(t *testing.T) {
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

	ctx, err := ParseDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}

	candidate, ok := ctx.CandidateMap["module.tm.azurerm_traffic_manager_azure_endpoint.endpoint"]
	if !ok {
		t.Fatalf("expected traffic manager endpoint candidate, got %#v", ctx.CandidateMap)
	}
	if got := candidate.ResourceGroup; got != "rg-net" {
		t.Fatalf("expected derived resource group rg-net, got %s", got)
	}
	if got := candidate.TrafficManagerProfile; got != "tm-profile" {
		t.Fatalf("expected derived traffic manager profile tm-profile, got %s", got)
	}
}
