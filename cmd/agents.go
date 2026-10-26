package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/util"
	platformgo "github.com/axilioai/platform-go"
	"github.com/spf13/cobra"
)

// maxAgentsListLimit mirrors the backend's agents-list page bound.
const maxAgentsListLimit int64 = 500

// maxAgentRevisionsLimit mirrors the backend's revisions-list page bound.
const maxAgentRevisionsLimit int64 = 200

// maxAgentSourceBytes mirrors the backend's cap on a code revision's
// source. Preflighting it keeps an oversized push a clear usage error instead
// of a 422 after the bytes have already moved.
const maxAgentSourceBytes = 256 * 1024

// agentNameRe mirrors the backend's agent-name rule (^[A-Za-z0-9_-]+$,
// unique within the org).
var agentNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func agentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Create, inspect, edit, and version agents.",
		Long: "Manage agents in the active organization. `agents list` discovers " +
			"agent IDs; create, get, and delete manage the records; pull, push, " +
			"revisions, and rollback round-trip the Python code: pull the current " +
			"code to a file, edit locally, push it back as a new revision, and " +
			"roll back to any earlier revision. Start a run of an agent with `runs " +
			"start`.\n\n" +
			"Running `axilio agents` without a subcommand is equivalent to " +
			"`axilio agents --help`: it only displays this help and does not list " +
			"agents. Global flags shown here therefore have no effect. Pass flags " +
			"to an agents subcommand instead.",
	}
	cmd.AddCommand(
		agentsListCmd(),
		agentsCreateCmd(),
		agentsGetCmd(),
		agentsDeleteCmd(),
		agentsPullCmd(),
		agentsPushCmd(),
		agentsRevisionsCmd(),
		agentsRollbackCmd(),
	)
	return cmd
}

func agentsListCmd() *cobra.Command {
	var (
		limit  int64
		offset int64
		search string
		kind   string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List agents in your org.",
		Long: "List agents in the active organization. Results include agent ID, " +
			"name, model, and last-run time. Use --search for a name substring, " +
			"--kind to keep only code or instructions agents, and --limit and " +
			"--offset to page. A listed agent ID can be passed directly to " +
			"`runs start`.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit < 1 || limit > maxAgentsListLimit {
				return exit.Usagef("--limit must be between 1 and %d (got %d)", maxAgentsListLimit, limit)
			}
			if offset < 0 {
				return exit.Usagef("--offset must be zero or positive (got %d)", offset)
			}
			req := &platformgo.AgentsListRequest{Limit: &limit, Offset: &offset}
			if search != "" {
				req.Search = &search
			}
			if kind != "" {
				k, err := platformgo.NewAgentsListRequestKindFromString(strings.ToLower(strings.TrimSpace(kind)))
				if err != nil || k == platformgo.AgentsListRequestKindEmpty {
					return exit.Usagef("--kind must be code or instructions (got %q)", kind)
				}
				req.Kind = &k
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Agents.List(cmd.Context(), req)
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() {
				if len(resp.Agents) == 0 {
					p.Result("No agents found.")
					return
				}
				rows := [][]string{{"AGENT ID", "NAME", "MODEL", "LAST RUN"}}
				for _, a := range resp.Agents {
					rows = append(rows, []string{a.ID, a.Name, a.Model, util.OrDash(tsp(a.LastRunAt))})
				}
				p.Table(rows)
				if resp.Offset+int64(len(resp.Agents)) < resp.Total {
					p.Note("showing %d of %d; use --limit / --offset to page", len(resp.Agents), resp.Total)
				}
			})
		},
	}
	cmd.Flags().Int64Var(&limit, "limit", 20, "Maximum agents in this page (1-500)")
	cmd.Flags().Int64Var(&offset, "offset", 0, "Number of matching agents to skip before this page")
	cmd.Flags().StringVar(&search, "search", "", "Filter agent names by substring")
	cmd.Flags().StringVar(&kind, "kind", "", "Only agents whose current revision is code or instructions")
	return cmd
}

