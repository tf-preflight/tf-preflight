package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/tf-preflight/tf-preflight/internal/model"
	"github.com/tf-preflight/tf-preflight/internal/ui"
)

type ResourceMeta struct {
	Namespace   string
	ExistsPath  string
	ImportPath  string
	QuotaPath   string
	QuotaChecks []string
}

const (
	ResourceManagerAudience      = "https://management.azure.com"
	KeyVaultAudience             = "https://vault.azure.net"
	storageAPIVersion            = "2025-06-01"
	frontDoorAPIVersion          = "2025-04-15"
	sqlCapabilitiesAPIVersion    = "2025-01-01"
	sqlServerAPIVersion          = "2023-02-01"
	keyVaultSecretAPIVersion     = "7.4"
	keyVaultManagementAPIVersion = "2022-07-01"
	unknownSubscriptionID        = "__tfpreflight_unknown__"
)

type locationCatalog struct {
	known           map[string]struct{}
	canonicalByName map[string]string
	apiNameByName   map[string]string
}

var resourceMeta = map[string]ResourceMeta{
	"azurerm_resource_group": {
		Namespace:  "Microsoft.Resources",
		ExistsPath: "/subscriptions/%s/resourceGroups/%s?api-version=2022-09-01",
		ImportPath: "/subscriptions/%s/resourceGroups/%s",
		QuotaPath:  "",
	},
	"azurerm_service_plan": {
		Namespace:   "Microsoft.Web",
		ExistsPath:  "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/serverFarms/%s?api-version=2023-01-01",
		ImportPath:  "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/serverFarms/%s",
		QuotaPath:   "/subscriptions/%s/providers/Microsoft.Web/locations/%s/usages?api-version=2025-03-01",
		QuotaChecks: []string{"cores usage"},
	},
	"azurerm_storage_account": {
		Namespace:  "Microsoft.Storage",
		ExistsPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Storage/storageAccounts/%s?api-version=" + storageAPIVersion,
		ImportPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Storage/storageAccounts/%s",
	},
	"azurerm_windows_web_app": {
		Namespace:  "Microsoft.Web",
		ExistsPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s?api-version=2023-01-01",
		ImportPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s",
	},
	"azurerm_linux_web_app": {
		Namespace:  "Microsoft.Web",
		ExistsPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s?api-version=2023-01-01",
		ImportPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s",
	},
	"azurerm_cdn_frontdoor_profile": {
		Namespace:  "Microsoft.Cdn",
		ExistsPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s?api-version=" + frontDoorAPIVersion,
		ImportPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s",
	},
	"azurerm_cdn_frontdoor_endpoint": {
		Namespace:  "Microsoft.Cdn",
		ExistsPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/afdEndpoints/%s?api-version=" + frontDoorAPIVersion,
		ImportPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/afdEndpoints/%s",
	},
	"azurerm_cdn_frontdoor_origin_group": {
		Namespace:  "Microsoft.Cdn",
		ExistsPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/originGroups/%s?api-version=" + frontDoorAPIVersion,
		ImportPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/originGroups/%s",
	},
	"azurerm_cdn_frontdoor_origin": {
		Namespace:  "Microsoft.Cdn",
		ExistsPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/originGroups/%s/origins/%s?api-version=" + frontDoorAPIVersion,
		ImportPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/originGroups/%s/origins/%s",
	},
	"azurerm_cdn_frontdoor_route": {
		Namespace:  "Microsoft.Cdn",
		ExistsPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/afdEndpoints/%s/routes/%s?api-version=" + frontDoorAPIVersion,
		ImportPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/afdEndpoints/%s/routes/%s",
	},
	"azurerm_traffic_manager_profile": {
		Namespace:   "Microsoft.Network",
		ExistsPath:  "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/trafficManagerProfiles/%s?api-version=2023-04-01",
		ImportPath:  "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/trafficManagerProfiles/%s",
		QuotaPath:   "/subscriptions/%s/providers/Microsoft.Network/locations/%s/usages?api-version=2022-01-01",
		QuotaChecks: []string{"traffic manager profiles"},
	},
	"azurerm_traffic_manager_azure_endpoint": {
		Namespace:  "Microsoft.Network",
		ExistsPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/trafficManagerProfiles/%s/AzureEndpoints/%s?api-version=2022-04-01",
		ImportPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/trafficManagerProfiles/%s/AzureEndpoints/%s",
	},
	"azurerm_virtual_network": {
		Namespace:  "Microsoft.Network",
		ExistsPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s?api-version=2023-09-01",
		ImportPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s",
	},
	"azurerm_subnet": {
		Namespace:  "Microsoft.Network",
		ExistsPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s/subnets/%s?api-version=2023-09-01",
		ImportPath: "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s/subnets/%s",
	},
	"azurerm_mssql_server": {
		Namespace:   "Microsoft.Sql",
		ExistsPath:  "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Sql/servers/%s?api-version=2023-02-01",
		ImportPath:  "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Sql/servers/%s",
		QuotaPath:   "/subscriptions/%s/providers/Microsoft.Sql/locations/%s/usages?api-version=2022-02-01",
		QuotaChecks: []string{"logical servers", "servers"},
	},
	"azurerm_mssql_database": {
		Namespace:   "Microsoft.Sql",
		ExistsPath:  "",
		QuotaPath:   "/subscriptions/%s/providers/Microsoft.Sql/locations/%s/usages?api-version=2022-02-01",
		QuotaChecks: []string{"databases"},
	},
	"azurerm_mssql_firewall_rule": {
		Namespace:  "Microsoft.Sql",
		ExistsPath: "",
		QuotaPath:  "",
	},
	"azurerm_key_vault_secret": {
		Namespace: "Microsoft.KeyVault",
	},
}

