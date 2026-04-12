package report

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/tf-preflight/tf-preflight/internal/model"
)

func BuildReport(tfDir, planPath string, autoPlan bool, subscription string, candidates []model.Candidate, findings []model.Finding) model.Report {
	summary := model.Summary{TotalCandidates: len(candidates)}
	for _, f := range findings {
		switch f.Severity {
		case "error":
			summary.Errors++
		case "warn":
			summary.Warnings++
		}
	}
	for _, c := range candidates {
		switch c.Action {
		case "create":
			summary.Actions.Create++
		case "update":
			summary.Actions.Update++
		case "delete":
			summary.Actions.Delete++
		case "replace":
			summary.Actions.Update++
		default:
			summary.Actions.Noop++
		}
	}

	return model.Report{
		GeneratedAt:  time.Now().UTC(),
		TfDirectory:  tfDir,
		PlanPath:     planPath,
		AutoPlan:     autoPlan,
		Subscription: subscription,
		Summary:      summary,
		Candidates:   candidates,
		Findings:     findings,
	}
}

func WriteText(r model.Report) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	_, _ = fmt.Fprintf(w, "Terraform preflight report\n")
	_, _ = fmt.Fprintf(w, "Directory:\t%s\n", r.TfDirectory)
	_, _ = fmt.Fprintf(w, "Plan:\t%s\n", r.PlanPath)
	_, _ = fmt.Fprintf(w, "Candidates:\t%d\n", r.Summary.TotalCandidates)
	_, _ = fmt.Fprintf(w, "Errors:\t%d\n", r.Summary.Errors)
	_, _ = fmt.Fprintf(w, "Warnings:\t%d\n", r.Summary.Warnings)
	_, _ = fmt.Fprintf(w, "Status:\t%s\n\n", statusFromFindings(r.Findings))

	if len(r.Findings) == 0 {
		return
	}

	_, _ = fmt.Fprintf(w, "SEV\tCODE\tRESOURCE\tMESSAGE\n")
	for _, f := range r.Findings {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", f.Severity, f.Code, f.Resource, f.Message)
	}
}

func WriteJSON(r model.Report, outPath string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if outPath == "" {
		_, err = os.Stdout.Write(data)
		if err == nil {
			_, err = os.Stdout.WriteString("\n")
		}
		return err
	}
	return os.WriteFile(outPath, data, 0o644)
}

func IsFailure(findings []model.Finding, threshold string) bool {
	if threshold == "warn" {
		return len(findings) > 0
	}
	for _, f := range findings {
		if f.Severity == "error" {
			return true
		}
	}
	return false
}

func SortedFindings(findings []model.Finding) []model.Finding {
	sorted := append([]model.Finding{}, findings...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Severity == sorted[j].Severity {
			return sorted[i].Code < sorted[j].Code
		}
		return sorted[i].Severity < sorted[j].Severity
	})
	return sorted
}

func statusFromFindings(findings []model.Finding) string {
	if len(findings) == 0 {
		return "PASS"
	}
	for _, f := range findings {
		if f.Severity == "error" {
			return "FAIL"
		}
	}
	return "WARN"
}
