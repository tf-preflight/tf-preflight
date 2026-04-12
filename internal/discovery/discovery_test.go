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