func ResolveNamespace(resourceType string) (ResourceMeta, bool) {
	meta, ok := resourceMeta[resourceType]
	return meta, ok
}

func BuildExistsPath(candidate model.Candidate) (string, []string, bool) {
	return buildResourcePath(candidate, true)
}

func BuildImportID(candidate model.Candidate) (string, []string, bool) {
	return buildResourcePath(candidate, false)
}

func buildResourcePath(candidate model.Candidate, escaped bool) (string, []string, bool) {
	meta, ok := ResolveNamespace(candidate.ResourceType)
	if !ok {
		return "", nil, false
	}

	template := meta.ImportPath
	if escaped {
		template = meta.ExistsPath
	}
	if template == "" {
		return "", nil, false
	}

	value := func(raw string) string {
		if !escaped {
			return raw
		}
		return url.PathEscape(raw)
	}

	switch candidate.ResourceType {
	case "azurerm_resource_group":
		missing := missingFields(map[string]string{
			"subscription_id": candidate.SubscriptionID,
			"name":            candidate.Name,
		})
		if len(missing) > 0 {
			return "", missing, true
		}
		return fmt.Sprintf(template, candidate.SubscriptionID, value(candidate.Name)), nil, true
	case "azurerm_service_plan",
		"azurerm_storage_account",
		"azurerm_windows_web_app",
		"azurerm_linux_web_app",
		"azurerm_cdn_frontdoor_profile",
		"azurerm_virtual_network",
		"azurerm_traffic_manager_profile",
		"azurerm_mssql_server":
		missing := missingFields(map[string]string{
			"subscription_id": candidate.SubscriptionID,
			"resource_group":  candidate.ResourceGroup,
			"name":            candidate.Name,
		})
		if len(missing) > 0 {
			return "", missing, true
		}
		return fmt.Sprintf(template, candidate.SubscriptionID, value(candidate.ResourceGroup), value(candidate.Name)), nil, true
	case "azurerm_cdn_frontdoor_endpoint":
		missing := missingFields(map[string]string{
			"subscription_id":   candidate.SubscriptionID,
			"resource_group":    candidate.ResourceGroup,
			"frontdoor_profile": candidate.FrontDoorProfile,
			"name":              candidate.Name,
		})
		if len(missing) > 0 {
			return "", missing, true
		}
		return fmt.Sprintf(template, candidate.SubscriptionID, value(candidate.ResourceGroup), value(candidate.FrontDoorProfile), value(candidate.Name)), nil, true
	case "azurerm_cdn_frontdoor_origin_group":
		missing := missingFields(map[string]string{
			"subscription_id":   candidate.SubscriptionID,
			"resource_group":    candidate.ResourceGroup,
			"frontdoor_profile": candidate.FrontDoorProfile,
			"name":              candidate.Name,
		})
		if len(missing) > 0 {
			return "", missing, true
		}
		return fmt.Sprintf(template, candidate.SubscriptionID, value(candidate.ResourceGroup), value(candidate.FrontDoorProfile), value(candidate.Name)), nil, true
	case "azurerm_cdn_frontdoor_origin":
		missing := missingFields(map[string]string{
			"subscription_id":        candidate.SubscriptionID,
			"resource_group":         candidate.ResourceGroup,
			"frontdoor_profile":      candidate.FrontDoorProfile,
			"frontdoor_origin_group": candidate.FrontDoorOriginGroup,
			"name":                   candidate.Name,
		})
		if len(missing) > 0 {
			return "", missing, true
		}
		return fmt.Sprintf(template, candidate.SubscriptionID, value(candidate.ResourceGroup), value(candidate.FrontDoorProfile), value(candidate.FrontDoorOriginGroup), value(candidate.Name)), nil, true
	case "azurerm_cdn_frontdoor_route":
		missing := missingFields(map[string]string{
			"subscription_id":    candidate.SubscriptionID,
			"resource_group":     candidate.ResourceGroup,
			"frontdoor_profile":  candidate.FrontDoorProfile,
			"frontdoor_endpoint": candidate.FrontDoorEndpoint,
			"name":               candidate.Name,
		})
		if len(missing) > 0 {
			return "", missing, true
		}
		return fmt.Sprintf(template, candidate.SubscriptionID, value(candidate.ResourceGroup), value(candidate.FrontDoorProfile), value(candidate.FrontDoorEndpoint), value(candidate.Name)), nil, true
	case "azurerm_subnet":
		missing := missingFields(map[string]string{
			"subscription_id": candidate.SubscriptionID,
			"resource_group":  candidate.ResourceGroup,
			"virtual_network": candidate.VirtualNetwork,
			"name":            candidate.Name,
		})
		if len(missing) > 0 {
			return "", missing, true
		}
		return fmt.Sprintf(template, candidate.SubscriptionID, value(candidate.ResourceGroup), value(candidate.VirtualNetwork), value(candidate.Name)), nil, true
	case "azurerm_traffic_manager_azure_endpoint":
		missing := missingFields(map[string]string{
			"subscription_id":         candidate.SubscriptionID,
			"resource_group":          candidate.ResourceGroup,
			"traffic_manager_profile": candidate.TrafficManagerProfile,
			"name":                    candidate.Name,
		})
		if len(missing) > 0 {
			return "", missing, true
		}
		return fmt.Sprintf(template, candidate.SubscriptionID, value(candidate.ResourceGroup), value(candidate.TrafficManagerProfile), value(candidate.Name)), nil, true
	default:
		return "", nil, false
	}
}

