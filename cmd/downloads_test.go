package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

func TestDownloadsListJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "downloads", "list")
	if err != nil {
		t.Fatalf("downloads list: %v", err)
	}
	var got struct {
		Downloads []map[string]any `json:"downloads"`
		Total     int64            `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	if got.Total != 2 || len(got.Downloads) != 2 {
		t.Fatalf("unexpected page: %+v", got)
	}
	// A failed capture is a visible row with its state, not an absence.
	if got.Downloads[1]["id"] != "d2" || got.Downloads[1]["capture_state"] != "skipped_size" {
		t.Fatalf("skipped capture not surfaced: %v", got.Downloads[1])
	}
}

// sessions downloads must hit the session-scoped endpoint, not filter the
// library list client-side. The fake serves a one-row page there vs two on
// /downloads, so the total tells them apart.
func TestSessionsDownloadsUsesSessionEndpoint(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "sessions", "downloads", "s1")
	if err != nil {
		t.Fatalf("sessions downloads: %v", err)
	}
	got := mustJSON(t, out)
	if got["total"] != float64(1) {
		t.Fatalf("expected the session-scoped page, got: %v", got)
	}
}

func TestDownloadsGetSavesFile(t *testing.T) {
	srv := fakeAPI(t)
	dest := filepath.Join(t.TempDir(), "receipt.png")
	out, err := run(t, srv, "-o", "json", "downloads", "get", "d1", "--out", dest)
	if err != nil {
		t.Fatalf("downloads get: %v", err)
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
	if _, err := run(t, srv, "downloads", "get", "d1", "--out", dest); exit.Classify(err) != exit.Usage {
		t.Fatalf("existing destination: got %v, want usage error", err)
	}
	if _, err := run(t, srv, "downloads", "get", "d1", "--out", dest, "--force"); err != nil {
		t.Fatalf("downloads get --force: %v", err)
	}
}

// A capture without bytes (in flight or terminally skipped) is refused with
// its capture state rather than a nil-URL panic or an opaque HTTP error.
func TestDownloadsGetRefusesCaptureWithoutBytes(t *testing.T) {
	srv := fakeAPI(t)
	_, err := run(t, srv, "downloads", "get", "d2", "--out", filepath.Join(t.TempDir(), "x"))
	if err == nil || !strings.Contains(err.Error(), "skipped_size") {
		t.Fatalf("got %v, want capture-state refusal", err)
	}
}

func TestDownloadsDeleteJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "downloads", "delete", "d1", "--yes")
	if err != nil {
		t.Fatalf("downloads delete: %v", err)
	}
	got := mustJSON(t, out)
	if got["id"] != "d1" || got["deleted"] != true || got["phones_pending_removal"] != float64(2) {
		t.Fatalf("unexpected payload: %v", got)
	}
}
