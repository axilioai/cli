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

func TestWorkflowsCreateJSON(t *testing.T) {
	srv := fakeAPI(t)
	code := filepath.Join(t.TempDir(), "wf.py")
	if err := os.WriteFile(code, []byte("print('hi')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, srv, "-o", "json", "workflows", "create", "checkout-flow", "--platform", "android", "--code", code)
	if err != nil {
		t.Fatalf("workflows create: %v", err)
	}
	got := mustJSON(t, out)
	if got["workflow_id"] != "w2" || got["revision"] != float64(1) || got["revision_id"] != "rev1" {
		t.Fatalf("unexpected payload: %v", got)
	}
}

func TestWorkflowsCreateRejectsBadInputBeforeRequest(t *testing.T) {
	cases := [][]string{
		{"workflows", "create", "bad name"},
		{"workflows", "create", "ok-name", "--platform", "windows"},
		{"workflows", "create", "ok-name", "--code", filepath.Join(t.TempDir(), "missing.py")},
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

func TestWorkflowsGetJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "workflows", "get", "w1")
	if err != nil {
		t.Fatalf("workflows get: %v", err)
	}
	if !strings.Contains(out, `"id": "w1"`) || !strings.Contains(out, `"total_runs": 4`) {
		t.Fatalf("expected workflow and stats in output:\n%s", out)
	}
}

func TestWorkflowsPullStdoutIsRawSource(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "workflows", "pull", "w1")
	if err != nil {
		t.Fatalf("workflows pull: %v", err)
	}
	if out != "print('hello')\n" {
		t.Fatalf("pull stdout = %q, want the revision source byte-for-byte", out)
	}
}

func TestWorkflowsPullOutWritesFile(t *testing.T) {
	srv := fakeAPI(t)
	dest := filepath.Join(t.TempDir(), "pulled.py")
	out, err := run(t, srv, "-o", "json", "workflows", "pull", "w1", "--out", dest)
	if err != nil {
		t.Fatalf("workflows pull --out: %v", err)
	}
	written, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != "print('hello')\n" {
		t.Fatalf("file = %q, want the revision source byte-for-byte", written)
	}
	got := mustJSON(t, out)
	if got["path"] != dest || got["revision"] != float64(2) {
		t.Fatalf("unexpected payload: %v", got)
	}
}

func TestWorkflowsPushJSON(t *testing.T) {
	srv := fakeAPI(t)
	code := filepath.Join(t.TempDir(), "wf.py")
	if err := os.WriteFile(code, []byte("print('bye')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, srv, "-o", "json", "workflows", "push", "w1", code, "-m", "tweak")
	if err != nil {
		t.Fatalf("workflows push: %v", err)
	}
	got := mustJSON(t, out)
	if got["revision"] != float64(3) || got["no_op"] != false {
		t.Fatalf("unexpected payload: %v", got)
	}
}

func TestWorkflowsPushRejectsOversizeBeforeRequest(t *testing.T) {
	_, requests := noRequestServer(t)
	code := filepath.Join(t.TempDir(), "big.py")
	if err := os.WriteFile(code, bytes.Repeat([]byte{'#'}, maxWorkflowSourceBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := execRoot(t, "workflows", "push", "w1", code)
	if err == nil || exit.Classify(err) != exit.Usage {
		t.Fatalf("oversize push: got %v, want usage error", err)
	}
	if *requests != 0 {
		t.Fatalf("oversize push made %d HTTP requests", *requests)
	}
}

func TestWorkflowsRevisionsJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "workflows", "revisions", "w1")
	if err != nil {
		t.Fatalf("workflows revisions: %v", err)
	}
	if !strings.Contains(out, `"id": "rev2"`) || !strings.Contains(out, `"revision": 1`) {
		t.Fatalf("expected both fake revisions in output:\n%s", out)
	}
}

func TestWorkflowsRestoreJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "workflows", "restore", "w1", "rev1")
	if err != nil {
		t.Fatalf("workflows restore: %v", err)
	}
	got := mustJSON(t, out)
	if got["revision"] != float64(4) || got["revision_id"] != "rev4" {
		t.Fatalf("unexpected payload: %v", got)
	}
}

func TestWorkflowsDeleteJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "workflows", "delete", "w1", "--yes")
	if err != nil {
		t.Fatalf("workflows delete: %v", err)
	}
	got := mustJSON(t, out)
	if got["id"] != "w1" || got["deleted"] != true {
		t.Fatalf("unexpected payload: %v", got)
	}
}