func missingFields(required map[string]string) []string {
	missing := make([]string, 0, len(required))
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, field)
		}
	}
	sort.Strings(missing)
	return missing
}

type AzureClient struct {
	HTTPClient *http.Client
	Token      string
	BaseURL    string
}

type locationResponse struct {
	Value []struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	} `json:"value"`
}

type providerResponse struct {
	RegistrationState string `json:"registrationState"`
}

type azureAPIError struct {
	StatusCode int
	Status     string
}

func (e *azureAPIError) Error() string {
	return fmt.Sprintf("azure api error: %s", e.Status)
}

type usageResponse struct {
	Value []struct {
		Name struct {
			Value string `json:"value"`
		} `json:"name"`
		CurrentValue float64 `json:"currentValue"`
		Limit        float64 `json:"limit"`
	} `json:"value"`
}

type sqlLocationCapabilities struct {
	Name                    string                     `json:"name"`
	Status                  string                     `json:"status"`
	Reason                  string                     `json:"reason"`
	SupportedServerVersions []sqlServerVersionResponse `json:"supportedServerVersions"`
}

type sqlServerVersionResponse struct {
	Name              string                     `json:"name"`
	Status            string                     `json:"status"`
	Reason            string                     `json:"reason"`
	SupportedEditions []sqlEditionCapabilityItem `json:"supportedEditions"`
}

type sqlEditionCapabilityItem struct {
	Name                            string                              `json:"name"`
	Status                          string                              `json:"status"`
	Reason                          string                              `json:"reason"`
	SupportedServiceLevelObjectives []sqlServiceObjectiveCapabilityItem `json:"supportedServiceLevelObjectives"`
}

type sqlServiceObjectiveCapabilityItem struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason"`
	Sku    struct {
		Name string `json:"name"`
	} `json:"sku"`
}

type keyVaultManagementResponse struct {
	Properties struct {
		VaultURI string `json:"vaultUri"`
	} `json:"properties"`
}

type armResourceLocationResponse struct {
	Location string `json:"location"`
}

func NewAzureClient(token string) *AzureClient {
	return &AzureClient{
		HTTPClient: &http.Client{Timeout: 20 * time.Second},
		Token:      token,
		BaseURL:    "https://management.azure.com",
	}
}

func (c *AzureClient) baseURL() string {
	if strings.TrimSpace(c.BaseURL) == "" {
		return "https://management.azure.com"
	}
	return strings.TrimRight(c.BaseURL, "/")
}

func (c *AzureClient) doRequest(ctx context.Context, method, path string) (*http.Response, error) {
	return c.doRequestWithBody(ctx, method, path, nil, "")
}

