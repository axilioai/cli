package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

// The rename preflight mirrors the backend's 1-100 nickname bound (AXI-1680):
// an out-of-bounds nickname is a usage error before credentials or any HTTP
// request, like the list-limit preflights.
func TestPhonesRenamePreflightsNicknameBound(t *testing.T) {
	cases := map[string]string{
		"empty":    "",
		"too long": strings.Repeat("x", 101),
	}
	for name, nickname := range cases {
		t.Run(name, func(t *testing.T) {
			_, requests := noRequestServer(t)
			_, err := execRoot(t, "phones", "rename", "p1", nickname)
			if err == nil || exit.Classify(err) != exit.Usage {
				t.Fatalf("nickname len %d: got %v, want usage error", len(nickname), err)
			}
			if *requests != 0 {
				t.Fatalf("nickname len %d: %d requests, want 0", len(nickname), *requests)
			}
		})
	}
}

// Wipe is destructive: redirected execution must refuse without --yes and
// must not reach the API.
func TestPhonesWipeRequiresYesWhenRedirected(t *testing.T) {
	_, requests := noRequestServer(t)
	_, _, err := execRootStreams(t, "", "phones", "wipe", "p1")
	if err == nil || exit.Classify(err) != exit.Usage {
		t.Fatalf("wipe without --yes: got %v, want usage error", err)
	}
	if *requests != 0 {
		t.Fatalf("wipe without --yes made %d requests, want 0", *requests)
	}
}

// With --yes, wipe posts to the :wipe action and surfaces the API's message
// as machine-readable JSON.
func TestPhonesWipeJSON(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"Wipe requested"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_API_KEY", "axl_test")
	t.Setenv("AXILIO_BASE_URL", srv.URL)

	out, err := execRoot(t, "-o", "json", "phones", "wipe", "p1", "--yes")
	if err != nil {
		t.Fatalf("phones wipe: %v", err)
	}
	if gotMethod != http.MethodPost || !strings.HasSuffix(gotPath, "/phones/p1:wipe") {
		t.Fatalf("unexpected request %s %s", gotMethod, gotPath)
	}
	if got := mustJSON(t, out); got["message"] != "Wipe requested" {
		t.Fatalf("unexpected payload: %v", got)
	}
}

// find-all-text mirrors the driver contract: contains and --pattern are
// mutually exclusive, refused as a usage error before any connection.
func TestPhoneFindAllTextRejectsBothCriteria(t *testing.T) {
	_, requests := noRequestServer(t)
	_, err := execRoot(t, "phone", "find-all-text", "sign", "--pattern", "^S")
	if err == nil || exit.Classify(err) != exit.Usage {
		t.Fatalf("both criteria: got %v, want usage error", err)
	}
	if *requests != 0 {
		t.Fatalf("both criteria made %d requests, want 0", *requests)
	}
}
