package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/session"
	"github.com/axilioai/cli/internal/util"
	platformgo "github.com/axilioai/platform-go"
	"github.com/spf13/cobra"
)

func sessionsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "sessions", Short: "Acquire, list, and release sessions."}
	cmd.AddCommand(sessionsListCmd(), sessionsStartCmd(), sessionsStopCmd())
	return cmd
}

func sessionsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List active sessions.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Phones.ActiveSessions(context.Background(), &platformgo.PhonesActiveSessionsRequest{})
			if err != nil {
				return err
			}
			printer().Emit(resp, func() {
				if len(resp.Sessions) == 0 {
					fmt.Println("No active sessions.")
					return
				}
				rows := [][]string{{"SESSION", "PHONE", "TYPE", "MODEL"}}
				for _, s := range resp.Sessions {
					rows = append(rows, []string{
						s.SessionID, s.PhoneID, util.OrDash(enumv(s.PhoneType)), util.OrDash(strv(s.ModelName)),
					})
				}
				output.Table(rows)
			})
			return nil
		},
	}
}

func sessionsStartCmd() *cobra.Command {
	var phoneType, phoneID, workflowID string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Acquire a phone and open a session; the lease persists until you stop it.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			req := &platformgo.PhoneAllocateRequest{
				PhoneType: platformgo.PhoneAllocateRequestPhoneType(strings.ToLower(strings.TrimSpace(phoneType))),
			}
			if phoneID != "" {
				req.PhoneID = &phoneID
			}
			if workflowID != "" {
				req.WorkflowID = &workflowID
			}
			a, err := cl.Phones.Allocate(context.Background(), req)
			if err != nil {
				return err
			}
			// Record the lease as the current session so `axilio phone ...`
			// verbs target it by default. control_url is minted only here, so
			// capturing it now is what lets us drive the phone later.
			if a.ControlURL != nil {
				_ = session.Save(session.Session{
					SessionID:  a.SessionID,
					PhoneID:    a.PhoneID,
					PhoneType:  strings.ToLower(strings.TrimSpace(phoneType)),
					ControlURL: *a.ControlURL,
				})
			}
			p := printer()
			p.Emit(a, func() {
				output.KV([][2]string{
					{"Session", a.SessionID},
					{"Phone", a.PhoneID},
					{"Region", util.OrDash(strv(a.Region))},
					{"Live view", util.OrDash(strv(a.LiveViewURL))},
					{"Control URL", util.OrDash(strv(a.ControlURL))},
				})
			})
			if a.ControlURL != nil {
				p.Note("\nThis is now the current session. Drive it:  axilio phone observe")
			}
			p.Note("Release it with:  axilio sessions stop %s", a.SessionID)
			return nil
		},
	}
	cmd.Flags().StringVar(&phoneType, "phone-type", "android", "android or iphone")
	cmd.Flags().StringVar(&phoneID, "phone-id", "", "Pin a dedicated phone")
	cmd.Flags().StringVar(&workflowID, "workflow", "", "Attach the session to a workflow (omit for an interactive lease)")
	return cmd
}

func sessionsStopCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "stop <session-id|phone-id>",
		Short: "Release a session by session id or phone id.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			id := args[0]
			phoneID := id
			// deallocate takes a phone_id; resolve a session_id to it via the active list.
			if resp, err := cl.Phones.ActiveSessions(context.Background(), &platformgo.PhonesActiveSessionsRequest{}); err == nil {
				for _, s := range resp.Sessions {
					if id == s.SessionID || id == s.PhoneID {
						phoneID = s.PhoneID
						break
					}
				}
			}
			if !yes && !util.Confirm(fmt.Sprintf("Release %s?", phoneID)) {
				return fmt.Errorf("aborted")
			}
			if _, err := cl.Phones.Deallocate(context.Background(), &platformgo.PhonesDeallocateRequest{PhoneID: phoneID}); err != nil {
				return err
			}
			// Drop the current-session record if this was it.
			if cur, ok := session.Load(); ok && (cur.Matches(id) || cur.Matches(phoneID)) {
				_ = session.Clear()
			}
			printer().Note("Released %s.", phoneID)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt")
	return cmd
}