func (c *AzureClient) doRequestWithBody(ctx context.Context, method, path string, body []byte, contentType string) (*http.Response, error) {
	requestURL := c.baseURL() + path
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", firstNonEmptyString(contentType, "application/json"))
	}

	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (c *AzureClient) callJSON(ctx context.Context, method, path string, out any) error {
	res, err := c.doRequest(ctx, method, path)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == 401 || res.StatusCode == 403 {
		return fmt.Errorf("azure unauthorized (check login or token)")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &azureAPIError{StatusCode: res.StatusCode, Status: res.Status}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *AzureClient) ProbePath(ctx context.Context, method, path string) (int, error) {
	return c.ProbePathWithBody(ctx, method, path, nil, "")
}

func (c *AzureClient) ProbePathWithBody(ctx context.Context, method, path string, body []byte, contentType string) (int, error) {
	res, err := c.doRequestWithBody(ctx, method, path, body, contentType)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	if res.StatusCode == 401 || res.StatusCode == 403 {
		return res.StatusCode, fmt.Errorf("azure unauthorized (check login or token)")
	}
	return res.StatusCode, nil
}

func RunChecks(ctx context.Context, candidates []model.Candidate, client *AzureClient, subscriptionID string, threshold string, progress *ui.Progress, tokenResolver func(resource string) (string, error)) ([]model.Finding, error) {
	_ = threshold
	if client == nil || strings.TrimSpace(client.Token) == "" {
		return nil, fmt.Errorf("no azure token available")
	}
	if subscriptionID == "" {
		sub, err := ResolveSubscriptionFromCLI()
		if err != nil {
			return nil, fmt.Errorf("subscription could not be resolved: %w", err)
		}
		subscriptionID = sub
	}

	for i := range candidates {
		if candidates[i].SubscriptionID == "" {
			candidates[i].SubscriptionID = subscriptionID
		}
		hydrateDerivedResourceIDs(&candidates[i])
	}

	findings := []model.Finding{}
	if progress != nil {
		progress.Start("checking subscription locations", 1)
	}
	locs, err := fetchLocations(ctx, client, subscriptionID)
	if err != nil {
		findings = append(findings, model.Finding{Severity: "error", Code: "SUBSCRIPTION_LOCATIONS", Message: fmt.Sprintf("cannot read subscription locations: %v", err)})
		if progress != nil {
			progress.Tick("subscription locations unavailable")
		}
	} else if progress != nil {
		progress.Tick("subscription locations OK")
	}

	providers := map[string]struct{}{}
	sqlServerLookup := buildCandidateLookupByImportID(candidates, "azurerm_mssql_server")
	sqlCapabilityCache := map[string]*sqlLocationCapabilities{}
	if progress != nil {
		progress.Start("evaluating resources", len(candidates))
	}
	for _, candidate := range candidates {
		meta, ok := ResolveNamespace(candidate.ResourceType)
		if !ok {
			findings = append(findings, model.Finding{Severity: "warn", Code: "UNSUPPORTED_RESOURCE_TYPE", Message: fmt.Sprintf("resource type %s is not mapped", candidate.ResourceType), Resource: candidate.Address})
			if progress != nil {
				progress.Tick(fmt.Sprintf("skipped unsupported type for %s (%s)", candidate.Address, candidate.ResourceType))
			}
			continue
		}
		if progress != nil {
			progress.Message(fmt.Sprintf("checking %s (%s)", candidate.Address, candidate.ResourceType))
		}
		candidate.Namespace = meta.Namespace
		if candidate.Location == "" {
			if !requiresExplicitLocation(candidate.ResourceType) {
				// Some resources inherit placement from a parent resource and do not expose a location field.
			} else {
				findings = append(findings, model.Finding{Severity: "warn", Code: "MISSING_LOCATION", Message: "resource location is missing", Resource: candidate.Address})
			}
		} else if locs != nil && !isLocationAvailable(locs, candidate.Location) {
			findings = append(findings, model.Finding{Severity: "error", Code: "INVALID_LOCATION", Message: fmt.Sprintf("%s not available in subscription", candidate.Location), Resource: candidate.Address})
		}
		providers[meta.Namespace] = struct{}{}

		if candidate.Action == "create" || candidate.Action == "update" || candidate.Action == "replace" {
			findings = append(findings, runSQLCapabilityCheck(ctx, client, candidate, subscriptionID, locs, sqlServerLookup, sqlCapabilityCache)...)
			if candidate.ResourceType == "azurerm_key_vault_secret" {
				findings = append(findings, runKeyVaultSecretAccessCheck(ctx, client, candidate, tokenResolver)...)
			} else {
				findings = append(findings, runExistenceCheck(ctx, client, candidate)...)
				findings = append(findings, runQuotaCheck(ctx, client, meta, candidate, locs)...)
			}
		}
		if progress != nil {
			progress.Tick(fmt.Sprintf("checked %s", candidate.Address))
		}
	}
	if progress != nil {
		progress.Done("resource checks complete")
		progress.Start("checking provider registrations", len(providers))
	}

	namespaces := make([]string, 0, len(providers))
	for ns := range providers {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)

	for _, ns := range namespaces {
		registered, err := isProviderRegistered(ctx, client, subscriptionID, ns)
		if err != nil {
			findings = append(findings, model.Finding{Severity: "error", Code: "PROVIDER_QUERY_FAILED", Message: fmt.Sprintf("provider %s registration check failed: %v", ns, err)})
			if progress != nil {
				progress.Tick(fmt.Sprintf("provider %s query failed", ns))
			}
			continue
		}
		if !registered {
			findings = append(findings, model.Finding{Severity: "error", Code: "PROVIDER_NOT_REGISTERED", Message: fmt.Sprintf("provider %s is not registered", ns)})
			if progress != nil {
				progress.Tick(fmt.Sprintf("provider %s not registered", ns))
			}
			continue
		}
		if progress != nil {
			progress.Tick(fmt.Sprintf("checked provider %s", ns))
		}
	}
	if progress != nil {
		progress.Done("provider checks complete")
	}

	return findings, nil
}

func requiresExplicitLocation(resourceType string) bool {
	switch resourceType {
	case "azurerm_subnet",
		"azurerm_cdn_frontdoor_profile",
		"azurerm_cdn_frontdoor_endpoint",
		"azurerm_cdn_frontdoor_origin_group",
		"azurerm_cdn_frontdoor_origin",
		"azurerm_cdn_frontdoor_route",
		"azurerm_traffic_manager_profile",
		"azurerm_traffic_manager_azure_endpoint",
		"azurerm_mssql_database",
		"azurerm_key_vault_secret":
		return false
	default:
		return true
	}
}

func runSQLCapabilityCheck(ctx context.Context, client *AzureClient, candidate model.Candidate, subscriptionID string, locations *locationCatalog, serverLookup map[string]model.Candidate, cache map[string]*sqlLocationCapabilities) []model.Finding {
	switch candidate.ResourceType {
	case "azurerm_mssql_server":
		location := strings.TrimSpace(candidate.Location)
		if location == "" {
			return nil
		}
		if locations != nil && !isLocationAvailable(locations, location) {
			return nil
		}
		return evaluateSQLLocationCapability(ctx, client, candidate, subscriptionID, locations, location, "", cache)
	case "azurerm_mssql_database":
		location, err := resolveSQLDatabaseLocation(ctx, client, candidate, serverLookup)
		if err != nil {
			return []model.Finding{{
				Severity: "warn",
				Code:     "SQL_LOCATION_UNRESOLVED",
				Message:  fmt.Sprintf("unable to resolve SQL database location before capability check: %v", err),
				Resource: candidate.Address,
			}}
		}
		if locations != nil && !isLocationAvailable(locations, location) {
			return nil
		}
		return evaluateSQLLocationCapability(ctx, client, candidate, subscriptionID, locations, location, strings.TrimSpace(candidate.Sku), cache)
	default:
		return nil
	}
}

func evaluateSQLLocationCapability(ctx context.Context, client *AzureClient, candidate model.Candidate, subscriptionID string, locations *locationCatalog, location, sku string, cache map[string]*sqlLocationCapabilities) []model.Finding {
	capabilities, err := fetchSQLCapabilities(ctx, client, subscriptionID, locations, location, cache)
	if err != nil {
		return []model.Finding{{
			Severity: "warn",
			Code:     "SQL_CAPABILITY_QUERY_FAILED",
			Message:  fmt.Sprintf("SQL capability check unavailable for %s in %s: %v", candidate.ResourceType, displayLocation(locations, location), err),
			Resource: candidate.Address,
		}}
	}

	resolvedLocation := displayLocation(locations, firstNonEmptyString(capabilities.Name, location))
	if !isCapabilityProvisionable(capabilities.Status) {
		return []model.Finding{{
			Severity: "error",
			Code:     "SQL_PROVISIONING_RESTRICTED",
			Message:  fmt.Sprintf("Microsoft.Sql provisioning is restricted in %s%s; choose another region or satisfy the stated provider restriction", resolvedLocation, formatCapabilityReason(capabilities.Reason)),
			Resource: candidate.Address,
		}}
	}

	if strings.TrimSpace(sku) == "" {
		return nil
	}
	available, reason, found := sqlServiceObjectiveAvailability(capabilities, sku)
	if found && available {
		return nil
	}

	message := fmt.Sprintf("SQL SKU %s is not available in %s for this subscription; choose a supported SKU/region combination", sku, resolvedLocation)
	if found && strings.TrimSpace(reason) != "" {
		message += fmt.Sprintf(" (%s)", reason)
	}
	return []model.Finding{{
		Severity: "error",
		Code:     "SQL_SKU_UNAVAILABLE",
		Message:  message,
		Resource: candidate.Address,
	}}
}

func resolveSQLDatabaseLocation(ctx context.Context, client *AzureClient, candidate model.Candidate, serverLookup map[string]model.Candidate) (string, error) {
	if location := strings.TrimSpace(candidate.Location); location != "" {
		return location, nil
	}

	serverID := strings.TrimSpace(candidate.ServerID)
	if serverID == "" {
		return "", fmt.Errorf("server_id is missing")
	}

	if serverCandidate, ok := serverLookup[normalizeResourceID(serverID)]; ok {
		if location := strings.TrimSpace(serverCandidate.Location); location != "" {
			return location, nil
		}
	}

	response := &armResourceLocationResponse{}
	path := fmt.Sprintf("%s?api-version=%s", serverID, sqlServerAPIVersion)
	if err := client.callJSON(ctx, "GET", path, response); err != nil {
		return "", fmt.Errorf("cannot read SQL server location from %s: %v", serverID, err)
	}
	if strings.TrimSpace(response.Location) == "" {
		return "", fmt.Errorf("SQL server %s returned an empty location", serverID)
	}
	return response.Location, nil
}

func fetchSQLCapabilities(ctx context.Context, client *AzureClient, subscriptionID string, locations *locationCatalog, location string, cache map[string]*sqlLocationCapabilities) (*sqlLocationCapabilities, error) {
	apiLocation := sqlLocationValue(locations, location)
	cacheKey := subscriptionID + "|" + strings.ToLower(apiLocation)
	if cached, ok := cache[cacheKey]; ok {
		return cached, nil
	}

	response := &sqlLocationCapabilities{}
	path := fmt.Sprintf("/subscriptions/%s/providers/Microsoft.Sql/locations/%s/capabilities?api-version=%s", subscriptionID, url.PathEscape(apiLocation), sqlCapabilitiesAPIVersion)
	if err := client.callJSON(ctx, "GET", path, response); err != nil {
		return nil, err
	}
	cache[cacheKey] = response
	return response, nil
}

func sqlServiceObjectiveAvailability(capabilities *sqlLocationCapabilities, sku string) (bool, string, bool) {
	sku = strings.TrimSpace(sku)
	if capabilities == nil || sku == "" {
		return false, "", false
	}

	found := false
	reason := ""
	for _, version := range capabilities.SupportedServerVersions {
		for _, edition := range version.SupportedEditions {
			for _, objective := range edition.SupportedServiceLevelObjectives {
				if !strings.EqualFold(strings.TrimSpace(objective.Name), sku) && !strings.EqualFold(strings.TrimSpace(objective.Sku.Name), sku) {
					continue
				}
				found = true
				if isCapabilityProvisionable(objective.Status) {
					return true, "", true
				}
				if reason == "" {
					reason = firstNonEmptyString(objective.Reason, edition.Reason, version.Reason)
				}
			}
		}
	}
	return false, reason, found
}

func isCapabilityProvisionable(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "available", "default":
		return true
	default:
		return false
	}
}

