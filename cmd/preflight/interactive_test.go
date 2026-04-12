package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tf-preflight/tf-preflight/internal/model"
)

func TestParseIgnoreSelection_EmptyInput(t *testing.T) {
	warnings := []model.Finding{{Code: "MODULE_SOURCE_NOT_FOUND", Severity: "warn"}}
	indexes, codes, err := parseIgnoreSelection("", 1, warnings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(indexes) != 0 || len(codes) != 0 {
		t.Fatalf("expected no ignored selections, got indexes=%v codes=%v", indexes, codes)
	}
}

func TestParseIgnoreSelection_IndexAndCode(t *testing.T) {
	warnings := []model.Finding{
		{Code: "MODULE_SOURCE_UNKNOWN", Severity: "warn"},
		{Code: "MODULE_SOURCE_EMPTY", Severity: "warn"},
	}
	indexes, codes, err := parseIgnoreSelection("1 MODULE_SOURCE_EMPTY", 2, warnings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := indexes[1]; !ok {
		t.Fatalf("expected index 1 to be ignored")
	}
	if _, ok := codes["MODULE_SOURCE_EMPTY"]; !ok {
		t.Fatalf("expected code MODULE_SOURCE_EMPTY to be ignored")
	}
}

func TestParseIgnoreSelection_InvalidIndex(t *testing.T) {
	warnings := []model.Finding{{Code: "MODULE_SOURCE_UNKNOWN", Severity: "warn"}}
	_, _, err := parseIgnoreSelection("2", 1, warnings)
	if err == nil {
		t.Fatalf("expected out-of-range error")
	}
}

func TestFilterModuleWarningFindings(t *testing.T) {
	all := []model.Finding{
		{Code: "MODULE_SOURCE_UNKNOWN", Severity: "warn", Message: "warn 1"},
		{Code: "MODULE_SOURCE_EMPTY", Severity: "warn", Message: "warn 2"},
		{Code: "MODULE_SOURCE_NOT_FOUND", Severity: "error", Message: "error 1"},
		{Code: "OTHER", Severity: "warn", Message: "non-module"},
	}
	warnings := []model.Finding{
		all[0],
		all[1],
	}
	filtered := filterModuleWarningFindings(all, warnings, map[int]struct{}{2: {}}, nil)
	if len(filtered) != 3 {
		t.Fatalf("expected one warning filtered out, got %d findings", len(filtered))
	}
	if filtered[0].Code != "MODULE_SOURCE_UNKNOWN" {
		t.Fatalf("expected first warning to remain, got %s", filtered[0].Code)
	}
	if filtered[1].Code != "MODULE_SOURCE_NOT_FOUND" {
		t.Fatalf("expected module error finding to remain, got %s", filtered[1].Code)
	}
}

func TestDiscoverPlanCandidates(t *testing.T) {
	base := t.TempDir()
	requireFile := func(path string) {
		if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
			t.Fatalf("unable to create file %q: %v", path, err)
		}
	}
	requireDir := func(path string) {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("unable to create dir %q: %v", path, err)
		}
	}

	nested := filepath.Join(base, "nested")
	requireDir(nested)
	requireFile(filepath.Join(base, "plan.tfplan"))
	requireFile(filepath.Join(base, "notes.txt"))
	requireFile(filepath.Join(base, "my-plan.json"))
	requireFile(filepath.Join(nested, "legacy.plan"))
	requireDir(filepath.Join(base, ".terraform"))
	requireFile(filepath.Join(base, ".terraform", "should-not-collect.tfplan"))

	plans, err := discoverPlanCandidates(base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plans) != 3 {
		t.Fatalf("expected 3 plan candidates, got %d: %v", len(plans), plans)
	}
}

