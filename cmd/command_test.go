package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/axilioai/cli/internal/exit"
)

// recordingPolls counts recording status lookups for the s-late session so a
// test can prove --wait re-polls rather than trusting the first answer.
var recordingPolls atomic.Int32

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
		case strings.HasSuffix(p, "/phones/sessions"):
			body = `{"sessions":[],"total":0,"limit":100,"offset":0}`
		case strings.Contains(p, ":deallocate"):
			body = `{"phone_id":"p1","session_id":"s1","agent_id":"a1","deallocated_at":"2026-08-05T20:00:00Z"}`
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
		case strings.Contains(p, "/usage/metrics"):
			body = `{"points":[
				{"period_start_at":"2026-08-01T00:00:00Z","session_cost_microdollars":1000000,"inference_cost_microdollars":250000,"total_cost_microdollars":1250000},
				{"period_start_at":"2026-08-02T00:00:00Z","session_cost_microdollars":250000,"inference_cost_microdollars":500000,"total_cost_microdollars":750000}]}`
		case strings.Contains(p, "/usage/inferences"):
			body = `{"inferences":[
				{"inference_id":"inf1","endpoint":"locate","model":"axilio-ground-1","cost_microdollars":1234,"latency_ms":321,"created_at":"2026-08-20T00:00:00Z","ocr_pages":0}],
				"total":1,"limit":50,"offset":0}`
		case strings.HasSuffix(p, "/runs/metrics"):
			body = `{"points":[
				{"agent":"a1","period_start_at":"2026-08-10T00:00:00Z","runs":4,"completed_runs":3,"failed_runs":1,"model_steps":0,"success_rate":0.75,"total_cost_microdollars":420000},
				{"agent":"a1","period_start_at":"2026-08-11T00:00:00Z","runs":3,"completed_runs":3,"failed_runs":0,"model_steps":0,"success_rate":1,"total_cost_microdollars":80000},
				{"agent":"a2","period_start_at":"2026-08-10T00:00:00Z","runs":2,"completed_runs":0,"failed_runs":2,"model_steps":0,"success_rate":0,"total_cost_microdollars":10000}]}`
		case strings.Contains(p, ":cancel"):
			// run cancel: POST /runs/{run_id}:cancel (custom method).
			body = `{"success":true}`
		case strings.HasSuffix(p, "/runs") && r.Method == http.MethodPost:
			// run creation: POST /runs. A saved-agent run names agent_id;
			// reject its absence so this test stays faithful to the contract.
			var reqBody struct {
				AgentID   *string        `json:"agent_id"`
				Variables map[string]any `json:"variables"`
			}
			_ = json.NewDecoder(r.Body).Decode(&reqBody)
			if reqBody.AgentID == nil {
				http.Error(w, `{"title":"Unprocessable Entity","status":422,"detail":"expected agent_id or task"}`, http.StatusUnprocessableEntity)
				return
			}
			body = `{"id":"r1","mode":"code","status":"queued","trigger":"api","agent_id":"a1","user_id":"u1","created_at":"2026-08-10T00:00:00Z","updated_at":"2026-08-10T00:00:00Z"}`
		case strings.HasSuffix(p, "/runs") && r.Method == http.MethodGet:
			if got := r.URL.Query().Get("agent_id"); got != "" && got != "a1" {
				t.Errorf("runs list requested agent_id %q", got)
			}
			body = `{"runs":[
				{"id":"r1","mode":"code","status":"completed","trigger":"manual","agent_id":"a1","agent_name":"demo","success":true,"user_id":"u1","created_at":"2026-08-10T00:00:00Z","updated_at":"2026-08-10T00:00:00Z"}],
				"total":1,"limit":20,"offset":0}`
		case strings.HasSuffix(p, "/blob"):
			// Stands in for the storage service behind a capture's signed URL.
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, "captured-bytes")
			return
		case strings.HasSuffix(p, "/recording.mp4"):
			// Stands in for the storage service behind a recording's presigned URL.
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = io.WriteString(w, "mp4-bytes")
			return
		case strings.HasSuffix(p, "/recording-redirect"):
			http.Redirect(w, r, "/recording.mp4", http.StatusFound)
			return
		case strings.HasSuffix(p, "/recording-gone"):
			http.Error(w, "expired", http.StatusForbidden)
			return
		case strings.HasSuffix(p, "/recording-cut"):
			// Advertise more bytes than are sent, then drop the connection, so
			// the client sees a body that ends early.
			w.Header().Set("Content-Length", "1024")
			_, _ = io.WriteString(w, "partial")
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					_ = conn.Close()
				}
			}
			return
		case strings.Contains(p, "/phones/sessions/") && strings.HasSuffix(p, "/recording"):
			// Recording status keyed by session id: s1 ready, s-pending pending,
			// s-expired expired, s-late pending on the first call then ready, and
			// s-gone/s-redirect/s-cut ready with URLs that exercise the download
			// failure modes.
			id := strings.TrimSuffix(strings.TrimPrefix(p[strings.Index(p, "/phones/sessions/")+len("/phones/sessions/"):], ""), "/recording")
			base := "http://" + r.Host
			switch id {
			case "s-pending":
				body = `{"status":"pending"}`
			case "s-expired":
				body = `{"status":"expired"}`
			case "s-late":
				recordingPolls.Add(1)
				if recordingPolls.Load() < 3 {
					body = `{"status":"pending"}`
				} else {
					body = `{"status":"ready","url":"` + base + `/recording.mp4"}`
				}
			case "s-gone":
				body = `{"status":"ready","url":"` + base + `/recording-gone"}`
			case "s-redirect":
				body = `{"status":"ready","url":"` + base + `/recording-redirect"}`
			case "s-cut":
				body = `{"status":"ready","url":"` + base + `/recording-cut"}`
			default:
				body = `{"status":"ready","url":"` + base + `/recording.mp4"}`
			}
		case strings.Contains(p, "/phones/sessions/") && strings.HasSuffix(p, "/files"):
			body = `{"files":[
				{"id":"d1","source":"capture","surface":"phone","filename":"receipt.png","mime_type":"image/png","size_bytes":2048,"status":"ready","capture_state":"ready","preview_state":"ready","on_phone_count":1,"session_id":"s1","created_at":"2026-08-22T10:00:00Z","download_url":"http://` + r.Host + `/blob"}],
				"total":1,"usage":{"file_count":1,"file_limit":10000,"total_bytes":2048,"byte_limit":53687091200}}`
		case strings.HasSuffix(p, "/files") && r.Method == http.MethodGet:
			body = `{"files":[
				{"id":"d1","source":"capture","surface":"phone","filename":"receipt.png","mime_type":"image/png","size_bytes":2048,"status":"ready","capture_state":"ready","preview_state":"ready","on_phone_count":1,"session_id":"s1","created_at":"2026-08-22T10:00:00Z","download_url":"http://` + r.Host + `/blob"},
				{"id":"d2","source":"capture","surface":"phone","filename":"clip.mp4","mime_type":"video/mp4","size_bytes":9999,"status":"uploading","capture_state":"skipped_size","capture_error":"file exceeds the capture ceiling","preview_state":"unavailable","on_phone_count":0,"session_id":"s1","created_at":"2026-08-22T09:00:00Z"}],
				"total":2,"usage":{"file_count":2,"file_limit":10000,"total_bytes":12047,"byte_limit":53687091200}}`
		case strings.Contains(p, "/files/") && r.Method == http.MethodDelete:
			// phones_pending_removal is deprecated and always 0 since a library
			// delete stopped touching phones; the CLI no longer reads it.
			body = `{"message":"file deleted successfully","phones_pending_removal":0}`
		case strings.HasSuffix(p, ":rollback") && r.Method == http.MethodPost:
			// agent rollback: POST /agents/{id}:rollback.
			body = `{"id":"rev4","agent_id":"a1","author_user_id":"u1","kind":"code","revision":4,"created_at":"2026-08-05T20:00:00Z"}`
		case strings.HasSuffix(p, "/revisions") && r.Method == http.MethodPost:
			// agent revision create: POST /agents/{id}/revisions. The backend
			// requires kind and, for code, the code; reject their absence so
			// the test seam stays faithful to the real contract.
			var reqBody struct {
				Kind *string `json:"kind"`
				Code *string `json:"code"`
			}
			_ = json.NewDecoder(r.Body).Decode(&reqBody)
			if reqBody.Kind == nil || *reqBody.Kind != "code" || reqBody.Code == nil {
				http.Error(w, `{"title":"Unprocessable Entity","status":422,"detail":"expected kind code with code"}`, http.StatusUnprocessableEntity)
				return
			}
			body = `{"id":"rev3","agent_id":"a1","author_user_id":"u1","kind":"code","revision":3,"code":"x","created_at":"2026-08-05T20:00:00Z"}`
		case strings.HasSuffix(p, "/revisions") && r.Method == http.MethodGet:
			body = `{"revisions":[
				{"id":"rev2","agent_id":"a1","kind":"code","revision":2,"author_user_id":"u1","bytes":14,"message":"tweak","created_at":"2026-08-05T20:00:00Z"},
				{"id":"rev1","agent_id":"a1","kind":"code","revision":1,"author_user_id":"u1","bytes":12,"created_at":"2026-08-04T20:00:00Z"}],
				"total":2,"limit":50,"offset":0}`
		case strings.Contains(p, "/revisions/") && r.Method == http.MethodGet:
			body = `{"id":"rev2","agent_id":"a1","author_user_id":"u1","kind":"code","revision":2,"code":"print('hello')\n","created_at":"2026-08-05T20:00:00Z"}`
		case strings.HasSuffix(p, "/agents") && r.Method == http.MethodPost:
			// agent create: POST /agents. Echo back a first revision only
			// when code was provided, like the real endpoint.
			var reqBody struct {
				Name string  `json:"name"`
				Code *string `json:"code"`
			}
			_ = json.NewDecoder(r.Body).Decode(&reqBody)
			if reqBody.Name == "" {
				http.Error(w, `{"title":"Unprocessable Entity","status":422,"detail":"expected required property name to be present"}`, http.StatusUnprocessableEntity)
				return
			}
			if reqBody.Code != nil {
				body = `{"id":"a2","name":"checkout-flow","model":"m1","current_revision_id":"rev1"}`
			} else {
				body = `{"id":"a2","name":"checkout-flow","model":"m1"}`
			}
		case strings.Contains(p, "/agents/") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
			return
		case strings.Contains(p, "/agents/") && r.Method == http.MethodGet:
			body = `{"id":"a1","name":"demo","model":"m1","current_revision_id":"rev2","recording_enabled":false,"telemetry_enabled":true,"capture_enabled":true,"accessibility_enabled":false,"max_steps":500,"timeout_seconds":3600,"max_cost_microdollars":10000000,"pause_timeout_seconds":600,"created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-05T00:00:00Z"}`
		case strings.Contains(p, "/agents"):
			body = `{"agents":[
				{"id":"a1","name":"demo","model":"m1"}],
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
		_, _ = io.WriteString(w, `{"phone_id":"p1","session_id":"s1","allocated_at":"2026-08-03T00:00:00Z"}`)
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

func TestAgentsListJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "agents", "list")
	if err != nil {
		t.Fatalf("agents list: %v", err)
	}
	if !strings.Contains(out, `"id": "a1"`) {
		t.Fatalf("expected the fake agent in output:\n%s", out)
	}
}

func TestRunsStartJSON(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "runs", "start", "--agent", "a1")
	if err != nil {
		t.Fatalf("runs start: %v", err)
	}
	if !strings.Contains(out, `"id": "r1"`) || !strings.Contains(out, `"mode": "code"`) {
		t.Fatalf("expected the created run in output:\n%s", out)
	}
}

func TestRunsStartRequiresAgentBeforeRequest(t *testing.T) {
	_, requests := noRequestServer(t)
	out, err := execRoot(t, "runs", "start")
	if err == nil || exit.Classify(err) != exit.Usage {
		t.Fatalf("runs start without --agent: got %v, want usage error", err)
	}
	if out != "" {
		t.Fatalf("wrote stdout on failure: %q", out)
	}
	if *requests != 0 {
		t.Fatalf("made %d HTTP requests", *requests)
	}
}

func TestRunsListFilters(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "runs", "list", "--agent", "a1", "--mode", "code", "--status", "completed", "--status", "failed")
	if err != nil {
		t.Fatalf("runs list: %v", err)
	}
	for _, want := range []string{"code", "completed", "demo"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output:\n%s", want, out)
		}
	}
	_, requests := noRequestServer(t)
	for _, args := range [][]string{
		{"runs", "list", "--mode", "workflow"},
		{"runs", "list", "--status", "done"},
	} {
		if _, err := execRoot(t, args...); err == nil || exit.Classify(err) != exit.Usage {
			t.Fatalf("%v: got %v, want usage error", args, err)
		}
	}
	if *requests != 0 {
		t.Fatalf("invalid filters made %d HTTP requests", *requests)
	}
}

func TestRunsMetricsFoldsPerAgent(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "runs", "metrics")
	if err != nil {
		t.Fatalf("runs metrics: %v", err)
	}
	// a1: 7 runs, 6 completed, 1 failed; 6 of 7 finished = 86%.
	if !strings.Contains(out, "a1") || !strings.Contains(out, "86%") || !strings.Contains(out, "$0.5000") || !strings.Contains(out, "a2") {
		t.Fatalf("unexpected metrics table:\n%s", out)
	}
}

func TestRunsMetricsAgentFilter(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "-o", "json", "runs", "metrics", "--agent", "a2")
	if err != nil {
		t.Fatalf("runs metrics --agent: %v", err)
	}
	got := mustJSON(t, out)
	points, _ := got["points"].([]any)
	if len(points) != 1 || strings.Contains(out, `"a1"`) {
		t.Fatalf("expected only agent a2 points:\n%s", out)
	}
}

func TestRunsHistoryAndStatsAreGone(t *testing.T) {
	for _, name := range []string{"history", "stats"} {
		if c, _, _ := Root().Find([]string{"runs", name}); c != nil && c.Name() == name {
			t.Errorf("runs %s should be removed", name)
		}
	}
	if c, _, _ := Root().Find([]string{"workflows"}); c != nil && c.Name() == "workflows" {
		t.Error("workflows should be removed")
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
