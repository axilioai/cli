package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/axilioai/cli/internal/config"
	"github.com/axilioai/cli/internal/exit"
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
	out, err := execRoot(t, "-o", "json", "config")
	if err != nil {
		t.Fatalf("config show: %v", err)
	}
	var view map[string]string
	if e := json.Unmarshal([]byte(out), &view); e != nil {
		t.Fatalf("config show is not JSON: %v\n%s", e, out)
	}
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
	out, _ = execRoot(t, "-o", "json", "config")
	_ = json.Unmarshal([]byte(out), &view)
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