func formatCapabilityReason(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return ""
	}
	return ": " + strings.TrimSpace(reason)
}

func runKeyVaultSecretAccessCheck(ctx context.Context, client *AzureClient, candidate model.Candidate, tokenResolver func(resource string) (string, error)) []model.Finding {
	missing := missingFields(map[string]string{
		"key_vault_id": candidate.KeyVaultID,
		"name":         candidate.Name,
	})
	if len(missing) > 0 {
		return []model.Finding{{
			Severity: "warn",
			Code:     "KEY_VAULT_SECRET_CHECK_FAILED",
			Message:  fmt.Sprintf("key vault secret access check skipped; missing fields: %s", strings.Join(missing, ", ")),
			Resource: candidate.Address,
			Detail: map[string]any{
				"missing_fields": missing,
			},
		}}
	}

	vaultResponse := &keyVaultManagementResponse{}
	managementPath := fmt.Sprintf("%s?api-version=%s", candidate.KeyVaultID, keyVaultManagementAPIVersion)
	if err := client.callJSON(ctx, "GET", managementPath, vaultResponse); err != nil {
		return []model.Finding{{
			Severity: "warn",
			Code:     "KEY_VAULT_SECRET_CHECK_FAILED",
			Message:  fmt.Sprintf("cannot read Key Vault metadata for %s: %v", candidate.KeyVaultID, err),
			Resource: candidate.Address,
		}}
	}

	vaultURI := strings.TrimSpace(vaultResponse.Properties.VaultURI)
	if vaultURI == "" {
		return []model.Finding{{
			Severity: "warn",
			Code:     "KEY_VAULT_SECRET_CHECK_FAILED",
			Message:  fmt.Sprintf("Key Vault %s did not return a vaultUri for secret access checks", candidate.KeyVaultID),
			Resource: candidate.Address,
		}}
	}

	if tokenResolver == nil {
		return []model.Finding{{
			Severity: "warn",
			Code:     "KEY_VAULT_SECRET_CHECK_FAILED",
			Message:  "Key Vault secret access check requires a vault-scoped token resolver",
			Resource: candidate.Address,
		}}
	}

	vaultToken, err := tokenResolver(KeyVaultAudience)
	if err != nil {
		return []model.Finding{{
			Severity: "warn",
			Code:     "KEY_VAULT_SECRET_CHECK_FAILED",
			Message:  fmt.Sprintf("cannot resolve Key Vault data-plane token for %s: %v", vaultURI, err),
			Resource: candidate.Address,
		}}
	}

	vaultClient := NewAzureClient(vaultToken)
	vaultClient.BaseURL = strings.TrimRight(vaultURI, "/")
	vaultClient.HTTPClient = client.HTTPClient

	findings := []model.Finding{}
	secretPath := fmt.Sprintf("/secrets/%s?api-version=%s", url.PathEscape(candidate.Name), keyVaultSecretAPIVersion)

	readStatus, readErr := vaultClient.ProbePath(ctx, "GET", secretPath)
	switch {
	case readStatus == http.StatusForbidden:
		findings = append(findings, model.Finding{
			Severity: "error",
			Code:     "KEY_VAULT_SECRET_ACCESS_DENIED",
			Message:  fmt.Sprintf("Key Vault secret read access denied for %s; grant secrets/get or an equivalent Key Vault data-plane role/access policy", vaultURI),
			Resource: candidate.Address,
			Detail: map[string]any{
				"permission": "secrets/get",
				"vault_uri":  vaultURI,
			},
		})
	case readErr != nil:
		findings = append(findings, model.Finding{
			Severity: "warn",
			Code:     "KEY_VAULT_SECRET_CHECK_FAILED",
			Message:  fmt.Sprintf("Key Vault secret read probe failed for %s: %v", vaultURI, readErr),
			Resource: candidate.Address,
		})
	case readStatus == http.StatusNotFound:
		// A 404 confirms the caller can read the vault but the secret does not exist yet.
	case readStatus >= 200 && readStatus < 300:
		// Read access confirmed.
	default:
		findings = append(findings, model.Finding{
			Severity: "warn",
			Code:     "KEY_VAULT_SECRET_CHECK_FAILED",
			Message:  fmt.Sprintf("Key Vault secret read probe returned unexpected status %d for %s", readStatus, vaultURI),
			Resource: candidate.Address,
			Detail: map[string]any{
				"status_code": readStatus,
				"permission":  "secrets/get",
			},
		})
	}

	writeStatus, writeErr := vaultClient.ProbePathWithBody(ctx, "PUT", secretPath, []byte(`{}`), "application/json")
	switch {
	case writeStatus == http.StatusForbidden:
		findings = append(findings, model.Finding{
			Severity: "error",
			Code:     "KEY_VAULT_SECRET_ACCESS_DENIED",
			Message:  fmt.Sprintf("Key Vault secret write access denied for %s; grant secrets/set or an equivalent Key Vault data-plane role/access policy", vaultURI),
			Resource: candidate.Address,
			Detail: map[string]any{
				"permission": "secrets/set",
				"vault_uri":  vaultURI,
			},
		})
	case writeErr != nil:
		findings = append(findings, model.Finding{
			Severity: "warn",
			Code:     "KEY_VAULT_SECRET_CHECK_FAILED",
			Message:  fmt.Sprintf("Key Vault secret write probe failed for %s: %v", vaultURI, writeErr),
			Resource: candidate.Address,
		})
	case writeStatus == http.StatusBadRequest:
		// A 400 from an intentionally invalid payload proves authorization succeeded.
	case writeStatus >= 200 && writeStatus < 300:
		findings = append(findings, model.Finding{
			Severity: "warn",
			Code:     "KEY_VAULT_SECRET_CHECK_FAILED",
			Message:  fmt.Sprintf("Key Vault secret write probe unexpectedly succeeded for %s", vaultURI),
			Resource: candidate.Address,
			Detail: map[string]any{
				"status_code": writeStatus,
				"permission":  "secrets/set",
			},
		})
	default:
		findings = append(findings, model.Finding{
			Severity: "warn",
			Code:     "KEY_VAULT_SECRET_CHECK_FAILED",
			Message:  fmt.Sprintf("Key Vault secret write probe returned unexpected status %d for %s", writeStatus, vaultURI),
			Resource: candidate.Address,
			Detail: map[string]any{
				"status_code": writeStatus,
				"permission":  "secrets/set",
			},
		})
	}

	return findings
}

