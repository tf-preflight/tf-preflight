package discovery

import (
	"encoding/json"
	"testing"

	"github.com/tf-preflight/tf-preflight/internal/model"
)

func TestCandidatesFromPlanMergesHCLAndPlan(t *testing.T) {
	hcl := &HCLContext{
		Candidates: []model.Candidate{
			{
				Address:      "azurerm_service_plan.asp",
				ResourceType: "azurerm_service_plan",
				Name:         "asp",
				Location:     "west europe",
			},
		},
	}

	planJSON := map[string]any{
		"resource_changes": []map[string]any{
			{
				"address": "module.web.azurerm_service_plan.asp",
				"type":    "azurerm_service_plan",
				"name":    "asp",
				"change": map[string]any{
					"actions": []string{"create"},
					"after": map[string]any{
						"location": "East US",
						"sku":      map[string]any{"name": "S1"},
					},
					"after_unknown": map[string]any{},
				},
			},
		},
	}

	blob, _ := json.Marshal(planJSON)
	cands, err := CandidatesFromPlan(blob, hcl)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
	if got := cands[0].Location; got != "East US" {
		t.Fatalf("expected plan location East US, got %s", got)
	}
	if got := cands[0].Sku; got != "S1" {
		t.Fatalf("expected sku S1, got %s", got)
	}
}
