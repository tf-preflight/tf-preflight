package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/tf-preflight/tf-preflight/internal/model"
)

func BuildReport(tfDir, planPath string, autoPlan bool, subscription string, candidates []model.Candidate, findings []model.Finding) model.Report {
	normalizedFindings := DecorateAndSortFindings(findings)
	summary := model.Summary{TotalCandidates: len(candidates)}
	for _, f := range normalizedFindings {
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
		Decision:     BuildDecision(normalizedFindings),
		Candidates:   candidates,
		Findings:     normalizedFindings,
	}
}

func WriteText(r model.Report) {
	writeText(os.Stdout, r)
}

func writeText(out io.Writer, r model.Report) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	defer w.Flush()

	_, _ = fmt.Fprintf(w, "Terraform preflight report\n")
	_, _ = fmt.Fprintf(w, "Directory:\t%s\n", r.TfDirectory)
	_, _ = fmt.Fprintf(w, "Plan:\t%s\n", r.PlanPath)
	_, _ = fmt.Fprintf(w, "Candidates:\t%d\n", r.Summary.TotalCandidates)
	_, _ = fmt.Fprintf(w, "Errors:\t%d\n", r.Summary.Errors)
	_, _ = fmt.Fprintf(w, "Warnings:\t%d\n", r.Summary.Warnings)
	_, _ = fmt.Fprintf(w, "Decision:\n")
	_, _ = fmt.Fprintf(w, "  Result:\t%s\n", r.Decision.Result)
	_, _ = fmt.Fprintf(w, "  Deployability:\t%s\n", deployabilityLabel(r.Decision.Deployable))
	_, _ = fmt.Fprintf(w, "  Confidence:\t%s\n", r.Decision.Confidence)
	_, _ = fmt.Fprintf(w, "  Blockers:\t%d\n", r.Decision.Blockers)
	_, _ = fmt.Fprintf(w, "  Degraded checks:\t%d\n", r.Decision.Degraded)
	_, _ = fmt.Fprintf(w, "  Advisories:\t%d\n\n", r.Decision.Advisories)

	if len(r.Findings) == 0 {
		return
	}

	_, _ = fmt.Fprintf(w, "CLASS\tSEV\tCODE\tRESOURCE\tMESSAGE\n")
	for _, f := range r.Findings {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", findingCategory(f), f.Severity, f.Code, f.Resource, f.Message)
	}
}

func WriteJSON(r model.Report, outPath string) error {
	return writeJSONOutput(r, outPath)
}

func writeJSONOutput(v any, outPath string) error {
	data, err := json.MarshalIndent(v, "", "  ")
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
		if categoryRank(findingCategory(sorted[i])) != categoryRank(findingCategory(sorted[j])) {
			return categoryRank(findingCategory(sorted[i])) < categoryRank(findingCategory(sorted[j]))
		}
		if severityRank(sorted[i].Severity) != severityRank(sorted[j].Severity) {
			return severityRank(sorted[i].Severity) < severityRank(sorted[j].Severity)
		}
		if sorted[i].Code != sorted[j].Code {
			return sorted[i].Code < sorted[j].Code
		}
		if sorted[i].Resource != sorted[j].Resource {
			return sorted[i].Resource < sorted[j].Resource
		}
		return sorted[i].Message < sorted[j].Message
	})
	return sorted
}

func DecorateAndSortFindings(findings []model.Finding) []model.Finding {
	decorated := append([]model.Finding{}, findings...)
	for i := range decorated {
		decorated[i].Category = classifyFinding(decorated[i])
	}
	return SortedFindings(decorated)
}

func BuildDecision(findings []model.Finding) model.Decision {
	decision := model.Decision{
		Deployable: true,
		Confidence: "HIGH",
		Result:     "PASS",
	}

	for _, finding := range findings {
		switch findingCategory(finding) {
		case "BLOCKER":
			decision.Blockers++
		case "DEGRADED":
			decision.Degraded++
		default:
			decision.Advisories++
		}
	}

	switch {
	case decision.Blockers > 0:
		decision.Result = "BLOCKED"
		decision.Deployable = false
	case decision.Degraded > 0:
		decision.Result = "DEGRADED"
	case decision.Advisories > 0:
		decision.Result = "ADVISORY"
	}
	if decision.Degraded > 0 {
		decision.Confidence = "DEGRADED"
	}

	return decision
}

func classifyFinding(finding model.Finding) string {
	switch strings.ToUpper(strings.TrimSpace(finding.Code)) {
	case "SUBSCRIPTION_LOCATIONS",
		"PROVIDER_QUERY_FAILED",
		"QUOTA_UNKNOWN",
		"QUOTA_CHECK_UNSUPPORTED",
		"RESOURCE_EXISTS_CHECK_FAILED",
		"RESOURCE_EXISTS_CHECK_UNSUPPORTED",
		"RESOURCE_EXISTS_CHECK_INCOMPLETE",
		"UNSUPPORTED_RESOURCE_TYPE",
		"MISSING_LOCATION",
		"MODULE_SOURCE_UNKNOWN",
		"MODULE_SOURCE_UNREADABLE":
		return "DEGRADED"
	}
	if strings.EqualFold(strings.TrimSpace(finding.Severity), "error") {
		return "BLOCKER"
	}
	return "ADVISORY"
}

func findingCategory(finding model.Finding) string {
	if strings.TrimSpace(finding.Category) != "" {
		return finding.Category
	}
	return classifyFinding(finding)
}

func categoryRank(category string) int {
	switch category {
	case "BLOCKER":
		return 0
	case "DEGRADED":
		return 1
	default:
		return 2
	}
}

func severityRank(severity string) int {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "error":
		return 0
	case "warn":
		return 1
	default:
		return 2
	}
}

func deployabilityLabel(v bool) string {
	if v {
		return "not blocked"
	}
	return "blocked"
}
