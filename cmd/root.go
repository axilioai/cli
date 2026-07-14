// Package cmd wires the axilio CLI command tree.
package cmd

import (
	"os"

	"github.com/axilioai/cli/internal/config"
	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/util"
	"github.com/axilioai/platform-go/client"
	"github.com/axilioai/platform-go/option"
	"github.com/spf13/cobra"
)

// Build metadata, stamped by goreleaser via -ldflags -X at release time and
// left at these defaults for local/dev builds.
var (
	Version = "dev"
	Commit  = "none"
)

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
	}
	pf := root.PersistentFlags()
	pf.StringVarP(&flagOutput, "output", "o", "table", "Output format: table or json")
	pf.BoolVar(&flagNoColor, "no-color", false, "Disable colored output")
	pf.BoolVarP(&flagQuiet, "quiet", "q", false, "Suppress stderr chrome (notes/prompts) for non-interactive use")
	pf.StringVar(&flagAPIKey, "api-key", "", "Override the API key for this call")
	pf.StringVar(&flagBaseURL, "base-url", "", "Override the API host")
	pf.StringVar(&flagOrg, "org", "", "Organization slug (reserved for multi-org keys)")

	root.AddCommand(loginCmd(), logoutCmd(), statusCmd(), doctorCmd(), configCmd(), sessionsCmd(), phonesCmd(), phoneCmd(), runsCmd(), apiKeysCmd())
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

// newClient builds an authenticated SDK client, or a friendly error when no key is set.
func newClient() (*client.Client, error) {
	key, host := resolvedCreds()
	if key == "" {
		return nil, exit.Authf("no API key found; run `axilio login` or set AXILIO_API_KEY")
	}
	return client.NewClient(option.WithAPIKey(key), option.WithBaseURL(sdkBaseURL(host))), nil
}
