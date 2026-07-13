package cmd

import (
	"fmt"

	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/util"
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
			cl, err := client()
			if err != nil {
				return err
			}
			raw, r, err := cl.AvailablePhones()
			if err != nil {
				return err
			}
			printer().Raw(raw, func() {
				if len(r.Phones) == 0 {
					fmt.Println("No phones available.")
					return
				}
				rows := [][]string{{"PHONE ID", "TYPE", "MODEL", "STATUS"}}
				for _, p := range r.Phones {
					rows = append(rows, []string{
						p.PhoneID, util.OrDash(p.PhoneType), util.OrDash(p.ModelName), util.OrDash(p.Status),
					})
				}
				output.Table(rows)
			})
			return nil
		},
	}
}
