package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/axilioai/cli/internal/config"
	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/oauth"
	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/session"
	"github.com/axilioai/platform-go/client"
	"github.com/axilioai/platform-go/core"
	"github.com/spf13/cobra"
)

// probeTimeout bounds each network check so `doctor` never hangs on a dead host.
const probeTimeout = 8 * time.Second

// checkStatus is the outcome of a single doctor check.
type checkStatus string

const (
	statusOK   checkStatus = "ok"
	statusWarn checkStatus = "warn"
	statusFail checkStatus = "fail"
)

// check is one line of the doctor report.
type check struct {
	Name     string      `json:"name"`
	Status   checkStatus `json:"status"`
	Detail   string      `json:"detail,omitempty"`
	required bool        // a failed required check makes doctor exit non-zero.
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that your setup is sane: auth, connectivity, and environment.",
		Long: "Run a one-shot health check of the CLI's setup — credentials, API " +
			"reachability, account, and local environment. Exits non-zero if a " +
			"required check fails, so scripts and agents can gate on it. Use -o json " +
			"to parse the checks.",
		RunE: func(_ *cobra.Command, _ []string) error {
			checks := runDoctor(context.Background())

			p := printer()
			p.Emit(doctorResult(checks), func() {
				rows := [][]string{{"CHECK", "STATUS", "DETAIL"}}
				for _, c := range checks {
					rows = append(rows, []string{c.Name, string(c.Status), c.Detail})
				}
				output.Table(rows)
			})

			// Gate on required checks: return the worst one's coded error so the
			// exit status is both non-zero and meaningful (auth vs unavailable).
			if code, failed := worstFailure(checks); failed != nil {
				return exit.With(code, fmt.Errorf("doctor: %s", failed.Detail))
			}
			return nil
		},
	}
}

// doctorResult is the JSON envelope: an overall verdict plus the checks.
func doctorResult(checks []check) map[string]any {
	ok := true
	for _, c := range checks {
		if c.required && c.Status == statusFail {
			ok = false
		}
	}
	return map[string]any{"ok": ok, "checks": checks}
}

// worstFailure returns the exit code and the first failed required check, if any.
// Auth failures outrank connectivity so the most actionable cause wins.
func worstFailure(checks []check) (exit.Code, *check) {
	var connFail *check
	for i := range checks {
		c := &checks[i]
		if !c.required || c.Status != statusFail {
			continue
		}
		switch c.Name {
		case "Credentials", "Authentication":
			return exit.Auth, c
		case "Connectivity":
			connFail = c
		}
	}
	if connFail != nil {
		return exit.Unavailable, connFail
	}
	return exit.OK, nil
}

// runDoctor executes the checks in order and returns the report: the gating
// checks (auth, connectivity, account) first, informational environment last.
func runDoctor(ctx context.Context) []check {
	var checks []check

	key, source := credSource()
	switch {
	case key != "":
		checks = append(checks,
			check{Name: "Authentication", Status: statusOK, Detail: "method: api-key (source: " + source + ")"},
			check{Name: "Credentials", Status: statusOK, Detail: "API key present (axl_…)"},
		)
		// One authenticated call decides both connectivity and auth validity.
		checks = append(checks, verifyAPIKey(ctx)...)
	case oauth.HasSession():
		checks = append(checks,
			check{Name: "Authentication", Status: statusOK, Detail: "method: oauth (browser session)"},
			check{Name: "Credentials", Status: statusOK, Detail: "oauth session present"},
		)
		checks = append(checks, verifyAPIKey(ctx)...)
	default:
		checks = append(checks,
			check{Name: "Authentication", Status: statusOK, Detail: "method: none configured (api-key or oauth)"},
			check{Name: "Credentials", Status: statusFail, required: true,
				Detail: "not signed in; run `axilio login` or set AXILIO_API_KEY"},
			probeConnectivity(ctx),
		)
	}

	return append(checks, environmentChecks()...)
}

