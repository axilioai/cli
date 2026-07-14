package cmd

import "testing"

func TestDashboardBaseURL(t *testing.T) {
	t.Setenv("AXILIO_DASHBOARD_URL", "")
	cases := map[string]string{
		"https://api.axilio.ai":         "https://app.axilio.ai",
		"https://staging-api.axilio.ai": "https://staging-app.axilio.ai",
	}
	for in, want := range cases {
		if got := dashboardBaseURL(in); got != want {
			t.Fatalf("dashboardBaseURL(%q) = %q, want %q", in, got, want)
		}
	}

	// The env override wins.
	t.Setenv("AXILIO_DASHBOARD_URL", "https://custom.example")
	if got := dashboardBaseURL("https://api.axilio.ai"); got != "https://custom.example" {
		t.Fatalf("env override not honored: %q", got)
	}
}
