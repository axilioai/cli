package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/axilioai/cli/internal/config"
	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/platform-go/client"
	"github.com/axilioai/platform-go/option"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func loginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Store and verify your API key.",
		RunE: func(_ *cobra.Command, _ []string) error {
			key := flagAPIKey
			if key == "" {
				fmt.Fprint(os.Stderr, "Axilio API key (axl_...): ")
				b, _ := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Fprintln(os.Stderr)
				key = strings.TrimSpace(string(b))
			}
			if key == "" {
				return fmt.Errorf("no key provided")
			}
			if !strings.HasPrefix(key, "axl_") {
				return fmt.Errorf("that does not look like an Axilio key (expected an axl_... value)")
			}

			// Verify the key against the API before persisting it.
			cl := client.NewClient(option.WithAPIKey(key), option.WithBaseURL(sdkBaseURL(flagBaseURL)))
			bal, err := cl.Billing.GetBalance(context.Background())
			if err != nil {
				return fmt.Errorf("could not verify the key: %w", err)
			}

			cfg := config.Load()
			cfg.APIKey = key
			if flagBaseURL != "" {
				cfg.BaseURL = flagBaseURL
			}
			if err := config.Save(cfg); err != nil {
				return err
			}
			p := printer()
			p.Note("Saved credentials to %s.", config.Path())
			p.Note("Signed in. Balance: %s.", bal.BalanceDisplay)
			return nil
		},
	}
}

func logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := config.Load()
			if cfg.APIKey == "" {
				printer().Note("Already signed out.")
				return nil
			}
			cfg.APIKey = ""
			if err := config.Save(cfg); err != nil {
				return err
			}
			printer().Note("Signed out.")
			return nil
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check your credentials and reach the API.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			bal, err := cl.Billing.GetBalance(context.Background())
			if err != nil {
				return err
			}
			_, host := resolvedCreds()
			apiHost := sdkBaseURL(host)
			printer().Emit(
				map[string]string{"status": "ok", "api_host": apiHost, "balance": bal.BalanceDisplay},
				func() {
					output.KV([][2]string{
						{"Status", "ok"},
						{"API host", apiHost},
						{"Balance", bal.BalanceDisplay},
					})
				},
			)
			return nil
		},
	}
}
