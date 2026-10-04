package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/session"
	"github.com/axilioai/platform-go/drivers/mobile"
	"github.com/coder/websocket"
)

// dcpFrame is one command the fake phone received.
type dcpFrame struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// dcpReply is what the fake phone answers a command with: a result, or a
// DCP error of the given kind.
type dcpReply struct {
	result any
	kind   string
}

// fakePhone is a DCP control socket: it records every command and answers
// each with respond(method). It is the phone-side test seam, the websocket
// twin of fakeAPI.
type fakePhone struct {
	mu   sync.Mutex
	sent []dcpFrame
	srv  *httptest.Server
}

func newFakePhone(t *testing.T, respond func(method string) dcpReply) *fakePhone {
	t.Helper()
	fp := &fakePhone{}
	fp.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		ctx := context.Background()
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var f dcpFrame
			if err := json.Unmarshal(data, &f); err != nil {
				t.Errorf("fake phone: bad frame %s", data)
				return
			}
			fp.mu.Lock()
			fp.sent = append(fp.sent, f)
			fp.mu.Unlock()
			reply := respond(f.Method)
			out := map[string]any{"id": f.ID}
			if reply.kind != "" {
				out["error"] = map[string]any{"code": -32000, "message": "fake " + reply.kind, "data": map[string]any{"kind": reply.kind}}
			} else {
				out["result"] = reply.result
			}
			b, _ := json.Marshal(out)
			if err := c.Write(ctx, websocket.MessageText, b); err != nil {
				return
			}
		}
	}))
	t.Cleanup(fp.srv.Close)
	return fp
}

func (fp *fakePhone) frames() []dcpFrame {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	return append([]dcpFrame(nil), fp.sent...)
}

// runPhone saves a session pointing at the fake phone and runs a phone
// command against it, capturing stdout.
func runPhone(t *testing.T, fp *fakePhone, args ...string) (string, error) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(session.EnvVar, "")
	controlURL := "ws" + strings.TrimPrefix(fp.srv.URL, "http") + "/control?token=t"
	if err := session.Save(session.Session{SessionID: "s1", PhoneID: "p1", ControlURL: controlURL}); err != nil {
		t.Fatalf("save session: %v", err)
	}

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

func locatorResult() dcpReply {
	return dcpReply{result: map[string]any{
		"resolvedBy": "a11y",
		"bounds":     map[string]int{"x": 80, "y": 1420, "width": 920, "height": 120},
		"tookMs":     95,
	}}
}

func TestPhoneTapByRoleSendsTreeLocator(t *testing.T) {
	fp := newFakePhone(t, func(string) dcpReply { return locatorResult() })
	out, err := runPhone(t, fp, "phone", "tap", "--role", "button", "--name", "Log in", "--exact", "--strategy", "accessibility")
	if err != nil {
		t.Fatalf("phone tap: %v", err)
	}
	if !strings.Contains(out, `Tapped button "Log in" at 540,1480`) {
		t.Fatalf("unexpected output: %q", out)
	}
	f := fp.frames()[0]
	if f.Method != "Locator.tap" {
		t.Fatalf("method = %q, want Locator.tap", f.Method)
	}
	var p struct {
		Locator  map[string]any `json:"locator"`
		Strategy string         `json:"strategy"`
	}
	_ = json.Unmarshal(f.Params, &p)
	if p.Locator["role"] != "button" || p.Locator["name"] != "Log in" || p.Locator["exact"] != true || p.Strategy != "accessibility" {
		t.Fatalf("bad params: %s", f.Params)
	}
}

func TestPhoneLocatorFlagValidation(t *testing.T) {
	fp := newFakePhone(t, func(string) dcpReply { return locatorResult() })
	cases := []struct {
		name string
		args []string
	}{
		{"unknown strategy", []string{"phone", "find", "--role", "button", "--strategy", "magic"}},
		{"find with no target", []string{"phone", "find"}},
		{"wait-for with no target", []string{"phone", "wait-for"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := runPhone(t, fp, c.args...)
			if got := exit.Classify(err); got != exit.Usage {
				t.Fatalf("exit = %d, want usage (2): %v", got, err)
			}
		})
	}
	if n := len(fp.frames()); n != 0 {
		t.Fatalf("a rejected invocation must send nothing, sent %d frames", n)
	}
}

