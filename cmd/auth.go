package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/axilioai/cli/internal/config"
	"github.com/axilioai/cli/internal/oauth"
	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/util"
	"github.com/axilioai/platform-go/client"
	"github.com/axilioai/platform-go/option"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func loginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Sign in: browser OAuth by default, or store an API key.",
		Long: "Sign in to Axilio. With no arguments on a terminal, opens your browser " +
			"to authorize the CLI (OAuth); the token is stored in your OS keychain. " +
			"Pass --api-key, or pipe a key on stdin, to store an axl_ API key instead " +
			"(which the SDKs also read from ~/.config/axilio/config.json).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			key := flagAPIKey
			// A key piped on stdin (echo $KEY | axilio login) selects the key path.
			if key == "" && !term.IsTerminal(int(os.Stdin.Fd())) {
				b, _ := io.ReadAll(os.Stdin)
				key = strings.TrimSpace(string(b))
			}
			if key != "" {
				return loginWithAPIKey(cmd.Context(), key)
			}
			return loginWithBrowser(cmd.Context())
		},
	}
}

// loginWithAPIKey verifies an axl_ key against the API and stores it in the
// shared config file (so the SDKs read it too).
func loginWithAPIKey(ctx context.Context, key string) error {
	if !strings.HasPrefix(key, "axl_") {
		return fmt.Errorf("that does not look like an Axilio key (expected an axl_... value)")
	}
	_, host := resolvedCreds()
	cl := client.NewClient(option.WithAPIKey(key), option.WithBaseURL(sdkBaseURL(host)), option.WithHTTPHeader(cliHeader("")))
	bal, err := cl.Billing.GetBalance(ctx)
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
}

// loginWithBrowser runs the PKCE browser flow and stores the session in the
// keychain (file fallback).
func loginWithBrowser(ctx context.Context) error {
	_, host := resolvedCreds()
	apiHost := util.FirstNonEmpty(host, defaultAPIHost)
	p := printer()
	p.Note("Opening your browser to authorize the CLI...")
	tokens, err := oauth.Login(ctx, apiHost, dashboardBaseURL(apiHost), func(u string) {
		p.Note("If your browser did not open, visit:\n  %s", u)
	})
	if err != nil {
		return err
	}
	if err := oauth.Save(tokens); err != nil {
		return err
	}
	cl := client.NewClient(option.WithHTTPHeader(cliHeader(tokens.AccessToken)), option.WithBaseURL(sdkBaseURL(host)))
	bal, err := cl.Billing.GetBalance(ctx)
	if err != nil {
		p.Note("Signed in. (Could not fetch balance: %v)", err)
		return nil
	}
	p.Note("Signed in. Balance: %s.", bal.BalanceDisplay)
	return nil
}

func logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials (API key and OAuth session).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.Load()
			hadKey := cfg.APIKey != ""
			hadOAuth := oauth.HasSession()
			// Revoke the refresh-token family server-side before dropping the
			// local copy, so the session is truly dead and not merely forgotten
			// (AXI-1279). Best-effort: clear locally regardless.
			if hadOAuth {
				if err := oauth.Revoke(cmd.Context()); err != nil {
					printer().Note("Could not revoke the session server-side (%v); clearing locally.", err)
				}
			}
			oauth.Clear()
			// Drop the API key and any active-org selection from the shared config.
			if hadKey || cfg.ActiveOrg != "" {
				cfg.APIKey = ""
				cfg.ActiveOrg = ""
				if err := config.Save(cfg); err != nil {
					return err
				}
			}
			if !hadKey && !hadOAuth {
				printer().Note("Already signed out.")
				return nil
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
			activeOrg := resolvedOrg()
			printer().Emit(
				map[string]string{"status": "ok", "api_host": apiHost, "balance": bal.BalanceDisplay, "active_org": activeOrg},
				func() {
					output.KV([][2]string{
						{"Status", "ok"},
						{"API host", apiHost},
						{"Active org", orgDisplay(activeOrg)},
						{"Balance", bal.BalanceDisplay},
					})
				},
			)
			return nil
		},
	}
}
