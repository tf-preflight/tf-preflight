package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestProgressClampsDisplayedCounts(t *testing.T) {
	var out bytes.Buffer
	progress := NewProgress(true, true, &out)

	progress.Start("preparing workspace", 1)
	progress.Tick("loading Terraform directory")
	progress.Tick("terraform directory parsed")

	text := out.String()
	if strings.Contains(text, "2/1") {
		t.Fatalf("expected displayed counts to clamp, got %q", text)
	}
	if !strings.Contains(text, "[progress] preparing workspace (1/1): terraform directory parsed") {
		t.Fatalf("expected clamped final progress line, got %q", text)
	}
}

func TestProgressWritesLineSeparatedOutput(t *testing.T) {
	var out bytes.Buffer
	progress := NewProgress(true, true, &out)

	progress.Start("evaluating resources", 2)
	progress.Message("checking azurerm_resource_group.rg (azurerm_resource_group)")
	progress.Tick("checked azurerm_resource_group.rg")
	progress.Done("resource checks complete")

	text := out.String()
	if strings.Contains(text, "\r") {
		t.Fatalf("expected line-based output without carriage returns, got %q", text)
	}

	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 progress lines, got %d: %q", len(lines), text)
	}
	if lines[0] != "[progress] evaluating resources (0/2): starting" {
		t.Fatalf("unexpected start line: %q", lines[0])
	}
	if lines[1] != "[info] checking azurerm_resource_group.rg (azurerm_resource_group)" {
		t.Fatalf("unexpected message line: %q", lines[1])
	}
	if lines[2] != "[progress] evaluating resources (1/2): checked azurerm_resource_group.rg" {
		t.Fatalf("unexpected tick line: %q", lines[2])
	}
	if lines[3] != "[progress] evaluating resources (2/2): done: resource checks complete" {
		t.Fatalf("unexpected done line: %q", lines[3])
	}
}

func TestProgressMessageRespectsVerboseFlag(t *testing.T) {
	var out bytes.Buffer
	progress := NewProgress(true, false, &out)

	progress.Message("hidden message")

	if out.Len() != 0 {
		t.Fatalf("expected non-verbose message to be suppressed, got %q", out.String())
	}
}
