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
	cmd := &cobra.Command{Use: "api-keys", Short: "List, create, and delete API keys."}
	cmd.AddCommand(apiKeysListCmd(), apiKeysCreateCmd(), apiKeysDeleteCmd())
	return cmd
}

func apiKeysListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the API keys on your organization.",
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
		Args:  cobra.ExactArgs(1),
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
		Args:  cobra.ExactArgs(1),
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
			printer().Note("Deleted %s", id)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt")
	return cmd
}
