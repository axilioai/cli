package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// liveFrames is the scripted session telemetry: a log, an sdk_call span, and
// the session-root end frame. The first two also exist in the archive by the
// time the live leg replays them, so a broken archive/live dedupe renders
// duplicates and fails the exactly-once assertions.
var liveFrames = []string{
	`{"kind":"log","trace_id":"t1","time_unix_nano":1755900000000000000,"severity":"INFO","log_type":"output_log","body":"hello from the phone"}`,
	`{"kind":"span","trace_id":"t1","span_id":"s-call","span_type":"sdk_call","name":"Screen.observe","phase":"end","start_time_unix_nano":1755900001000000000,"end_time_unix_nano":1755900001250000000,"status":{"code":"ok","message":""},"attributes":{"axilio.duration_ns":250000000}}`,
	`{"kind":"span","trace_id":"t1","span_id":"s-root","span_type":"session","name":"session","phase":"end","start_time_unix_nano":1755900000000000000,"end_time_unix_nano":1755900002000000000,"status":{"code":"ok","message":""}}`,
}

// inFlightSpan is a live-leg start phase of the s-call span: never rendered
// (spans appear on completion), and no duplicate when its end phase arrives.
const inFlightSpan = `{"kind":"span","trace_id":"t1","span_id":"s-call","span_type":"sdk_call","name":"Screen.observe","phase":"start","start_time_unix_nano":1755900001000000000}`

