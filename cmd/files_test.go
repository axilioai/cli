package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

func TestFilesListJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "files", "list")
	if err != nil {
		t.Fatalf("files list: %v", err)
	}
	var got struct {
		Files []map[string]any `json:"files"`
		Total int64            `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	if got.Total != 2 || len(got.Files) != 2 {
		t.Fatalf("unexpected page: %+v", got)
	}
	// A failed capture is a visible row with its state, not an absence.
	if got.Files[1]["id"] != "d2" || got.Files[1]["capture_state"] != "skipped_size" {
		t.Fatalf("skipped capture not surfaced: %v", got.Files[1])
	}
}

// sessions files must hit the session-scoped endpoint, not filter the library
// list client-side. The fake serves a one-row page there vs two on /files, so
// the total tells them apart.
func TestSessionsFilesUsesSessionEndpoint(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "sessions", "files", "s1")
	if err != nil {
		t.Fatalf("sessions files: %v", err)
	}
	got := mustJSON(t, out)
	if got["total"] != float64(1) {
		t.Fatalf("expected the session-scoped page, got: %v", got)
	}
}

func TestFilesDownloadSavesFile(t *testing.T) {
	srv := fakeAPI(t)
	dest := filepath.Join(t.TempDir(), "receipt.png")
	out, err := run(t, srv, "-o", "json", "files", "download", "d1", "--out", dest)
	if err != nil {
		t.Fatalf("files download: %v", err)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("saved file: %v", err)
	}
	if string(b) != "captured-bytes" {
		t.Fatalf("saved bytes = %q", b)
	}
	got := mustJSON(t, out)
	if got["id"] != "d1" || got["path"] != dest {
		t.Fatalf("unexpected payload: %v", got)
	}

	// A second run refuses the existing destination unless forced.
	if _, err := run(t, srv, "files", "download", "d1", "--out", dest); exit.Classify(err) != exit.Usage {
		t.Fatalf("existing destination: got %v, want usage error", err)
	}
	if _, err := run(t, srv, "files", "download", "d1", "--out", dest, "--force"); err != nil {
		t.Fatalf("files download --force: %v", err)
	}
}

// A file without bytes (in flight or terminally skipped) is refused with its
// state rather than a nil-URL panic or an opaque HTTP error.
func TestFilesDownloadRefusesFileWithoutBytes(t *testing.T) {
	srv := fakeAPI(t)
	_, err := run(t, srv, "files", "download", "d2", "--out", filepath.Join(t.TempDir(), "x"))
	if err == nil || !strings.Contains(err.Error(), "skipped_size") {
		t.Fatalf("got %v, want state refusal", err)
	}
}

func TestFilesDeleteJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "files", "delete", "d1", "--yes")
	if err != nil {
		t.Fatalf("files delete: %v", err)
	}
	got := mustJSON(t, out)
	if got["id"] != "d1" || got["deleted"] != true || got["phones_pending_removal"] != float64(2) {
		t.Fatalf("unexpected payload: %v", got)
	}
}
