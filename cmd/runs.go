package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/util"
	platformgo "github.com/axilioai/platform-go"
	"github.com/spf13/cobra"
)

const (
	// minListLimit/maxRunsListLimit mirror the backend's runs-list bounds.
	minListLimit     int64 = 1
	maxRunsListLimit int64 = 500

	// min/maxStartTimeout mirror the backend's start_timeout_seconds bounds.
	minStartTimeout int64 = 60
	maxStartTimeout int64 = 86400

	// runStatusValues quotes the run-status wire enum verbatim. The API
	// spells the terminal state with the double-L form, so user-facing docs
	// must show and accept exactly that spelling despite the linter's US locale.
	runStatusValues = "scheduled, queued, running, paused, completed, failed, cancelled" //nolint:misspell

	// runModeValues are the run modes the list filter accepts.
	runModeValues = "agent, code"
)

func runsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "Start, inspect, watch, and cancel agent runs.",
		Long: "Manage agent runs in the active organization. Discover an " +
			"agent ID with `agents list`, start a run, list recent run IDs and " +
			"statuses with `runs list`, inspect one with `runs get`, follow it with " +
			"`runs watch`, read success rate and cost with `runs metrics`, or " +
			"cancel queued and running work.\n\n" +
			"Running `axilio runs` without a subcommand is equivalent to " +
			"`axilio runs --help`: it only displays this help and does not list, " +
			"start, inspect, or cancel runs. Global flags shown here therefore have " +
			"no effect. Pass flags to a runs subcommand instead.",
	}
	cmd.AddCommand(runsListCmd(), runsStartCmd(), runsGetCmd(), runsCancelCmd(), runsWatchCmd(), runsMetricsCmd())
	return cmd
}

func runsMetricsCmd() *cobra.Command {
	var (
		agentID string
		from    string
		to      string
	)
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Show run counts, success rate, and cost per agent.",
		Long: "Summarize finished runs per agent: how many ran, how many completed, " +
			"failed, or were canceled, the success rate, and what they cost. " +
			"--agent keeps one agent. The window defaults to the last 7 days; " +
			"--from and --to change it, up to a year back. The success rate is " +
			"completed runs over completed and failed runs. A run counts when it " +
			"finishes, and its cost can land after it does.\n\n" +
			"JSON mode returns the daily points for the matching agents.\n\n" + windowFlagsHelp,
		RunE: func(cmd *cobra.Command, _ []string) error {
			granularity := platformgo.RunsGetMetricsRequestGranularityDay
			groupBy := platformgo.RunsGetMetricsRequestGroupByAgent
			req := &platformgo.RunsGetMetricsRequest{Granularity: &granularity, GroupBy: &groupBy}
			if from != "" {
				t, err := parseWindowTime("from", from)
				if err != nil {
					return err
				}
				req.PeriodStartAt = &t
			}
			if to != "" {
				t, err := parseWindowTime("to", to)
				if err != nil {
					return err
				}
				req.PeriodEndAt = &t
			}
			if req.PeriodStartAt != nil && req.PeriodEndAt != nil && !req.PeriodStartAt.Before(*req.PeriodEndAt) {
				return exit.Usagef("--from must be before --to (got %s >= %s)", from, to)
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Runs.GetMetrics(cmd.Context(), req)
			if err != nil {
				return err
			}
			// The metrics route has no agent filter: it splits by agent, so
			// keep the requested agent's points here.
			if agentID != "" {
				kept := resp.Points[:0:0]
				for _, pt := range resp.Points {
					if pt.Agent != nil && *pt.Agent == agentID {
						kept = append(kept, pt)
					}
				}
				resp.Points = kept
			}
			p := printer()
			return p.Emit(resp, func() {
				if len(resp.Points) == 0 {
					p.Result("No finished runs in this window.")
					return
				}
				p.Table(metricsRows(resp.Points))
			})
		},
	}
	cmd.Flags().StringVar(&agentID, "agent", "", "Only this agent ID")
	cmd.Flags().StringVar(&from, "from", "", "Start of the window (default: 7 days before --to)")
	cmd.Flags().StringVar(&to, "to", "", "End of the window (default: now)")
	return cmd
}

