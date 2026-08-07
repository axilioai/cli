package cmd

import (
	"encoding/json"
	"testing"

	"github.com/axilioai/cli/internal/config"
	"github.com/axilioai/cli/internal/exit"
	"github.com/zalando/go-keyring"
)

// AXI-1507: every successful runnable application command must leave valid
// JSON on stdout in -o json mode — including mutation verbs that used to speak
// only in stderr chrome, which JSON mode suppresses entirely. Cobra's built-in
// help/completion and version paths remain text. These tests parse stdout, so
// an empty or non-JSON success regresses loudly.

// mustJSON fails the test unless out is a single valid JSON document, and
// returns it decoded for shape assertions.
func mustJSON(t *testing.T, out string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%q", err, out)
	}
	return got
}

func TestConfigSetJSON(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_API_KEY", "")
	t.Setenv("AXILIO_BASE_URL", "")

	out, err := execRoot(t, "-o", "json", "config", "set", "base-url", "https://staging-api.axilio.ai")
	if err != nil {
		t.Fatalf("config set: %v", err)
	}
	got := mustJSON(t, out)
	if got["key"] != "base-url" || got["value"] != "https://staging-api.axilio.ai" {
		t.Fatalf("unexpected set payload: %v", got)
	}

	out, err = execRoot(t, "-o", "json", "config", "unset", "base-url")
	if err != nil {
		t.Fatalf("config unset: %v", err)
	}
	got = mustJSON(t, out)
	if got["key"] != "base-url" || got["unset"] != true {
		t.Fatalf("unexpected unset payload: %v", got)
	}
}

func TestLogoutJSON(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_API_KEY", "")
	t.Setenv("AXILIO_BASE_URL", "")

	// Nothing stored yet: still a success, still JSON.
	out, err := execRoot(t, "-o", "json", "logout")
	if err != nil {
		t.Fatalf("logout (signed out): %v", err)
	}
	if got := mustJSON(t, out); got["status"] != "already_signed_out" {
		t.Fatalf("unexpected payload: %v", got)
	}

	// With a stored API key, logout removes it and reports so.
	cfg := config.Load()
	cfg.APIKey = "axl_test"
	if err := config.Save(cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	out, err = execRoot(t, "-o", "json", "logout")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	if got := mustJSON(t, out); got["status"] != "signed_out" {
		t.Fatalf("unexpected payload: %v", got)
	}
	if config.Load().APIKey != "" {
		t.Fatal("logout left the API key in config")
	}
}

func TestAPIKeysDeleteJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "api-keys", "delete", "k1", "--yes")
	if err != nil {
		t.Fatalf("api-keys delete: %v", err)
	}
	got := mustJSON(t, out)
	if got["id"] != "k1" || got["deleted"] != true {
		t.Fatalf("unexpected payload: %v", got)
	}
}

func TestRunsCancelJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "runs", "cancel", "r1", "--yes")
	if err != nil {
		t.Fatalf("runs cancel: %v", err)
	}
	got := mustJSON(t, out)
	if got["id"] != "r1" || got["canceled"] != true {
		t.Fatalf("unexpected payload: %v", got)
	}
}

func TestSessionsStopJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "sessions", "stop", "p1", "--yes")
	if err != nil {
		t.Fatalf("sessions stop: %v", err)
	}
	got := mustJSON(t, out)
	if got["phone_id"] != "p1" || got["session_id"] != "s1" || got["workflow_id"] != "w1" ||
		got["deallocated_at"] != "2026-08-05T20:00:00Z" {
		t.Fatalf("unexpected payload: %v", got)
	}
	if _, ok := got["released"]; ok {
		t.Fatalf("payload retained the CLI-only released field: %v", got)
	}
}

func TestSessionsCurrentMissingIsNotFound(t *testing.T) {
	srv := fakeAPI(t)
	for _, args := range [][]string{
		{"sessions", "current"},
		{"-o", "json", "sessions", "current"},
		{"--quiet", "sessions", "current"},
	} {
		out, err := run(t, srv, args...)
		if err == nil || exit.Classify(err) != exit.NotFound {
			t.Fatalf("%v: got %v, want not-found error", args, err)
		}
		if out != "" {
			t.Fatalf("%v: failure wrote stdout %q", args, out)
		}
	}
}

func TestUpgradeDevBuildJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "upgrade")
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	// Tests run as a dev build (Version = "dev"), so upgrade reports itself
	// toolchain-managed — as JSON, without touching the network.
	if got := mustJSON(t, out); got["status"] != "dev-build" {
		t.Fatalf("unexpected payload: %v", got)
	}
}
