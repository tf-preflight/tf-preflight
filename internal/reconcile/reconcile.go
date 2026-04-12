package reconcile

import (
	"context"
	"fmt"
	"strings"

	"github.com/tf-preflight/tf-preflight/internal/azure"
	"github.com/tf-preflight/tf-preflight/internal/model"
	"github.com/tf-preflight/tf-preflight/internal/ui"
)

type Result struct {
	EvaluatedCandidates int
	Findings            []model.Finding
	Recommendations     []model.ImportRecommendation
}

func Run(ctx context.Context, candidates []model.Candidate, client *azure.AzureClient, subscriptionID string, workingDir string, progress *ui.Progress) (Result, error) {
	if client == nil || client.Token == "" {
		return Result{}, fmt.Errorf("no azure token available")
	}

	filtered := FilterCandidates(candidates)
	result := Result{EvaluatedCandidates: len(filtered)}

	if progress != nil {
		progress.Start("checking import gaps", len(filtered))
	}

	for _, candidate := range filtered {
		if candidate.SubscriptionID == "" {
			candidate.SubscriptionID = subscriptionID
		}
		if progress != nil {
			progress.Message(fmt.Sprintf("reconciling %s (%s)", candidate.Address, candidate.ResourceType))
		}

		existsPath, existsMissing, existsSupported := azure.BuildExistsPath(candidate)
		importID, importMissing, importSupported := azure.BuildImportID(candidate)

		if !existsSupported || !importSupported {
			result.Findings = append(result.Findings, model.Finding{
				Severity: "warn",
				Code:     "IMPORT_ID_UNSUPPORTED",
				Message:  fmt.Sprintf("resource type %s is not supported for import reconciliation", candidate.ResourceType),
				Resource: candidate.Address,
			})
			if progress != nil {
				progress.Tick(fmt.Sprintf("%s unsupported", candidate.Address))
			}
			continue
		}

		missing := uniqueStrings(append(existsMissing, importMissing...))
		if len(missing) > 0 {
			result.Findings = append(result.Findings, model.Finding{
				Severity: "warn",
				Code:     "IMPORT_CANDIDATE_INCOMPLETE",
				Message:  fmt.Sprintf("cannot build import ID for %s; missing %s", candidate.Address, strings.Join(missing, ", ")),
				Resource: candidate.Address,
				Detail: map[string]any{
					"missing_fields": missing,
				},
			})
			if progress != nil {
				progress.Tick(fmt.Sprintf("%s missing candidate data", candidate.Address))
			}
			continue
		}

		status, err := client.ProbePath(ctx, "GET", existsPath)
		if err != nil {
			result.Findings = append(result.Findings, model.Finding{
				Severity: "error",
				Code:     "IMPORT_LOOKUP_FAILED",
				Message:  fmt.Sprintf("azure lookup failed for %s: %v", candidate.Address, err),
				Resource: candidate.Address,
			})
			if progress != nil {
				progress.Tick(fmt.Sprintf("%s lookup failed", candidate.Address))
			}
			continue
		}

		switch {
		case status == 404:
			if progress != nil {
				progress.Tick(fmt.Sprintf("%s not found", candidate.Address))
			}
			continue
		case status >= 200 && status < 300:
			recommendation := model.ImportRecommendation{
				TerraformAddress: candidate.Address,
				ResourceType:     candidate.ResourceType,
				ImportID:         importID,
				WorkingDirectory: workingDir,
				Command:          renderImportCommand(candidate.Address, importID),
			}
			result.Recommendations = append(result.Recommendations, recommendation)
			result.Findings = append(result.Findings, model.Finding{
				Severity: "error",
				Code:     "IMPORT_REQUIRED",
				Message:  fmt.Sprintf("resource already exists in Azure and should be imported: %s", importID),
				Resource: candidate.Address,
				Detail: map[string]any{
					"import_id": importID,
					"command":   recommendation.Command,
				},
			})
			if progress != nil {
				progress.Tick(fmt.Sprintf("%s import required", candidate.Address))
			}
		default:
			result.Findings = append(result.Findings, model.Finding{
				Severity: "error",
				Code:     "IMPORT_LOOKUP_FAILED",
				Message:  fmt.Sprintf("azure lookup for %s returned unexpected status %d", candidate.Address, status),
				Resource: candidate.Address,
			})
			if progress != nil {
				progress.Tick(fmt.Sprintf("%s lookup failed", candidate.Address))
			}
		}
	}

	if progress != nil {
		progress.Done("import gap checks complete")
	}

	return result, nil
}

func FilterCandidates(candidates []model.Candidate) []model.Candidate {
	filtered := make([]model.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Action != "create" {
			continue
		}
		if candidate.Mode != "" && candidate.Mode != "managed" {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}

func renderImportCommand(address, importID string) string {
	return fmt.Sprintf("terraform import %s %s", shellQuote(address), shellQuote(importID))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
