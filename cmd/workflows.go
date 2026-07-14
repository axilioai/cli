package cmd

import (
	"context"
	"fmt"

	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/util"
	platformgo "github.com/axilioai/platform-go"
	"github.com/spf13/cobra"
)

func workflowsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "workflows", Short: "Inspect workflows (read-only)."}
	cmd.AddCommand(workflowsListCmd(), workflowsGetCmd())
	return cmd
}

func workflowsListCmd() *cobra.Command {
	var limit int64
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List workflows, most recent first.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Workflows.List(context.Background(), &platformgo.WorkflowsListRequest{Limit: &limit})
			if err != nil {
				return err
			}
			printer().Emit(resp, func() {
				if len(resp.Workflows) == 0 {
					fmt.Println("No workflows.")
					return
				}
				rows := [][]string{{"ID", "NAME", "PLATFORM", "STATUS", "RUNS", "LAST RUN"}}
				for _, item := range resp.Workflows {
					w := item.Workflow
					if w == nil {
						continue
					}
					runs := "-"
					if item.Stats != nil {
						runs = fmt.Sprintf("%d", item.Stats.TotalRuns)
					}
					rows = append(rows, []string{
						w.ID, w.Name, string(w.Platform), string(w.Status), runs, util.OrDash(tsp(w.LastRunAt)),
					})
				}
				output.Table(rows)
			})
			return nil
		},
	}
	cmd.Flags().Int64Var(&limit, "limit", 20, "Maximum workflows to return")
	return cmd
}

func workflowsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <workflow-id>",
		Short: "Show a single workflow in detail.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Workflows.Get(context.Background(), &platformgo.WorkflowsGetRequest{WorkflowID: args[0]})
			if err != nil {
				return err
			}
			printer().Emit(resp, func() {
				w := resp.Workflow
				if w == nil {
					fmt.Println("No workflow.")
					return
				}
				pairs := [][2]string{
					{"ID", w.ID},
					{"Name", w.Name},
					{"Platform", string(w.Platform)},
					{"Status", string(w.Status)},
					{"OCR engine", string(w.OcrEngine)},
					{"Created", ts(w.CreatedAt)},
					{"Updated", ts(w.UpdatedAt)},
					{"Last run", util.OrDash(tsp(w.LastRunAt))},
				}
				if s := resp.Stats; s != nil {
					pairs = append(pairs,
						[2]string{"Total runs", fmt.Sprintf("%d", s.TotalRuns)},
						[2]string{"Success rate", fmt.Sprintf("%.0f%%", s.SuccessRate*100)},
					)
				}
				output.KV(pairs)
			})
			return nil
		},
	}
}
