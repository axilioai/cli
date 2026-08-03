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
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Acquire, list, select, and release phone sessions.",
		Long: "Manage the phone sessions used by `axilio phone`. Sessions remain active " +
			"in Axilio until stopped; the CLI saves connection information locally so " +
			"later phone commands can reconnect. Session selection precedence is " +
			"--session, AXILIO_SESSION, the only locally saved session, the most " +
			"recently started session, then an ambiguity error. `sessions list " +
			"--remote` asks the API for all active Axilio sessions, including sessions " +
			"not saved on this computer.\n\n" +
			"Running `axilio sessions` without a subcommand is equivalent to " +
			"`axilio sessions --help`: it only displays this help and does not acquire, " +
			"list, select, or stop sessions. Global flags shown here therefore have no " +
			"effect. Pass flags to a sessions subcommand instead.",
	}
	cmd.AddCommand(sessionsListCmd(), sessionsStartCmd(), sessionsStopCmd(), sessionsCurrentCmd())
	return cmd
}

func sessionsListCmd() *cobra.Command {
	var remote bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List sessions saved by this CLI (--remote for all active Axilio sessions).",
		Long: "List sessions saved locally by default. The `*` marker identifies the " +
			"session that phone commands would select in this shell when selection is " +
			"not ambiguous. Use --remote to list all active Axilio sessions instead; " +
			"remote rows include session ID, phone ID, phone type, and model but do not " +
			"indicate which session this CLI would select.",
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
					fmt.Println("No sessions saved locally. Run `axilio sessions start` to start one.")
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
	cmd.Flags().BoolVar(&remote, "remote", false, "List all active Axilio sessions instead of sessions saved locally")
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
		Short: "Show the session currently selected for phone commands.",
		Long: "Resolve and show the session selected for phone commands. Phone command " +
			"session selection precedence is --session, AXILIO_SESSION, the only locally " +
			"saved session, the most recently started session, then an ambiguity error. " +
			"This command has no --session flag, so it starts at AXILIO_SESSION. If no " +
			"session can be selected, the command exits with not-found status 4.",
		RunE: func(_ *cobra.Command, _ []string) error {
			s, err := session.Resolve("")
			if err != nil {
				return exit.With(exit.NotFound, err)
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
		Short: "Acquire a phone and open a session that remains active until stopped.",
		Long: "Acquire an Android phone. The CLI currently supports Android allocation " +
			"only; --phone-type remains available for scripts that pass android explicitly, " +
			"and any other value is a usage error. The session remains active in Axilio " +
			"until stopped; the CLI saves its connection information locally and marks " +
			"it as the most recently started session. Pin a dedicated phone discovered " +
			"through `phones mine` with --phone-id, or " +
			"attach the session to a workflow with --workflow. --export prints only " +
			"`export AXILIO_SESSION=<id>` for shell eval; that shell syntax also wins " +
			"when -o json is supplied.",
		RunE: func(_ *cobra.Command, _ []string) error {
			normalizedPhoneType := strings.ToLower(strings.TrimSpace(phoneType))
			if normalizedPhoneType != string(platformgo.PhoneAllocateRequestPhoneTypeAndroid) {
				return exit.Usagef("unsupported --phone-type %q; supported value: android", phoneType)
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			req := &platformgo.PhoneAllocateRequest{
				PhoneType: platformgo.PhoneAllocateRequestPhoneTypeAndroid,
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
					PhoneType:  normalizedPhoneType,
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
	cmd.Flags().StringVar(&phoneType, "phone-type", "android", "Phone platform to allocate (currently only android)")
	cmd.Flags().StringVar(&phoneID, "phone-id", "", "Pin a dedicated phone ID from `phones mine` instead of pool allocation")
	cmd.Flags().StringVar(&workflowID, "workflow", "", "Workflow ID to attach; omit for an interactive session")
	cmd.Flags().BoolVar(&export, "export", false, "Print only `export AXILIO_SESSION=<id>` for shell eval")
	return cmd
}

func sessionsStopCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "stop <session-id|phone-id>",
		Short: "Release a session by session id or phone id.",
		Long: "Release an active phone allocation using either its session ID or phone " +
			"ID. Discover IDs with `sessions list --remote`; matching locally saved " +
			"session information and the most-recent-session marker are removed after " +
			"release. Without --yes, table " +
			"mode reads confirmation from stdin, including redirected input. JSON and " +
			"quiet modes do not prompt and require --yes.",
		Args: cobra.ExactArgs(1),
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
			p := printer()
			p.Emit(map[string]any{"phone_id": phoneID, "released": true}, func() {
				p.Note("Released %s.", phoneID)
			})
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Release without prompting; required in JSON or quiet mode")
	return cmd
}
