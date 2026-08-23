package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
	"github.com/zalando/go-keyring"
)

// fakeAPI is an httptest server that routes on path substring and returns canned
// JSON, so the real command path (cobra -> SDK -> HTTP) runs end-to-end with no
// network. This is the command-level test seam: point --base-url at it.
func fakeAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		var body string
		switch {
		case strings.Contains(p, "/billing/balance"):
			body = `{"balance_display":"$5.00","balance_microdollars":5000000}`
		case strings.Contains(p, "/phones/sessions/active"):
			body = `{"sessions":[]}`
		case strings.Contains(p, ":deallocate"):
			body = `{"phone_id":"p1","session_id":"s1","workflow_id":"w1","deallocated_at":"2026-08-05T20:00:00Z"}`
		case strings.HasSuffix(p, "/phones"):
			if got := r.URL.Query().Get("ownership"); got != "dedicated" {
				t.Errorf("phones list requested ownership %q, want dedicated", got)
			}
			body = `{"phones":[
				{"phone_id":"p1","nickname":"ci-phone","phone_type":"android","model_name":"Pixel 8","status":"active","ownership_type":"dedicated","created_at":"2026-08-05T20:00:00Z"}],
				"total":1,"limit":50,"offset":0}`
		case strings.Contains(p, "/api-keys"):
			body = `{"api_keys":[
				{"id":"k1","name":"ci","key_preview":"axl_ci…","created_at":"2026-07-14T00:00:00Z"}],
				"total":1,"limit":50,"offset":0}`
		case strings.Contains(p, ":cancel"):
			// run cancel: POST /runs/{run_id}:cancel (custom method, 0.72 API).
			body = `{"success":true}`
		case strings.HasSuffix(p, "/runs") && r.Method == http.MethodPost:
			// run creation: POST /workflows/{workflow_id}/runs (0.72 API). The
			// backend requires a non-empty `runs` array (one config per run);
			// reject its absence so this test stays faithful to the real contract.
			var reqBody struct {
				Runs []map[string]any `json:"runs"`
			}
			_ = json.NewDecoder(r.Body).Decode(&reqBody)
			if len(reqBody.Runs) == 0 {
				http.Error(w, `{"title":"Unprocessable Entity","status":422,"detail":"expected required property runs to be present"}`, http.StatusUnprocessableEntity)
				return
			}
			if _, ok := reqBody.Runs[0]["variables"]; !ok {
				http.Error(w, `{"title":"Unprocessable Entity","status":422,"detail":"expected required property variables to be present"}`, http.StatusUnprocessableEntity)
				return
			}
			body = `{"run_ids":["r1"]}`
		case strings.HasSuffix(p, "/runs") && r.Method == http.MethodGet:
			body = `{"runs":[
				{"id":"r1","status":"completed","trigger":"manual","workflow_id":"w1","success":true}],
				"total":1,"limit":20,"offset":0}`
		case strings.HasSuffix(p, "/blob"):
			// Stands in for the storage service behind a capture's signed URL.
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, "captured-bytes")
			return
		case strings.Contains(p, "/phones/sessions/") && strings.HasSuffix(p, "/downloads"):
			body = `{"downloads":[
				{"id":"d1","filename":"receipt.png","mime_type":"image/png","size_bytes":2048,"capture_state":"ready","preview_state":"ready","on_phone_count":1,"session_id":"s1","created_at":"2026-08-22T10:00:00Z","download_url":"http://` + r.Host + `/blob"}],
				"total":1}`
		case strings.HasSuffix(p, "/downloads") && r.Method == http.MethodGet:
			body = `{"downloads":[
				{"id":"d1","filename":"receipt.png","mime_type":"image/png","size_bytes":2048,"capture_state":"ready","preview_state":"ready","on_phone_count":1,"session_id":"s1","created_at":"2026-08-22T10:00:00Z","download_url":"http://` + r.Host + `/blob"},
				{"id":"d2","filename":"clip.mp4","mime_type":"video/mp4","size_bytes":9999,"capture_state":"skipped_size","capture_error":"file exceeds the capture ceiling","preview_state":"unavailable","on_phone_count":0,"session_id":"s1","created_at":"2026-08-22T09:00:00Z"}],
				"total":2}`
		case strings.Contains(p, "/downloads/") && r.Method == http.MethodDelete:
			body = `{"message":"file deleted","phones_pending_removal":2}`
		case strings.Contains(p, "/workflows"):
			body = `{"workflows":[
				{"workflow":{"id":"w1","name":"demo","platform":"android","status":"active"}}],
				"total":1,"limit":20,"offset":0}`
		default:
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// run executes the root command with args against the fake API, capturing stdout.
func run(t *testing.T, srv *httptest.Server, args ...string) (string, error) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_API_KEY", "axl_test")
	t.Setenv("AXILIO_BASE_URL", srv.URL)

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

func TestStatusJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var got map[string]any
	if e := json.Unmarshal([]byte(out), &got); e != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", e, out)
	}
	if got["status"] != "ok" || got["balance"] != "$5.00" {
		t.Fatalf("unexpected status payload: %v", got)
	}
}

