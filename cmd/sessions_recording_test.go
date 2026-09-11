package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axilioai/cli/internal/exit"
)

// The lookup form reports all three states as a normal result: the caller
// branches on status, and a pending or expired recording is not a failure.
func TestSessionsRecordingLookupReportsEveryState(t *testing.T) {
	srv := fakeAPI(t)
	for _, tc := range []struct {
		session, status string
		wantURL         bool
	}{
		{"s1", "ready", true},
		{"s-pending", "pending", false},
		{"s-expired", "expired", false},
	} {
		out, err := run(t, srv, "-o", "json", "sessions", "recording", tc.session)
		if err != nil {
			t.Fatalf("recording %s: %v", tc.session, err)
		}
		got := mustJSON(t, out)
		if got["session_id"] != tc.session || got["status"] != tc.status {
			t.Fatalf("%s: unexpected payload %v", tc.session, got)
		}
		_, hasURL := got["url"]
		if hasURL != tc.wantURL {
			t.Fatalf("%s: url present=%v, want %v: %v", tc.session, hasURL, tc.wantURL, got)
		}
		if _, hasPath := got["path"]; hasPath {
			t.Fatalf("%s: lookup must not report a path: %v", tc.session, got)
		}
	}
}

func TestSessionsRecordingDownloadSavesFileAtomically(t *testing.T) {
	srv := fakeAPI(t)
	dir := t.TempDir()
	dest := filepath.Join(dir, "session.mp4")
	out, err := run(t, srv, "-o", "json", "sessions", "recording", "s1", "--out", dest)
	if err != nil {
		t.Fatalf("recording --out: %v", err)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("saved file: %v", err)
	}
	if string(b) != "mp4-bytes" {
		t.Fatalf("saved bytes = %q", b)
	}
	got := mustJSON(t, out)
	if got["status"] != "ready" || got["path"] != dest || got["size_bytes"] != float64(len("mp4-bytes")) {
		t.Fatalf("unexpected payload: %v", got)
	}
	// The download must not echo the bearer-like URL once it has saved the file.
	if _, hasURL := got["url"]; hasURL {
		t.Fatalf("download payload leaked the presigned URL: %v", got)
	}
	assertNoPartFiles(t, dir)

	// An existing destination is refused unless forced, and --force replaces it.
	if _, err := run(t, srv, "sessions", "recording", "s1", "--out", dest); exit.Classify(err) != exit.Usage {
		t.Fatalf("existing destination: got %v, want usage error", err)
	}
	if err := os.WriteFile(dest, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, srv, "sessions", "recording", "s1", "--out", dest, "--force"); err != nil {
		t.Fatalf("--force: %v", err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "mp4-bytes" {
		t.Fatalf("--force did not replace the file: %q", b)
	}
}

// The storage service answers the presigned URL with a redirect first; the
// download must follow it rather than save the redirect page.
func TestSessionsRecordingDownloadFollowsRedirect(t *testing.T) {
	srv := fakeAPI(t)
	dest := filepath.Join(t.TempDir(), "r.mp4")
	if _, err := run(t, srv, "sessions", "recording", "s-redirect", "--out", dest); err != nil {
		t.Fatalf("redirected download: %v", err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "mp4-bytes" {
		t.Fatalf("saved bytes after redirect = %q", b)
	}
}

// Every download failure leaves neither a destination nor a temporary file
// behind: a non-2xx from storage, a body that ends early, and an unwritable
// destination directory.
func TestSessionsRecordingDownloadFailuresLeaveNothingBehind(t *testing.T) {
	srv := fakeAPI(t)
	for _, tc := range []struct {
		name, session, wantErr string
	}{
		{"non-2xx", "s-gone", "403"},
		{"interrupted", "s-cut", "downloading recording"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			dest := filepath.Join(dir, "x.mp4")
			_, err := run(t, srv, "sessions", "recording", tc.session, "--out", dest)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("got %v, want error containing %q", err, tc.wantErr)
			}
			if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("destination exists after a failed download (stat err %v)", statErr)
			}
			assertNoPartFiles(t, dir)
		})
	}
	t.Run("unwritable directory", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "missing", "x.mp4")
		_, err := run(t, srv, "sessions", "recording", "s1", "--out", dest)
		if err == nil || !strings.Contains(err.Error(), "creating temporary file") {
			t.Fatalf("got %v, want temp-file creation error", err)
		}
	})
}

