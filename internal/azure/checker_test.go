package azure

import "testing"

func TestIsLocationAvailable(t *testing.T) {
	locations := map[string]struct{}{
		"west europe": {},
		"westeurope":  {},
	}
	if !isLocationAvailable(locations, "West Europe") {
		t.Fatalf("expected location to be available")
	}
}

func TestIsQuotaExceeded(t *testing.T) {
	items := []usageResponseItem{
		{Name: struct {
			Value string `json:"value"`
		}{Value: "sites"}, CurrentValue: 10, Limit: 10},
	}
	exceeded, metric := isQuotaExceeded(items, []string{"sites"})
	if !exceeded {
		t.Fatalf("expected exceeded")
	}
	if metric != "sites" {
		t.Fatalf("unexpected metric %s", metric)
	}
}
