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
	cmd := &cobra.Command{Use: "runs", Short: "Inspect and manage workflow runs."}
	cmd.AddCommand(runsListCmd(), runsGetCmd(), runsCancelCmd())
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
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			req := &platformgo.RunListRequest{Limit: limit}
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
	cmd.Flags().Int64Var(&limit, "limit", 20, "Maximum runs to return")
	cmd.Flags().StringVar(&workflowID, "workflow", "", "Filter by workflow id")
	return cmd
}

func runsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <run-id>",
		Short: "Show a single run in detail.",
		Args:  cobra.ExactArgs(1),
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
		Args:  cobra.ExactArgs(1),
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
			printer().Note("Cancelled %s", id)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt")
	return cmd
}