// A pending recording cannot be downloaded without --wait, and an expired one
// can never be downloaded; each refusal names the state and its remedy.
func TestSessionsRecordingDownloadRefusesPendingAndExpired(t *testing.T) {
	srv := fakeAPI(t)
	dest := filepath.Join(t.TempDir(), "x.mp4")
	_, err := run(t, srv, "sessions", "recording", "s-pending", "--out", dest)
	if err == nil || !strings.Contains(err.Error(), "--wait") {
		t.Fatalf("pending: got %v, want a --wait hint", err)
	}
	_, err = run(t, srv, "sessions", "recording", "s-expired", "--out", dest)
	if exit.Classify(err) != exit.NotFound || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired: got %v, want not-found refusal", err)
	}
	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a refused download created %s", dest)
	}
}

// --wait re-polls a pending recording and downloads once it turns ready.
func TestSessionsRecordingWaitPollsUntilReady(t *testing.T) {
	srv := fakeAPI(t)
	recordingPolls.Store(0)
	prev := recordingPollInterval
	recordingPollInterval = time.Millisecond
	t.Cleanup(func() { recordingPollInterval = prev })

	dest := filepath.Join(t.TempDir(), "late.mp4")
	out, err := run(t, srv, "-o", "json", "sessions", "recording", "s-late", "--wait", "--out", dest)
	if err != nil {
		t.Fatalf("--wait: %v", err)
	}
	if got := mustJSON(t, out); got["status"] != "ready" || got["path"] != dest {
		t.Fatalf("unexpected payload: %v", got)
	}
	if n := recordingPolls.Load(); n < 3 {
		t.Fatalf("polled %d times, want the fake's three", n)
	}
}

// --wait gives up with the timeout exit code, so a script can tell "still
// pending" from a broken command.
func TestSessionsRecordingWaitTimesOut(t *testing.T) {
	srv := fakeAPI(t)
	prev := recordingPollInterval
	recordingPollInterval = time.Millisecond
	t.Cleanup(func() { recordingPollInterval = prev })

	_, err := run(t, srv, "sessions", "recording", "s-pending", "--wait", "--timeout", "20ms")
	if exit.Classify(err) != exit.Timeout {
		t.Fatalf("got %v (code %d), want timeout", err, exit.Classify(err))
	}
}

func TestSessionsRecordingFlagValidation(t *testing.T) {
	srv := fakeAPI(t)
	if _, err := run(t, srv, "sessions", "recording", "s1", "--timeout", "1m"); exit.Classify(err) != exit.Usage {
		t.Fatalf("--timeout without --wait: got %v, want usage error", err)
	}
	if _, err := run(t, srv, "sessions", "recording", "s1", "--wait", "--timeout", "0"); exit.Classify(err) != exit.Usage {
		t.Fatalf("zero --timeout: got %v, want usage error", err)
	}
	if _, err := run(t, srv, "sessions", "recording"); exit.Classify(err) != exit.Usage {
		t.Fatalf("missing session id: got %v, want usage error", err)
	}
}

// Quiet mode keeps the status lookup's primary result and drops the guidance.
func TestSessionsRecordingQuiet(t *testing.T) {
	srv := fakeAPI(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_API_KEY", "axl_test")
	t.Setenv("AXILIO_BASE_URL", srv.URL)
	stdout, stderr, err := execRootStreams(t, "", "--quiet", "sessions", "recording", "s-pending")
	if err != nil {
		t.Fatalf("quiet lookup: %v", err)
	}
	if !strings.Contains(stdout, "pending") {
		t.Fatalf("quiet stdout lost the status: %q", stdout)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("quiet stderr not empty: %q", stderr)
	}
}

// assertNoPartFiles fails if a download left a temporary file in dir.
func assertNoPartFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Fatalf("temporary file left behind: %s", e.Name())
		}
	}
}
