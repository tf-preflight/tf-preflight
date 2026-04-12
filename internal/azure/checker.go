package azure

import (
	"context"
	"encoding/json"
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
		QuotaPath:   "/subscriptions/%s/providers/Microsoft.Web/locations/%s/usages?api-version=2022-03-01",
		QuotaChecks: []string{"sites", "total sites", "serverfams"},
	},
	"azurerm_windows_web_app": {
		Namespace:   "Microsoft.Web",
		ExistsPath:  "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s?api-version=2023-01-01",
		ImportPath:  "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s",
		QuotaPath:   "/subscriptions/%s/providers/Microsoft.Web/locations/%s/usages?api-version=2022-03-01",
		QuotaChecks: []string{"sites", "total sites", "app service plans"},
	},
	"azurerm_linux_web_app": {
		Namespace:   "Microsoft.Web",
		ExistsPath:  "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s?api-version=2023-01-01",
		ImportPath:  "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s",
		QuotaPath:   "/subscriptions/%s/providers/Microsoft.Web/locations/%s/usages?api-version=2022-03-01",
		QuotaChecks: []string{"sites", "total sites", "app service plans"},
	},
	"azurerm_traffic_manager_profile": {
		Namespace:   "Microsoft.Network",
		ExistsPath:  "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/trafficManagerProfiles/%s?api-version=2023-04-01",
		ImportPath:  "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/trafficManagerProfiles/%s",
		QuotaPath:   "/subscriptions/%s/providers/Microsoft.Network/locations/%s/usages?api-version=2022-01-01",
		QuotaChecks: []string{"traffic manager profiles"},
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
		"azurerm_windows_web_app",
		"azurerm_linux_web_app",
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

type usageResponse struct {
	Value []struct {
		Name struct {
			Value string `json:"value"`
		} `json:"name"`
		CurrentValue float64 `json:"currentValue"`
		Limit        float64 `json:"limit"`
	} `json:"value"`
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
	requestURL := c.baseURL() + path
	req, err := http.NewRequestWithContext(ctx, method, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))
	req.Header.Set("Accept", "application/json")

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
		return fmt.Errorf("azure api error: %s", res.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *AzureClient) ProbePath(ctx context.Context, method, path string) (int, error) {
	res, err := c.doRequest(ctx, method, path)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	if res.StatusCode == 401 || res.StatusCode == 403 {
		return res.StatusCode, fmt.Errorf("azure unauthorized (check login or token)")
	}
	return res.StatusCode, nil
}

func RunChecks(ctx context.Context, candidates []model.Candidate, client *AzureClient, subscriptionID string, threshold string, progress *ui.Progress) ([]model.Finding, error) {
	_ = threshold
	if client == nil || client.Token == "" {
		return nil, fmt.Errorf("no azure token available")
	}
	if subscriptionID == "" {
		sub, err := resolveSubscriptionFromCLI()
		if err != nil {
			return nil, fmt.Errorf("subscription could not be resolved: %w", err)
		}
		subscriptionID = sub
	}

	for i := range candidates {
		if candidates[i].SubscriptionID == "" {
			candidates[i].SubscriptionID = subscriptionID
		}
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
	if progress != nil {
		progress.Start("evaluating resources", len(candidates))
	}
	for _, candidate := range candidates {
		meta, ok := ResolveNamespace(candidate.ResourceType)
		if !ok {
			findings = append(findings, model.Finding{Severity: "warn", Code: "UNSUPPORTED_RESOURCE_TYPE", Message: fmt.Sprintf("resource type %s is not mapped", candidate.ResourceType), Resource: candidate.Address})
			if progress != nil {
				progress.Tick(fmt.Sprintf("%s: type %s unsupported", candidate.Address, candidate.ResourceType))
			}
			continue
		}
		if progress != nil {
			progress.Message(fmt.Sprintf("validating %s (%s)", candidate.Address, candidate.ResourceType))
		}
		candidate.Namespace = meta.Namespace
		if candidate.Location == "" {
			findings = append(findings, model.Finding{Severity: "warn", Code: "MISSING_LOCATION", Message: "resource location is missing", Resource: candidate.Address})
		} else if locs != nil && !isLocationAvailable(locs, candidate.Location) {
			findings = append(findings, model.Finding{Severity: "error", Code: "INVALID_LOCATION", Message: fmt.Sprintf("%s not available in subscription", candidate.Location), Resource: candidate.Address})
		}
		providers[meta.Namespace] = struct{}{}

		if candidate.Action == "create" || candidate.Action == "update" || candidate.Action == "replace" {
			if path, missing, ok := BuildExistsPath(candidate); ok && len(missing) == 0 {
				if status, err := client.ProbePath(ctx, "GET", path); err == nil && status >= 200 && status < 300 {
					findings = append(findings, model.Finding{Severity: "warn", Code: "RESOURCE_EXISTS", Message: "resource already exists", Resource: candidate.Address})
				}
			}
			if meta.QuotaPath != "" && candidate.Location != "" {
				q := fmt.Sprintf(meta.QuotaPath, candidate.SubscriptionID, url.PathEscape(strings.ToLower(candidate.Location)))
				usage, err := fetchUsages(ctx, client, q)
				if err != nil {
					findings = append(findings, model.Finding{Severity: "warn", Code: "QUOTA_UNKNOWN", Message: fmt.Sprintf("quota check unavailable for %s: %v", candidate.ResourceType, err), Resource: candidate.Address})
				} else if exceeded, metric := isQuotaExceeded(usage, meta.QuotaChecks); exceeded {
					findings = append(findings, model.Finding{Severity: "error", Code: "QUOTA_EXCEEDED", Message: fmt.Sprintf("quota limit reached (%s)", metric), Resource: candidate.Address})
				}
			}
		}
		if progress != nil {
			progress.Tick(fmt.Sprintf("%s checked", candidate.Address))
		}
	}
	if progress != nil {
		progress.Done("resource checks complete")
		progress.Start("checking provider registrations", len(providers))
	}

	for ns := range providers {
		registered, err := isProviderRegistered(ctx, client, subscriptionID, ns)
		if err != nil {
			findings = append(findings, model.Finding{Severity: "error", Code: "PROVIDER_QUERY_FAILED", Message: fmt.Sprintf("provider %s registration check failed: %v", ns, err)})
			if progress != nil {
				progress.Tick(fmt.Sprintf("provider %s failed", ns))
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
			progress.Tick(fmt.Sprintf("provider %s", ns))
		}
	}
	if progress != nil {
		progress.Done("provider checks complete")
	}

	return findings, nil
}

func isLocationAvailable(locations map[string]struct{}, location string) bool {
	_, ok := locations[strings.ToLower(strings.TrimSpace(location))]
	return ok
}

func fetchLocations(ctx context.Context, client *AzureClient, subscription string) (map[string]struct{}, error) {
	path := fmt.Sprintf("/subscriptions/%s/locations?api-version=2020-01-01", subscription)
	resp := &locationResponse{}
	if err := client.callJSON(ctx, "GET", path, resp); err != nil {
		return nil, err
	}
	known := map[string]struct{}{}
	for _, item := range resp.Value {
		known[strings.ToLower(item.Name)] = struct{}{}
		known[strings.ToLower(item.DisplayName)] = struct{}{}
	}
	return known, nil
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
		it, ok := lookup[strings.ToLower(check)]
		if !ok {
			continue
		}
		if it.Limit > 0 && it.CurrentValue >= it.Limit {
			return true, it.Name.Value
		}
	}
	return false, ""
}

func resolveSubscriptionFromCLI() (string, error) {
	cmd := exec.Command("az", "account", "show", "--query", "id", "-o", "tsv")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