// metricsRows folds the daily per-agent points into one row per agent.
func metricsRows(points []*platformgo.RunMetricsPoint) [][]string {
	type total struct {
		runs, completed, failed, canceled, cost int64
	}
	byAgent := map[string]*total{}
	for _, pt := range points {
		id := util.OrDash(strv(pt.Agent))
		t := byAgent[id]
		if t == nil {
			t = &total{}
			byAgent[id] = t
		}
		t.runs += pt.Runs
		t.completed += pt.CompletedRuns
		t.failed += pt.FailedRuns
		t.canceled += pt.CancelledRuns
		t.cost += pt.TotalCostMicrodollars
	}
	ids := make([]string, 0, len(byAgent))
	for id := range byAgent {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rows := [][]string{{"AGENT ID", "RUNS", "COMPLETED", "FAILED", "CANCELED", "SUCCESS RATE", "COST"}}
	for _, id := range ids {
		t := byAgent[id]
		rate := "-"
		if t.completed+t.failed > 0 {
			rate = fmt.Sprintf("%.0f%%", float64(t.completed)/float64(t.completed+t.failed)*100)
		}
		rows = append(rows, []string{
			id, fmt.Sprintf("%d", t.runs), fmt.Sprintf("%d", t.completed), fmt.Sprintf("%d", t.failed),
			fmt.Sprintf("%d", t.canceled), rate, fmt.Sprintf("$%.4f", float64(t.cost)/1e6),
		})
	}
	return rows
}

func runsListCmd() *cobra.Command {
	var (
		limit      int64
		offset     int64
		agentID    string
		scheduleID string
		mode       string
		statuses   []string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent runs, most recent first.",
		Long: "List recent runs in the active organization, most recent first. Filter " +
			"by --agent, --schedule, --mode (" + runModeValues + "), and --status " +
			"(repeatable), and page with --limit and --offset. Table rows include " +
			"run ID, mode, status, trigger, agent, and creation time; use a returned " +
			"run ID with `runs get`, `runs watch`, or `runs cancel`.",
		RunE: func(_ *cobra.Command, _ []string) error {
			if limit < minListLimit || limit > maxRunsListLimit {
				return exit.Usagef("--limit must be between %d and %d (got %d)", minListLimit, maxRunsListLimit, limit)
			}
			if offset < 0 {
				return exit.Usagef("--offset must be zero or positive (got %d)", offset)
			}
			req := &platformgo.RunsListRequest{Limit: &limit, Offset: &offset}
			if agentID != "" {
				req.AgentID = &agentID
			}
			if scheduleID != "" {
				req.ScheduleID = &scheduleID
			}
			if mode != "" {
				m, err := platformgo.NewRunsListRequestModeFromString(mode)
				if err != nil {
					return exit.Usagef("--mode must be one of %s (got %q)", runModeValues, mode)
				}
				req.Mode = &m
			}
			for _, s := range statuses {
				item, err := platformgo.NewRunsListRequestStatusItemFromString(s)
				if err != nil {
					return exit.Usagef("--status must be one of %s (got %q)", runStatusValues, s)
				}
				req.Status = append(req.Status, item)
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Runs.List(context.Background(), req)
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() {
				if len(resp.Runs) == 0 {
					p.Result("No runs found.")
					return
				}
				rows := [][]string{{"RUN ID", "MODE", "STATUS", "TRIGGER", "AGENT", "CREATED"}}
				for _, r := range resp.Runs {
					rows = append(rows, []string{
						r.ID, string(r.Mode), string(r.Status), string(r.Trigger), runAgentLabel(r), ts(r.CreatedAt),
					})
				}
				p.Table(rows)
				if resp.Offset+int64(len(resp.Runs)) < resp.Total {
					p.Note("showing %d of %d; use --limit / --offset to page", len(resp.Runs), resp.Total)
				}
			})
		},
	}
	cmd.Flags().Int64Var(&limit, "limit", 20, "Maximum runs in this page (1-500)")
	cmd.Flags().Int64Var(&offset, "offset", 0, "Number of matching runs to skip before this page")
	cmd.Flags().StringVar(&agentID, "agent", "", "Return only runs for this agent ID")
	cmd.Flags().StringVar(&scheduleID, "schedule", "", "Return only runs started by this schedule ID")
	cmd.Flags().StringVar(&mode, "mode", "", "Return only runs of this mode: "+runModeValues)
	cmd.Flags().StringSliceVar(&statuses, "status", nil, "Restrict to run statuses: "+runStatusValues+" (repeatable)")
	return cmd
}

// runAgentLabel names the agent a run belongs to, preferring its name; an ad
// hoc run has none.
func runAgentLabel(r *platformgo.RunResponse) string {
	if n := strv(r.AgentName); n != "" {
		return n
	}
	return util.OrDash(strv(r.AgentID))
}

// parseRunVariables validates the --variables flag value: one JSON object,
// applied to every created run. An empty flag means "no variables set" and
// yields the empty object the wire shape requires. Anything that is not a
// JSON object (arrays, scalars, null, malformed input) is a usage error.
func parseRunVariables(raw string) (map[string]any, error) {
	variables := map[string]any{}
	if raw == "" {
		return variables, nil
	}
	if err := json.Unmarshal([]byte(raw), &variables); err != nil {
		return nil, exit.Usagef("--variables must be a JSON object: %v", err)
	}
	if variables == nil {
		return nil, exit.Usagef("--variables must be a JSON object (got null)")
	}
	return variables, nil
}

func runsStartCmd() *cobra.Command {
	var (
		agentID       string
		startTimeout  int64
		phoneID       string
		variablesJSON string
		watch         bool
	)
	cmd := &cobra.Command{
		Use:   "start --agent <agent-id>",
		Short: "Start a run of an agent.",
		Long: "Start a run of the saved agent --agent, using its current revision " +
			"and settings. Discover an agent ID with `agents list`. A code agent " +
			"runs its Python; an instructions agent hands its task to a model.\n\n" +
			"--phone-id pins the run to a specific dedicated phone.\n\n" +
			"--variables is a JSON object of run variables, exposed to agent " +
			"code as the `variables` dict. Values are stored and delivered in " +
			"plaintext - do not put secrets here.\n\n" +
			"--start-timeout is the number " +
			"of whole seconds a queued run may wait for a phone before auto-cancel, " +
			"between 60 and 86400 inclusive; zero or negative values omit the field " +
			"and use the server default (300).\n\n" +
			"--watch follows the created run to completion exactly like " +
			"`runs watch`.\n\n" +
			"Successful output contains the created run.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if agentID == "" {
				return exit.Usagef("--agent is required; discover an agent ID with `axilio agents list`")
			}
			if startTimeout > 0 && (startTimeout < minStartTimeout || startTimeout > maxStartTimeout) {
				return exit.Usagef("--start-timeout must be between %d and %d seconds (got %d)", minStartTimeout, maxStartTimeout, startTimeout)
			}
			variables, err := parseRunVariables(variablesJSON)
			if err != nil {
				return err
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			req := &platformgo.AgentRunCreateRequest{AgentID: &agentID, Variables: variables}
			if phoneID != "" {
				req.Targets = []*platformgo.RunTarget{{
					DeviceClass: platformgo.RunTargetDeviceClassPhone,
					PhoneID:     &phoneID,
				}}
			}
			if startTimeout > 0 {
				req.StartTimeoutSeconds = &startTimeout
			}
			run, err := cl.Runs.Start(context.Background(), req)
			if err != nil {
				return err
			}
			p := printer()
			if watch {
				// Streaming mode: the created run becomes the first NDJSON
				// line (or Ack), then the watch stream follows.
				if err := p.JSONLine(run); err != nil {
					return err
				}
				p.Ack("Started run %s", run.ID)
				if err := p.Err(); err != nil {
					return err
				}
				return watchRun(cl, p, run.ID)
			}
			return p.Emit(run, func() {
				p.Ack("Started run %s", run.ID)
			})
		},
	}
	cmd.Flags().StringVar(&agentID, "agent", "", "Agent ID to run (required)")
	cmd.Flags().StringVar(&phoneID, "phone-id", "", "Dedicated phone ID to pin the run to")
	cmd.Flags().StringVar(&variablesJSON, "variables", "", "JSON object of run variables (plaintext; no secrets)")
	cmd.Flags().Int64Var(&startTimeout, "start-timeout", 0, startTimeoutHelp)
	cmd.Flags().BoolVar(&watch, "watch", false, "Follow the created run to completion")
	return cmd
}

func runsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <run-id>",
		Short: "Show a single run in detail.",
		Long: "Fetch one run by the ID returned from `runs list` or `runs start`. The " +
			"result includes mode, status, trigger, agent, session, phone, created, " +
			"started and completed times, error, and video URL when present.",
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
			p := printer()
			return p.Emit(r, func() {
				p.KV([][2]string{
					{"Run", r.ID},
					{"Mode", string(r.Mode)},
					{"Status", string(r.Status)},
					{"Trigger", string(r.Trigger)},
					{"Agent", runAgentLabel(r)},
					{"Session", util.OrDash(strv(r.SessionID))},
					{"Phone", util.OrDash(strv(r.PhoneID))},
					{"Created", ts(r.CreatedAt)},
					{"Started", util.OrDash(tsp(r.StartedAt))},
					{"Completed", util.OrDash(tsp(r.CompletedAt))},
					{"Error", util.OrDash(strv(r.Error))},
					{"Video", util.OrDash(strv(r.VideoURL))},
				})
			})
		},
	}
}

func runsCancelCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "cancel <run-id>",
		Short: "Cancel a queued or running run.",
		Long: "Cancel a queued or running run by an ID discovered with `runs list`. " +
			"Without --yes, table mode prompts only when stdin is a terminal. " +
			"Redirected, JSON, and quiet execution do not prompt and require --yes. " +
			"JSON success reports the canceled run ID.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			p := printer()
			id := args[0]
			if !yes && !p.Confirm(fmt.Sprintf("Cancel run %s?", id)) {
				if err := p.Err(); err != nil {
					return err
				}
				return exit.Usagef("aborted (pass --yes to cancel non-interactively)")
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			if _, err := cl.Runs.Cancel(context.Background(), &platformgo.RunsCancelRequest{RunID: id}); err != nil {
				return err
			}
			return p.Emit(map[string]any{"id": id, "canceled": true}, func() {
				p.Ack("Canceled %s", id)
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Cancel without prompting; required in JSON, quiet, or redirected execution")
	return cmd
}
