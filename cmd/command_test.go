package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

// fakeAPI is an httptest server that routes on path substring and returns canned
// JSON, so the real command path (cobra -> SDK -> HTTP) runs end-to-end with no
// network. This is the command-level test seam: point --base-url at it.
func fakeAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		var body string
		switch {
		case strings.Contains(p, "/billing/balance"):
			body = `{"balance_display":"$5.00","balance_microdollars":5000000}`
		case strings.Contains(p, "/phones/available"):
			body = `{"android_count":1,"iphone_count":0,"phones":[
				{"phone_id":"p1","phone_type":"android","model_name":"Pixel 8","status":"active"}]}`
		case strings.Contains(p, "/api-keys"):
			body = `{"api_keys":[
				{"id":"k1","name":"ci","key_preview":"axl_ci…","created_at":"2026-07-14T00:00:00Z"}],
				"total":1,"limit":50,"offset":0}`
		case strings.HasSuffix(p, "/runs"):
			body = `{"runs":[
				{"id":"r1","status":"completed","trigger":"manual","workflow_id":"w1","success":true}],
				"total":1,"limit":20,"offset":0}`
		case strings.HasSuffix(p, "/workflows"):
			body = `{"workflows":[
				{"workflow":{"id":"wf1","name":"demo","platform":"android","status":"active"},
				 "stats":{"total_runs":3,"success_rate":0.66}}],
				"total":1,"limit":20,"offset":0}`
		case strings.Contains(p, "/usage/metrics"):
			body = `{"period_start":"2026-07-01T00:00:00Z","period_end":"2026-07-14T00:00:00Z",
				"compute_minutes":{"total_minutes":12.5,"change":0},
				"cost_by_product":{"inference":1.2,"sessions":3.4,"other":0}}`
		default:
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// run executes the root command with args against the fake API, capturing stdout.
func run(t *testing.T, srv *httptest.Server, args ...string) (string, error) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_API_KEY", "axl_test")
	t.Setenv("AXILIO_BASE_URL", srv.URL)

	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := Root()
	root.SetArgs(args)
	err := root.Execute()

	w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String(), err
}

func TestStatusJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var got map[string]any
	if e := json.Unmarshal([]byte(out), &got); e != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", e, out)
	}
	if got["status"] != "ok" || got["balance"] != "$5.00" {
		t.Fatalf("unexpected status payload: %v", got)
	}
}

func TestPhonesListJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "phones", "list")
	if err != nil {
		t.Fatalf("phones list: %v", err)
	}
	if !strings.Contains(out, `"phone_id": "p1"`) {
		t.Fatalf("expected the fake phone in output:\n%s", out)
	}
}

func TestRunsListJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "runs", "list")
	if err != nil {
		t.Fatalf("runs list: %v", err)
	}
	if !strings.Contains(out, `"id": "r1"`) {
		t.Fatalf("expected the fake run in output:\n%s", out)
	}
}

func TestWorkflowsListJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "workflows", "list")
	if err != nil {
		t.Fatalf("workflows list: %v", err)
	}
	if !strings.Contains(out, `"id": "wf1"`) {
		t.Fatalf("expected the fake workflow in output:\n%s", out)
	}
}

func TestUsageSummaryJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "usage", "summary")
	if err != nil {
		t.Fatalf("usage summary: %v", err)
	}
	if !strings.Contains(out, `"total_minutes": 12.5`) {
		t.Fatalf("expected compute minutes in output:\n%s", out)
	}
}

// A rejected key must surface as the Auth exit code, not a generic error.
func TestAuthFailureExitCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"invalid API key"}`, http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	_, err := run(t, srv, "status")
	if err == nil {
		t.Fatal("expected an error from a 401")
	}
	if got := exit.Classify(err); got != exit.Auth {
		t.Fatalf("exit code = %d, want %d (auth)", got, exit.Auth)
	}
}

// No credentials at all must classify as Auth, without touching the network.
func TestNoCredentialsExitCode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_API_KEY", "")
	t.Setenv("AXILIO_BASE_URL", "")

	root := Root()
	root.SetArgs([]string{"status"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error with no credentials")
	}
	if got := exit.Classify(err); got != exit.Auth {
		t.Fatalf("exit code = %d, want %d (auth)", got, exit.Auth)
	}
}
