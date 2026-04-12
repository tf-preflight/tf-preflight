package discovery

import (
	"fmt"
	"strings"

	"github.com/tf-preflight/tf-preflight/internal/model"
)

func mergeTrafficManagerProfileFields(candidate *model.Candidate, profileName, profileID string) {
	if candidate == nil {
		return
	}
	if strings.TrimSpace(profileName) != "" && candidate.TrafficManagerProfile == "" {
		candidate.TrafficManagerProfile = strings.TrimSpace(profileName)
	}

	resourceGroup, parsedProfile, ok := parseTrafficManagerProfileID(profileID)
	if !ok {
		return
	}
	if candidate.ResourceGroup == "" {
		candidate.ResourceGroup = resourceGroup
	}
	if candidate.TrafficManagerProfile == "" {
		candidate.TrafficManagerProfile = parsedProfile
	}
}

func parseTrafficManagerProfileID(raw string) (string, string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", false
	}

	parts := strings.Split(trimmed, "/")
	var resourceGroup string
	var profile string

	for i := 0; i < len(parts)-1; i++ {
		switch {
		case strings.EqualFold(parts[i], "resourceGroups"):
			resourceGroup = parts[i+1]
		case strings.EqualFold(parts[i], "trafficManagerProfiles"):
			profile = parts[i+1]
		}
	}

	if strings.TrimSpace(resourceGroup) == "" || strings.TrimSpace(profile) == "" {
		return "", "", false
	}
	return resourceGroup, profile, true
}

func buildCandidateResourceID(candidate model.Candidate) (string, bool) {
	subscriptionID := candidate.SubscriptionID
	if strings.TrimSpace(subscriptionID) == "" {
		subscriptionID = "__tfpreflight_unknown__"
	}
	switch candidate.ResourceType {
	case "azurerm_resource_group":
		if strings.TrimSpace(candidate.Name) == "" {
			return "", false
		}
		return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", subscriptionID, candidate.Name), true
	case "azurerm_service_plan":
		if missingCandidateFields(candidate.ResourceGroup, candidate.Name) {
			return "", false
		}
		return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/serverFarms/%s", subscriptionID, candidate.ResourceGroup, candidate.Name), true
	case "azurerm_windows_web_app", "azurerm_linux_web_app":
		if missingCandidateFields(candidate.ResourceGroup, candidate.Name) {
			return "", false
		}
		return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s", subscriptionID, candidate.ResourceGroup, candidate.Name), true
	case "azurerm_traffic_manager_profile":
		if missingCandidateFields(candidate.ResourceGroup, candidate.Name) {
			return "", false
		}
		return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/trafficManagerProfiles/%s", subscriptionID, candidate.ResourceGroup, candidate.Name), true
	case "azurerm_virtual_network":
		if missingCandidateFields(candidate.ResourceGroup, candidate.Name) {
			return "", false
		}
		return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s", subscriptionID, candidate.ResourceGroup, candidate.Name), true
	case "azurerm_subnet":
		if missingCandidateFields(candidate.ResourceGroup, candidate.VirtualNetwork, candidate.Name) {
			return "", false
		}
		return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s/subnets/%s", subscriptionID, candidate.ResourceGroup, candidate.VirtualNetwork, candidate.Name), true
	case "azurerm_mssql_server":
		if missingCandidateFields(candidate.ResourceGroup, candidate.Name) {
			return "", false
		}
		return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Sql/servers/%s", subscriptionID, candidate.ResourceGroup, candidate.Name), true
	case "azurerm_key_vault":
		if missingCandidateFields(candidate.ResourceGroup, candidate.Name) {
			return "", false
		}
		return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.KeyVault/vaults/%s", subscriptionID, candidate.ResourceGroup, candidate.Name), true
	default:
		return "", false
	}
}

func missingCandidateFields(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return true
		}
	}
	return false
}
