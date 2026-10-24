package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
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

// --sort/--order fold into the API's single order_by expression and --search
// goes out as search.
func TestFilesListSortAndSearchMapToQuery(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"files":[],"total":0,"usage":{"file_count":0,"file_limit":10000,"total_bytes":0,"byte_limit":53687091200}}`)
	}))
	t.Cleanup(srv.Close)
	if _, err := run(t, srv, "files", "list", "--search", "receipt", "--sort", "size_bytes", "--order", "asc"); err != nil {
		t.Fatalf("files list: %v", err)
	}
	if got := gotQuery.Get("order_by"); got != "size_bytes asc" {
		t.Fatalf("order_by = %q, want %q", got, "size_bytes asc")
	}
	if got := gotQuery.Get("search"); got != "receipt" {
		t.Fatalf("search = %q, want receipt", got)
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
	want := map[string]any{"id": "d1", "deleted": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %v, want %v", got, want)
	}
}