// fakeTelemetryWS serves the telemetry dialect: handler runs once per
// accepted connection, ordinal-numbered; refuse pre-upgrade by writing an
// HTTP status instead of accepting.
func fakeTelemetryWS(t *testing.T, handler func(conn int, w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	conns := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conns++
		handler(conns, w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// wsServe upgrades and plays the given wire messages, then closes with the
// given status (normal closure = the hub's clean end).
func wsServe(t *testing.T, w http.ResponseWriter, r *http.Request, messages []string, status websocket.StatusCode) {
	t.Helper()
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, m := range messages {
		if err := c.Write(ctx, websocket.MessageText, []byte(m)); err != nil {
			return
		}
	}
	_ = c.Close(status, "")
}

// liveAPI wires the HTTP fake for live-attach tests: a running run with a
// session, an archive holding archived[0:archiveLen] (offset-sliced, cost
// maps attached), and a telemetry-token endpoint pointing at wsURL (or
// refusing with mintStatus when non-zero).
func liveAPI(t *testing.T, wsURL string, archive []string, mintStatus int, terminal string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case strings.Contains(p, "/telemetry-token"):
			if mintStatus != 0 {
				http.Error(w, fmt.Sprintf(`{"title":"Conflict","status":%d,"detail":"session is not active"}`, mintStatus), mintStatus)
				return
			}
			fmt.Fprintf(w, `{"session_id":"sess-1","telemetry_url":%q}`, wsURL)
		case strings.Contains(p, "/frames"):
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			if offset > len(archive) {
				offset = len(archive)
			}
			fmt.Fprintf(w, `{"frames":[%s],"total":%d,"limit":1000,"offset":%d,"retention_expired":false,"sdk_call_costs":{"s-call":3100},"inference_costs":{}}`,
				strings.Join(archive[offset:], ","), len(archive), offset)
		case strings.Contains(p, "/runs/") && r.Method == http.MethodGet:
			fmt.Fprintf(w, `{"id":"r1","status":%q,"trigger":"manual","workflow_id":"w1","created_at":"2026-08-22T20:00:00Z","session_id":"sess-1"}`, terminal)
		default:
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func wsURLOf(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// The live leg replays frames the archive already delivered, plus transport
// frames and an in-flight span start: each frame renders exactly once, the
// in-flight start not at all, and the live end frame finishes the watch.
func TestRunsWatchLiveAttachNoDuplicates(t *testing.T) {
	fastWatch(t)
	ws := fakeTelemetryWS(t, func(conn int, w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("resume"); got != "1" {
			t.Errorf("live attach did not opt into cursor resume (resume=%q)", got)
		}
		wsServe(t, w, r, []string{
			`{"type":"CURSOR","cursor":"c-1"}`,
			inFlightSpan,
			// Array batching plus full replay of the archived prefix.
			"[" + liveFrames[0] + "," + liveFrames[1] + "]",
			liveFrames[2],
		}, websocket.StatusNormalClosure)
	})
	// The archive already holds the first two frames when watch drains it.
	api := liveAPI(t, wsURLOf(ws), liveFrames[:2], 0, "completed")

	out, err := run(t, api, "runs", "watch", "r1")
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	for _, want := range []string{"hello from the phone", "Screen.observe", "Run r1 completed"} {
		if n := strings.Count(out, want); n != 1 {
			t.Fatalf("%q rendered %d times, want exactly once:\n%s", want, n, out)
		}
	}
	if n := strings.Count(out, "span  session"); n != 1 {
		t.Fatalf("session end frame rendered %d times, want once:\n%s", n, out)
	}
}

// A refused mint (session already over, or not eligible) must leave the
// polling path fully intact — live attach is never a new failure mode.
func TestRunsWatchMintRefusedFallsBackToPolling(t *testing.T) {
	fastWatch(t)
	api := liveAPI(t, "ws://unreachable.invalid", liveFrames, http.StatusConflict, "completed")
	out, err := run(t, api, "runs", "watch", "r1")
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	for _, want := range []string{"hello from the phone", "Run r1 completed"} {
		if n := strings.Count(out, want); n != 1 {
			t.Fatalf("%q rendered %d times, want exactly once:\n%s", want, n, out)
		}
	}
}

// A live leg that dies mid-stream (token rejected on redial) degrades to the
// archive: every frame still arrives exactly once and the watch completes.
func TestRunsWatchLiveFailureFallsBack(t *testing.T) {
	fastWatch(t)
	ws := fakeTelemetryWS(t, func(conn int, w http.ResponseWriter, r *http.Request) {
		if conn == 1 {
			// One frame, then an abrupt drop (no close frame).
			wsServe(t, w, r, liveFrames[:1], websocket.StatusInternalError)
			return
		}
		// The redial: token now rejected — a terminal, non-retryable refusal.
		http.Error(w, "token rejected", http.StatusUnauthorized)
	})
	api := liveAPI(t, wsURLOf(ws), liveFrames, 0, "completed")

	out, err := run(t, api, "runs", "watch", "r1")
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	for _, want := range []string{"hello from the phone", "Screen.observe", "Run r1 completed"} {
		if n := strings.Count(out, want); n != 1 {
			t.Fatalf("%q rendered %d times, want exactly once:\n%s", want, n, out)
		}
	}
}

// --follow renders the archived table, appends live rows with COST "-", and
// ends cleanly on the live end frame.
func TestSessionsTraceFollowTable(t *testing.T) {
	fastWatch(t)
	ws := fakeTelemetryWS(t, func(conn int, w http.ResponseWriter, r *http.Request) {
		wsServe(t, w, r, []string{
			`{"kind":"span","trace_id":"t1","span_id":"s-live","span_type":"sdk_call","name":"Screen.tap","phase":"end","start_time_unix_nano":1755900001500000000,"end_time_unix_nano":1755900001600000000,"status":{"code":"ok","message":""}}`,
			liveFrames[2],
		}, websocket.StatusNormalClosure)
	})
	api := liveAPI(t, wsURLOf(ws), liveFrames[:2], 0, "completed")

	out, err := run(t, api, "sessions", "trace", "sess-1", "--follow")
	if err != nil {
		t.Fatalf("trace --follow: %v", err)
	}
	// Archived span keeps its cost join; the live-only span shows "-".
	if !strings.Contains(out, "$0.0031") {
		t.Fatalf("archived cost join missing:\n%s", out)
	}
	liveLine := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Screen.tap") {
			liveLine = line
		}
	}
	if liveLine == "" || !strings.HasSuffix(strings.TrimSpace(liveLine), "-") {
		t.Fatalf("live row missing or does not render COST as '-': %q\n%s", liveLine, out)
	}
	if n := strings.Count(out, "Screen.observe"); n != 1 {
		t.Fatalf("archived span rendered %d times, want once:\n%s", n, out)
	}
}

// --follow in JSON mode is NDJSON: every line one document — archived frames,
// live frames, then the trace_end summary with the merged cost maps.
func TestSessionsTraceFollowJSONContract(t *testing.T) {
	fastWatch(t)
	ws := fakeTelemetryWS(t, func(conn int, w http.ResponseWriter, r *http.Request) {
		wsServe(t, w, r, []string{liveFrames[2]}, websocket.StatusNormalClosure)
	})
	api := liveAPI(t, wsURLOf(ws), liveFrames[:2], 0, "completed")

	out, err := run(t, api, "-o", "json", "sessions", "trace", "sess-1", "--follow")
	if err != nil {
		t.Fatalf("trace --follow: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != len(liveFrames)+1 {
		t.Fatalf("got %d NDJSON lines, want %d frames + 1 summary:\n%s", len(lines), len(liveFrames), out)
	}
	for i, line := range lines {
		var doc map[string]any
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			t.Fatalf("line %d is not JSON: %v\n%q", i, err, line)
		}
	}
	var last map[string]any
	_ = json.Unmarshal([]byte(lines[len(lines)-1]), &last)
	if last["trace_end"] != true || last["session_ended"] != true {
		t.Fatalf("unexpected trace_end summary: %v", last)
	}
	costs, _ := last["sdk_call_costs"].(map[string]any)
	if costs["s-call"] != float64(3100) {
		t.Fatalf("trace_end summary lost the cost join: %v", last)
	}
}

// --follow on a session that is no longer active (mint refused, archive has
// no end frame) drains once, says so, and exits cleanly.
func TestSessionsTraceFollowInactiveSession(t *testing.T) {
	fastWatch(t)
	api := liveAPI(t, "ws://unreachable.invalid", liveFrames[:2], http.StatusConflict, "completed")

	out, err := run(t, api, "sessions", "trace", "sess-1", "--follow")
	if err != nil {
		t.Fatalf("trace --follow: %v", err)
	}
	if n := strings.Count(out, "Screen.observe"); n != 1 {
		t.Fatalf("archived span rendered %d times, want once:\n%s", n, out)
	}
}