func agentsCreateCmd() *cobra.Command {
	var (
		codePath     string
		instructions string
		model        string
		recording    bool
		telemetry    bool
		capture      bool
		access       bool
	)
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create an agent, optionally seeding its first revision.",
		Long: "Create an agent in the active organization. The name must contain only " +
			"letters, digits, hyphens, and underscores, and be unique within the org. " +
			"Pass --code with a Python file to make it the agent's first revision " +
			"(a code revision), or --instructions with a task for a model to work " +
			"through (an instructions revision); the two cannot be combined. Omit " +
			"both to create an empty agent and `agents push` code later. Recording " +
			"defaults to off and telemetry and capture to on server-side, and " +
			"accessibility mode to off; each toggle flag is sent only when set " +
			"explicitly, so omitted toggles keep the server defaults. " +
			"--accessibility turns accessibility mode on for the agent's runs, " +
			"and every run then requires a phone that supports it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !agentNameRe.MatchString(name) {
				return exit.Usagef("agent name must contain only letters, digits, hyphens, and underscores (got %q)", name)
			}
			if codePath != "" && instructions != "" {
				return exit.Usagef("--code and --instructions cannot be combined; pick one first revision")
			}
			req := &platformgo.AgentCreateRequest{Name: name}
			if model != "" {
				req.Model = &model
			}
			// Send a toggle only when the user set it, so the backend keeps
			// owning the defaults.
			if cmd.Flags().Changed("recording") {
				req.RecordingEnabled = &recording
			}
			if cmd.Flags().Changed("telemetry") {
				req.TelemetryEnabled = &telemetry
			}
			if cmd.Flags().Changed("capture") {
				req.CaptureEnabled = &capture
			}
			if cmd.Flags().Changed("accessibility") {
				req.AccessibilityEnabled = &access
			}
			if codePath != "" {
				code, err := readAgentSource(codePath, "--code")
				if err != nil {
					return err
				}
				req.Code = &code
			}
			if instructions != "" {
				req.Instructions = &instructions
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Agents.Create(cmd.Context(), req)
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() {
				pairs := [][2]string{{"Agent", resp.ID}, {"Name", resp.Name}}
				if id := resp.GetCurrentRevisionID(); id != nil {
					pairs = append(pairs, [2]string{"Revision ID", *id})
				}
				p.KV(pairs)
			})
		},
	}
	cmd.Flags().StringVar(&codePath, "code", "", "Python file saved as the agent's first revision")
	cmd.Flags().StringVar(&instructions, "instructions", "", "Task text saved as the agent's first revision, for a model to work through")
	cmd.Flags().StringVar(&model, "model", "", "Model for instructions runs; omitted uses the platform default")
	cmd.Flags().BoolVar(&recording, "recording", false, "Record this agent's runs; off by default")
	cmd.Flags().BoolVar(&telemetry, "telemetry", true, "Persist telemetry spans for runs; --telemetry=false skips the durable trace store")
	cmd.Flags().BoolVar(&capture, "capture", true, "Capture media produced on the phone into the org library; --capture=false disables it")
	cmd.Flags().BoolVar(&access, "accessibility", false, "Turn on accessibility mode for runs, which then require a phone that supports it; off by default")
	return cmd
}

func agentsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <agent-id>",
		Short: "Show one agent's details.",
		Long: "Show a single agent: name, model, current revision, the " +
			"recording/telemetry/capture/accessibility toggles, run limits, and " +
			"timestamps. Use `agents list` to discover the agent ID, `agents pull` " +
			"for its code, `agents revisions` for its history, and `runs metrics " +
			"--agent` for its run statistics.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			a, err := cl.Agents.Get(cmd.Context(), &platformgo.AgentsGetRequest{AgentID: args[0]})
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(a, func() {
				p.KV([][2]string{
					{"Agent", a.ID},
					{"Name", a.Name},
					{"Model", a.Model},
					{"Revision ID", util.OrDash(strv(a.CurrentRevisionID))},
					{"Recording", fmt.Sprintf("%t", a.RecordingEnabled)},
					{"Telemetry", fmt.Sprintf("%t", a.TelemetryEnabled)},
					{"Capture", fmt.Sprintf("%t", a.CaptureEnabled)},
					{"Accessibility", fmt.Sprintf("%t", a.AccessibilityEnabled)},
					{"Max steps", fmt.Sprintf("%d", a.MaxSteps)},
					{"Timeout", fmt.Sprintf("%ds", a.TimeoutSeconds)},
					{"Created", ts(a.CreatedAt)},
					{"Updated", ts(a.UpdatedAt)},
					{"Last run", util.OrDash(tsp(a.LastRunAt))},
				})
			})
		},
	}
}

func agentsDeleteCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "delete <agent-id>",
		Aliases: []string{"rm"},
		Short:   "Delete an agent.",
		Long: "Delete an agent from the active organization, including its " +
			"revisions. Runs already recorded against it are not removed. Without " +
			"--yes, table mode prompts only when stdin is a terminal. Redirected, " +
			"JSON, and quiet execution do not prompt and require --yes. The alias " +
			"`agents rm` performs the same operation.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			id := args[0]
			p := printer()
			prompt := fmt.Sprintf("Delete agent %s and its revisions?", id)
			if !yes && !p.Confirm(prompt) {
				if err := p.Err(); err != nil {
					return err
				}
				return exit.Usagef("aborted (pass --yes to delete non-interactively)")
			}
			if err := cl.Agents.Delete(cmd.Context(), &platformgo.AgentsDeleteRequest{AgentID: id}); err != nil {
				return err
			}
			// Emit rather than Note so `--output json` produces a result a
			// script can read, matching the other delete verbs.
			return p.Emit(deletedAgent{ID: id, Deleted: true}, func() {
				p.Ack("Deleted %s", id)
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Delete without prompting; required in JSON, quiet, or redirected execution")
	return cmd
}

func agentsPullCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "pull <agent-id>",
		Short: "Fetch the agent's current code.",
		Long: "Fetch the Python code of the agent's current revision. By " +
			"default the code prints to stdout so it can be piped; --out writes it " +
			"to a file instead, overwriting existing contents without confirmation. " +
			"An agent with no revisions yet yields empty code at revision 0. An " +
			"agent whose current revision is instructions has no code to pull. " +
			"Edit the pulled file locally and `agents push` it back as a new " +
			"revision.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			a, err := cl.Agents.Get(cmd.Context(), &platformgo.AgentsGetRequest{AgentID: args[0]})
			if err != nil {
				return err
			}
			pulled := pulledAgentCode{Path: out}
			var code string
			if id := a.GetCurrentRevisionID(); id != nil {
				rev, err := cl.Agents.GetRevision(cmd.Context(), &platformgo.AgentsGetRevisionRequest{
					AgentID:    args[0],
					RevisionID: *id,
				})
				if err != nil {
					return err
				}
				if rev.Kind != platformgo.AgentRevisionKindCode {
					return exit.Usagef("agent %s's current revision is %s, not code; there is no code to pull", args[0], rev.Kind)
				}
				code = strv(rev.Code)
				pulled.Revision = rev.Revision
				pulled.RevisionID = &rev.ID
			}
			pulled.Bytes = len(code)
			p := printer()
			if out == "" {
				return p.Emit(agentCode{Source: code, Revision: pulled.Revision, RevisionID: pulled.RevisionID}, func() {
					// Result appends one newline; trim the code's own so the
					// bytes round-trip through pull | push unchanged.
					p.Result("%s", strings.TrimSuffix(code, "\n"))
				})
			}
			if err := os.WriteFile(out, []byte(code), 0o644); err != nil {
				return err
			}
			return p.Emit(pulled, func() {
				p.Ack("Wrote %s (revision %d, %d bytes)", out, pulled.Revision, len(code))
			})
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "File to write the code to; omitted prints it to stdout")
	return cmd
}

func agentsPushCmd() *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "push <agent-id> <file>",
		Short: "Save a local file as the agent's new revision.",
		Long: "Save a local Python file as a new code revision of the agent, with " +
			"an optional commit-style --message. The new revision becomes the one " +
			"runs use. Code is capped at 256 KiB. Use `agents revisions` to " +
			"inspect history and `agents rollback` to return to an earlier " +
			"revision.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := readAgentSource(args[1], "the code")
			if err != nil {
				return err
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			req := &platformgo.AgentRevisionCreateRequest{
				AgentID: args[0],
				Kind:    platformgo.AgentRevisionCreateRequestKindCode,
				Code:    &code,
			}
			if message != "" {
				req.Message = &message
			}
			resp, err := cl.Agents.CreateRevision(cmd.Context(), req)
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() {
				p.Ack("Saved revision %d (%s)", resp.Revision, resp.ID)
			})
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "Commit-style note stored with the revision")
	return cmd
}

