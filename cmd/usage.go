package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/axilioai/cli/internal/output"
	platformgo "github.com/axilioai/platform-go"
	"github.com/spf13/cobra"
)

func usageCmd() *cobra.Command {
	// Bare `axilio usage` prints help; the verbs are summary and inferences.
	cmd := &cobra.Command{Use: "usage", Short: "Inspect usage and cost metrics (read-only)."}
	cmd.AddCommand(usageSummaryCmd(), usageInferencesCmd())
	return cmd
}

// window returns [now-days, now] in UTC; both usage endpoints require a range.
func window(days int) (start, end time.Time) {
	end = time.Now().UTC()
	return end.AddDate(0, 0, -days), end
}

func usageSummaryCmd() *cobra.Command {
	var days int
	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Compute minutes and cost, broken down by product.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			start, end := window(days)
			resp, err := cl.Usage.GetMetrics(context.Background(), &platformgo.UsageGetMetricsRequest{
				StartDate: start, EndDate: end,
			})
			if err != nil {
				return err
			}
			printer().Emit(resp, func() {
				pairs := [][2]string{
					{"Period", fmt.Sprintf("%s to %s", ts(resp.PeriodStart), ts(resp.PeriodEnd))},
				}
				if m := resp.ComputeMinutes; m != nil {
					pairs = append(pairs, [2]string{"Compute minutes", fmt.Sprintf("%.1f", m.TotalMinutes)})
				}
				if c := resp.CostByProduct; c != nil {
					pairs = append(pairs,
						[2]string{"Cost: inference", usd(c.Inference)},
						[2]string{"Cost: sessions", usd(c.Sessions)},
						[2]string{"Cost: other", usd(c.Other)},
					)
				}
				if i := resp.InfraCosts; i != nil {
					pairs = append(pairs, [2]string{"Infra cost (period)", usd(i.ThisPeriod)})
				}
				output.KV(pairs)
			})
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 30, "Number of days back from now")
	return cmd
}

func usageInferencesCmd() *cobra.Command {
	var (
		days  int
		limit int64
	)
	cmd := &cobra.Command{
		Use:   "inferences",
		Short: "List individual inferences with latency and cost.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			start, end := window(days)
			resp, err := cl.Usage.ListInferences(context.Background(), &platformgo.UsageInferencesRequest{
				StartDate: start, EndDate: end, Limit: &limit,
			})
			if err != nil {
				return err
			}
			printer().Emit(resp, func() {
				if len(resp.Inferences) == 0 {
					fmt.Println("No inferences in this window.")
					return
				}
				rows := [][]string{{"CREATED", "ENDPOINT", "MODEL", "LATENCY", "COST"}}
				for _, in := range resp.Inferences {
					rows = append(rows, []string{
						ts(in.CreatedAt), string(in.Endpoint), in.Model,
						fmt.Sprintf("%dms", in.LatencyMs), usdMicro(in.CostMicrodollars),
					})
				}
				output.Table(rows)
			})
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 7, "Number of days back from now")
	cmd.Flags().Int64Var(&limit, "limit", 20, "Maximum inferences to return")
	return cmd
}

// usd formats a dollar amount; usdMicro formats a microdollar amount (1e6 = $1).
func usd(v float64) string    { return fmt.Sprintf("$%.2f", v) }
func usdMicro(m int64) string { return fmt.Sprintf("$%.4f", float64(m)/1e6) }
