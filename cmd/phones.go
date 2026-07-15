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
	cmd.AddCommand(phonesMineCmd())
	return cmd
}

func phonesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List phones you can start a session on right now (shared pool + your free dedicated phones).",
		Long: "List the phones available to start a session on immediately: every active phone in the shared " +
			"pool, plus your org's own dedicated phones that are currently free. Busy or offline dedicated " +
			"phones do not appear here - use `axilio phones mine` to see your full dedicated inventory.",
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

func phonesMineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mine",
		Short: "List your org's dedicated phones, including ones currently in use.",
		Long: "List your organization's full dedicated (private/rented) phone inventory in every state - free, " +
			"busy in an active session, or offline. This is how you discover a phone_id to pin with " +
			"`axilio sessions start --phone-id`. The SESSION column shows the id of the session holding a busy phone.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Phones.Mine(context.Background(), &platformgo.PhonesMineRequest{})
			if err != nil {
				return err
			}
			printer().Emit(resp, func() {
				if len(resp.Phones) == 0 {
					fmt.Println("No dedicated phones.")
					return
				}
				rows := [][]string{{"PHONE ID", "NICKNAME", "TYPE", "MODEL", "STATUS", "SESSION"}}
				for _, ph := range resp.Phones {
					rows = append(rows, []string{
						ph.PhoneID,
						util.OrDash(strv(ph.Nickname)),
						util.OrDash(enumv(ph.PhoneType)),
						util.OrDash(strv(ph.ModelName)),
						string(ph.Status),
						util.OrDash(strv(ph.CurrentSessionID)),
					})
				}
				output.Table(rows)
			})
			return nil
		},
	}
}