func runQuotaCheck(ctx context.Context, client *AzureClient, meta ResourceMeta, candidate model.Candidate, locations *locationCatalog) []model.Finding {
	if meta.QuotaPath == "" || candidate.Location == "" {
		return nil
	}

	q := fmt.Sprintf(meta.QuotaPath, candidate.SubscriptionID, url.PathEscape(quotaLocationValue(meta, locations, candidate.Location)))
	usage, err := fetchUsages(ctx, client, q)
	if err != nil {
		if isMicrosoftWebQuotaUnsupported(meta, err) {
			return []model.Finding{{
				Severity: "warn",
				Code:     "QUOTA_CHECK_UNSUPPORTED",
				Message:  fmt.Sprintf("quota check is not supported for %s via Microsoft.Web usages API", candidate.ResourceType),
				Resource: candidate.Address,
			}}
		}
		return []model.Finding{{
			Severity: "warn",
			Code:     "QUOTA_UNKNOWN",
			Message:  fmt.Sprintf("quota check unavailable for %s: %v", candidate.ResourceType, err),
			Resource: candidate.Address,
		}}
	}
	if exceeded, metric := isQuotaExceeded(usage, meta.QuotaChecks); exceeded {
		return []model.Finding{{
			Severity: "error",
			Code:     "QUOTA_EXCEEDED",
			Message:  fmt.Sprintf("quota limit reached (%s)", metric),
			Resource: candidate.Address,
		}}
	}
	return nil
}