func agentsRevisionsCmd() *cobra.Command {
	var (
		limit  int64
		offset int64
	)
	cmd := &cobra.Command{
		Use:   "revisions <agent-id>",
		Short: "List an agent's revisions, newest first.",
		Long: "List the agent's revision history in reverse-chronological " +
			"order: revision number, revision ID, kind, author, size, save time, " +
			"and the optional message. Page older history with --limit and " +
			"--offset. A listed revision ID can be passed to `agents rollback`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit < 1 || limit > maxAgentRevisionsLimit {
				return exit.Usagef("--limit must be between 1 and %d (got %d)", maxAgentRevisionsLimit, limit)
			}
			if offset < 0 {
				return exit.Usagef("--offset must be zero or positive (got %d)", offset)
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Agents.ListRevisions(cmd.Context(), &platformgo.AgentsListRevisionsRequest{
				AgentID: args[0],
				Limit:   &limit,
				Offset:  &offset,
			})
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() {
				if len(resp.Revisions) == 0 {
					p.Result("No revisions found.")
					return
				}
				rows := [][]string{{"REV", "REVISION ID", "KIND", "AUTHOR", "SIZE", "CREATED", "MESSAGE"}}
				for _, rev := range resp.Revisions {
					rows = append(rows, []string{
						fmt.Sprintf("%d", rev.Revision), rev.ID, string(rev.Kind), rev.AuthorUserID,
						humanBytes(rev.Bytes), ts(rev.CreatedAt), util.OrDash(strv(rev.Message)),
					})
				}
				p.Table(rows)
				if resp.Offset+int64(len(resp.Revisions)) < resp.Total {
					p.Note("showing %d of %d; use --limit / --offset to page", len(resp.Revisions), resp.Total)
				}
			})
		},
	}
	cmd.Flags().Int64Var(&limit, "limit", 50, "Maximum revisions to return (1-200)")
	cmd.Flags().Int64Var(&offset, "offset", 0, "Number of newer revisions to skip before this page")
	return cmd
}

func agentsRollbackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rollback <agent-id> <revision-id>",
		Short: "Bring an earlier revision back as the newest one.",
		Long: "Copy an earlier revision forward as the agent's newest revision, " +
			"which becomes the one runs use. Nothing is deleted, so the rollback " +
			"itself is visible in `agents revisions` history. Use `agents " +
			"revisions` to discover revision IDs.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Agents.Rollback(cmd.Context(), &platformgo.AgentRollbackRequest{
				AgentID:    args[0],
				RevisionID: args[1],
			})
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() {
				p.Ack("Rolled back to %s as new revision %d (%s)", args[1], resp.Revision, resp.ID)
			})
		},
	}
}

// readAgentSource reads a local Python file destined for a code revision
// and preflights the backend's code cap, so an unreadable or oversized file
// is a usage error before credentials or any HTTP request.
func readAgentSource(path, label string) (string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return "", exit.Usagef("cannot read %s file: %v", label, err)
	}
	if len(src) > maxAgentSourceBytes {
		return "", exit.Usagef("%s file exceeds the %d KiB source cap (got %d bytes)",
			label, maxAgentSourceBytes/1024, len(src))
	}
	return string(src), nil
}

// deletedAgent is the JSON shape of a successful delete, mirroring the
// other delete verbs so scripts read one contract.
type deletedAgent struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

// agentCode is the JSON shape of `agents pull` printed to stdout.
type agentCode struct {
	Source     string  `json:"source"`
	Revision   int64   `json:"revision"`
	RevisionID *string `json:"revision_id"`
}

// pulledAgentCode is the JSON shape of `agents pull --out`: the code
// went to the file, so the document carries provenance rather than the bytes.
type pulledAgentCode struct {
	Path       string  `json:"path"`
	Bytes      int     `json:"bytes"`
	Revision   int64   `json:"revision"`
	RevisionID *string `json:"revision_id"`
}
