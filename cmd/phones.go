package cmd

import (
	"context"
	"fmt"

	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/util"
	platformgo "github.com/axilioai/platform-go"
	"github.com/spf13/cobra"
)

func phonesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "phones", Short: "Inspect phones."}
	cmd.AddCommand(phonesListCmd())
	return cmd
}

func phonesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List phones you can claim from the shared pool.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Phones.Available(context.Background(), &platformgo.PhonesAvailableRequest{})
			if err != nil {
				return err
			}
			printer().Emit(resp, func() {
				if len(resp.Phones) == 0 {
					fmt.Println("No phones available.")
					return
				}
				rows := [][]string{{"PHONE ID", "TYPE", "MODEL", "STATUS"}}
				for _, ph := range resp.Phones {
					rows = append(rows, []string{
						ph.PhoneID, util.OrDash(enumv(ph.PhoneType)), util.OrDash(strv(ph.ModelName)), string(ph.Status),
					})
				}
				output.Table(rows)
			})
			return nil
		},
	}
}
