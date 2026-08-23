package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/session"
	"github.com/axilioai/cli/internal/util"
	platformgo "github.com/axilioai/platform-go"
	"github.com/spf13/cobra"
)

func sessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Acquire, list, select, and release phone sessions.",
		Long: "Manage the phone sessions used by `axilio phone`. Sessions remain active " +
			"in Axilio until stopped; the CLI saves connection information locally so " +
			"later phone commands can reconnect. Session selection precedence is " +
			"--session, AXILIO_SESSION, the only locally saved session, the most " +
			"recently started session, then an ambiguity error. `sessions list " +
			"--remote` asks the API for all active Axilio sessions, including sessions " +
			"not saved on this computer.\n\n" +
			"Running `axilio sessions` without a subcommand is equivalent to " +
			"`axilio sessions --help`: it only displays this help and does not acquire, " +
			"list, select, or stop sessions. Global flags shown here therefore have no " +
			"effect. Pass flags to a sessions subcommand instead.",
	}
	cmd.AddCommand(sessionsListCmd(), sessionsStartCmd(), sessionsStopCmd(), sessionsCurrentCmd(),
		sessionsGetCmd(), sessionsTraceCmd())
	return cmd
}

// Server-side bounds and enums for `sessions list --history`, mirrored so bad
// input is a usage error before any HTTP request.
const (
	minSessionsListLimit int64 = 1
	maxSessionsListLimit int64 = 100
)

var (
	sessionStatusValues = []string{"ACTIVE", "COMPLETED", "CANCELLED", "EXPIRED"}
	sessionSourceValues = []string{"workflow", "interactive"}
	sessionTypeValues   = []string{"shared", "dedicated"}
	sessionSortValues   = []string{"started", "ended", "status", "duration", "source"}
)

// historyFilters carries the `sessions list` flags that address the
// historical list op. Any of them being set switches the command to history
// mode, so filters work without also passing --history.
type historyFilters struct {
	history       bool
	status        []string
	source        []string
	phoneKind     []string
	search        string
	workflowID    string
	startedAfter  string
	startedBefore string
	endedAfter    string
	endedBefore   string
	sort          string
	order         string
	limit         int64
	offset        int64
}

// oneOf normalizes value against allowed case-insensitively; empty stays empty.
func oneOf(flag, value string, allowed []string) (string, error) {
	if value == "" {
		return "", nil
	}
	for _, a := range allowed {
		if strings.EqualFold(value, a) {
			return a, nil
		}
	}
	return "", exit.Usagef("%s must be one of %s (got %q)", flag, strings.Join(allowed, "|"), value)
}

