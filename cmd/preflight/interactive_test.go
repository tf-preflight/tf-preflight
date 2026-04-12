package main

import (
	"os"
	"path/filepath"
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
