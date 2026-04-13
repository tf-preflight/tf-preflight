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

func mergeFrontDoorProfileFields(candidate *model.Candidate, profileID string) {
	if candidate == nil {
		return
	}

	resourceGroup, profile, ok := parseFrontDoorProfileID(profileID)
	if !ok {
		return
	}
	if candidate.ResourceGroup == "" {
		candidate.ResourceGroup = resourceGroup
	}
	if candidate.FrontDoorProfile == "" {
		candidate.FrontDoorProfile = profile
	}
}

func mergeFrontDoorEndpointFields(candidate *model.Candidate, endpointID string) {
	if candidate == nil {
		return
	}

	resourceGroup, profile, endpoint, ok := parseFrontDoorEndpointID(endpointID)
	if !ok {
		return
	}
	if candidate.ResourceGroup == "" {
		candidate.ResourceGroup = resourceGroup
	}
	if candidate.FrontDoorProfile == "" {
		candidate.FrontDoorProfile = profile
	}
	if candidate.FrontDoorEndpoint == "" {
		candidate.FrontDoorEndpoint = endpoint
	}
}

func mergeFrontDoorOriginGroupFields(candidate *model.Candidate, originGroupID string) {
	if candidate == nil {
		return
	}

	resourceGroup, profile, originGroup, ok := parseFrontDoorOriginGroupID(originGroupID)
	if !ok {
		return
	}
	if candidate.ResourceGroup == "" {
		candidate.ResourceGroup = resourceGroup
	}
	if candidate.FrontDoorProfile == "" {
		candidate.FrontDoorProfile = profile
	}
	if candidate.FrontDoorOriginGroup == "" {
		candidate.FrontDoorOriginGroup = originGroup
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

func parseFrontDoorProfileID(raw string) (string, string, bool) {
	resourceGroup, profile, _, _, ok := parseFrontDoorResourceID(raw)
	if !ok {
		return "", "", false
	}
	return resourceGroup, profile, true
}

func parseFrontDoorEndpointID(raw string) (string, string, string, bool) {
	resourceGroup, profile, endpoint, _, ok := parseFrontDoorResourceID(raw)
	if !ok || strings.TrimSpace(endpoint) == "" {
		return "", "", "", false
	}
	return resourceGroup, profile, endpoint, true
}

func parseFrontDoorOriginGroupID(raw string) (string, string, string, bool) {
	resourceGroup, profile, _, originGroup, ok := parseFrontDoorResourceID(raw)
	if !ok || strings.TrimSpace(originGroup) == "" {
		return "", "", "", false
	}
	return resourceGroup, profile, originGroup, true
}

func parseFrontDoorResourceID(raw string) (string, string, string, string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", "", "", false
	}

	parts := strings.Split(trimmed, "/")
	var resourceGroup string
	var profile string
	var endpoint string
	var originGroup string

	for i := 0; i < len(parts)-1; i++ {
		switch {
		case strings.EqualFold(parts[i], "resourceGroups"):
			resourceGroup = parts[i+1]
		case strings.EqualFold(parts[i], "profiles"):
			profile = parts[i+1]
		case strings.EqualFold(parts[i], "afdEndpoints"):
			endpoint = parts[i+1]
		case strings.EqualFold(parts[i], "originGroups"):
			originGroup = parts[i+1]
		}
	}

	if strings.TrimSpace(resourceGroup) == "" || strings.TrimSpace(profile) == "" {
		return "", "", "", "", false
	}
	return resourceGroup, profile, endpoint, originGroup, true
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
	case "azurerm_storage_account":
		if missingCandidateFields(candidate.ResourceGroup, candidate.Name) {
			return "", false
		}
		return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Storage/storageAccounts/%s", subscriptionID, candidate.ResourceGroup, candidate.Name), true
	case "azurerm_windows_web_app", "azurerm_linux_web_app":
		if missingCandidateFields(candidate.ResourceGroup, candidate.Name) {
			return "", false
		}
		return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s", subscriptionID, candidate.ResourceGroup, candidate.Name), true
	case "azurerm_cdn_frontdoor_profile":
		if missingCandidateFields(candidate.ResourceGroup, candidate.Name) {
			return "", false
		}
		return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s", subscriptionID, candidate.ResourceGroup, candidate.Name), true
	case "azurerm_cdn_frontdoor_endpoint":
		if missingCandidateFields(candidate.ResourceGroup, candidate.FrontDoorProfile, candidate.Name) {
			return "", false
		}
		return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/afdEndpoints/%s", subscriptionID, candidate.ResourceGroup, candidate.FrontDoorProfile, candidate.Name), true
	case "azurerm_cdn_frontdoor_origin_group":
		if missingCandidateFields(candidate.ResourceGroup, candidate.FrontDoorProfile, candidate.Name) {
			return "", false
		}
		return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/originGroups/%s", subscriptionID, candidate.ResourceGroup, candidate.FrontDoorProfile, candidate.Name), true
	case "azurerm_cdn_frontdoor_origin":
		if missingCandidateFields(candidate.ResourceGroup, candidate.FrontDoorProfile, candidate.FrontDoorOriginGroup, candidate.Name) {
			return "", false
		}
		return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/originGroups/%s/origins/%s", subscriptionID, candidate.ResourceGroup, candidate.FrontDoorProfile, candidate.FrontDoorOriginGroup, candidate.Name), true
	case "azurerm_cdn_frontdoor_route":
		if missingCandidateFields(candidate.ResourceGroup, candidate.FrontDoorProfile, candidate.FrontDoorEndpoint, candidate.Name) {
			return "", false
		}
		return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/afdEndpoints/%s/routes/%s", subscriptionID, candidate.ResourceGroup, candidate.FrontDoorProfile, candidate.FrontDoorEndpoint, candidate.Name), true
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
