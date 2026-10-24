package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/util"
	platformgo "github.com/axilioai/platform-go"
	"github.com/spf13/cobra"
)

const (
	// minInferencesLimit/maxInferencesLimit mirror the backend's
	// usage-inferences page bounds.
	minInferencesLimit int64 = 1
	maxInferencesLimit int64 = 100

	windowFlagsHelp = "Accepts an RFC 3339 timestamp (2026-08-01T00:00:00Z) or a " +
		"bare YYYY-MM-DD date, which reads as midnight UTC of that day."
)

func usageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "Report usage spend and per-inference costs.",
		Long: "Read the active organization's usage: aggregate spend and compute " +
			"metrics over a reporting window, or the individual billed inference " +
			"calls behind the inference line item.\n\n" +
			"Running `axilio usage` without a subcommand only displays this help.",
	}
	cmd.AddCommand(usageMetricsCmd(), usageInferencesCmd())
	return cmd
}

// parseWindowTime parses one --from/--to value.
func parseWindowTime(name, value string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t, nil
	}
	return time.Time{}, exit.Usagef("--%s must be an RFC 3339 timestamp or a YYYY-MM-DD date (got %q)", name, value)
}

// parseWindow validates the shared reporting-window flags. --from is
// required; an empty --to means "now".
func parseWindow(from, to string) (time.Time, time.Time, error) {
	if from == "" {
		return time.Time{}, time.Time{}, exit.Usagef("--from is required. %s", windowFlagsHelp)
	}
	start, err := parseWindowTime("from", from)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end := time.Now().UTC()
	if to != "" {
		if end, err = parseWindowTime("to", to); err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if !start.Before(end) {
		return time.Time{}, time.Time{}, exit.Usagef("--from must be before --to (got %s >= %s)", from, end.Format(time.RFC3339))
	}
	return start, end, nil
}

func usageMetricsCmd() *cobra.Command {
	var (
		from        string
		to          string
		granularity string
		timezone    string
	)
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Summarize usage spend over a reporting window.",
		Long: "Summarize the organization's usage spend between --from and --to, " +
			"split into phone-session and inference spend.\n\n" +
			"--granularity picks the bucket size (hour or day; hour needs a window " +
			"of 7 days or less); the server chooses when omitted. --tz names an " +
			"IANA time zone (e.g. America/Los_Angeles) the buckets follow.\n\n" +
			"Table mode prints the totals; the per-bucket series is available in " +
			"--output json.\n\n" + windowFlagsHelp,
		RunE: func(_ *cobra.Command, _ []string) error {
			start, end, err := parseWindow(from, to)
			if err != nil {
				return err
			}
			req := &platformgo.UsageGetMetricsRequest{PeriodStartAt: &start, PeriodEndAt: &end}
			if granularity != "" {
				g, err := platformgo.NewUsageGetMetricsRequestGranularityFromString(granularity)
				if err != nil {
					return exit.Usagef("--granularity must be hour or day (got %q)", granularity)
				}
				req.Granularity = g.Ptr()
			}
			if timezone != "" {
				req.TimeZone = &timezone
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Usage.GetMetrics(context.Background(), req)
			if err != nil {
				return err
			}
			p := printer()
			var session, inference, total int64
			for _, pt := range resp.Points {
				session += pt.SessionCostMicrodollars
				inference += pt.InferenceCostMicrodollars
				total += pt.TotalCostMicrodollars
			}
			return p.Emit(resp, func() {
				p.KV([][2]string{
					{"Period", fmt.Sprintf("%s - %s", ts(start), ts(end))},
					{"Session spend", formatMicrodollars(session)},
					{"Inference spend", formatMicrodollars(inference)},
					{"Total spend", formatMicrodollars(total)},
				})
			})
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "Start of the reporting window (required)")
	cmd.Flags().StringVar(&to, "to", "", "End of the reporting window (default: now)")
	cmd.Flags().StringVar(&granularity, "granularity", "", "Bucket size: hour or day (default: server-chosen)")
	cmd.Flags().StringVar(&timezone, "tz", "", "IANA time zone the buckets follow, such as America/Los_Angeles")
	return cmd
}

func usageInferencesCmd() *cobra.Command {
	var (
		from      string
		to        string
		limit     int64
		offset    int64
		endpoints []string
		model     string
		sessionID string
		search    string
		orderBy   string
	)
	cmd := &cobra.Command{
		Use:   "inferences",
		Short: "List individual billed inference calls.",
		Long: "List the billed vision inference calls between --from and --to, most " +
			"recent first, with the cost and latency of each. Filter by endpoint " +
			"(detect, locate or element_locate, repeatable), model, or session; --search matches " +
			"inference-ID substrings.\n\n" +
			"--order-by is a '<field> <asc|desc>' expression; the field is one of " +
			"created_at, cost_microdollars, latency_ms, endpoint, model, or " +
			"inference_id.\n\n" +
			"Page with --limit and --offset.\n\n" + windowFlagsHelp,
		RunE: func(_ *cobra.Command, _ []string) error {
			start, end, err := parseWindow(from, to)
			if err != nil {
				return err
			}
			if limit < minInferencesLimit || limit > maxInferencesLimit {
				return exit.Usagef("--limit must be between %d and %d (got %d)", minInferencesLimit, maxInferencesLimit, limit)
			}
			if offset < 0 {
				return exit.Usagef("--offset must be zero or positive (got %d)", offset)
			}
			req := &platformgo.UsageListInferencesRequest{
				CreatedAfter:  start,
				CreatedBefore: end,
				Limit:         &limit,
				Offset:        &offset,
			}
			for _, e := range endpoints {
				item, err := platformgo.NewUsageListInferencesRequestEndpointItemFromString(e)
				if err != nil {
					return exit.Usagef("--endpoint must be detect, locate or element_locate (got %q)", e)
				}
				req.Endpoint = append(req.Endpoint, item)
			}
			if model != "" {
				req.Model = &model
			}
			if sessionID != "" {
				req.SessionID = &sessionID
			}
			if search != "" {
				req.Search = &search
			}
			if orderBy != "" {
				req.OrderBy = &orderBy
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Usage.ListInferences(context.Background(), req)
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() {
				if len(resp.Inferences) == 0 {
					p.Result("No inferences found.")
					return
				}
				rows := [][]string{{"TIME", "INFERENCE ID", "ENDPOINT", "MODEL", "COST", "LATENCY", "SESSION"}}
				for _, inf := range resp.Inferences {
					rows = append(rows, []string{
						ts(inf.CreatedAt),
						inf.InferenceID,
						string(inf.Endpoint),
						inf.Model,
						usd(inf.CostMicrodollars),
						fmt.Sprintf("%d ms", inf.LatencyMs),
						util.OrDash(strv(inf.SessionID)),
					})
				}
				p.Table(rows)
				if int64(len(resp.Inferences)) < resp.Total {
					p.Note("showing %d of %d; use --limit / --offset to page", len(resp.Inferences), resp.Total)
				}
			})
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "Start of the query window (required)")
	cmd.Flags().StringVar(&to, "to", "", "End of the query window (default: now)")
	cmd.Flags().Int64Var(&limit, "limit", 50, "Maximum inference calls in this page (1-100)")
	cmd.Flags().Int64Var(&offset, "offset", 0, "Number of matching calls to skip before this page")
	cmd.Flags().StringSliceVar(&endpoints, "endpoint", nil, "Restrict to a vision endpoint: detect, locate or element_locate (repeatable)")
	cmd.Flags().StringVar(&model, "model", "", "Restrict to a single model name")
	cmd.Flags().StringVar(&sessionID, "session", "", "Restrict to inferences that ran under one phone session")
	cmd.Flags().StringVar(&search, "search", "", "Filter by inference-ID substring")
	cmd.Flags().StringVar(&orderBy, "order-by", "", "Sort expression '<field> <asc|desc>' (default: created_at desc)")
	return cmd
}
