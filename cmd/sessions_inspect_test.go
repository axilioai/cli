package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

// sessionsAPI fakes the session detail, thumbnail, and frames endpoints. The
// frames endpoint serves two pages so the pagination merge is exercised, and
// includes a frame with an unknown kind to pin the tolerant-reader contract.
func sessionsAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(p, "/frames"):
			if r.URL.Query().Get("offset") == "0" {
				_, _ = io.WriteString(w, `{
					"frames":[
						{"kind":"span","span_id":"sp1","trace_id":"t1","name":"Screen.observe","span_type":"sdk_call","phase":"end","start_time_unix_nano":1000000000,"end_time_unix_nano":2000000000,"status":{"code":"ok","message":""}},
						{"kind":"span","span_id":"sp2","trace_id":"t1","name":"inference","span_type":"inference","phase":"end","parent_span_id":"sp1","start_time_unix_nano":1100000000,"end_time_unix_nano":1900000000,"status":{"code":"ok","message":""},"attributes":{"axilio.inference.id":"inf1"}}
					],
					"sdk_call_costs":{"sp1":3100},"inference_costs":{"inf1":2000},
					"total":4,"limit":2,"offset":0,"retention_expired":false}`)
				return
			}
			_, _ = io.WriteString(w, `{
				"frames":[
					{"kind":"log","trace_id":"t1","log_type":"output_log","severity":"INFO","body":"hello","time_unix_nano":2500000000},
					{"kind":"hologram","trace_id":"t1","payload":{"future":"field"}}
				],
				"sdk_call_costs":{"sp1":3100},"inference_costs":{"inf1":2000},
				"total":4,"limit":2,"offset":2,"retention_expired":false}`)
		case strings.HasSuffix(p, "/thumbnail"):
			_, _ = io.WriteString(w, `{"status":"ready","url":"https://cdn.example/thumb.jpg"}`)
		case strings.Contains(p, "/phones/sessions/"):
			_, _ = io.WriteString(w, `{
				"session_id":"s1","phone_id":"p1","status":"active","source":"interactive",
				"allocated_at":"2026-08-05T20:00:00Z","capture_enabled":true,
				"is_dedicated_phone":false,"telemetry_disabled":false,
				"phone_status":"active","recording_status":"pending",
				"tags":{"team":"qa"}}`)
		default:
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSessionsGetJSONIncludesThumbnail(t *testing.T) {
	srv := sessionsAPI(t)
	out, err := run(t, srv, "-o", "json", "sessions", "get", "s1")
	if err != nil {
		t.Fatalf("sessions get: %v", err)
	}
	got := mustJSON(t, out)
	if got["session_id"] != "s1" || got["status"] != "active" || got["phone_id"] != "p1" {
		t.Fatalf("canonical detail fields missing: %v", got)
	}
	if got["thumbnail_url"] != "https://cdn.example/thumb.jpg" || got["thumbnail_status"] != "ready" {
		t.Fatalf("thumbnail enrichment missing: %v", got)
	}
}

func TestSessionsTraceMergesPagesAndKeepsUnknownFrames(t *testing.T) {
	srv := sessionsAPI(t)
	out, err := run(t, srv, "-o", "json", "sessions", "trace", "s1")
	if err != nil {
		t.Fatalf("sessions trace: %v", err)
	}
	var got struct {
		Frames         []map[string]any `json:"frames"`
		SdkCallCosts   map[string]int64 `json:"sdk_call_costs"`
		InferenceCosts map[string]int64 `json:"inference_costs"`
		Total          int64            `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	if len(got.Frames) != 4 || got.Total != 4 {
		t.Fatalf("expected all 4 frames across pages, got %d (total %d)", len(got.Frames), got.Total)
	}
	// The unknown-kind frame must survive the round-trip with its raw JSON,
	// never be dropped or fail the command (tolerant-reader contract).
	last := got.Frames[3]
	if last["kind"] != "hologram" {
		t.Fatalf("unknown frame kind lost: %v", last)
	}
	if got.SdkCallCosts["sp1"] != 3100 || got.InferenceCosts["inf1"] != 2000 {
		t.Fatalf("cost maps not merged: %v %v", got.SdkCallCosts, got.InferenceCosts)
	}
}

func TestSessionsTraceTableJoinsCosts(t *testing.T) {
	srv := sessionsAPI(t)
	out, err := run(t, srv, "sessions", "trace", "s1")
	if err != nil {
		t.Fatalf("sessions trace: %v", err)
	}
	// sdk_call priced by span id; inference priced via its inference-id
	// attribute; unknown kinds rendered generically.
	for _, want := range []string{"$0.0031", "$0.0020", "hologram", "Screen.observe"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table output missing %q:\n%s", want, out)
		}
	}
}

func TestSessionsListHistoryFiltersMapToQuery(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/phones/sessions") {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"sessions":[],"total":0,"limit":50,"offset":0}`)
	}))
	t.Cleanup(srv.Close)

	_, err := run(t, srv, "sessions", "list",
		"--status", "completed", "--source", "workflow",
		"--started-after", "2026-08-01", "--sort", "duration", "--order", "asc")
	if err != nil {
		t.Fatalf("sessions list history: %v", err)
	}
	if gotQuery == nil {
		t.Fatal("filters did not route to the historical list endpoint")
	}
	// Status normalizes to the API's uppercase enum; bare dates become RFC3339.
	if got := gotQuery["status"]; len(got) != 1 || got[0] != "COMPLETED" {
		t.Fatalf("status = %v, want [COMPLETED]", got)
	}
	if got := gotQuery.Get("started_after"); got != "2026-08-01T00:00:00Z" {
		t.Fatalf("started_after = %q, want RFC3339 midnight", got)
	}
	if gotQuery.Get("sort") != "duration" || gotQuery.Get("order") != "asc" {
		t.Fatalf("sort/order not forwarded: %v", gotQuery)
	}
}

func TestSessionsListHistoryRejectsBadInputBeforeRequest(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_API_KEY", "axl_test")
	t.Setenv("AXILIO_BASE_URL", srv.URL)

	for _, args := range [][]string{
		{"sessions", "list", "--history", "--limit", "101"},
		{"sessions", "list", "--status", "paused"},
		{"sessions", "list", "--started-after", "yesterday"},
		{"sessions", "list", "--remote", "--status", "completed"},
	} {
		_, err := execRoot(t, args...)
		if err == nil || exit.Classify(err) != exit.Usage {
			t.Fatalf("%v: got %v, want usage error", args, err)
		}
	}
	if requests != 0 {
		t.Fatalf("invalid input made %d HTTP requests", requests)
	}
}
