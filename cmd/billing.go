package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	platformgo "github.com/axilioai/platform-go"
	"github.com/axilioai/platform-go/core"
	"github.com/spf13/cobra"
)

func billingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "billing",
		Short: "Read your organization's balance and plan.",
		Long: "Read-only billing visibility: `billing balance` shows remaining credit " +
			"and `billing plan` shows the subscription. The CLI deliberately has no " +
			"billing writes — adding funds, checkout, and plan changes stay in the " +
			"dashboard.\n\n" +
			"Running `axilio billing` without a subcommand only displays this help.",
	}
	cmd.AddCommand(billingBalanceCmd(), billingPlanCmd())
	return cmd
}

// billingBalancePlan is the plan slice attached to `billing balance` output; the
// full subscription object lives under `billing plan`.
type billingBalancePlan struct {
	PlanID           string    `json:"plan_id"`
	PlanName         string    `json:"plan_name"`
	BillingCycle     string    `json:"billing_cycle"`
	CurrentPeriodEnd time.Time `json:"current_period_end"`
}

func billingBalanceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "balance",
		Short: "Show remaining credit: total, included, and purchased.",
		Long: "Show the organization's credit balance: the total, the plan-included " +
			"portion (spent first, resets at renewal), and the purchased portion " +
			"(spent after included credit, expires a year after purchase). Includes " +
			"the active plan and its renewal date when the organization has one. " +
			"Useful before allocating: a session that fails on insufficient funds " +
			"is better preempted than debugged.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			ctx := context.Background()
			bal, err := cl.Billing.GetBalance(ctx)
			if err != nil {
				return err
			}
			// The plan is supplementary here; an org without an active
			// subscription (404) still has a balance worth showing.
			sub, err := cl.Billing.GetSubscription(ctx)
			if err != nil && !isNotFound(err) {
				return err
			}
			if err != nil {
				sub = nil
			}
			var plan *billingBalancePlan
			if sub != nil {
				plan = &billingBalancePlan{
					PlanID:           sub.PlanID,
					PlanName:         sub.PlanName,
					BillingCycle:     string(sub.BillingCycle),
					CurrentPeriodEnd: sub.CurrentPeriodEnd,
				}
			}
			p := printer()
			return p.Emit(map[string]any{"balance": bal, "plan": plan}, func() {
				purchased := formatMicrodollars(bal.PurchasedMicrodollars)
				if bal.PurchasedNextExpiry != nil {
					purchased += fmt.Sprintf(" (next expiry %s)", ts(*bal.PurchasedNextExpiry))
				}
				planLine := "(none)"
				if plan != nil {
					planLine = fmt.Sprintf("%s (%s, renews %s)", plan.PlanName, plan.BillingCycle, ts(plan.CurrentPeriodEnd))
				}
				p.KV([][2]string{
					{"Balance", bal.BalanceDisplay},
					{"Included", formatMicrodollars(bal.IncludedMicrodollars) + " (resets at renewal)"},
					{"Purchased", purchased},
					{"Plan", planLine},
				})
			})
		},
	}
}

func billingPlanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "plan",
		Short: "Show the active subscription plan.",
		Long: "Show the organization's active subscription: plan, status, billing " +
			"cycle, price, per-cycle included credit, concurrent-run limit, and the " +
			"current period. Fails with not-found when the organization has no " +
			"active subscription. JSON output returns the full subscription object.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			sub, err := cl.Billing.GetSubscription(context.Background())
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(sub, func() {
				price := fmt.Sprintf("$%.2f/mo", sub.MonthlyPrice)
				if sub.BillingCycle == platformgo.SubscriptionResponseBillingCycleYearly {
					price = fmt.Sprintf("$%.2f/yr", sub.YearlyPrice)
				}
				pairs := [][2]string{
					{"Plan", fmt.Sprintf("%s (%s)", sub.PlanName, sub.PlanID)},
					{"Status", string(sub.Status)},
					{"Billing cycle", string(sub.BillingCycle)},
					{"Price", price},
					{"Included / cycle", sub.IncludedBalanceDisplay},
					{"Max concurrent runs", fmt.Sprintf("%d", sub.MaxConcurrentRuns)},
					{"Current period", fmt.Sprintf("%s → %s", ts(sub.CurrentPeriodStart), ts(sub.CurrentPeriodEnd))},
				}
				if sub.CancelAtPeriodEnd {
					pairs = append(pairs, [2]string{"Cancels", "at period end"})
				}
				if sub.PendingDowngradePlanID != nil {
					when := ""
					if sub.PendingDowngradeEffectiveDate != nil {
						when = " on " + ts(*sub.PendingDowngradeEffectiveDate)
					}
					pairs = append(pairs, [2]string{"Pending downgrade", *sub.PendingDowngradePlanID + when})
				}
				if sub.TrialEnd != nil {
					pairs = append(pairs, [2]string{"Trial ends", ts(*sub.TrialEnd)})
				}
				p.KV(pairs)
			})
		},
	}
}

// isNotFound matches the SDK's 404 in both shapes it can take: the typed
// NotFoundError (endpoints generated with an error decoder) and the plain
// core.APIError carrying the status code (endpoints without one, billing
// included today).
func isNotFound(err error) bool {
	var notFound *platformgo.NotFoundError
	if errors.As(err, &notFound) {
		return true
	}
	var apiErr *core.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// formatMicrodollars renders a microdollar amount (1_000_000 = $1.00) as a
// dollar string, rounding to the nearest cent. Used for balance components the
// API returns without a display twin; totals prefer the server's own display.
func formatMicrodollars(md int64) string {
	sign := ""
	if md < 0 {
		sign = "-"
		md = -md
	}
	cents := (md + 5000) / 10000
	return fmt.Sprintf("%s$%d.%02d", sign, cents/100, cents%100)
}
