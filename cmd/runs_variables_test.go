package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

// parseRunVariables accepts exactly one JSON object (or nothing) and rejects
// every other JSON shape as a usage error.
func TestParseRunVariables(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    map[string]any
		wantErr bool
	}{
		{"empty flag", "", map[string]any{}, false},
		{"empty object", "{}", map[string]any{}, false},
		{"flat object", `{"who":"cli","n":1}`, map[string]any{"who": "cli", "n": float64(1)}, false},
		{"nested object", `{"a":{"b":true}}`, map[string]any{"a": map[string]any{"b": true}}, false},
		{"malformed", "not-json", nil, true},
		{"array", "[1,2]", nil, true},
		{"string", `"str"`, nil, true},
		{"number", "42", nil, true},
		{"null", "null", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseRunVariables(tc.raw)
			if tc.wantErr {
				if err == nil || exit.Classify(err) != exit.Usage {
					t.Fatalf("parseRunVariables(%q): got %v, want usage error", tc.raw, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRunVariables(%q): %v", tc.raw, err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Fatalf("parseRunVariables(%q) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

// An invalid --variables value must fail as a usage error before credentials
// are read or any HTTP request is made, mirroring the --count preflight.
func TestRunsStartRejectsInvalidVariablesBeforeRequest(t *testing.T) {
	_, requests := noRequestServer(t)
	out, err := execRoot(t, "runs", "start", "w1", "--variables", "[1,2]")
	if err == nil || exit.Classify(err) != exit.Usage {
		t.Fatalf("--variables [1,2]: got %v, want usage error", err)
	}
	if out != "" {
		t.Fatalf("--variables [1,2] wrote stdout on failure: %q", out)
	}
	if *requests != 0 {
		t.Fatalf("invalid variables made %d HTTP requests", *requests)
	}
}

// --variables rides every created run config in the one-element-array wire
// shape; without the flag each config still carries a single empty object.
func TestRunsStartSendsVariables(t *testing.T) {
	var got struct {
		Runs []struct {
			Variables []map[string]any `json:"variables"`
		} `json:"runs"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/runs") && r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&got)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"run_ids":["r1","r2"]}`))
			return
		}
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	if _, err := run(t, srv, "runs", "start", "w1", "--count", "2", "--variables", `{"who":"cli","n":1}`); err != nil {
		t.Fatalf("runs start --variables: %v", err)
	}
	want := []map[string]any{{"who": "cli", "n": float64(1)}}
	if len(got.Runs) != 2 {
		t.Fatalf("got %d run configs, want 2", len(got.Runs))
	}
	for i, rc := range got.Runs {
		if !reflect.DeepEqual(want, rc.Variables) {
			t.Fatalf("run %d carried variables %#v, want %#v", i, rc.Variables, want)
		}
	}

	got.Runs = nil
	if _, err := run(t, srv, "runs", "start", "w1"); err != nil {
		t.Fatalf("runs start (no variables): %v", err)
	}
	wantDefault := []map[string]any{{}}
	if len(got.Runs) != 1 || !reflect.DeepEqual(wantDefault, got.Runs[0].Variables) {
		t.Fatalf("default run config = %+v, want one empty variables object", got.Runs)
	}
}
