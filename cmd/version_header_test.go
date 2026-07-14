package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Every request must carry X-Axilio-Cli-Version (support / telemetry signal).
func TestCLIVersionHeaderSent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("X-Axilio-Cli-Version"); v != "" {
			got = v
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"balance_display":"$0.00","balance_microdollars":0}`)
	}))
	t.Cleanup(srv.Close)

	if _, err := run(t, srv, "-o", "json", "status"); err != nil {
		t.Fatalf("status: %v", err)
	}
	if got == "" {
		t.Fatal("X-Axilio-Cli-Version header was not sent")
	}
}
