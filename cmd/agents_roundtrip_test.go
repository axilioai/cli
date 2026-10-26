package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

// The pull/push pair is the CLI's code round-trip; its byte fidelity and JSON
// shapes are the contract coding agents script against.

func TestAgentsCreateJSON(t *testing.T) {
	srv := fakeAPI(t)
	code := filepath.Join(t.TempDir(), "agent.py")
	if err := os.WriteFile(code, []byte("print('hi')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, srv, "-o", "json", "agents", "create", "checkout-flow", "--code", code)
	if err != nil {
		t.Fatalf("agents create: %v", err)
	}
	got := mustJSON(t, out)
	if got["id"] != "a2" || got["current_revision_id"] != "rev1" {
		t.Fatalf("unexpected payload: %v", got)
	}
}

func TestAgentsCreateRejectsBadInputBeforeRequest(t *testing.T) {
	cases := [][]string{
		{"agents", "create", "bad name"},
		{"agents", "create", "ok-name", "--code", "x.py", "--instructions", "do it"},
		{"agents", "create", "ok-name", "--code", filepath.Join(t.TempDir(), "missing.py")},
	}
	for _, args := range cases {
		_, requests := noRequestServer(t)
		out, err := execRoot(t, args...)
		if err == nil || exit.Classify(err) != exit.Usage {
			t.Fatalf("%v: got %v, want usage error", args, err)
		}
		if out != "" {
			t.Fatalf("%v wrote stdout on failure: %q", args, out)
		}
		if *requests != 0 {
			t.Fatalf("%v made %d HTTP requests", args, *requests)
		}
	}
}

func TestAgentsGetJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "agents", "get", "a1")
	if err != nil {
		t.Fatalf("agents get: %v", err)
	}
	if !strings.Contains(out, `"id": "a1"`) || !strings.Contains(out, `"current_revision_id": "rev2"`) {
		t.Fatalf("expected the agent in output:\n%s", out)
	}
}

func TestAgentsPullStdoutIsRawSource(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "agents", "pull", "a1")
	if err != nil {
		t.Fatalf("agents pull: %v", err)
	}
	if out != "print('hello')\n" {
		t.Fatalf("pull stdout = %q, want the revision source byte-for-byte", out)
	}
}

func TestAgentsPullOutWritesFile(t *testing.T) {
	srv := fakeAPI(t)
	dest := filepath.Join(t.TempDir(), "pulled.py")
	out, err := run(t, srv, "-o", "json", "agents", "pull", "a1", "--out", dest)
	if err != nil {
		t.Fatalf("agents pull --out: %v", err)
	}
	written, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != "print('hello')\n" {
		t.Fatalf("file = %q, want the revision source byte-for-byte", written)
	}
	got := mustJSON(t, out)
	if got["path"] != dest || got["revision"] != float64(2) || got["revision_id"] != "rev2" {
		t.Fatalf("unexpected payload: %v", got)
	}
}

func TestAgentsPushJSON(t *testing.T) {
	srv := fakeAPI(t)
	code := filepath.Join(t.TempDir(), "agent.py")
	if err := os.WriteFile(code, []byte("print('bye')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, srv, "-o", "json", "agents", "push", "a1", code, "-m", "tweak")
	if err != nil {
		t.Fatalf("agents push: %v", err)
	}
	got := mustJSON(t, out)
	if got["revision"] != float64(3) || got["id"] != "rev3" {
		t.Fatalf("unexpected payload: %v", got)
	}
}

func TestAgentsPushRejectsOversizeBeforeRequest(t *testing.T) {
	_, requests := noRequestServer(t)
	code := filepath.Join(t.TempDir(), "big.py")
	if err := os.WriteFile(code, bytes.Repeat([]byte{'#'}, maxAgentSourceBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := execRoot(t, "agents", "push", "a1", code)
	if err == nil || exit.Classify(err) != exit.Usage {
		t.Fatalf("oversize push: got %v, want usage error", err)
	}
	if *requests != 0 {
		t.Fatalf("oversize push made %d HTTP requests", *requests)
	}
}

func TestAgentsRevisionsJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "agents", "revisions", "a1")
	if err != nil {
		t.Fatalf("agents revisions: %v", err)
	}
	if !strings.Contains(out, `"id": "rev2"`) || !strings.Contains(out, `"revision": 1`) {
		t.Fatalf("expected both fake revisions in output:\n%s", out)
	}
}

func TestAgentsRollbackJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "agents", "rollback", "a1", "rev1")
	if err != nil {
		t.Fatalf("agents rollback: %v", err)
	}
	got := mustJSON(t, out)
	if got["revision"] != float64(4) || got["id"] != "rev4" {
		t.Fatalf("unexpected payload: %v", got)
	}
}

func TestAgentsDeleteJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "agents", "delete", "a1", "--yes")
	if err != nil {
		t.Fatalf("agents delete: %v", err)
	}
	got := mustJSON(t, out)
	if got["id"] != "a1" || got["deleted"] != true {
		t.Fatalf("unexpected payload: %v", got)
	}
}

func TestAgentsCreateRejectsCodeAndInstructionsTogether(t *testing.T) {
	_, requests := noRequestServer(t)
	code := filepath.Join(t.TempDir(), "agent.py")
	if err := os.WriteFile(code, []byte("print('hi')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := execRoot(t, "agents", "create", "ok-name", "--code", code, "--instructions", "do it")
	if err == nil || exit.Classify(err) != exit.Usage {
		t.Fatalf("got %v, want usage error", err)
	}
	if *requests != 0 {
		t.Fatalf("made %d HTTP requests", *requests)
	}
}

func TestAgentsRevisionsTableShowsKind(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "agents", "revisions", "a1")
	if err != nil {
		t.Fatalf("agents revisions: %v", err)
	}
	for _, want := range []string{"KIND", "code", "rev2", "tweak"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output:\n%s", want, out)
		}
	}
}
