package cmd

import (
	"net/url"
	"strings"

	"github.com/axilioai/cli/internal/config"
	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/output"
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
		Long: "Inspect and set CLI configuration without hand-editing files. Running " +
			"`axilio config` with no subcommand shows the effective settings; " +
			"`config set`/`unset` edit the config file. Complements `login` and `doctor`.",
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
	}

	printer().Emit(
		map[string]string{
			"api_host":     apiHost,
			"auth_method":  method,
			"auth_source":  source,
			"config_path":  config.Path(),
			"sessions_dir": session.Dir(),
		},
		func() {
			output.KV([][2]string{
				{"API host", apiHost},
				{"Auth method", authMethodDisplay(method, source)},
				{"Config file", config.Path()},
				{"Sessions dir", session.Dir()},
			})
		},
	)
	return nil
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
		Long: "Set a config value in the config file. Supported keys:\n" +
			"  base-url   the API host, e.g. https://api.axilio.ai (do not include /api/v1)",
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
			printer().Note("Set %s = %s in %s", key, val, config.Path())
			return nil
		},
	}
}

func configUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <key>",
		Short: "Clear a config value. Supported keys: base-url.",
		Args:  cobra.ExactArgs(1),
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
			printer().Note("Unset %s in %s", key, config.Path())
			return nil
		},
	}
}
