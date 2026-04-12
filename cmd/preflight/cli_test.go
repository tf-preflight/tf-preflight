package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tf-preflight/tf-preflight/internal/discovery"
	"github.com/tf-preflight/tf-preflight/internal/model"
)

func TestParseReconcileOptions_AutoPlan(t *testing.T) {
	opts, err := parseReconcileOptions([]string{"--tf-dir", "/tmp/project", "--auto-plan", "--output", "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.TfDir != "/tmp/project" {
		t.Fatalf("unexpected tf dir: %s", opts.TfDir)
	}
	if !opts.AutoPlan {
		t.Fatalf("expected auto plan to be enabled")
	}
	if opts.Output != "json" {
		t.Fatalf("unexpected output: %s", opts.Output)
	}
}

func TestParseReconcileOptions_RequiresPlanSource(t *testing.T) {
	_, err := parseReconcileOptions([]string{"--tf-dir", "/tmp/project"})
	if !errors.Is(err, errUsage) {
		t.Fatalf("expected errUsage, got %v", err)
	}
}

func TestParseScanOptions_Help(t *testing.T) {
	_, err := parseScanOptions([]string{"--help"})
	if !errors.Is(err, errHelp) {
		t.Fatalf("expected errHelp, got %v", err)
	}
	if exitCodeForError(err) != 0 {
		t.Fatalf("expected help exit code 0, got %d", exitCodeForError(err))
	}
	if !strings.Contains(err.Error(), "tf-preflight scan") {
		t.Fatalf("expected scan help output, got %q", err.Error())
	}
}

func TestParseReconcileOptions_Help(t *testing.T) {
	_, err := parseReconcileOptions([]string{"--help"})
	if !errors.Is(err, errHelp) {
		t.Fatalf("expected errHelp, got %v", err)
	}
	if exitCodeForError(err) != 0 {
		t.Fatalf("expected help exit code 0, got %d", exitCodeForError(err))
	}
	if !strings.Contains(err.Error(), "tf-preflight reconcile") {
		t.Fatalf("expected reconcile help output, got %q", err.Error())
	}
}

func TestExitCodeForError(t *testing.T) {
	if got := exitCodeForError(nil); got != 0 {
		t.Fatalf("expected exit code 0, got %d", got)
	}
	if got := exitCodeForError(&helpError{text: "help"}); got != 0 {
		t.Fatalf("expected exit code 0 for help, got %d", got)
	}
	if got := exitCodeForError(errUsage); got != 2 {
		t.Fatalf("expected exit code 2 for errUsage, got %d", got)
	}
	if got := exitCodeForError(errChecksFailed); got != 1 {
		t.Fatalf("expected exit code 1 for errChecksFailed, got %d", got)
	}
}

func TestResolvePlan_AutoPlanUsesTerraformCommands(t *testing.T) {
	binDir := t.TempDir()
	terraformPath := filepath.Join(binDir, "terraform")
	script := `#!/bin/sh
set -eu
cmd="$1"
shift
case "$cmd" in
  init)
    exit 0
    ;;
  plan)
    out=""
    while [ "$#" -gt 0 ]; do
      if [ "$1" = "-out" ]; then
        out="$2"
        shift 2
        continue
      fi
      shift
    done
    : > "$out"
    exit 0
    ;;
  show)
    printf '%s\n' '{"format_version":"1.2","resource_changes":[{"address":"azurerm_resource_group.rg","type":"azurerm_resource_group","mode":"managed","name":"rg","change":{"actions":["create"],"after":{"name":"rg-test","location":"westeurope"},"after_unknown":{}}}]}'
    exit 0
    ;;
esac
exit 1
`
	if err := os.WriteFile(terraformPath, []byte(script), 0o755); err != nil {
		t.Fatalf("unable to write terraform stub: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	data, planPath, err := resolvePlan(context.Background(), model.CommandOptions{
		AutoPlan: true,
	}, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected resolvePlan error: %v", err)
	}
	if !strings.Contains(string(data), `"resource_changes"`) {
		t.Fatalf("expected JSON plan output, got %q", string(data))
	}
	if !strings.HasSuffix(planPath, ".tfplan") {
		t.Fatalf("expected terraform plan path, got %s", planPath)
	}
}

func TestResolvePlan_RejectsNonTerraformJSONPlan(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, []byte(`{"hello":"world"}`), 0o644); err != nil {
		t.Fatalf("unable to write plan file: %v", err)
	}

	_, _, err := resolvePlan(context.Background(), model.CommandOptions{
		PlanPath: planPath,
	}, dir)
	if err == nil {
		t.Fatal("expected resolvePlan to reject non-Terraform JSON")
	}
	if !strings.Contains(err.Error(), "not a Terraform plan JSON document") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveSubscriptionID_FallsBackToAzureCLI(t *testing.T) {
	binDir := t.TempDir()
	azPath := filepath.Join(binDir, "az")
	script := `#!/bin/sh
set -eu
printf '%s\n' 'sub-from-az'
`
	if err := os.WriteFile(azPath, []byte(script), 0o755); err != nil {
		t.Fatalf("unable to write az stub: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ARM_SUBSCRIPTION_ID", "")
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")

	subscriptionID, err := resolveSubscriptionID(model.CommandOptions{}, &discovery.HCLContext{})
	if err != nil {
		t.Fatalf("unexpected resolveSubscriptionID error: %v", err)
	}
	if subscriptionID != "sub-from-az" {
		t.Fatalf("expected Azure CLI fallback subscription, got %q", subscriptionID)
	}
}
