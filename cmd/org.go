package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/axilioai/cli/internal/config"
	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/oauth"
	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/util"
	"github.com/spf13/cobra"
)

// orgFetchTimeout bounds the raw /organizations call.
const orgFetchTimeout = 30 * time.Second

// orgSummary is the subset of GET /organizations the CLI renders.
type orgSummary struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type orgListResponse struct {
	Organizations []orgSummary `json:"organizations"`
}

func orgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "orgs",
		Aliases: []string{"org"},
		Short:   "List and switch the active organization (OAuth sessions).",
		Long: "Work across the organizations you belong to without signing in again. " +
			"`orgs list` shows them, `orgs use <slug>` sets the active org for future " +
			"commands, and `--org` / `AXILIO_ORG` override it for a single call. API " +
			"keys are bound to one org, so this applies to `axilio login` (OAuth) sessions.",
		// Bare `axilio orgs` lists the organizations you belong to.
		RunE: func(_ *cobra.Command, _ []string) error { return runOrgList() },
	}
	cmd.AddCommand(orgListCmd(), orgUseCmd(), orgClearCmd())
	return cmd
}

func orgListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the organizations you belong to.",
		RunE:  func(_ *cobra.Command, _ []string) error { return runOrgList() },
	}
}

func runOrgList() error {
	orgs, err := fetchMyOrgs(context.Background())
	if err != nil {
		return err
	}
	active := resolvedOrg()
	printer().Emit(
		map[string]any{"organizations": orgs, "active": active},
		func() {
			if len(orgs) == 0 {
				fmt.Println("You are not a member of any organizations.")
				return
			}
			rows := [][]string{{"", "SLUG", "NAME", "ID"}}
			for _, o := range orgs {
				mark := ""
				if isActiveOrg(o, active) {
					mark = "*"
				}
				rows = append(rows, []string{mark, o.Slug, o.Name, o.ID})
			}
			output.Table(rows)
			if active == "" {
				printer().Note("\nNo active org set; using your session default. Set one with `axilio orgs use <slug>`.")
			}
		},
	)
	return nil
}

func orgUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <slug-or-id>",
		Short: "Set the active organization for future commands.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			sel := args[0]
			orgs, err := fetchMyOrgs(context.Background())
			if err != nil {
				return err
			}
			var match *orgSummary
			for i := range orgs {
				if orgs[i].Slug == sel || orgs[i].ID == sel {
					match = &orgs[i]
					break
				}
			}
			if match == nil {
				return exit.Usagef("you are not a member of an organization matching %q (run `axilio orgs list`)", sel)
			}
			cfg := config.Load()
			cfg.ActiveOrg = match.Slug // slug is human-readable; the header resolves slug or id server-side
			if err := config.Save(cfg); err != nil {
				return err
			}
			printer().Note("Active organization set to %s (%s).", match.Slug, match.Name)
			return nil
		},
	}
}

func orgClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Clear the active organization (revert to your session default).",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := config.Load()
			if cfg.ActiveOrg == "" {
				printer().Note("No active organization set.")
				return nil
			}
			cfg.ActiveOrg = ""
			if err := config.Save(cfg); err != nil {
				return err
			}
			printer().Note("Cleared the active organization; using your session default.")
			return nil
		},
	}
}

func isActiveOrg(o orgSummary, active string) bool {
	return active != "" && (o.Slug == active || o.ID == active)
}

// orgDisplay renders the active org selector for status/config, labeling the
// empty (unset) case as the session default.
func orgDisplay(active string) string {
	if active == "" {
		return "(session default)"
	}
	return active
}

// fetchMyOrgs GETs /organizations with the OAuth session token. Org switching is
// an OAuth-session capability: API keys are single-org, so an API-key-only config
// gets a friendly error rather than a call it cannot authenticate. The endpoint
// is not in the generated SDK (it lives on the internal/dashboard surface), so
// this is a small raw request rather than an SDK method.
func fetchMyOrgs(ctx context.Context) ([]orgSummary, error) {
	key, host := resolvedCreds()
	apiHost := util.FirstNonEmpty(host, defaultAPIHost)
	if key != "" {
		return nil, exit.Usagef("`orgs` needs an OAuth login (API keys are bound to one org); run `axilio login`")
	}
	tok, err := oauth.ValidAccessToken(ctx, apiHost)
	if err != nil {
		return nil, exit.Authf("not signed in; run `axilio login`")
	}

	ctx, cancel := context.WithTimeout(ctx, orgFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiHost+"/api/v1/organizations", nil)
	if err != nil {
		return nil, err
	}
	req.Header = cliHeader(tok)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("could not list organizations (HTTP %d)", resp.StatusCode)
	}
	var out orgListResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Organizations, nil
}
