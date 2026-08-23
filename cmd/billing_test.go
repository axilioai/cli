package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFormatMicrodollars(t *testing.T) {
	cases := []struct {
		md   int64
		want string
	}{
		{0, "$0.00"},
		{12_500_000, "$12.50"},
		{1_000_000, "$1.00"},
		{4_999, "$0.00"}, // below half a cent rounds down
		{5_000, "$0.01"}, // half a cent rounds up
		{-2_500_000, "-$2.50"},
		{10_000, "$0.01"},
	}
	for _, tc := range cases {
		if got := formatMicrodollars(tc.md); got != tc.want {
			t.Errorf("formatMicrodollars(%d) = %q, want %q", tc.md, got, tc.want)
		}
	}
}

// billingAPI fakes the two public billing reads; withPlan=false turns the
// subscription endpoint into the 404 an org without an active plan receives.
func billingAPI(t *testing.T, withPlan bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/billing/balance"):
			_, _ = io.WriteString(w, `{
				"balance_display":"$12.50","balance_microdollars":12500000,
				"included_microdollars":10000000,"purchased_microdollars":2500000,
				"purchased_next_expiry":"2027-08-01T00:00:00Z"}`)
		case strings.HasSuffix(r.URL.Path, "/billing/subscription"):
			if !withPlan {
				http.Error(w, `{"title":"Not Found","status":404,"detail":"no active subscription"}`, http.StatusNotFound)
				return
			}
			_, _ = io.WriteString(w, `{
				"id":"sub_1","plan_id":"pro","plan_name":"Pro","status":"active",
				"billing_cycle":"monthly","monthly_price":20,"yearly_price":200,
				"balance_display":"$12.50","balance_microdollars":12500000,
				"included_balance_display":"$10.00","included_balance_microdollars":10000000,
				"max_concurrent_runs":5,"cancel_at_period_end":false,
				"current_period_start":"2026-08-12T00:00:00Z",
				"current_period_end":"2026-09-12T00:00:00Z",
				"price_per_second_microdollars":100}`)
		default:
			http.Error(w, `{"title":"Not Found","status":404}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// billing balance emits {"balance":…,"plan":…} with the plan slice attached.
func TestBillingBalanceJSON(t *testing.T) {
	srv := billingAPI(t, true)
	out, err := run(t, srv, "-o", "json", "billing", "balance")
	if err != nil {
		t.Fatalf("billing balance: %v", err)
	}
	got := mustJSON(t, out)
	bal, ok := got["balance"].(map[string]any)
	if !ok || bal["balance_display"] != "$12.50" {
		t.Fatalf("unexpected balance payload: %v", got)
	}
	plan, ok := got["plan"].(map[string]any)
	if !ok || plan["plan_name"] != "Pro" || plan["billing_cycle"] != "monthly" {
		t.Fatalf("unexpected plan payload: %v", got)
	}
}

// An org without an active subscription still gets its balance; plan is null.
func TestBillingBalanceJSONWithoutPlan(t *testing.T) {
	srv := billingAPI(t, false)
	out, err := run(t, srv, "-o", "json", "billing", "balance")
	if err != nil {
		t.Fatalf("billing balance (no plan): %v", err)
	}
	got := mustJSON(t, out)
	if got["plan"] != nil {
		t.Fatalf("plan should be null without a subscription: %v", got)
	}
	if bal, ok := got["balance"].(map[string]any); !ok || bal["balance_display"] != "$12.50" {
		t.Fatalf("unexpected balance payload: %v", got)
	}
}

// billing plan passes the subscription object through unmodified.
func TestBillingPlanJSON(t *testing.T) {
	srv := billingAPI(t, true)
	out, err := run(t, srv, "-o", "json", "billing", "plan")
	if err != nil {
		t.Fatalf("billing plan: %v", err)
	}
	got := mustJSON(t, out)
	if got["plan_name"] != "Pro" || got["max_concurrent_runs"] != float64(5) {
		t.Fatalf("unexpected plan payload: %v", got)
	}
}
