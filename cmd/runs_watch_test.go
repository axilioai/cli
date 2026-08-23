package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/axilioai/cli/internal/exit"
)

// watchFrames is the scripted session archive: a log, an sdk_call span, a
// frame of an unknown kind (the tolerant-reader case), and the session-root
// end frame that terminates the watch.
var watchFrames = []string{
	`{"kind":"log","trace_id":"t1","time_unix_nano":1755900000000000000,"severity":"INFO","log_type":"output_log","body":"hello from the phone"}`,
	`{"kind":"span","trace_id":"t1","span_id":"s-call","span_type":"sdk_call","name":"Screen.observe","phase":"end","start_time_unix_nano":1755900001000000000,"end_time_unix_nano":1755900001250000000,"status":{"code":"ok","message":""},"attributes":{"axilio.duration_ns":250000000}}`,
	`{"kind":"hologram","trace_id":"t1","shimmer":true}`,
	`{"kind":"span","trace_id":"t1","span_id":"s-root","span_type":"session","name":"session","phase":"end","start_time_unix_nano":1755900000000000000,"end_time_unix_nano":1755900002000000000,"status":{"code":"ok","message":""}}`,
}

// watchAPI scripts a run lifecycle across polls: the first run read is queued
// with no session, later reads are running with session s1, and once the
// archive has been fully served the run reports its terminal status. Frames
// are served strictly by offset in two pages, so a watcher that fails to
// resume by offset renders duplicates and fails the ordering assertions.
func watchAPI(t *testing.T, terminal string, errorMessage string) *httptest.Server {
	t.Helper()
	runReads := 0
	frameReads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case strings.Contains(p, "/frames"):
			frameReads++
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			// Two pages: the live middle of the session, then the rest.
			end := 2
			if frameReads > 1 {
				end = len(watchFrames)
			}
			if offset > end {
				offset = end
			}
			fmt.Fprintf(w, `{"frames":[%s],"total":%d,"limit":1000,"offset":%d,"retention_expired":false,"sdk_call_costs":{},"inference_costs":{}}`,
				strings.Join(watchFrames[offset:end], ","), end, offset)
		case strings.Contains(p, "/runs/") && r.Method == http.MethodGet:
			runReads++
			session := `,"session_id":"sess-1"`
			status := "running"
			if runReads == 1 {
				session = ""
				status = "queued"
			}
			if frameReads >= 2 {
				status = terminal
			}
			msg := ""
			if errorMessage != "" {
				msg = fmt.Sprintf(`,"error_message":%q`, errorMessage)
			}
			fmt.Fprintf(w, `{"id":"r1","status":%q,"trigger":"manual","workflow_id":"w1","created_at":"2026-08-22T20:00:00Z"%s%s}`, status, session, msg)
		case strings.HasSuffix(p, "/runs") && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"run_ids":["r1"]}`)
		default:
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fastWatch(t *testing.T) {
	t.Helper()
	orig := watchPollInterval
	watchPollInterval = time.Millisecond
	t.Cleanup(func() { watchPollInterval = orig })
}

// The human stream renders every frame exactly once, in archive order,
// including the unknown kind, and ends successfully on the session end frame.
func TestRunsWatchStreamsToCompletion(t *testing.T) {
	fastWatch(t)
	srv := watchAPI(t, "completed", "")
	out, err := run(t, srv, "runs", "watch", "r1")
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	wantOrder := []string{
		"hello from the phone",
		"sdk_call",
		"hologram frame (unrecognized kind)",
		"span  session",
		"Run r1 completed",
	}
	at := 0
	for _, want := range wantOrder {
		idx := strings.Index(out[at:], want)
		if idx < 0 {
			t.Fatalf("output missing %q after byte %d:\n%s", want, at, out)
		}
		at += idx + len(want)
	}
	if n := strings.Count(out, "hello from the phone"); n != 1 {
		t.Fatalf("log frame rendered %d times, want exactly once (offset resume broken):\n%s", n, out)
	}
	if !strings.Contains(out, "250ms") {
		t.Fatalf("span line does not render the producer duration:\n%s", out)
	}
}

// JSON mode is newline-delimited: every line one valid JSON document — the
// frames verbatim, then the watch_end summary.
func TestRunsWatchJSONLinesContract(t *testing.T) {
	fastWatch(t)
	srv := watchAPI(t, "completed", "")
	out, err := run(t, srv, "-o", "json", "runs", "watch", "r1")
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != len(watchFrames)+1 {
		t.Fatalf("got %d NDJSON lines, want %d frames + 1 summary:\n%s", len(lines), len(watchFrames), out)
	}
	for i, line := range lines {
		var doc map[string]any
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			t.Fatalf("line %d is not JSON: %v\n%q", i, err, line)
		}
	}
	var last map[string]any
	_ = json.Unmarshal([]byte(lines[len(lines)-1]), &last)
	if last["watch_end"] != true || last["status"] != "completed" || last["run_id"] != "r1" {
		t.Fatalf("unexpected watch_end summary: %v", last)
	}
	// The unknown-kind frame must survive the round trip verbatim.
	var unknown map[string]any
	_ = json.Unmarshal([]byte(lines[2]), &unknown)
	if unknown["kind"] != "hologram" || unknown["shimmer"] != true {
		t.Fatalf("unknown frame did not round-trip its raw JSON: %v", unknown)
	}
}

// Terminal run states map onto the published exit-code contract: failed is a
// generic error carrying the run's message, cancelled is the canceled code.
func TestRunsWatchOutcomeExitCodes(t *testing.T) {
	cases := []struct {
		terminal string
		message  string
		want     exit.Code
	}{
		{"failed", "phone rebooted mid-run", exit.Err},
		{"cancelled", "", exit.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.terminal, func(t *testing.T) {
			fastWatch(t)
			srv := watchAPI(t, tc.terminal, tc.message)
			_, err := run(t, srv, "runs", "watch", "r1")
			if err == nil || exit.Classify(err) != tc.want {
				t.Fatalf("terminal %s: got %v (code %d), want code %d", tc.terminal, err, exit.Classify(err), tc.want)
			}
			if tc.message != "" && !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("failed-run error does not carry the run's message: %v", err)
			}
		})
	}
}

// --watch on runs start follows the run it created; with any other count the
// flag is a preflight usage error that sends nothing over the wire.
func TestRunsStartWatchFlag(t *testing.T) {
	t.Run("count preflight", func(t *testing.T) {
		_, requests := noRequestServer(t)
		_, err := execRoot(t, "runs", "start", "w1", "--watch", "--count", "2")
		if err == nil || exit.Classify(err) != exit.Usage {
			t.Fatalf("got %v, want usage error", err)
		}
		if *requests != 0 {
			t.Fatalf("made %d HTTP requests before the usage error", *requests)
		}
	})
	t.Run("follows created run", func(t *testing.T) {
		fastWatch(t)
		srv := watchAPI(t, "completed", "")
		out, err := run(t, srv, "runs", "start", "w1", "--watch")
		if err != nil {
			t.Fatalf("start --watch: %v", err)
		}
		for _, want := range []string{"Started run r1", "hello from the phone", "Run r1 completed"} {
			if !strings.Contains(out, want) {
				t.Fatalf("output missing %q:\n%s", want, out)
			}
		}
	})
}
