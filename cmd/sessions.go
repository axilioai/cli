package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/session"
	"github.com/axilioai/cli/internal/util"
	platformgo "github.com/axilioai/platform-go"
	"github.com/spf13/cobra"
)

func sessionsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "sessions", Short: "Acquire, list, and release phone sessions."}
	cmd.AddCommand(sessionsListCmd(), sessionsStartCmd(), sessionsStopCmd(), sessionsCurrentCmd())
	return cmd
}

func sessionsListCmd() *cobra.Command {
	var remote bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the phone leases this CLI holds (--remote for all server sessions).",
		RunE: func(_ *cobra.Command, _ []string) error {
			if remote {
				return listRemoteSessions()
			}
			leases := session.List()
			// The lease the phone verbs would target in this shell (best-effort;
			// an ambiguous resolve just leaves nothing marked).
			sel, _ := session.Resolve("")
			printer().Emit(leases, func() {
				if len(leases) == 0 {
					fmt.Println("No active leases. Run `axilio sessions start` to acquire one.")
					return
				}
				rows := [][]string{{"", "SESSION", "PHONE", "TYPE"}}
				for _, s := range leases {
					marker := ""
					if s.SessionID == sel.SessionID {
						marker = "*"
					}
					rows = append(rows, []string{marker, s.SessionID, s.PhoneID, util.OrDash(s.PhoneType)})
				}
				output.Table(rows)
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&remote, "remote", false, "List all active sessions on the server instead of local leases")
	return cmd
}

func listRemoteSessions() error {
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
}

func sessionsCurrentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show which session the phone verbs target in this shell.",
		RunE: func(_ *cobra.Command, _ []string) error {
			s, err := session.Resolve("")
			if err != nil {
				printer().Note("%s", err)
				return nil
			}
			printer().Emit(s, func() {
				output.KV([][2]string{
					{"Session", s.SessionID},
					{"Phone", s.PhoneID},
					{"Type", util.OrDash(s.PhoneType)},
				})
			})
			return nil
		},
	}
}

func sessionsStartCmd() *cobra.Command {
	var phoneType, phoneID, workflowID string
	var export bool
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
			// Record the lease in the registry (captures control_url, minted only
			// here) so `axilio phone ...` can drive it. Many leases coexist; this
			// one becomes the current-session pointer.
			if a.ControlURL != nil {
				_ = session.Save(session.Session{
					SessionID:  a.SessionID,
					PhoneID:    a.PhoneID,
					PhoneType:  strings.ToLower(strings.TrimSpace(phoneType)),
					ControlURL: *a.ControlURL,
				})
			}
			// --export: emit ONLY the eval-able line so a shell/agent can pin this
			// phone to the process: eval "$(axilio sessions start --export ...)".
			if export {
				fmt.Printf("export %s=%s\n", session.EnvVar, a.SessionID)
				return nil
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
				p.Note("\nDrive it:  axilio phone observe")
				p.Note("Pin it to this shell (for parallel work):  export %s=%s", session.EnvVar, a.SessionID)
			}
			p.Note("Release it with:  axilio sessions stop %s", a.SessionID)
			return nil
		},
	}
	cmd.Flags().StringVar(&phoneType, "phone-type", "android", "android or iphone")
	cmd.Flags().StringVar(&phoneID, "phone-id", "", "Pin a dedicated phone")
	cmd.Flags().StringVar(&workflowID, "workflow", "", "Attach the session to a workflow (omit for an interactive lease)")
	cmd.Flags().BoolVar(&export, "export", false, "Emit only `export AXILIO_SESSION=<id>` for `eval` in the current shell")
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
			if !yes && !printer().Confirm(fmt.Sprintf("Release %s?", phoneID)) {
				return exit.Usagef("aborted (pass --yes to release non-interactively)")
			}
			if _, err := cl.Phones.Deallocate(context.Background(), &platformgo.PhonesDeallocateRequest{PhoneID: phoneID}); err != nil {
				return err
			}
			// Drop the lease from the registry (clears the current pointer if it was it).
			_ = session.Remove(id)
			printer().Note("Released %s.", phoneID)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt")
	return cmd
}
