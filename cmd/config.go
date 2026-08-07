package cmd

import (
	"net/url"
	"strings"

	"github.com/axilioai/cli/internal/config"
	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/oauth"
	"github.com/axilioai/cli/internal/session"
	"github.com/axilioai/cli/internal/util"
	"github.com/spf13/cobra"
)

// defaultAPIHost is the host used when none is configured.
const defaultAPIHost = "https://api.axilio.ai"

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show and edit CLI configuration (API host, paths, auth).",
		Long: "Inspect and edit CLI configuration without hand-editing files. Bare " +
			"`axilio config` reports the effective API host, API-key authentication " +
			"or matching browser-session authentication summary, active organization, " +
			"config path, and sessions directory. This is a local summary; `status` " +
			"verifies that the effective credentials are usable. The only editable key " +
			"is base-url.\n\n" +
			"Current ways to change these values are:\n" +
			"  API host         axilio config set base-url [url]\n" +
			"                   axilio config unset base-url\n" +
			"  API key          axilio login --api-key [api_key]\n" +
			"                   axilio logout clears saved credentials\n" +
			"  Active org       axilio orgs use [org_name]\n" +
			"                   axilio orgs clear\n" +
			"  Config path      set XDG_CONFIG_HOME; file is [value]/axilio/config.json\n" +
			"                   (default ~/.config/axilio/config.json)\n" +
			"  Sessions dir     set XDG_CONFIG_HOME; directory is [value]/axilio/sessions\n" +
			"                   (default ~/.config/axilio/sessions)",
		// Bare `axilio config` shows the current configuration.
		RunE: func(_ *cobra.Command, _ []string) error {
			return showConfig()
		},
	}
	cmd.AddCommand(configSetCmd(), configUnsetCmd())
	return cmd
}

// showConfig prints the effective configuration: the resolved host, where auth
// comes from, and the on-disk paths.
func showConfig() error {
	_, host := resolvedCreds()
	apiHost := util.FirstNonEmpty(host, defaultAPIHost)
	_, source := credSource()
	method := "none"
	if source != "" {
		method = "api-key"
	} else if oauth.HasSessionForHost(apiHost) {
		method = "oauth"
		source = "browser-session"
	}

	activeOrg := resolvedOrg()
	p := printer()
	return p.Emit(
		map[string]string{
			"api_host":     apiHost,
			"auth_method":  method,
			"auth_source":  source,
			"active_org":   activeOrg,
			"config_path":  config.Path(),
			"sessions_dir": session.Dir(),
		},
		func() {
			p.KV([][2]string{
				{"API host", apiHost},
				{"Auth method", authMethodDisplay(method, source)},
				{"Active org", orgDisplay(activeOrg)},
				{"Config file", config.Path()},
				{"Sessions dir", session.Dir()},
			})
		},
	)
}

func authMethodDisplay(method, source string) string {
	if method == "none" {
		return "none (run `axilio login`)"
	}
	return method + " (source: " + source + ")"
}

func configSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value. Supported keys: base-url.",
		Long: "Set base-url in the shared config file. Use an http or https API " +
			"origin such as https://api.axilio.ai, without /api/v1 or another path. " +
			"Validation currently accepts any URL with a scheme and host, so a " +
			"different scheme or path is saved even though requests may then fail. " +
			"Requests resolve the host from --base-url, AXILIO_BASE_URL, this saved " +
			"value, then https://api.axilio.ai.",
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			key, val := args[0], args[1]
			cfg := config.Load()
			switch key {
			case "base-url":
				val = strings.TrimRight(val, "/")
				u, err := url.Parse(val)
				if err != nil || u.Scheme == "" || u.Host == "" {
					return exit.Usagef("invalid base-url %q (want e.g. https://api.axilio.ai)", val)
				}
				cfg.BaseURL = val
			default:
				return exit.Usagef("unknown config key %q (supported: base-url)", key)
			}
			if err := config.Save(cfg); err != nil {
				return err
			}
			p := printer()
			return p.Emit(map[string]string{"key": key, "value": val, "config_path": config.Path()}, func() {
				p.Ack("Set %s = %s in %s", key, val, config.Path())
			})
		},
	}
}

func configUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <key>",
		Short: "Clear a config value. Supported keys: base-url.",
		Long: "Remove the saved base-url from the shared config file. Subsequent " +
			"requests fall back to --base-url, AXILIO_BASE_URL, then " +
			"https://api.axilio.ai. This does not remove credentials.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			key := args[0]
			cfg := config.Load()
			switch key {
			case "base-url":
				cfg.BaseURL = ""
			default:
				return exit.Usagef("unknown config key %q (supported: base-url)", key)
			}
			if err := config.Save(cfg); err != nil {
				return err
			}
			p := printer()
			return p.Emit(map[string]any{"key": key, "unset": true, "config_path": config.Path()}, func() {
				p.Ack("Unset %s in %s", key, config.Path())
			})
		},
	}
}