func TestPhonesListJSONUsesDedicatedContract(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "phones", "list")
	if err != nil {
		t.Fatalf("phones list: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode phones list JSON: %v", err)
	}
	// The 0.72 API dropped the shared-pool availability shape: no iphone_count
	// and no android_count. `phones list` now returns the dedicated inventory.
	if _, ok := got["iphone_count"]; ok {
		t.Error("phones list exposed the removed iphone_count field")
	}
	if _, ok := got["android_count"]; ok {
		t.Error("phones list still exposes the removed android_count field")
	}
	if got["phones"] == nil {
		t.Fatalf("phones list omitted its phones inventory: %s", out)
	}
}

func TestPhonesListJSONPreservesEmptyPhonesArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/phones") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"phones":[],"total":0,"limit":50,"offset":0}`)
	}))
	t.Cleanup(srv.Close)

	out, err := run(t, srv, "-o", "json", "phones", "list")
	if err != nil {
		t.Fatalf("phones list: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode phones list JSON: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(got["phones"]), []byte("[]")) {
		t.Fatalf("phones list did not preserve its required empty array: %s", out)
	}
}

func TestSessionsStartRejectsUnsupportedPhoneTypeBeforeAuth(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_API_KEY", "")
	t.Setenv("AXILIO_BASE_URL", "")

	root := Root()
	root.SetArgs([]string{"sessions", "start", "--phone-type", "iphone"})
	err := root.Execute()
	if got := exit.Classify(err); got != exit.Usage {
		t.Fatalf("exit code = %d, want %d (usage): %v", got, exit.Usage, err)
	}
}

func TestSessionsStartNormalizesAndroidPhoneType(t *testing.T) {
	var gotPhoneType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":allocate") {
			http.NotFound(w, r)
			return
		}
		var body struct {
			PhoneType string `json:"phone_type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode allocation request: %v", err)
		}
		gotPhoneType = body.PhoneType
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"phone_id":"p1","session_id":"s1","workflow_started_at":"2026-08-03T00:00:00Z"}`)
	}))
	t.Cleanup(srv.Close)

	if _, err := run(t, srv, "sessions", "start", "--phone-type", " ANDROID "); err != nil {
		t.Fatalf("sessions start: %v", err)
	}
	if gotPhoneType != "android" {
		t.Fatalf("allocation phone_type = %q, want android", gotPhoneType)
	}
}

func TestRunsListJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "runs", "list")
	if err != nil {
		t.Fatalf("runs list: %v", err)
	}
	if !strings.Contains(out, `"id": "r1"`) {
		t.Fatalf("expected the fake run in output:\n%s", out)
	}
}

func TestWorkflowsListJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "workflows", "list")
	if err != nil {
		t.Fatalf("workflows list: %v", err)
	}
	if !strings.Contains(out, `"id": "w1"`) {
		t.Fatalf("expected the fake workflow in output:\n%s", out)
	}
}

func TestRunsStartJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "runs", "start", "w1")
	if err != nil {
		t.Fatalf("runs start: %v", err)
	}
	if !strings.Contains(out, `"r1"`) {
		t.Fatalf("expected the created run id in output:\n%s", out)
	}
}

func TestRunsStartRejectsInvalidCountBeforeRequest(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_API_KEY", "axl_test")
	t.Setenv("AXILIO_BASE_URL", srv.URL)

	for _, count := range []string{"-1", "0", "1001"} {
		out, err := execRoot(t, "runs", "start", "w1", "--count", count)
		if err == nil || exit.Classify(err) != exit.Usage {
			t.Fatalf("--count %s: got %v, want usage error", count, err)
		}
		if out != "" {
			t.Fatalf("--count %s wrote stdout on failure: %q", count, out)
		}
	}
	if requests != 0 {
		t.Fatalf("invalid counts made %d HTTP requests", requests)
	}
}

func TestRunsStartAcceptsCountUpperBound(t *testing.T) {
	srv := fakeAPI(t)
	if _, err := run(t, srv, "runs", "start", "w1", "--count", "1000"); err != nil {
		t.Fatalf("--count 1000: %v", err)
	}
}

// A rejected key must surface as the Auth exit code, not a generic error.
func TestAuthFailureExitCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"invalid API key"}`, http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	_, err := run(t, srv, "status")
	if err == nil {
		t.Fatal("expected an error from a 401")
	}
	if got := exit.Classify(err); got != exit.Auth {
		t.Fatalf("exit code = %d, want %d (auth)", got, exit.Auth)
	}
}

// No credentials at all must classify as Auth, without touching the network.
func TestNoCredentialsExitCode(t *testing.T) {
	keyring.MockInit() // a developer's real OAuth session must not leak in
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_API_KEY", "")
	t.Setenv("AXILIO_BASE_URL", "")

	root := Root()
	root.SetArgs([]string{"status"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error with no credentials")
	}
	if got := exit.Classify(err); got != exit.Auth {
		t.Fatalf("exit code = %d, want %d (auth)", got, exit.Auth)
	}
}
