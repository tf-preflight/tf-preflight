package discovery

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tf-preflight/tf-preflight/internal/model"
)

type planFile struct {
	PlannedValues struct {
		RootModule struct {
			Resources []planResource `json:"resources"`
		} `json:"root_module"`
	} `json:"planned_values"`
	ResourceChanges []planChange `json:"resource_changes"`
}

type planResource struct {
	Address string         `json:"address"`
	Type    string         `json:"type"`
	Mode    string         `json:"mode"`
	Name    string         `json:"name"`
	Values  map[string]any `json:"values"`
}

type planChange struct {
	Address string         `json:"address"`
	Type    string         `json:"type"`
	Mode    string         `json:"mode"`
	Name    string         `json:"name"`
	Change  planChangeBody `json:"change"`
}

type planChangeBody struct {
	Actions      []string       `json:"actions"`
	After        map[string]any `json:"after"`
	AfterUnknown map[string]any `json:"after_unknown"`
}

// CandidatesFromPlan returns candidates with concrete values from plan output.
func CandidatesFromPlan(data []byte, hclContext *HCLContext) ([]model.Candidate, error) {
	var plan planFile
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("failed to decode plan json: %w", err)
	}

	addressIndex := newAddressIndex(hclContext)

	candidates := []model.Candidate{}
	seen := map[string]struct{}{}

	for _, item := range plan.ResourceChanges {
		base := model.Candidate{
			Address:         item.Address,
			ResourceType:    item.Type,
			Mode:            item.Mode,
			Name:            item.Name,
			Action:          classifyActions(item.Change.Actions),
			Source:          "plan",
			RawRestrictions: map[string]any{},
		}
		base.Location = firstString(item.Change.After, "location")
		base.ResourceGroup = firstString(item.Change.After, "resource_group_name")
		base.ServerID = firstString(item.Change.After, "server_id")
		base.KeyVaultID = firstString(item.Change.After, "key_vault_id")
		base.VirtualNetwork = firstString(item.Change.After, "virtual_network_name")
		base.Name = firstString(item.Change.After, "name")
		mergeTrafficManagerProfileFields(&base, firstString(item.Change.After, "profile_name"), firstString(item.Change.After, "profile_id"))
		if sku := pickNestedString(item.Change.After, "sku", "name"); sku != "" {
			base.Sku = sku
		}
		if base.Sku == "" {
			base.Sku = firstString(item.Change.After, "sku_name")
		}
		for _, r := range []string{"ip_restriction", "firewall_rules"} {
			if v, ok := item.Change.After[r]; ok {
				base.RawRestrictions[r] = v
			}
		}
		if isUnknown(item.Change.AfterUnknown, "location") {
			base.PlanUnknown = true
			base.Warnings = append(base.Warnings, "plan did not resolve location")
		}

		hcl, matchWarning, _ := addressIndex.find(item.Address)
		if hcl.Address != "" {
			if base.Location == "" {
				base.Location = hcl.Location
			}
			if base.ResourceGroup == "" {
				base.ResourceGroup = hcl.ResourceGroup
			}
			if base.ServerID == "" {
				base.ServerID = hcl.ServerID
			}
			if base.KeyVaultID == "" {
				base.KeyVaultID = hcl.KeyVaultID
			}
			if base.VirtualNetwork == "" {
				base.VirtualNetwork = hcl.VirtualNetwork
			}
			if base.TrafficManagerProfile == "" {
				base.TrafficManagerProfile = hcl.TrafficManagerProfile
			}
			if base.Name == "" {
				base.Name = hcl.Name
			}
			if base.Sku == "" {
				base.Sku = hcl.Sku
			}
			if len(base.RawRestrictions) == 0 {
				base.RawRestrictions = hcl.RawRestrictions
			}
			if base.Address == "" {
				base.Address = hcl.Address
			}
			base.Source = "merged"
		} else if matchWarning != "" {
			base.Warnings = append(base.Warnings, matchWarning)
		}

		if base.Address == "" {
			base.Address = item.Address
		}
		candidates = append(candidates, base)
		seen[base.Address] = struct{}{}
	}

	if len(plan.ResourceChanges) == 0 {
		for _, item := range plan.PlannedValues.RootModule.Resources {
			if _, ok := seen[item.Address]; ok {
				continue
			}
			base := model.Candidate{
				Address:         item.Address,
				ResourceType:    item.Type,
				Mode:            item.Mode,
				Name:            item.Name,
				Action:          "noop",
				Source:          "plan",
				RawRestrictions: map[string]any{},
			}
			base.Location = firstString(item.Values, "location")
			base.ResourceGroup = firstString(item.Values, "resource_group_name")
			base.ServerID = firstString(item.Values, "server_id")
			base.KeyVaultID = firstString(item.Values, "key_vault_id")
			base.VirtualNetwork = firstString(item.Values, "virtual_network_name")
			base.Name = firstString(item.Values, "name")
			mergeTrafficManagerProfileFields(&base, firstString(item.Values, "profile_name"), firstString(item.Values, "profile_id"))
			base.Sku = pickNestedString(item.Values, "sku", "name")
			if base.Sku == "" {
				base.Sku = firstString(item.Values, "sku_name")
			}
			candidates = append(candidates, base)
		}
	}

	return candidates, nil
}