func eachOf(flag string, values, allowed []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, v := range values {
		n, err := oneOf(flag, v, allowed)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// timeFlag accepts RFC3339 or a bare YYYY-MM-DD date (midnight UTC) and
// returns the RFC3339 string the API expects; empty stays empty.
func timeFlag(flag, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.Format(time.RFC3339), nil
	}
	return "", exit.Usagef("%s must be RFC3339 or YYYY-MM-DD (got %q)", flag, value)
}

func (h *historyFilters) active(cmd *cobra.Command) bool {
	if h.history {
		return true
	}
	for _, name := range []string{"status", "source", "type", "search", "workflow",
		"started-after", "started-before", "ended-after", "ended-before",
		"sort", "order", "offset"} {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

func (h *historyFilters) request() (*platformgo.PhonesListSessionsRequest, error) {
	if h.limit < minSessionsListLimit || h.limit > maxSessionsListLimit {
		return nil, exit.Usagef("--limit must be between %d and %d (got %d)", minSessionsListLimit, maxSessionsListLimit, h.limit)
	}
	if h.offset < 0 {
		return nil, exit.Usagef("--offset must be >= 0 (got %d)", h.offset)
	}
	status, err := eachOf("--status", h.status, sessionStatusValues)
	if err != nil {
		return nil, err
	}
	source, err := eachOf("--source", h.source, sessionSourceValues)
	if err != nil {
		return nil, err
	}
	phoneKind, err := eachOf("--type", h.phoneKind, sessionTypeValues)
	if err != nil {
		return nil, err
	}
	sort, err := oneOf("--sort", h.sort, sessionSortValues)
	if err != nil {
		return nil, err
	}
	order, err := oneOf("--order", h.order, []string{"asc", "desc"})
	if err != nil {
		return nil, err
	}
	req := &platformgo.PhonesListSessionsRequest{
		Limit:  &h.limit,
		Status: status,
		Source: source,
	}
	if h.offset > 0 {
		req.Offset = &h.offset
	}
	if len(phoneKind) > 0 {
		req.Dedicated = phoneKind
	}
	if h.search != "" {
		req.Search = &h.search
	}
	if h.workflowID != "" {
		req.WorkflowID = &h.workflowID
	}
	if sort != "" {
		req.Sort = &sort
	}
	if order != "" {
		req.Order = &order
	}
	for flag, field := range map[string]*string{
		"--started-after": &h.startedAfter, "--started-before": &h.startedBefore,
		"--ended-after": &h.endedAfter, "--ended-before": &h.endedBefore,
	} {
		v, err := timeFlag(flag, *field)
		if err != nil {
			return nil, err
		}
		*field = v
	}
	if h.startedAfter != "" {
		req.StartedAfter = &h.startedAfter
	}
	if h.startedBefore != "" {
		req.StartedBefore = &h.startedBefore
	}
	if h.endedAfter != "" {
		req.EndedAfter = &h.endedAfter
	}
	if h.endedBefore != "" {
		req.EndedBefore = &h.endedBefore
	}
	return req, nil
}

func listHistorySessions(h *historyFilters) error {
	req, err := h.request()
	if err != nil {
		return err
	}
	cl, err := newClient()
	if err != nil {
		return err
	}
	resp, err := cl.Phones.ListSessions(context.Background(), req)
	if err != nil {
		return err
	}
	p := printer()
	return p.Emit(resp, func() {
		if len(resp.Sessions) == 0 {
			p.Result("No sessions match.")
			return
		}
		rows := [][]string{{"SESSION", "STATUS", "SOURCE", "PHONE", "STARTED", "ENDED", "DURATION"}}
		for _, s := range resp.Sessions {
			dur := ""
			if s.DurationSeconds != nil {
				dur = humanDuration(time.Duration(*s.DurationSeconds) * time.Second)
			}
			rows = append(rows, []string{
				s.SessionID, string(s.Status), string(s.Source), s.PhoneID,
				ts(s.AllocatedAt), util.OrDash(tsp(s.DeallocatedAt)), util.OrDash(dur),
			})
		}
		p.Table(rows)
		p.Note("%d of %d sessions (limit %d, offset %d)", len(resp.Sessions), resp.Total, resp.Limit, resp.Offset)
	})
}

func sessionsListCmd() *cobra.Command {
	var remote bool
	h := &historyFilters{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List sessions: local by default, --remote active, --history archived.",
		Long: "List sessions saved locally by default. The `*` marker identifies the " +
			"session that phone commands would select in this shell when selection is " +
			"not ambiguous. Use --remote to list all active Axilio sessions instead; " +
			"remote rows include session ID, phone ID, phone type, and model but do not " +
			"indicate which session this CLI would select.\n\n" +
			"Use --history for the org's full session history, active and released. " +
			"Any history filter flag switches to history mode on its own: status, " +
			"source, phone type, search, workflow, and started/ended time bounds, " +
			"plus sort, order, and offset. Time bounds accept RFC3339 or " +
			"YYYY-MM-DD. Inspect a returned ID with `sessions get` or " +
			"`sessions trace`.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if h.active(cmd) {
				if remote {
					return exit.Usagef("--remote lists active sessions only and cannot be combined with history filters")
				}
				return listHistorySessions(h)
			}
			if remote {
				return listRemoteSessions()
			}
			leases := session.List()
			// The lease the phone verbs would target in this shell (best-effort;
			// an ambiguous resolve just leaves nothing marked).
			sel, _ := session.Resolve("")
			p := printer()
			return p.Emit(leases, func() {
				if len(leases) == 0 {
					p.Result("No sessions saved locally. Run `axilio sessions start` to start one.")
					return
				}
				rows := [][]string{{"", "SESSION", "PHONE", "TYPE"}}
				for _, s := range leases {
					marker := ""
					if s.SessionID == sel.SessionID {
						marker = "*"
					}
					rows = append(rows, []string{marker, s.SessionID, s.PhoneID, util.OrDash(s.PhoneType)})
				}
				p.Table(rows)
			})
		},
	}
	cmd.Flags().BoolVar(&remote, "remote", false, "List all active Axilio sessions instead of sessions saved locally")
	cmd.Flags().BoolVar(&h.history, "history", false, "List the org's session history, active and released")
	cmd.Flags().StringSliceVar(&h.status, "status", nil, "Filter history by status: active|completed|cancelled|expired (repeatable)")
	cmd.Flags().StringSliceVar(&h.source, "source", nil, "Filter history by source: workflow|interactive (repeatable)")
	cmd.Flags().StringSliceVar(&h.phoneKind, "type", nil, "Filter history by phone type: shared|dedicated (repeatable)")
	cmd.Flags().StringVar(&h.search, "search", "", "Case-insensitive match on phone, session, or workflow")
	cmd.Flags().StringVar(&h.workflowID, "workflow", "", "Only sessions for this workflow ID")
	cmd.Flags().StringVar(&h.startedAfter, "started-after", "", "Only sessions started at or after this time")
	cmd.Flags().StringVar(&h.startedBefore, "started-before", "", "Only sessions started at or before this time")
	cmd.Flags().StringVar(&h.endedAfter, "ended-after", "", "Only sessions released at or after this time")
	cmd.Flags().StringVar(&h.endedBefore, "ended-before", "", "Only sessions released at or before this time")
	cmd.Flags().StringVar(&h.sort, "sort", "", "History sort column: started|ended|status|duration|source")
	cmd.Flags().StringVar(&h.order, "order", "", "History sort direction: asc|desc")
	cmd.Flags().Int64Var(&h.limit, "limit", 50, "Maximum history rows to return (1-100)")
	cmd.Flags().Int64Var(&h.offset, "offset", 0, "History rows to skip for pagination")
	return cmd
}

func listRemoteSessions() error {
	cl, err := newClient()
	if err != nil {
		return err
	}
	resp, err := cl.Phones.ActiveSessions(context.Background(), &platformgo.PhonesActiveSessionsRequest{})
	if err != nil {
		return err
	}
	p := printer()
	return p.Emit(resp, func() {
		if len(resp.Sessions) == 0 {
			p.Result("No active sessions.")
			return
		}
		rows := [][]string{{"SESSION", "PHONE", "TYPE", "MODEL"}}
		for _, s := range resp.Sessions {
			rows = append(rows, []string{
				s.SessionID, s.PhoneID, util.OrDash(enumv(s.PhoneType)), util.OrDash(strv(s.ModelName)),
			})
		}
		p.Table(rows)
	})
}

func sessionsCurrentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the session currently selected for phone commands.",
		Long: "Resolve and show the session selected for phone commands. Phone command " +
			"session selection precedence is --session, AXILIO_SESSION, the only locally " +
			"saved session, the most recently started session, then an ambiguity error. " +
			"This command has no --session flag, so it starts at AXILIO_SESSION. If no " +
			"session can be selected, the command exits with not-found status 4.",
		RunE: func(_ *cobra.Command, _ []string) error {
			s, err := session.Resolve("")
			if err != nil {
				return exit.With(exit.NotFound, err)
			}
			p := printer()
			return p.Emit(s, func() {
				p.KV([][2]string{
					{"Session", s.SessionID},
					{"Phone", s.PhoneID},
					{"Type", util.OrDash(s.PhoneType)},
				})
			})
		},
	}
}

