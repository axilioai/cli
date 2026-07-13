package cmd

import (
	"fmt"
	"strings"

	"github.com/axilioai/cli/internal/api"
	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/util"
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
			cl, err := client()
			if err != nil {
				return err
			}
			raw, r, err := cl.ActiveSessions()
			if err != nil {
				return err
			}
			printer().Raw(raw, func() {
				if len(r.Sessions) == 0 {
					fmt.Println("No active sessions.")
					return
				}
				rows := [][]string{{"SESSION", "PHONE", "TYPE", "WORKFLOW"}}
				for _, s := range r.Sessions {
					rows = append(rows, []string{
						s.SessionID, s.PhoneID, util.OrDash(s.PhoneType), util.OrDash(s.WorkflowName),
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
			cl, err := client()
			if err != nil {
				return err
			}
			raw, a, err := cl.Allocate(api.AllocateRequest{
				PhoneType:  strings.ToUpper(strings.TrimSpace(phoneType)),
				PhoneID:    phoneID,
				WorkflowID: workflowID,
			})
			if err != nil {
				return err
			}
			p := printer()
			p.Raw(raw, func() {
				output.KV([][2]string{
					{"Session", a.SessionID},
					{"Phone", a.PhoneID},
					{"Region", util.OrDash(a.Region)},
					{"Live view", util.OrDash(a.LiveViewURL)},
					{"Control URL", util.OrDash(a.ControlURL)},
				})
			})
			p.Note("\nRelease it with:  axilio sessions stop %s", a.SessionID)
			return nil
		},
	}
	cmd.Flags().StringVar(&phoneType, "phone-type", "android", "android or ios")
	cmd.Flags().StringVar(&phoneID, "phone-id", "", "Pin a dedicated phone")
	cmd.Flags().StringVar(&workflowID, "workflow", "", "Attach the session to a workflow")
	return cmd
}

func sessionsStopCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "stop <session-id|phone-id>",
		Short: "Release a session by session id or phone id.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := client()
			if err != nil {
				return err
			}
			id := args[0]
			phoneID := id
			// deallocate takes a phone_id; resolve a session_id to it via the active list.
			if _, r, err := cl.ActiveSessions(); err == nil {
				for _, s := range r.Sessions {
					if id == s.SessionID || id == s.PhoneID {
						phoneID = s.PhoneID
						break
					}
				}
			}
			if !yes && !util.Confirm(fmt.Sprintf("Release %s?", phoneID)) {
				return fmt.Errorf("aborted")
			}
			if _, err := cl.Deallocate(phoneID); err != nil {
				return err
			}
			printer().Note("Released %s.", phoneID)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt")
	return cmd
}
