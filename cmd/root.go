// Package cmd wires the axilio CLI command tree.
package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/axilioai/cli/internal/config"
	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/oauth"
	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/update"
	"github.com/axilioai/cli/internal/util"
	"github.com/axilioai/platform-go/client"
	"github.com/axilioai/platform-go/option"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Build metadata, stamped by goreleaser via -ldflags -X at release time and
// filled from the Go build's VCS stamp otherwise (so a source build still
// reports a real commit and date).
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// versionString renders the full version line, matching a conventional CLI
// format: "<version> (<commit>) <go-version> <build-date>". When -ldflags did
// not set the commit/date (a plain `go build` or `go install`), they are read
// from the module's embedded VCS stamp.
func versionString() string {
	v, commit, date := Version, Commit, Date
	fromLdflags := commit != "" // a release build; its commit is authoritative
	if info, ok := debug.ReadBuildInfo(); ok {
		var dirty bool
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if commit == "" {
					commit = s.Value
				}
			case "vcs.time":
				if date == "" {
					date = s.Value
				}
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		// Only mark a VCS-derived commit dirty; never an ldflags-pinned one.
		if dirty && !fromLdflags && commit != "" {
			commit += "-dirty"
		}
	}
	if commit == "" {
		commit = "unknown"
	}
	if date == "" {
		date = "unknown"
	}
	return fmt.Sprintf("%s (%s) %s %s", v, commit, runtime.Version(), date)
}

// Persistent (global) flags, resolved once for every command.
var (
	flagOutput  string
	flagNoColor bool
	flagQuiet   bool
	flagAPIKey  string
	flagBaseURL string
	flagOrg     string
)

// Root builds the root command with its global flags and subcommands.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:           "axilio",
		Short:         "Acquire and drive Axilio phones from the command line.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			switch flagOutput {
			case "", "table", "json":
				return nil
			default:
				return exit.Usagef("invalid --output %q (want table or json)", flagOutput)
			}
		},
		// Runs only after a command succeeds. A passive, once-a-day upgrade
		// nudge on stderr; suppressed in quiet/JSON and non-interactive shells.
		PersistentPostRun: func(cmd *cobra.Command, _ []string) {
			if flagQuiet || flagOutput == "json" || !term.IsTerminal(int(os.Stderr.Fd())) {
				return
			}
			update.Notify(cmd.Context(), os.Stderr, Version)
		},
	}
	pf := root.PersistentFlags()
	pf.StringVarP(&flagOutput, "output", "o", "table", "Output format: table or json")
	pf.BoolVar(&flagNoColor, "no-color", false, "Disable colored output")
	pf.BoolVarP(&flagQuiet, "quiet", "q", false, "Suppress stderr chrome (notes/prompts) for non-interactive use")
	pf.StringVar(&flagAPIKey, "api-key", "", "Override the API key for this call")
	pf.StringVar(&flagBaseURL, "base-url", "", "Override the API host")
	pf.StringVar(&flagOrg, "org", "", "Organization slug or id to act as for this call (OAuth sessions only; overrides `org use`)")

	root.AddCommand(loginCmd(), logoutCmd(), statusCmd(), doctorCmd(), configCmd(), orgCmd(), upgradeCmd(), sessionsCmd(), phonesCmd(), phoneCmd(), runsCmd(), apiKeysCmd())

	// Own the --version output (fang truncates the commit and adds a "version"
	// word); cobra adds the --version flag when root.Version is set.
	root.Version = versionString()
	root.SetVersionTemplate("{{.Name}} {{.Version}}\n")
	return root
}

func printer() *output.Printer { return output.New(flagOutput, flagNoColor, flagQuiet) }

// resolvedCreds applies flag > env > config precedence for the key and host.
func resolvedCreds() (apiKey, baseURL string) {
	cfg := config.Load()
	apiKey = util.FirstNonEmpty(flagAPIKey, os.Getenv("AXILIO_API_KEY"), cfg.APIKey)
	baseURL = util.FirstNonEmpty(flagBaseURL, os.Getenv("AXILIO_BASE_URL"), cfg.BaseURL)
	return apiKey, baseURL
}

// sdkBaseURL turns a host (or empty) into the base URL the SDK expects: the SDK's
// generated default is just "/api/v1", so it needs the host prepended.
func sdkBaseURL(host string) string {
	if host == "" {
		host = "https://api.axilio.ai"
	}
	return host + "/api/v1"
}

// newClient builds an authenticated SDK client. An explicit API key (flag / env
// / config) wins; otherwise it uses a stored OAuth session (refreshed
// proactively). A friendly error results when neither is present.
func newClient() (*client.Client, error) {
	key, host := resolvedCreds()
	base := sdkBaseURL(host)
	if key != "" {
		return client.NewClient(option.WithAPIKey(key), option.WithBaseURL(base), option.WithHTTPHeader(cliHeader(""))), nil
	}
	apiHost := util.FirstNonEmpty(host, defaultAPIHost)
	if tok, err := oauth.ValidAccessToken(context.Background(), apiHost); err == nil {
		return client.NewClient(option.WithBaseURL(base), option.WithHTTPHeader(cliHeader(tok))), nil
	}
	return nil, exit.Authf("not signed in; run `axilio login` or set AXILIO_API_KEY")
}

// cliHeader builds the per-request headers: the CLI version on every request
// (X-Axilio-Cli-Version, for support/telemetry); the active org selector when
// one is set (X-Axilio-Org, honored only for OAuth sessions — AXI-1280); plus
// an OAuth Bearer when a token is supplied.
func cliHeader(bearerToken string) http.Header {
	h := http.Header{}
	h.Set("X-Axilio-Cli-Version", versionString())
	if org := resolvedOrg(); org != "" {
		h.Set("X-Axilio-Org", org)
	}
	if bearerToken != "" {
		h.Set("Authorization", "Bearer "+bearerToken)
	}
	return h
}

// resolvedOrg applies flag > env > config precedence for the active org selector
// (an org slug or id). It is sent as X-Axilio-Org and re-scopes an OAuth session
// to another org the user belongs to; API-key auth ignores it (keys are
// single-org). Empty means "use the session's default org".
func resolvedOrg() string {
	return util.FirstNonEmpty(flagOrg, os.Getenv("AXILIO_ORG"), config.Load().ActiveOrg)
}

// dashboardBaseURL is where the CLI opens the OAuth consent page. The consent
// page lives on the dashboard host, not the API host, so derive it from the API
// host (api.axilio.ai -> app.axilio.ai); AXILIO_DASHBOARD_URL overrides.
func dashboardBaseURL(apiHost string) string {
	if v := strings.TrimSpace(os.Getenv("AXILIO_DASHBOARD_URL")); v != "" {
		return v
	}
	if u, err := url.Parse(apiHost); err == nil && u.Host != "" {
		u.Host = strings.Replace(u.Host, "api", "app", 1)
		return u.Scheme + "://" + u.Host
	}
	return "https://app.axilio.ai"
}