type addressIndex struct {
	exact      map[string]model.Candidate
	normalized map[string][]model.Candidate
}

func newAddressIndex(hclContext *HCLContext) addressIndex {
	idx := addressIndex{
		exact:      map[string]model.Candidate{},
		normalized: map[string][]model.Candidate{},
	}
	if hclContext == nil {
		return idx
	}

	for _, c := range hclContext.Candidates {
		if strings.TrimSpace(c.Address) == "" {
			continue
		}
		idx.exact[c.Address] = c
		norm := normalizeTerraformAddress(c.Address)
		idx.normalized[norm] = append(idx.normalized[norm], c)
	}

	return idx
}

func (idx addressIndex) find(planAddress string) (model.Candidate, string, bool) {
	if c, ok := idx.exact[planAddress]; ok {
		return c, "", true
	}

	norm := normalizeTerraformAddress(planAddress)
	matches := idx.normalized[norm]
	switch len(matches) {
	case 1:
		return matches[0], "", true
	case 0:
		return model.Candidate{}, fmt.Sprintf("no matching HCL resource found for plan address %q; using plan values only", planAddress), false
	default:
		return model.Candidate{}, fmt.Sprintf("multiple HCL resources matched normalized plan address %q; using plan values only", norm), false
	}
}

func normalizeTerraformAddress(address string) string {
	var b strings.Builder
	depth := 0
	for _, r := range address {
		switch r {
		case '[':
			depth++
			continue
		case ']':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth == 0 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func classifyActions(actions []string) string {
	if len(actions) == 1 {
		switch actions[0] {
		case "create":
			return "create"
		case "delete":
			return "delete"
		case "no-op":
			return "noop"
		case "read":
			return "noop"
		}
	}
	if len(actions) == 2 && actions[0] == "delete" && actions[1] == "create" {
		return "replace"
	}
	for _, a := range actions {
		if a == "update" {
			return "update"
		}
		if a == "create" {
			return "create"
		}
	}
	return "noop"
}

func isUnknown(flags map[string]any, key string) bool {
	if flags == nil {
		return false
	}
	if value, ok := flags[key]; ok {
		if v, ok := value.(bool); ok {
			return v
		}
	}
	return false
}

func firstString(m map[string]any, path string) string {
	if m == nil {
		return ""
	}
	if val, ok := m[path]; ok {
		if s, ok := toStringFromAny(val); ok {
			return s
		}
	}
	return ""
}

func pickNestedString(m map[string]any, path ...string) string {
	var current any = m
	for _, key := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		next, ok := obj[key]
		if !ok {
			return ""
		}
		current = next
	}
	if s, ok := toStringFromAny(current); ok {
		return s
	}
	return ""
}

func toStringFromAny(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return fmt.Sprintf("%v", x), true
	case int:
		return fmt.Sprintf("%d", x), true
	case bool:
		return fmt.Sprintf("%t", x), true
	}
	return "", false
}