func TestPrintPreflightPlanSummary(t *testing.T) {
	candidates := []model.Candidate{
		{Address: "azurerm_resource_group.rg1", ResourceType: "azurerm_resource_group", Action: "create", Location: "westeurope", ResourceGroup: "rg-rg1", Name: "rg1", Source: "plan", PlanUnknown: true},
		{Address: "azurerm_service_plan.asp1", ResourceType: "azurerm_service_plan", Action: "create", Location: "westeurope", ResourceGroup: "rg-rg1", Source: "plan"},
		{Address: "azurerm_service_plan.asp2", ResourceType: "azurerm_service_plan", Action: "update", Location: "westeurope", ResourceGroup: "rg-rg1", Source: "plan"},
		{Address: "azurerm_service_plan.asp3", ResourceType: "azurerm_service_plan", Action: "delete", Location: "westeurope", ResourceGroup: "rg-rg1", Source: "plan"},
		{Address: "azurerm_service_plan.asp4", ResourceType: "azurerm_service_plan", Action: "replace", Location: "westeurope", ResourceGroup: "rg-rg1", Source: "plan"},
		{Address: "azurerm_service_plan.asp5", ResourceType: "azurerm_service_plan", Action: "noop", Location: "westeurope", ResourceGroup: "rg-rg1", Source: "plan"},
		{Address: "azurerm_service_plan.asp6", ResourceType: "azurerm_service_plan", Action: "create", Location: "westeurope", ResourceGroup: "rg-rg1", Source: "plan"},
		{Address: "azurerm_service_plan.asp7", ResourceType: "azurerm_service_plan", Action: "create", Location: "westeurope", ResourceGroup: "rg-rg1", Source: "plan"},
		{Address: "azurerm_service_plan.asp8", ResourceType: "azurerm_service_plan", Action: "create", Location: "westeurope", ResourceGroup: "rg-rg1", Source: "plan"},
		{Address: "azurerm_service_plan.asp9", ResourceType: "azurerm_service_plan", Action: "create", Location: "westeurope", ResourceGroup: "rg-rg1", Source: "plan"},
		{Address: "azurerm_service_plan.asp10", ResourceType: "azurerm_service_plan", Action: "create", Location: "westeurope", ResourceGroup: "rg-rg1", Source: "plan"},
		{Address: "azurerm_service_plan.asp11", ResourceType: "azurerm_service_plan", Action: "create", Location: "westeurope", ResourceGroup: "rg-rg1", Source: "plan"},
	}

	findings := []model.Finding{
		{Severity: "error", Code: "MODULE_SOURCE_NOT_FOUND", Message: "module missing", Resource: "module.missing"},
		{Severity: "warn", Code: "MODULE_UNUSED_DIR", Message: "unused module", Resource: "modules/foo"},
	}

	out := &bytes.Buffer{}
	printPreflightPlanSummary(out, "/tmp/project", "plan.tfplan", "sub-1", model.CommandOptions{
		Output:            "text",
		SeverityThreshold: "error",
	}, candidates, findings)

	got := out.String()
	if !strings.Contains(got, "Preflight execution summary:") {
		t.Fatalf("expected summary header, got: %q", got)
	}
	if !strings.Contains(got, "Directory: /tmp/project") {
		t.Fatalf("expected directory in summary, got: %q", got)
	}
	if !strings.Contains(got, "Action counts:") {
		t.Fatalf("expected action counts, got: %q", got)
	}
	if !strings.Contains(got, "plan-unknown") {
		t.Fatalf("expected plan warning tag, got: %q", got)
	}
	if !strings.Contains(got, "... and 2 more candidate(s)") {
		t.Fatalf("expected truncation note, got: %q", got)
	}
	if !strings.Contains(got, "Blocking findings: 1") {
		t.Fatalf("expected blocking findings count, got: %q", got)
	}
}

func TestConfirmInteractiveRun_DoesPromptAfterSummary(t *testing.T) {
	candidates := []model.Candidate{
		{Address: "azurerm_resource_group.rg1", ResourceType: "azurerm_resource_group", Action: "create", Location: "westeurope", ResourceGroup: "rg-rg1", Source: "plan"},
	}
	findings := []model.Finding{
		{Severity: "error", Code: "MODULE_SOURCE_NOT_FOUND", Message: "module missing", Resource: "module.missing"},
	}

	in := bufio.NewReader(strings.NewReader("y\n"))
	out := &bytes.Buffer{}
	err := confirmInteractiveRun(in, out, "/tmp/project", "plan.tfplan", model.CommandOptions{
		Output: "text",
	}, "sub-1", candidates, findings)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(out.String(), "Preflight execution summary") {
		t.Fatalf("expected summary before prompt, got: %q", out.String())
	}
	if !strings.Contains(out.String(), "Proceed with scan?") {
		t.Fatalf("expected confirmation prompt, got: %q", out.String())
	}
}

func TestErrorSummaryLines(t *testing.T) {
	lines := errorSummaryLines([]model.Finding{
		{Severity: "error", Code: "X", Message: "bad", Resource: "res"},
		{Severity: "warn", Code: "Y", Message: "warn"},
	})
	if len(lines) != 1 {
		t.Fatalf("expected only error lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "X [res]") {
		t.Fatalf("unexpected error line: %q", lines[0])
	}
}

func TestRenderCountsStableOrder(t *testing.T) {
	got := renderCounts(map[string]int{
		"zebra": 3,
		"alpha": 1,
		"beta":  2,
	})
	if got != "alpha=1, beta=2, zebra=3" {
		t.Fatalf("unexpected counts ordering: %q", got)
	}
}
