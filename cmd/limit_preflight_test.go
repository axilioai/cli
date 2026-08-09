package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

// noRequestServer returns a server that fails the test if any request lands,
// plus a counter for the assertion message.
func noRequestServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_API_KEY", "axl_test")
	t.Setenv("AXILIO_BASE_URL", srv.URL)
	return srv, &requests
}

// The list commands preflight their pagination flags against the backend's
// bounds (runs 1-500, workflows 1-500, uploads 1-100, offsets >= 0) before
// credentials or any HTTP request, mirroring the runs start --count pattern.
func TestListLimitPreflightsRejectBeforeRequest(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"runs limit zero", []string{"runs", "list", "--limit", "0"}},
		{"runs limit negative", []string{"runs", "list", "--limit", "-5"}},
		{"runs limit above max", []string{"runs", "list", "--limit", "501"}},
		{"workflows limit zero", []string{"workflows", "list", "--limit", "0"}},
		{"workflows limit above max", []string{"workflows", "list", "--limit", "501"}},
		{"uploads limit zero", []string{"uploads", "list", "--limit", "0"}},
		{"uploads limit above max", []string{"uploads", "list", "--limit", "101"}},
		{"uploads negative offset", []string{"uploads", "list", "--offset", "-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, requests := noRequestServer(t)
			out, err := execRoot(t, tc.args...)
			if err == nil || exit.Classify(err) != exit.Usage {
				t.Fatalf("%v: got %v, want usage error", tc.args, err)
			}
			if out != "" {
				t.Fatalf("%v wrote stdout on failure: %q", tc.args, out)
			}
			if *requests != 0 {
				t.Fatalf("%v made %d HTTP requests", tc.args, *requests)
			}
		})
	}
}

// The inclusive upper bounds must be accepted and reach the API.
func TestListLimitUpperBoundsAccepted(t *testing.T) {
	srv := fakeAPI(t)
	if _, err := run(t, srv, "runs", "list", "--limit", "500"); err != nil {
		t.Fatalf("runs list --limit 500: %v", err)
	}
	if _, err := run(t, srv, "workflows", "list", "--limit", "500"); err != nil {
		t.Fatalf("workflows list --limit 500: %v", err)
	}
}

// --start-timeout must be within the backend's 60-86400 second bounds when
// positive; zero still means "omit the field, use the server default".
func TestRunsStartRejectsInvalidStartTimeout(t *testing.T) {
	_, requests := noRequestServer(t)
	for _, timeout := range []string{"1", "30", "59", "86401"} {
		out, err := execRoot(t, "runs", "start", "w1", "--start-timeout", timeout)
		if err == nil || exit.Classify(err) != exit.Usage {
			t.Fatalf("--start-timeout %s: got %v, want usage error", timeout, err)
		}
		if out != "" {
			t.Fatalf("--start-timeout %s wrote stdout on failure: %q", timeout, out)
		}
	}
	if *requests != 0 {
		t.Fatalf("invalid start timeouts made %d HTTP requests", *requests)
	}
}

func TestRunsStartAcceptsStartTimeoutBounds(t *testing.T) {
	srv := fakeAPI(t)
	for _, timeout := range []string{"60", "86400", "0"} {
		if _, err := run(t, srv, "runs", "start", "w1", "--start-timeout", timeout); err != nil {
			t.Fatalf("--start-timeout %s: %v", timeout, err)
		}
	}
}