func isMicrosoftWebQuotaUnsupported(meta ResourceMeta, err error) bool {
	if !strings.EqualFold(meta.Namespace, "Microsoft.Web") {
		return false
	}
	var apiErr *azureAPIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest
}

func runExistenceCheck(ctx context.Context, client *AzureClient, candidate model.Candidate) []model.Finding {
	path, missing, ok := BuildExistsPath(candidate)
	switch {
	case !ok:
		return []model.Finding{{
			Severity: "warn",
			Code:     "RESOURCE_EXISTS_CHECK_UNSUPPORTED",
			Message:  fmt.Sprintf("resource existence check is not supported for %s", candidate.ResourceType),
			Resource: candidate.Address,
			Detail: map[string]any{
				"resource_type": candidate.ResourceType,
			},
		}}
	case len(missing) > 0:
		return []model.Finding{{
			Severity: "warn",
			Code:     "RESOURCE_EXISTS_CHECK_INCOMPLETE",
			Message:  fmt.Sprintf("resource existence check skipped; missing fields: %s", strings.Join(missing, ", ")),
			Resource: candidate.Address,
			Detail: map[string]any{
				"missing_fields": missing,
			},
		}}
	}

	status, err := client.ProbePath(ctx, "GET", path)
	switch {
	case err != nil:
		return []model.Finding{{
			Severity: "error",
			Code:     "RESOURCE_EXISTS_CHECK_FAILED",
			Message:  fmt.Sprintf("resource existence probe failed: %v", err),
			Resource: candidate.Address,
		}}
	case status == http.StatusNotFound:
		return nil
	case status >= 200 && status < 300:
		return []model.Finding{{Severity: "warn", Code: "RESOURCE_EXISTS", Message: "resource already exists", Resource: candidate.Address}}
	default:
		return []model.Finding{{
			Severity: "error",
			Code:     "RESOURCE_EXISTS_CHECK_FAILED",
			Message:  fmt.Sprintf("resource existence probe returned unexpected status: %d", status),
			Resource: candidate.Address,
			Detail: map[string]any{
				"status_code": status,
			},
		}}
	}
}

func isLocationAvailable(locations *locationCatalog, location string) bool {
	if locations == nil {
		return false
	}
	_, ok := locations.known[strings.ToLower(strings.TrimSpace(location))]
	return ok
}

