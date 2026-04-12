package report

import (
	"testing"

	"github.com/tf-preflight/tf-preflight/internal/model"
)

func TestBuildReportAndFailureDecision(t *testing.T) {
	report := BuildReport("/tmp/project", "/tmp/plan.json", false, "123", nil, []model.Finding{
		{Severity: "warn", Code: "UNSUPPORTED_RESOURCE_TYPE", Message: "x"},
		{Severity: "error", Code: "INVALID_LOCATION", Message: "y"},
	})

	if report.Summary.Errors != 1 {
		t.Fatalf("expected one error, got %d", report.Summary.Errors)
	}
	if report.Summary.Warnings != 1 {
		t.Fatalf("expected one warning, got %d", report.Summary.Warnings)
	}
	if !IsFailure(report.Findings, "error") {
		t.Fatalf("expected failure for error threshold")
	}
	if !IsFailure(report.Findings, "warn") {
		t.Fatalf("expected failure for warn threshold")
	}
}
