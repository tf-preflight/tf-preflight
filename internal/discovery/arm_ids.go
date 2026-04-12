package discovery

import (
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
