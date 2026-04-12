package report

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/tf-preflight/tf-preflight/internal/model"
)

func BuildReconcileReport(tfDir, planPath string, autoPlan bool, subscription string, totalCandidates, evaluatedCandidates int, findings []model.Finding, recommendations []model.ImportRecommendation) model.ReconcileReport {
	summary := model.ReconcileSummary{
		TotalCandidates:     totalCandidates,
		EvaluatedCandidates: evaluatedCandidates,
		ImportRequired:      len(recommendations),
	}
	for _, finding := range findings {
		switch finding.Severity {
		case "error":
			summary.Errors++
		case "warn":
			summary.Warnings++
		}
	}

	return model.ReconcileReport{
		GeneratedAt:     time.Now().UTC(),
		TfDirectory:     tfDir,
		PlanPath:        planPath,
		AutoPlan:        autoPlan,
		Subscription:    subscription,
		Summary:         summary,
		Findings:        SortedFindings(findings),
		Recommendations: recommendations,
	}
}

func WriteReconcileText(r model.ReconcileReport) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	_, _ = fmt.Fprintf(w, "Terraform reconcile report\n")
	_, _ = fmt.Fprintf(w, "Directory:\t%s\n", r.TfDirectory)
	_, _ = fmt.Fprintf(w, "Plan:\t%s\n", r.PlanPath)
	_, _ = fmt.Fprintf(w, "Candidates:\t%d\n", r.Summary.TotalCandidates)
	_, _ = fmt.Fprintf(w, "Evaluated:\t%d\n", r.Summary.EvaluatedCandidates)
	_, _ = fmt.Fprintf(w, "Import required:\t%d\n", r.Summary.ImportRequired)
	_, _ = fmt.Fprintf(w, "Errors:\t%d\n", r.Summary.Errors)
	_, _ = fmt.Fprintf(w, "Warnings:\t%d\n", r.Summary.Warnings)
	_, _ = fmt.Fprintf(w, "Status:\t%s\n\n", reconcileStatusFromFindings(r.Findings))

	if len(r.Recommendations) > 0 {
		_, _ = fmt.Fprintf(w, "Recommended imports:\n")
		_, _ = fmt.Fprintf(w, "ADDRESS\tTYPE\tIMPORT ID\tCOMMAND\n")
		for _, recommendation := range r.Recommendations {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				recommendation.TerraformAddress,
				recommendation.ResourceType,
				recommendation.ImportID,
				recommendation.Command,
			)
		}
		_, _ = fmt.Fprintln(w)
	}

	if len(r.Findings) == 0 {
		return
	}

	_, _ = fmt.Fprintf(w, "SEV\tCODE\tRESOURCE\tMESSAGE\n")
	for _, finding := range r.Findings {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", finding.Severity, finding.Code, finding.Resource, finding.Message)
	}
}

func WriteReconcileJSON(r model.ReconcileReport, outPath string) error {
	return writeJSONOutput(r, outPath)
}

func IsReconcileFailure(findings []model.Finding) bool {
	for _, finding := range findings {
		if finding.Severity == "error" {
			return true
		}
	}
	return false
}

func reconcileStatusFromFindings(findings []model.Finding) string {
	if len(findings) == 0 {
		return "PASS"
	}
	hasImportRequired := false
	for _, finding := range findings {
		if finding.Code == "IMPORT_REQUIRED" {
			hasImportRequired = true
		}
		if finding.Severity == "error" && finding.Code != "IMPORT_REQUIRED" {
			return "FAIL"
		}
	}
	if hasImportRequired {
		return "IMPORT_REQUIRED"
	}
	return "WARN"
}
