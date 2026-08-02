package cmd

import (
	"context"
	"fmt"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/util"
	platformgo "github.com/axilioai/platform-go"
	"github.com/spf13/cobra"
)

func apiKeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api-keys",
		Short: "List, create, and delete organization API keys.",
		Long: "Running `axilio api-keys` without a subcommand is equivalent to " +
			"`axilio api-keys --help`: it only displays this help and does not list, " +
			"create, or delete API keys. Global flags shown here therefore have no " +
			"effect. Pass flags to an api-keys subcommand instead.\n\n" +
			"Manage API keys scoped to the active organization. List keys to discover " +
			"their IDs, create a named key whose secret is shown once, or delete a key " +
			"by ID. Organization access and API-key management permissions are enforced " +
			"by the API.",
		Example: `  axilio api-keys list
  axilio api-keys create ci
  axilio api-keys delete key_123 --yes`,
	}
	cmd.AddCommand(apiKeysListCmd(), apiKeysCreateCmd(), apiKeysDeleteCmd())
	return cmd
}

func apiKeysListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the API keys on your organization.",
		Long: "List API keys in the active organization. Results include key ID, name, " +
			"masked preview, last-used time, and creation time. Full secret values are " +
			"never returned; use the ID with `api-keys delete`.",
		Example: `  axilio api-keys list
  axilio api-keys list -o json`,
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.APIKeys.List(context.Background(), &platformgo.APIKeysListRequest{})
			if err != nil {
				return err
			}
			printer().Emit(resp, func() {
				if len(resp.APIKeys) == 0 {
					fmt.Println("No API keys found.")
					return
				}
				rows := [][]string{{"ID", "NAME", "KEY", "LAST USED", "CREATED"}}
				for _, k := range resp.APIKeys {
					rows = append(rows, []string{
						k.ID, k.Name, k.KeyPreview, util.OrDash(tsp(k.LastUsedAt)), ts(k.CreatedAt),
					})
				}
				output.Table(rows)
			})
			return nil
		},
	}
}

func apiKeysCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new API key; the secret is shown once.",
		Long: "Create a named API key in the active organization. The result includes " +
			"the ID, name, full secret, and creation time. Save the secret immediately: " +
			"later list calls show only a preview and the full value cannot be retrieved.",
		Example: `  axilio api-keys create ci
  axilio api-keys create "release automation" -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.APIKeys.Create(context.Background(), &platformgo.APIKeyCreateRequest{Name: args[0]})
			if err != nil {
				return err
			}
			p := printer()
			p.Emit(resp, func() {
				output.KV([][2]string{
					{"ID", resp.ID},
					{"Name", resp.Name},
					{"Key", resp.KeyValue},
					{"Created", ts(resp.CreatedAt)},
				})
			})
			p.Note("\nSave this key now; it will not be shown again.")
			return nil
		},
	}
}

func apiKeysDeleteCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <key-id>",
		Short: "Delete an API key by id.",
		Long: "Permanently delete an organization API key using an ID discovered with " +
			"`api-keys list`. Interactive use asks for confirmation. JSON, quiet, " +
			"or redirected use cannot confirm, so pass --yes for non-interactive " +
			"deletion. JSON success reports the deleted key ID.",
		Example: `  axilio api-keys list
  axilio api-keys delete key_123
  axilio api-keys delete key_123 --yes`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			id := args[0]
			if !yes && !printer().Confirm(fmt.Sprintf("Delete API key %s?", id)) {
				return exit.Usagef("aborted (pass --yes to delete non-interactively)")
			}
			if _, err := cl.APIKeys.Delete(context.Background(), &platformgo.APIKeysDeleteRequest{KeyID: id}); err != nil {
				return err
			}
			p := printer()
			p.Emit(map[string]any{"id": id, "deleted": true}, func() {
				p.Note("Deleted %s", id)
			})
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Delete without prompting; required for non-interactive use")
	return cmd
}
