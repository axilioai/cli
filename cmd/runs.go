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

func runsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "Start, inspect, and cancel workflow runs.",
		Long: "Manage workflow executions in the active organization. Discover a " +
			"workflow ID with `workflows list`, start one or more runs, list recent " +
			"run IDs and statuses, inspect a run in detail, or cancel queued and " +
			"running work.\n\n" +
			"Running `axilio runs` without a subcommand is equivalent to " +
			"`axilio runs --help`: it only displays this help and does not list, " +
			"start, inspect, or cancel runs. Global flags shown here therefore have " +
			"no effect. Pass flags to a runs subcommand instead.",
	}
	cmd.AddCommand(runsListCmd(), runsStartCmd(), runsGetCmd(), runsCancelCmd())
	return cmd
}

func runsListCmd() *cobra.Command {
	var (
		limit      int64
		workflowID string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent runs, most recent first.",
		Long: "List recent runs in the active organization, most recent first. Filter " +
			"by workflow ID and cap the result count with --limit. Table rows include " +
			"run ID, status, trigger, workflow ID, and creation time; use a returned " +
			"run ID with `runs get` or `runs cancel`.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			req := &platformgo.RunListRequest{Limit: &limit}
			if workflowID != "" {
				req.WorkflowID = &workflowID
			}
			resp, err := cl.Runs.List(context.Background(), req)
			if err != nil {
				return err
			}
			printer().Emit(resp, func() {
				if len(resp.Runs) == 0 {
					fmt.Println("No runs found.")
					return
				}
				rows := [][]string{{"RUN ID", "STATUS", "TRIGGER", "WORKFLOW", "CREATED"}}
				for _, r := range resp.Runs {
					rows = append(rows, []string{
						r.ID, string(r.Status), string(r.Trigger), r.WorkflowID, ts(r.CreatedAt),
					})
				}
				output.Table(rows)
			})
			return nil
		},
	}
	cmd.Flags().Int64Var(&limit, "limit", 20, "Maximum number of most-recent runs to return")
	cmd.Flags().StringVar(&workflowID, "workflow", "", "Return only runs for this workflow ID")
	return cmd
}

func runsStartCmd() *cobra.Command {
	var (
		count        int64
		startTimeout int64
		phoneID      string
	)
	cmd := &cobra.Command{
		Use:   "start <workflow-id>",
		Short: "Start one or more runs of a workflow.",
		Long: "Create runs for a workflow ID discovered with `workflows list`.\n\n" +
			"--count creates that many run configurations; v0.5.0 does not validate " +
			"its range locally, so use a positive count.\n\n" +
			"--phone-id pins every created run to a specific dedicated phone.\n\n" +
			"--start-timeout is the number " +
			"of whole seconds a queued run may wait for a phone before auto-cancel. " +
			"Positive values are sent to the server, which may reject unsupported " +
			"values; zero or negative values omit the field and use the server default.\n\n" +
			"Successful output contains the " +
			"created run IDs.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			// The backend requires a per-run config for each run (`runs` is
			// required, and it creates one run per entry). Send `count` empty
			// configs, optionally pinning each to a dedicated phone.
			runs := make([]*platformgo.RunConfig, count)
			for i := range runs {
				// `variables` is required on each run config; a single empty
				// map means "run with no variables set".
				rc := &platformgo.RunConfig{Variables: []map[string]any{{}}}
				if phoneID != "" {
					rc.PhoneID = &phoneID
				}
				runs[i] = rc
			}
			// count drives len(runs); the backend creates one run per RunConfig,
			// so the removed Count field is now implicit in the Runs slice.
			req := &platformgo.RunCreateRequest{WorkflowID: args[0], Runs: runs}
			if startTimeout > 0 {
				req.StartTimeoutSeconds = &startTimeout
			}
			resp, err := cl.Runs.Create(context.Background(), req)
			if err != nil {
				return err
			}
			printer().Emit(resp, func() {
				if len(resp.RunIDs) == 0 {
					fmt.Println("No runs created.")
					return
				}
				for _, id := range resp.RunIDs {
					printer().Note("Started run %s", id)
				}
			})
			return nil
		},
	}
	cmd.Flags().Int64Var(&count, "count", 1, "Number of run configurations to create; v0.5.0 does not validate the range")
	cmd.Flags().StringVar(&phoneID, "phone-id", "", "Dedicated phone ID to pin to every created run")
	cmd.Flags().Int64Var(&startTimeout, "start-timeout", 0, startTimeoutHelp)
	return cmd
}

func runsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <run-id>",
		Short: "Show a single run in detail.",
		Long: "Fetch one run by the ID returned from `runs list` or `runs start`. The " +
			"result includes status, trigger, workflow, session, phone, created, " +
			"started and completed times, error message, and video URL when present.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			r, err := cl.Runs.Get(context.Background(), &platformgo.RunsGetRequest{RunID: args[0]})
			if err != nil {
				return err
			}
			printer().Emit(r, func() {
				output.KV([][2]string{
					{"Run", r.ID},
					{"Status", string(r.Status)},
					{"Trigger", string(r.Trigger)},
					{"Workflow", r.WorkflowID},
					{"Session", util.OrDash(strv(r.SessionID))},
					{"Phone", util.OrDash(strv(r.PhoneID))},
					{"Created", ts(r.CreatedAt)},
					{"Started", util.OrDash(tsp(r.StartedAt))},
					{"Completed", util.OrDash(tsp(r.CompletedAt))},
					{"Error", util.OrDash(strv(r.ErrorMessage))},
					{"Video", util.OrDash(strv(r.VideoURL))},
				})
			})
			return nil
		},
	}
}

func runsCancelCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "cancel <run-id>",
		Short: "Cancel a queued or running run.",
		Long: "Cancel a queued or running run by an ID discovered with `runs list`. " +
			"Without --yes, table mode reads confirmation from stdin, including " +
			"redirected input. JSON and quiet modes do not prompt and require --yes. " +
			"JSON success reports the canceled run ID.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			id := args[0]
			if !yes && !printer().Confirm(fmt.Sprintf("Cancel run %s?", id)) {
				return exit.Usagef("aborted (pass --yes to cancel non-interactively)")
			}
			if _, err := cl.Runs.Cancel(context.Background(), &platformgo.RunsCancelRequest{RunID: id}); err != nil {
				return err
			}
			p := printer()
			p.Emit(map[string]any{"id": id, "canceled": true}, func() {
				p.Note("Canceled %s", id)
			})
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Cancel without prompting; required in JSON or quiet mode")
	return cmd
}