// environmentChecks are informational rows that never fail the command.
func environmentChecks() []check {
	cur := "none"
	if s, err := session.Resolve(""); err == nil {
		cur = s.SessionID + " (" + s.PhoneID + ")"
	}
	return []check{
		{Name: "CLI version", Status: statusOK, Detail: versionString()},
		{Name: "Config file", Status: statusOK, Detail: config.Path()},
		{Name: "Sessions dir", Status: statusOK, Detail: session.Dir()},
		{Name: "Current session", Status: statusOK, Detail: cur},
	}
}

// verifyAPIKey makes one authenticated call and derives Connectivity + Authentication
// + Account from the result. Reaching the server (any HTTP status) proves
// connectivity; a 401/403 means the key was rejected; a transport error means
// the host is unreachable.
func verifyAPIKey(ctx context.Context) []check {
	_, host := resolvedCreds()
	apiHost := sdkBaseURL(host)

	cl, err := newClient()
	if err != nil { // unreachable: key presence already established.
		return []check{{Name: "Connectivity", Status: statusFail, required: true, Detail: err.Error()}}
	}

	cctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	bal, err := cl.Billing.GetBalance(cctx)
	if err == nil {
		checks := []check{
			{Name: "Connectivity", Status: statusOK, Detail: apiHost + " reachable"},
			{Name: "Account", Status: statusOK, Detail: "balance: " + bal.BalanceDisplay},
		}
		if plan := planDetail(cctx, cl); plan != "" {
			checks = append(checks, check{Name: "Plan", Status: statusOK, Detail: plan})
		}
		return checks
	}

	var apiErr *core.APIError
	if errors.As(err, &apiErr) { // we reached the server.
		conn := check{Name: "Connectivity", Status: statusOK, Detail: apiHost + " reachable"}
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return []check{conn, {Name: "Authentication", Status: statusFail, required: true,
				Detail: fmt.Sprintf("API key rejected (HTTP %d); run `axilio login`", apiErr.StatusCode)}}
		default:
			return []check{conn, {Name: "Account", Status: statusWarn,
				Detail: fmt.Sprintf("balance check failed (HTTP %d)", apiErr.StatusCode)}}
		}
	}
	// No HTTP response: transport/dial failure.
	return []check{{Name: "Connectivity", Status: statusFail, required: true,
		Detail: apiHost + " unreachable: " + err.Error()}}
}

// planDetail returns a best-effort plan summary; empty when the subscription
// call fails (non-gating — some accounts or transient errors just omit it).
func planDetail(ctx context.Context, cl *client.Client) string {
	sub, err := cl.Billing.GetSubscription(ctx)
	if err != nil || sub == nil {
		return ""
	}
	return fmt.Sprintf("%s (%s, up to %d concurrent)", sub.PlanName, sub.Status, sub.MaxConcurrentRuns)
}

// probeConnectivity tests raw reachability without credentials (the no-key path).
func probeConnectivity(ctx context.Context) check {
	_, host := resolvedCreds()
	apiHost := sdkBaseURL(host)
	cctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, apiHost, nil)
	if err != nil {
		return check{Name: "Connectivity", Status: statusWarn, Detail: "could not build request: " + err.Error()}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return check{Name: "Connectivity", Status: statusWarn, Detail: apiHost + " unreachable: " + err.Error()}
	}
	_ = resp.Body.Close()
	return check{Name: "Connectivity", Status: statusOK, Detail: apiHost + " reachable"}
}

// credSource resolves the API key and names where it came from (flag > env > config).
func credSource() (key, source string) {
	if flagAPIKey != "" {
		return flagAPIKey, "flag"
	}
	if v := os.Getenv("AXILIO_API_KEY"); v != "" {
		return v, "env (AXILIO_API_KEY)"
	}
	if cfg := config.Load(); cfg.APIKey != "" {
		return cfg.APIKey, "config"
	}
	return "", ""
}