func TestPhoneWaitForIDGone(t *testing.T) {
	fp := newFakePhone(t, func(string) dcpReply { return dcpReply{result: map[string]any{"tookMs": 10}} })
	out, err := runPhone(t, fp, "-o", "json", "phone", "wait-for", "--id", "com.example.app:id/spinner", "--gone")
	if err != nil {
		t.Fatalf("phone wait-for: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	if got["id"] != "com.example.app:id/spinner" || got["gone"] != true {
		t.Fatalf("unexpected JSON: %v", got)
	}
	var p struct {
		Locator map[string]any `json:"locator"`
		State   string         `json:"state"`
	}
	_ = json.Unmarshal(fp.frames()[0].Params, &p)
	if p.Locator["id"] != "com.example.app:id/spinner" || p.State != "hidden" {
		t.Fatalf("bad params: %s", fp.frames()[0].Params)
	}
}

func TestPhoneTreeOutlineAndOptions(t *testing.T) {
	tree := map[string]any{
		"nodes": []any{
			map[string]any{"nodeId": "n1", "ignored": false, "role": map[string]any{"type": "role", "value": "generic"},
				"childIds": []string{"n7"}, "bounds": map[string]int{"x": 0, "y": 0, "width": 1080, "height": 2400},
				"windowId": "12", "actions": []string{}},
			map[string]any{"nodeId": "n7", "ignored": false, "role": map[string]any{"type": "role", "value": "button"},
				"name": map[string]any{"type": "computedString", "value": "Log in"}, "parentId": "n1", "childIds": []string{},
				"bounds": map[string]int{"x": 80, "y": 1420, "width": 920, "height": 120}, "windowId": "12", "actions": []string{"click"},
				"platform": map[string]any{"android": map[string]any{"viewIdResourceName": "com.example.app:id/login"}}},
		},
		"windows": []any{map[string]any{"windowId": "12", "type": "application", "app": "com.example.app", "focused": true,
			"bounds": map[string]int{"x": 0, "y": 0, "width": 1080, "height": 2400}, "rootId": "n1"}},
		"capturedAt": 1_700_000_000_000,
	}
	fp := newFakePhone(t, func(string) dcpReply { return dcpReply{result: tree} })
	out, err := runPhone(t, fp, "phone", "tree", "--window", "12", "--all", "--depth", "0")
	if err != nil {
		t.Fatalf("phone tree: %v", err)
	}
	want := "window 12 [application]  com.example.app  focused\n" +
		"  generic @540,1200 [n1]\n" +
		"    button \"Log in\" #com.example.app:id/login @540,1480 [n7]\n"
	if out != want {
		t.Fatalf("outline:\n%s\nwant:\n%s", out, want)
	}
	f := fp.frames()[0]
	var p map[string]any
	_ = json.Unmarshal(f.Params, &p)
	if f.Method != "Accessibility.getFullAXTree" || p["windowId"] != "12" || p["interestingOnly"] != false || p["depth"] != float64(0) {
		t.Fatalf("bad snapshot request: %s %s", f.Method, f.Params)
	}
}

func TestPhoneTreeWithTreeOffIsUsage(t *testing.T) {
	fp := newFakePhone(t, func(string) dcpReply { return dcpReply{kind: "StrategyUnavailable"} })
	_, err := runPhone(t, fp, "phone", "tree")
	if got := exit.Classify(err); got != exit.Usage {
		t.Fatalf("exit = %d, want usage (2): %v", got, err)
	}
}

func TestPhoneAccessibilityStatusAndToggle(t *testing.T) {
	fp := newFakePhone(t, func(method string) dcpReply {
		if method == "Accessibility.getState" {
			return dcpReply{result: map[string]any{"enabled": false, "toggleable": true}}
		}
		return dcpReply{result: map[string]any{}}
	})
	out, err := runPhone(t, fp, "-o", "json", "phone", "accessibility", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var st mobile.AccessibilityState
	if err := json.Unmarshal([]byte(out), &st); err != nil || st.Enabled || !st.Toggleable {
		t.Fatalf("bad status JSON (%v): %s", err, out)
	}
	out, err = runPhone(t, fp, "phone", "accessibility", "enable")
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if strings.TrimSpace(out) != "Accessibility on." {
		t.Fatalf("enable output = %q", out)
	}
	if _, err := runPhone(t, fp, "phone", "accessibility", "disable"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	var methods []string
	for _, f := range fp.frames() {
		methods = append(methods, f.Method)
	}
	if got := strings.Join(methods, ","); got != "Accessibility.getState,Accessibility.enable,Accessibility.disable" {
		t.Fatalf("methods = %s", got)
	}
}

func TestSessionsStartAccessibilityFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string // the raw accessibility value in the request, "" when omitted
	}{
		{"omitted", nil, ""},
		{"required", []string{"--accessibility"}, "true"},
		{"off", []string{"--no-accessibility"}, "false"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got json.RawMessage
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				_ = json.NewDecoder(r.Body).Decode(&body)
				got = body["accessibility"]
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"phone_id":"p1","session_id":"s1","workflow_started_at":"2026-08-03T00:00:00Z","accessibility":true}`)
			}))
			t.Cleanup(srv.Close)
			args := append([]string{"sessions", "start"}, c.args...)
			if _, err := run(t, srv, args...); err != nil {
				t.Fatalf("sessions start: %v", err)
			}
			if string(got) != c.want {
				t.Fatalf("accessibility in request = %q, want %q", got, c.want)
			}
		})
	}
}

func TestSessionsStartAccessibilityErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"status":409,"title":"Conflict","detail":"accessibility_unavailable: phone \"ph_old\" does not support accessibility mode"}`)
	}))
	t.Cleanup(srv.Close)

	_, err := run(t, srv, "sessions", "start", "--phone-id", "ph_old", "--accessibility")
	if got := exit.Classify(err); got != exit.Unavailable {
		t.Fatalf("exit = %d, want unavailable (6): %v", got, err)
	}
	if !strings.Contains(err.Error(), "does not support accessibility mode") {
		t.Fatalf("error = %v", err)
	}

	_, err = run(t, srv, "sessions", "start", "--accessibility", "--no-accessibility")
	if got := exit.Classify(err); got != exit.Usage {
		t.Fatalf("both flags: exit = %d, want usage (2): %v", got, err)
	}
}

// A bad locator invocation is a usage error even with no session to drive:
// the flags are checked before the session is resolved.
func TestPhoneLocatorValidationPrecedesSessionResolution(t *testing.T) {
	cases := [][]string{
		{"phone", "find"},
		{"phone", "wait-for"},
		{"phone", "find", "--role", "button", "--strategy", "magic"},
		{"phone", "tap", "--role", "button", "--strategy", "magic"},
		{"phone", "wait-for", "--id", "x", "--strategy", "magic"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv(session.EnvVar, "")
			root := Root()
			root.SetArgs(args)
			err := root.Execute()
			if got := exit.Classify(err); got != exit.Usage {
				t.Fatalf("exit = %d, want usage (2): %v", got, err)
			}
		})
	}
}
