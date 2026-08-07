package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/axilioai/cli/internal/config"
	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/oauth"
)

// execRoot runs the root command with args and captures stdout. Unlike the
// command_test.go run() helper it sets no API creds, so config lives entirely
// in the temp config file.
func execRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
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

func configJSON(t *testing.T, args ...string) map[string]string {
	t.Helper()
	args = append([]string{"-o", "json"}, args...)
	out, err := execRoot(t, args...)
	if err != nil {
		t.Fatalf("config command: %v", err)
	}
	var view map[string]string
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("config output is not JSON: %v\n%s", err, out)
	}
	return view
}

func TestConfigReportsMatchingStoredOAuthSession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_API_KEY", "")
	t.Setenv("AXILIO_BASE_URL", "https://staging-api.axilio.ai")
	oauth.Clear()
	t.Cleanup(oauth.Clear)

	// The summary is intentionally local: even an expired matching session is
	// reported without refreshing it. `status` owns credential validation.
	if err := oauth.Save(oauth.Tokens{
		AccessToken: "expired",
		Host:        "https://staging-api.axilio.ai",
		Expiry:      time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed OAuth session: %v", err)
	}

	view := configJSON(t, "config")
	if view["auth_method"] != "oauth" || view["auth_source"] != "browser-session" {
		t.Fatalf("unexpected OAuth summary: %v", view)
	}
}

func TestConfigAuthSummaryFollowsRequestPrecedence(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_BASE_URL", "https://staging-api.axilio.ai")
	oauth.Clear()
	t.Cleanup(oauth.Clear)

	if err := oauth.Save(oauth.Tokens{
		AccessToken: "stored",
		Host:        "https://api.axilio.ai",
		Expiry:      time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed OAuth session: %v", err)
	}

	// A session for another API host is not an effective credential.
	t.Setenv("AXILIO_API_KEY", "")
	view := configJSON(t, "config")
	if view["auth_method"] != "none" || view["auth_source"] != "" {
		t.Fatalf("different-host OAuth session became effective: %v", view)
	}

	// An explicit API key wins even when a matching browser session exists.
	t.Setenv("AXILIO_BASE_URL", "https://api.axilio.ai")
	view = configJSON(t, "--api-key", "axl_flag", "config")
	if view["auth_method"] != "api-key" || view["auth_source"] != "flag" {
		t.Fatalf("API key did not win auth precedence: %v", view)
	}
}

func TestConfigSetGetUnset(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_API_KEY", "")
	t.Setenv("AXILIO_BASE_URL", "")

	// set base-url (trailing slash trimmed)
	if _, err := execRoot(t, "config", "set", "base-url", "https://staging-api.axilio.ai/"); err != nil {
		t.Fatalf("config set: %v", err)
	}
	if got := config.Load().BaseURL; got != "https://staging-api.axilio.ai" {
		t.Fatalf("BaseURL = %q, want trimmed staging host", got)
	}

	// show reflects it
	view := configJSON(t, "config")
	if view["api_host"] != "https://staging-api.axilio.ai" {
		t.Fatalf("api_host = %q", view["api_host"])
	}
	if view["auth_method"] != "none" {
		t.Fatalf("auth_method = %q, want none", view["auth_method"])
	}

	// unset clears it; show falls back to the default host
	if _, err := execRoot(t, "config", "unset", "base-url"); err != nil {
		t.Fatalf("config unset: %v", err)
	}
	if got := config.Load().BaseURL; got != "" {
		t.Fatalf("BaseURL = %q after unset, want empty", got)
	}
	view = configJSON(t, "config")
	if view["api_host"] != defaultAPIHost {
		t.Fatalf("api_host = %q after unset, want default", view["api_host"])
	}
}

func TestConfigSetRejectsBadInput(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_API_KEY", "")
	t.Setenv("AXILIO_BASE_URL", "")

	// a value with no scheme is a usage error
	if _, err := execRoot(t, "config", "set", "base-url", "not-a-url"); err == nil || exit.Classify(err) != exit.Usage {
		t.Fatalf("expected a usage error for a bad url, got %v", err)
	}
	// an unknown key is a usage error
	if _, err := execRoot(t, "config", "set", "nope", "x"); err == nil || exit.Classify(err) != exit.Usage {
		t.Fatalf("expected a usage error for an unknown key, got %v", err)
	}
	// nothing was written
	if got := config.Load().BaseURL; got != "" {
		t.Fatalf("BaseURL = %q, want empty after failed sets", got)
	}
}
