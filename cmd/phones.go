package cmd

import (
	"context"

	"github.com/axilioai/cli/internal/util"
	platformgo "github.com/axilioai/platform-go"
	"github.com/spf13/cobra"
)

func phonesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "phones",
		Short: "Discover your dedicated phones.",
		Long: "Discover phones before starting a session. `phones list` shows the " +
			"dedicated phones that are free to start a session on right now. " +
			"`phones mine` shows the organization's complete dedicated inventory, " +
			"including busy and offline phones, and is the place to find a phone ID " +
			"for `sessions start --phone-id`. The shared pool is not listed; it is " +
			"drawn from automatically when you start a session without a phone ID.\n\n" +
			"Running `axilio phones` without a subcommand is equivalent to " +
			"`axilio phones --help`: it only displays this help and does not list or " +
			"change phones. Global flags shown here therefore have no effect. Pass " +
			"flags to `phones list` or `phones mine` instead.",
	}
	cmd.AddCommand(phonesListCmd())
	cmd.AddCommand(phonesMineCmd())
	return cmd
}

func phonesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your dedicated phones that are free to start a session on right now.",
		Long: "List your org's dedicated phones that are ready to start a session on immediately: active " +
			"and not currently held by a session. Busy, offline, or maintenance phones do not appear here; " +
			"use `axilio phones mine` to see the full dedicated inventory. The shared pool is not listed - it " +
			"is drawn from automatically when you start a session without a phone ID. Results include phone " +
			"ID, nickname, type, and model.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Phones.List(context.Background(), &platformgo.PhonesListRequest{
				Ownership: platformgo.PhonesListRequestOwnershipDedicated,
			})
			if err != nil {
				return err
			}
			// Startable now = an active dedicated phone not currently held by a
			// session. (The shared pool is not enumerable; it auto-allocates.)
			// Non-nil so an empty result still serializes as [] in JSON output.
			free := []*platformgo.PhoneSummary{}
			for _, ph := range resp.Phones {
				if ph.Status == "active" && ph.CurrentSessionID == nil {
					free = append(free, ph)
				}
			}
			resp.SetPhones(free)
			p := printer()
			return p.Emit(resp, func() {
				if len(resp.Phones) == 0 {
					p.Result("No free dedicated phones. Start a session without --phone-id to draw from the shared pool.")
					return
				}
				rows := [][]string{{"PHONE ID", "NICKNAME", "TYPE", "MODEL"}}
				for _, ph := range resp.Phones {
					rows = append(rows, []string{
						ph.PhoneID, util.OrDash(strv(ph.Nickname)), util.OrDash(enumv(ph.PhoneType)), util.OrDash(strv(ph.ModelName)),
					})
				}
				p.Table(rows)
			})
		},
	}
}

func phonesMineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mine",
		Short: "List your org's dedicated phones, including ones currently in use.",
		Long: "List your organization's full dedicated (private/rented) phone inventory in every state - free, " +
			"busy in an active session, or offline. This is how you discover a phone_id to pin with " +
			"`axilio sessions start --phone-id`. Results include phone ID, nickname, type, model, " +
			"status, and the session ID holding a busy phone.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Phones.List(context.Background(), &platformgo.PhonesListRequest{
				Ownership: platformgo.PhonesListRequestOwnershipDedicated,
			})
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() {
				if len(resp.Phones) == 0 {
					p.Result("No dedicated phones.")
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
				p.Table(rows)
			})
		},
	}
}
