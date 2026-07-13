// Package cmd wires the axilio CLI command tree.
package cmd

import (
	"fmt"
	"os"

	"github.com/axilioai/cli/internal/api"
	"github.com/axilioai/cli/internal/config"
	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/util"
	"github.com/spf13/cobra"
)

// Persistent (global) flags, resolved once for every command.
var (
	flagOutput  string
	flagNoColor bool
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
				return fmt.Errorf("invalid --output %q (want table or json)", flagOutput)
			}
		},
	}
	pf := root.PersistentFlags()
	pf.StringVarP(&flagOutput, "output", "o", "table", "Output format: table or json")
	pf.BoolVar(&flagNoColor, "no-color", false, "Disable coloured output")
	pf.StringVar(&flagAPIKey, "api-key", "", "Override the API key for this call")
	pf.StringVar(&flagBaseURL, "base-url", "", "Override the API host")
	pf.StringVar(&flagOrg, "org", "", "Organization slug (reserved for multi-org keys)")

	root.AddCommand(loginCmd(), logoutCmd(), statusCmd(), sessionsCmd(), phonesCmd())
	return root
}

func printer() *output.Printer { return output.New(flagOutput, flagNoColor) }

// resolvedCreds applies flag > env > config precedence for the key and host.
func resolvedCreds() (apiKey, baseURL string) {
	cfg := config.Load()
	apiKey = util.FirstNonEmpty(flagAPIKey, os.Getenv("AXILIO_API_KEY"), cfg.APIKey)
	baseURL = util.FirstNonEmpty(flagBaseURL, os.Getenv("AXILIO_BASE_URL"), cfg.BaseURL)
	return apiKey, baseURL
}

// client builds an authenticated client, or a friendly error when no key is set.
func client() (*api.Client, error) {
	key, base := resolvedCreds()
	if key == "" {
		return nil, fmt.Errorf("no API key found; run `axilio login` or set AXILIO_API_KEY")
	}
	return api.New(key, base), nil
}
