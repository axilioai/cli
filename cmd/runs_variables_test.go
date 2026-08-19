package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

// --variables must be a JSON object; anything else is a usage error caught
// before credentials or any HTTP request, mirroring the --count preflight.
func TestRunsStartRejectsInvalidVariablesBeforeRequest(t *testing.T) {
	_, requests := noRequestServer(t)
	for _, v := range []string{"not-json", "[1,2]", `"str"`, "42", "null"} {
		out, err := execRoot(t, "runs", "start", "w1", "--variables", v)
		if err == nil || exit.Classify(err) != exit.Usage {
			t.Fatalf("--variables %q: got %v, want usage error", v, err)
		}
		if out != "" {
			t.Fatalf("--variables %q wrote stdout on failure: %q", v, out)
		}
	}
	if *requests != 0 {
		t.Fatalf("invalid variables made %d HTTP requests", *requests)
	}
}

// --variables rides every created run config in the one-element-array wire
// shape; without the flag each config still carries a single empty object.
func TestRunsStartSendsVariables(t *testing.T) {
	type runConfig struct {
		Variables []map[string]any `json:"variables"`
	}
	var got struct {
		Runs []runConfig `json:"runs"`
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
	if len(got.Runs) != 2 {
		t.Fatalf("got %d run configs, want 2", len(got.Runs))
	}
	for i, rc := range got.Runs {
		if len(rc.Variables) != 1 {
			t.Fatalf("run %d: variables wrapper length %d, want 1", i, len(rc.Variables))
		}
		if rc.Variables[0]["who"] != "cli" || rc.Variables[0]["n"] != float64(1) {
			t.Fatalf("run %d carried variables %v, want the --variables object", i, rc.Variables[0])
		}
	}

	got.Runs = nil
	if _, err := run(t, srv, "runs", "start", "w1"); err != nil {
		t.Fatalf("runs start (no variables): %v", err)
	}
	if len(got.Runs) != 1 || len(got.Runs[0].Variables) != 1 || len(got.Runs[0].Variables[0]) != 0 {
		t.Fatalf("default run config = %+v, want one empty variables object", got.Runs)
	}
}
