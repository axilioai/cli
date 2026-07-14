package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

// AXILIO_ORG (and --org / `org use`) must ride on every request as X-Axilio-Org
// so the backend can re-scope an OAuth session to another org (AXI-1280).
func TestOrgHeaderSentFromEnv(t *testing.T) {
	var gotOrg string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("X-Axilio-Org"); v != "" {
			gotOrg = v
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"balance_display":"$0.00","balance_microdollars":0}`)
	}))
	t.Cleanup(srv.Close)

	t.Setenv("AXILIO_ORG", "acme")
	if _, err := run(t, srv, "-o", "json", "status"); err != nil {
		t.Fatalf("status: %v", err)
	}
	if gotOrg != "acme" {
		t.Fatalf("X-Axilio-Org = %q, want acme", gotOrg)
	}
}

// `org` is an OAuth-session capability: with only an API key configured it must
// fail as a usage error, not attempt a call it can't authenticate.
func TestOrgListRequiresOAuth(t *testing.T) {
	srv := fakeAPI(t) // run() configures an API key
	_, err := run(t, srv, "org", "list")
	if err == nil {
		t.Fatal("expected `org list` to fail with only an API key configured")
	}
	if got := exit.Classify(err); got != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage)", got, exit.Usage)
	}
}

func TestIsActiveOrg(t *testing.T) {
	o := orgSummary{ID: "org_123", Slug: "acme", Name: "Acme"}
	cases := []struct {
		active string
		want   bool
	}{
		{"", false}, {"acme", true}, {"org_123", true}, {"other", false},
	}
	for _, c := range cases {
		if got := isActiveOrg(o, c.active); got != c.want {
			t.Fatalf("isActiveOrg(%q) = %v, want %v", c.active, got, c.want)
		}
	}
}

func TestOrgDisplay(t *testing.T) {
	if got := orgDisplay(""); got != "(session default)" {
		t.Fatalf("orgDisplay(\"\") = %q, want session-default label", got)
	}
	if got := orgDisplay("acme"); got != "acme" {
		t.Fatalf("orgDisplay(%q) = %q, want passthrough", "acme", got)
	}
}
