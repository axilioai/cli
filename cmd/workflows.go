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
	cmd := &cobra.Command{Use: "workflows", Short: "Inspect workflows."}
	cmd.AddCommand(workflowsListCmd())
	return cmd
}

func workflowsListCmd() *cobra.Command {
	var (
		limit  int64
		search string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List workflows in your org, most recent first.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			req := &platformgo.WorkflowsListRequest{Limit: &limit}
			if search != "" {
				req.Search = &search
			}
			resp, err := cl.Workflows.List(context.Background(), req)
			if err != nil {
				return err
			}
			printer().Emit(resp, func() {
				if len(resp.Workflows) == 0 {
					fmt.Println("No workflows found.")
					return
				}
				rows := [][]string{{"WORKFLOW ID", "NAME", "PLATFORM", "STATUS", "LAST RUN"}}
				for _, w := range resp.Workflows {
					s := w.GetWorkflow()
					if s == nil {
						continue
					}
					rows = append(rows, []string{
						s.ID, s.Name, string(s.Platform), string(s.Status), util.OrDash(tsp(s.LastRunAt)),
					})
				}
				output.Table(rows)
			})
			return nil
		},
	}
	cmd.Flags().Int64Var(&limit, "limit", 20, "Maximum workflows to return")
	cmd.Flags().StringVar(&search, "search", "", "Filter by name substring")
	return cmd
}
