package cmd

import (
	"context"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/util"
	platformgo "github.com/axilioai/platform-go"
	"github.com/spf13/cobra"
)

// maxWorkflowsListLimit mirrors the backend's workflows-list page bound.
const maxWorkflowsListLimit int64 = 500

func workflowsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflows",
		Short: "Discover workflows that can be run.",
		Long: "Inspect workflows in the active organization. Use `workflows list` " +
			"to discover a workflow ID, then pass that ID to `runs start` or " +
			"`sessions start --workflow`.\n\n" +
			"Running `axilio workflows` without a subcommand is equivalent to " +
			"`axilio workflows --help`: it only displays this help and does not list " +
			"workflows. Global flags shown here therefore have no effect. Pass flags " +
			"to `workflows list` instead.",
	}
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
		Long: "List workflows in the active organization, most recent first. Results " +
			"include workflow ID, name, platform, status, and last-run time. Use " +
			"--search for a name substring and --limit to control the number returned. " +
			"A listed workflow ID can be passed directly to `runs start`.",
		RunE: func(_ *cobra.Command, _ []string) error {
			if limit < 1 || limit > maxWorkflowsListLimit {
				return exit.Usagef("--limit must be between 1 and %d (got %d)", maxWorkflowsListLimit, limit)
			}
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
			p := printer()
			return p.Emit(resp, func() {
				if len(resp.Workflows) == 0 {
					p.Result("No workflows found.")
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
				p.Table(rows)
			})
		},
	}
	cmd.Flags().Int64Var(&limit, "limit", 20, "Maximum number of most-recent workflows to return (1-500)")
	cmd.Flags().StringVar(&search, "search", "", "Filter workflow names by substring")
	return cmd
}