func sessionsStartCmd() *cobra.Command {
	var phoneType, phoneID, workflowID string
	var export bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Acquire a phone and open a session that remains active until stopped.",
		Long: "Acquire an Android phone. The CLI currently supports Android allocation " +
			"only; --phone-type remains available for scripts that pass android explicitly, " +
			"and any other value is a usage error. The session remains active in Axilio " +
			"until stopped; the CLI saves its connection information locally and marks " +
			"it as the most recently started session. Pin a dedicated phone discovered " +
			"through `phones mine` with --phone-id, or " +
			"attach the session to a workflow with --workflow. --export prints only " +
			"`export AXILIO_SESSION=<id>` for shell eval and cannot be combined with " +
			"-o json.",
		RunE: func(_ *cobra.Command, _ []string) error {
			if export && flagOutput == "json" {
				return exit.Usagef("--export cannot be combined with --output json")
			}
			normalizedPhoneType := strings.ToLower(strings.TrimSpace(phoneType))
			if normalizedPhoneType != string(platformgo.PhoneAllocateRequestPhoneTypeAndroid) {
				return exit.Usagef("unsupported --phone-type %q; supported value: android", phoneType)
			}
			p := printer()
			cl, err := newClient()
			if err != nil {
				return err
			}
			req := &platformgo.PhoneAllocateRequest{
				PhoneType: platformgo.PhoneAllocateRequestPhoneTypeAndroid,
			}
			if phoneID != "" {
				req.PhoneID = &phoneID
			}
			if workflowID != "" {
				req.WorkflowID = &workflowID
			}
			a, err := cl.Phones.Allocate(context.Background(), req)
			if err != nil {
				return err
			}
			// Record the lease in the registry (captures control_url, minted only
			// here) so `axilio phone ...` can drive it. Many leases coexist; this
			// one becomes the current-session pointer.
			if a.ControlURL != nil {
				_ = session.Save(session.Session{
					SessionID:  a.SessionID,
					PhoneID:    a.PhoneID,
					PhoneType:  normalizedPhoneType,
					ControlURL: *a.ControlURL,
				})
			}
			// --export: emit ONLY the eval-able line so a shell/agent can pin this
			// phone to the process: eval "$(axilio sessions start --export ...)".
			if export {
				p.Result("export %s=%s", session.EnvVar, a.SessionID)
				return p.Err()
			}
			if err := p.Emit(a, func() {
				p.KV([][2]string{
					{"Session", a.SessionID},
					{"Phone", a.PhoneID},
					{"Region", util.OrDash(strv(a.Region))},
					{"Live view", util.OrDash(strv(a.LiveViewURL))},
					{"Control URL", util.OrDash(strv(a.ControlURL))},
				})
			}); err != nil {
				return err
			}
			if a.ControlURL != nil {
				p.Note("\nDrive it:  axilio phone observe")
				p.Note("Pin it to this shell (for parallel work):  export %s=%s", session.EnvVar, a.SessionID)
			}
			p.Note("Release it with:  axilio sessions stop %s", a.SessionID)
			return p.Err()
		},
	}
	cmd.Flags().StringVar(&phoneType, "phone-type", "android", "Phone platform to allocate (currently only android)")
	cmd.Flags().StringVar(&phoneID, "phone-id", "", "Pin a dedicated phone ID from `phones mine` instead of pool allocation")
	cmd.Flags().StringVar(&workflowID, "workflow", "", "Workflow ID to attach; omit for an interactive session")
	cmd.Flags().BoolVar(&export, "export", false, "Print only `export AXILIO_SESSION=<id>` for shell eval")
	return cmd
}

func sessionsStopCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "stop <session-id|phone-id>",
		Short: "Release a session by session id or phone id.",
		Long: "Release an active phone allocation using either its session ID or phone " +
			"ID. Discover IDs with `sessions list --remote`; matching locally saved " +
			"session information and the most-recent-session marker are removed after " +
			"release. JSON output is the canonical API deallocation response, including " +
			"the phone, session, workflow, and deallocation time. Without --yes, table " +
			"mode prompts only when stdin is a terminal. " +
			"Redirected, JSON, and quiet execution do not prompt and require --yes.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			p := printer()
			cl, err := newClient()
			if err != nil {
				return err
			}
			id := args[0]
			phoneID := id
			// deallocate takes a phone_id; resolve a session_id to it via the active list.
			if resp, err := cl.Phones.ActiveSessions(context.Background(), &platformgo.PhonesActiveSessionsRequest{}); err == nil {
				for _, s := range resp.Sessions {
					if id == s.SessionID || id == s.PhoneID {
						phoneID = s.PhoneID
						break
					}
				}
			}
			if !yes && !p.Confirm(fmt.Sprintf("Release %s?", phoneID)) {
				if err := p.Err(); err != nil {
					return err
				}
				return exit.Usagef("aborted (pass --yes to release non-interactively)")
			}
			deallocation, err := cl.Phones.Deallocate(context.Background(), &platformgo.PhonesDeallocateRequest{PhoneID: phoneID})
			if err != nil {
				return err
			}
			// Drop the lease from the registry (clears the current pointer if it was it).
			_ = session.Remove(id)
			return p.Emit(deallocation, func() {
				p.Ack("Released %s.", deallocation.PhoneID)
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Release without prompting; required in JSON, quiet, or redirected execution")
	return cmd
}
