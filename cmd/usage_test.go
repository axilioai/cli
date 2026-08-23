package cmd

import (
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

// The reporting-window flags are validated before credentials or any HTTP
// request: --from is required, both flags must parse, and the window must be
// ordered. Shared by usage metrics/inferences and runs history.
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
		{"history missing from", []string{"runs", "history"}},
		{"history bad status", []string{"runs", "history", "--from", "2026-08-01", "--status", "exploded"}},
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

// The stats success_rate is a 0..1 fraction over finished runs only; the
// rendering multiplies by 100 and says what the denominator is.
func TestRunsStatsRendersRateAsPercentage(t *testing.T) {
	srv := fakeAPI(t)
	out, err := run(t, srv, "runs", "stats", "w1")
	if err != nil {
		t.Fatalf("runs stats: %v", err)
	}
	if !strings.Contains(out, "87.5% of finished runs") {
		t.Fatalf("success rate not rendered as a finished-runs percentage:\n%s", out)
	}
	if !strings.Contains(out, "8") {
		t.Fatalf("total runs missing:\n%s", out)
	}
}
