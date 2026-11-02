package cmd

import (
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

// The reporting-window flags are validated before credentials or any HTTP
// request: --from is required on usage, both flags must parse, and the window must be
// ordered. Shared by usage metrics/inferences and runs metrics.
func TestWindowFlagsRejectBeforeRequest(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"metrics missing from", []string{"usage", "metrics"}},
		{"metrics malformed from", []string{"usage", "metrics", "--from", "yesterday"}},
		{"metrics inverted window", []string{"usage", "metrics", "--from", "2026-08-02", "--to", "2026-08-01"}},
		{"metrics bad granularity", []string{"usage", "metrics", "--from", "2026-08-01", "--granularity", "weekly"}},
		{"inferences bad endpoint", []string{"usage", "inferences", "--from", "2026-08-01", "--endpoint", "classify"}},
		{"runs metrics malformed from", []string{"runs", "metrics", "--from", "yesterday"}},
		{"runs metrics inverted window", []string{"runs", "metrics", "--from", "2026-08-02", "--to", "2026-08-01"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, requests := noRequestServer(t)
			_, err := execRoot(t, tc.args...)
			if err == nil || exit.Classify(err) != exit.Usage {
				t.Fatalf("%v: got %v, want usage error", tc.args, err)
			}
			if *requests != 0 {
				t.Fatalf("%v made %d HTTP requests", tc.args, *requests)
			}
		})
	}
}

// Inference costs arrive in microdollars; the table must render them as
// dollars so an agent reading the column does not misjudge spend by 10^6.
func TestUsageInferencesRendersMicrodollarsAsDollars(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "usage", "inferences", "--from", "2026-08-01")
	if err != nil {
		t.Fatalf("usage inferences: %v", err)
	}
	if !strings.Contains(out, "$0.0012") {
		t.Fatalf("cost column did not render 1234 microdollars as $0.0012:\n%s", out)
	}
	if !strings.Contains(out, "321 ms") {
		t.Fatalf("latency column missing:\n%s", out)
	}
}

// Metrics arrive as per-bucket microdollar points; the summary sums them.
func TestUsageMetricsSumsPointsIntoDollars(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "usage", "metrics", "--from", "2026-08-01", "--to", "2026-08-03")
	if err != nil {
		t.Fatalf("usage metrics: %v", err)
	}
	for _, want := range []string{"$1.25", "$0.75", "$2.00"} {
		if !strings.Contains(out, want) {
			t.Fatalf("summary missing %q:\n%s", want, out)
		}
	}
}
