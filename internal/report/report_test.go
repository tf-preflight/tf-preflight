package report

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tf-preflight/tf-preflight/internal/model"
)

func TestBuildReportDecisionPass(t *testing.T) {
	report := BuildReport("/tmp/project", "/tmp/plan.json", false, "sub-123", []model.Candidate{
		{Action: "create"},
	}, nil)

	if report.Decision.Result != "PASS" {
		t.Fatalf("expected PASS, got %s", report.Decision.Result)
	}
	if !report.Decision.Deployable {
		t.Fatal("expected deployable to remain true")
	}
	if report.Decision.Confidence != "HIGH" {
		t.Fatalf("expected HIGH confidence, got %s", report.Decision.Confidence)
	}
	if report.Decision.Blockers != 0 || report.Decision.Degraded != 0 || report.Decision.Advisories != 0 {
		t.Fatalf("unexpected decision counts: %+v", report.Decision)
	}
}

func TestBuildReportDecisionBlocked(t *testing.T) {
	report := BuildReport("/tmp/project", "/tmp/plan.json", false, "sub-123", nil, []model.Finding{
		{Severity: "error", Code: "INVALID_LOCATION", Message: "bad location"},
	})

	if report.Decision.Result != "BLOCKED" {
		t.Fatalf("expected BLOCKED, got %s", report.Decision.Result)
	}
	if report.Decision.Deployable {
		t.Fatal("expected deployable to be false")
	}
	if report.Decision.Confidence != "HIGH" {
		t.Fatalf("expected HIGH confidence without degraded checks, got %s", report.Decision.Confidence)
	}
	if report.Decision.Blockers != 1 || report.Decision.Degraded != 0 || report.Decision.Advisories != 0 {
		t.Fatalf("unexpected decision counts: %+v", report.Decision)
	}
	if report.Findings[0].Category != "BLOCKER" {
		t.Fatalf("expected BLOCKER category, got %s", report.Findings[0].Category)
	}
	if !IsFailure(report.Findings, "error") {
		t.Fatalf("expected failure for error threshold")
	}
}

func TestBuildReportDecisionDegraded(t *testing.T) {
	report := BuildReport("/tmp/project", "/tmp/plan.json", false, "sub-123", nil, []model.Finding{
		{Severity: "error", Code: "RESOURCE_EXISTS_CHECK_FAILED", Message: "backend unavailable"},
		{Severity: "warn", Code: "QUOTA_UNKNOWN", Message: "quota endpoint unavailable"},
	})

	if report.Decision.Result != "DEGRADED" {
		t.Fatalf("expected DEGRADED, got %s", report.Decision.Result)
	}
	if !report.Decision.Deployable {
		t.Fatal("expected degraded report to remain not blocked")
	}
	if report.Decision.Confidence != "DEGRADED" {
		t.Fatalf("expected DEGRADED confidence, got %s", report.Decision.Confidence)
	}
	if report.Decision.Blockers != 0 || report.Decision.Degraded != 2 || report.Decision.Advisories != 0 {
		t.Fatalf("unexpected decision counts: %+v", report.Decision)
	}
	for _, finding := range report.Findings {
		if finding.Category != "DEGRADED" {
			t.Fatalf("expected DEGRADED finding category, got %+v", report.Findings)
		}
	}
	if !IsFailure(report.Findings, "error") {
		t.Fatalf("expected exit failure behavior to remain true for severity=error degraded findings")
	}
}

func TestBuildReportDecisionMixed(t *testing.T) {
	report := BuildReport("/tmp/project", "/tmp/plan.json", false, "sub-123", nil, []model.Finding{
		{Severity: "warn", Code: "RESOURCE_EXISTS", Message: "already exists", Resource: "a"},
		{Severity: "error", Code: "PROVIDER_QUERY_FAILED", Message: "lookup failed", Resource: "b"},
		{Severity: "error", Code: "INVALID_LOCATION", Message: "blocked", Resource: "c"},
	})

	if report.Decision.Result != "BLOCKED" {
		t.Fatalf("expected BLOCKED, got %s", report.Decision.Result)
	}
	if report.Decision.Deployable {
		t.Fatal("expected deployable to be false")
	}
	if report.Decision.Confidence != "DEGRADED" {
		t.Fatalf("expected DEGRADED confidence, got %s", report.Decision.Confidence)
	}
	if report.Decision.Blockers != 1 || report.Decision.Degraded != 1 || report.Decision.Advisories != 1 {
		t.Fatalf("unexpected decision counts: %+v", report.Decision)
	}

	gotCodes := []string{report.Findings[0].Code, report.Findings[1].Code, report.Findings[2].Code}
	wantCodes := []string{"INVALID_LOCATION", "PROVIDER_QUERY_FAILED", "RESOURCE_EXISTS"}
	for i := range wantCodes {
		if gotCodes[i] != wantCodes[i] {
			t.Fatalf("unexpected finding order: got %v want %v", gotCodes, wantCodes)
		}
	}
}

func TestWriteJSONIncludesDecisionSummary(t *testing.T) {
	report := BuildReport("/tmp/project", "/tmp/plan.json", false, "sub-123", nil, []model.Finding{
		{Severity: "warn", Code: "QUOTA_CHECK_UNSUPPORTED", Message: "quota unsupported"},
	})

	outPath := filepath.Join(t.TempDir(), "report.json")
	if err := WriteJSON(report, outPath); err != nil {
		t.Fatalf("unexpected json write error: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}

	var decoded struct {
		Decision model.Decision  `json:"decision"`
		Findings []model.Finding `json:"findings"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unexpected decode error: %v", err)
	}
	if decoded.Decision.Result != "DEGRADED" {
		t.Fatalf("expected DEGRADED decision, got %+v", decoded.Decision)
	}
	if decoded.Decision.Degraded != 1 || decoded.Decision.Blockers != 0 || decoded.Decision.Advisories != 0 {
		t.Fatalf("unexpected decision counts: %+v", decoded.Decision)
	}
	if len(decoded.Findings) != 1 || decoded.Findings[0].Category != "DEGRADED" {
		t.Fatalf("expected categorized findings in json, got %+v", decoded.Findings)
	}
}

func TestWriteTextIncludesFinalDecisionSection(t *testing.T) {
	report := BuildReport("/tmp/project", "/tmp/plan.json", false, "sub-123", nil, []model.Finding{
		{Severity: "warn", Code: "RESOURCE_EXISTS", Message: "already exists", Resource: "azurerm_resource_group.rg"},
		{Severity: "warn", Code: "QUOTA_UNKNOWN", Message: "quota unavailable", Resource: "azurerm_service_plan.asp"},
	})

	var buf bytes.Buffer
	writeText(&buf, report)
	text := buf.String()

	for _, needle := range []string{
		"Decision:",
		"Result:",
		"ADVISORY",
		"Confidence:",
		"Degraded checks:",
		"CLASS",
		"DEGRADED",
		"ADVISORY",
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("expected text output to contain %q, got:\n%s", needle, text)
		}
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