func fetchLocations(ctx context.Context, client *AzureClient, subscription string) (*locationCatalog, error) {
	path := fmt.Sprintf("/subscriptions/%s/locations?api-version=2020-01-01", subscription)
	resp := &locationResponse{}
	if err := client.callJSON(ctx, "GET", path, resp); err != nil {
		return nil, err
	}
	known := map[string]struct{}{}
	canonical := map[string]string{}
	apiNames := map[string]string{}
	for _, item := range resp.Value {
		nameKey := strings.ToLower(strings.TrimSpace(item.Name))
		displayKey := strings.ToLower(strings.TrimSpace(item.DisplayName))
		known[nameKey] = struct{}{}
		known[displayKey] = struct{}{}
		apiNames[nameKey] = item.Name
		if displayKey != "" {
			apiNames[displayKey] = item.Name
		}
		if strings.TrimSpace(item.DisplayName) != "" {
			canonical[nameKey] = item.DisplayName
			canonical[displayKey] = item.DisplayName
		}
	}
	return &locationCatalog{
		known:           known,
		canonicalByName: canonical,
		apiNameByName:   apiNames,
	}, nil
}

func isProviderRegistered(ctx context.Context, client *AzureClient, subscription, namespace string) (bool, error) {
	path := fmt.Sprintf("/subscriptions/%s/providers/%s?api-version=2021-04-01", subscription, namespace)
	resp := &providerResponse{}
	if err := client.callJSON(ctx, "GET", path, resp); err != nil {
		return false, err
	}
	return strings.EqualFold(resp.RegistrationState, "Registered"), nil
}

func fetchUsages(ctx context.Context, client *AzureClient, path string) ([]usageResponseItem, error) {
	type wrapped struct {
		Value []usageResponseItem `json:"value"`
	}
	resp := &wrapped{}
	if err := client.callJSON(ctx, "GET", path, resp); err != nil {
		return nil, err
	}
	return resp.Value, nil
}

type usageResponseItem struct {
	Name struct {
		Value string `json:"value"`
	} `json:"name"`
	CurrentValue float64 `json:"currentValue"`
	Limit        float64 `json:"limit"`
}

func isQuotaExceeded(items []usageResponseItem, checks []string) (bool, string) {
	if len(checks) == 0 {
		return false, ""
	}
	lookup := map[string]usageResponseItem{}
	for _, it := range items {
		lookup[strings.ToLower(it.Name.Value)] = it
	}
	for _, check := range checks {
		checkKey := strings.ToLower(strings.TrimSpace(check))
		for metric, it := range lookup {
			if metric != checkKey && !strings.Contains(metric, checkKey) {
				continue
			}
			if it.Limit > 0 && it.CurrentValue >= it.Limit {
				return true, it.Name.Value
			}
		}
	}
	return false, ""
}

func quotaLocationValue(meta ResourceMeta, locations *locationCatalog, location string) string {
	trimmed := strings.TrimSpace(location)
	if meta.Namespace == "Microsoft.Web" {
		key := strings.ToLower(trimmed)
		if locations != nil {
			if display := strings.TrimSpace(locations.canonicalByName[key]); display != "" {
				return display
			}
		}
		return trimmed
	}
	return strings.ToLower(trimmed)
}

func sqlLocationValue(locations *locationCatalog, location string) string {
	trimmed := strings.TrimSpace(location)
	if locations != nil {
		if apiName := strings.TrimSpace(locations.apiNameByName[strings.ToLower(trimmed)]); apiName != "" {
			return apiName
		}
	}
	return strings.ToLower(strings.ReplaceAll(trimmed, " ", ""))
}

func displayLocation(locations *locationCatalog, location string) string {
	trimmed := strings.TrimSpace(location)
	if trimmed == "" {
		return location
	}
	if locations != nil {
		if display := strings.TrimSpace(locations.canonicalByName[strings.ToLower(trimmed)]); display != "" {
			return display
		}
	}
	return trimmed
}

func buildCandidateLookupByImportID(candidates []model.Candidate, resourceType string) map[string]model.Candidate {
	lookup := map[string]model.Candidate{}
	for _, candidate := range candidates {
		if candidate.ResourceType != resourceType {
			continue
		}
		id, missing, ok := BuildImportID(candidate)
		if !ok || len(missing) > 0 {
			continue
		}
		lookup[normalizeResourceID(id)] = candidate
	}
	return lookup
}

func normalizeResourceID(resourceID string) string {
	return strings.ToLower(strings.TrimSpace(resourceID))
}

func hydrateDerivedResourceIDs(candidate *model.Candidate) {
	if candidate == nil || strings.TrimSpace(candidate.SubscriptionID) == "" {
		return
	}
	if strings.Contains(candidate.ServerID, unknownSubscriptionID) {
		candidate.ServerID = strings.ReplaceAll(candidate.ServerID, unknownSubscriptionID, candidate.SubscriptionID)
	}
	if strings.Contains(candidate.KeyVaultID, unknownSubscriptionID) {
		candidate.KeyVaultID = strings.ReplaceAll(candidate.KeyVaultID, unknownSubscriptionID, candidate.SubscriptionID)
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func ResolveSubscriptionFromCLI() (string, error) {
	cmd := exec.Command("az", "account", "show", "--query", "id", "-o", "tsv")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	subscriptionID := strings.TrimSpace(string(out))
	if subscriptionID == "" {
		return "", fmt.Errorf("azure cli returned empty subscription id")
	}
	return subscriptionID, nil
}
