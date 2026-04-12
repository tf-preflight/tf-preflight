package report

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestBuildReconcileReportAndJSON(t *testing.T) {
	reconcileReport := BuildReconcileReport("/tmp/project", "/tmp/plan.tfplan", true, "sub-123", 5, 2, []model.Finding{
		{Severity: "warn", Code: "IMPORT_ID_UNSUPPORTED", Message: "x"},
		{Severity: "error", Code: "IMPORT_REQUIRED", Message: "y"},
	}, []model.ImportRecommendation{
		{
			TerraformAddress: "azurerm_service_plan.asp",
			ResourceType:     "azurerm_service_plan",
			ImportID:         "/subscriptions/sub-123/resourceGroups/rg/providers/Microsoft.Web/serverFarms/asp",
			WorkingDirectory: "/tmp/project",
			Command:          "terraform import 'azurerm_service_plan.asp' '/subscriptions/sub-123/resourceGroups/rg/providers/Microsoft.Web/serverFarms/asp'",
		},
	})

	if reconcileReport.Summary.TotalCandidates != 5 {
		t.Fatalf("expected total candidates = 5, got %d", reconcileReport.Summary.TotalCandidates)
	}
	if reconcileReport.Summary.EvaluatedCandidates != 2 {
		t.Fatalf("expected evaluated candidates = 2, got %d", reconcileReport.Summary.EvaluatedCandidates)
	}
	if reconcileReport.Summary.ImportRequired != 1 {
		t.Fatalf("expected import required = 1, got %d", reconcileReport.Summary.ImportRequired)
	}
	if !IsReconcileFailure(reconcileReport.Findings) {
		t.Fatalf("expected reconcile failure when error findings exist")
	}

	outPath := filepath.Join(t.TempDir(), "reconcile.json")
	if err := WriteReconcileJSON(reconcileReport, outPath); err != nil {
		t.Fatalf("unexpected json write error: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}

	var decoded model.ReconcileReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unexpected decode error: %v", err)
	}
	if len(decoded.Recommendations) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(decoded.Recommendations))
	}
	if decoded.Recommendations[0].TerraformAddress != "azurerm_service_plan.asp" {
		t.Fatalf("unexpected terraform address: %s", decoded.Recommendations[0].TerraformAddress)
	}
}
