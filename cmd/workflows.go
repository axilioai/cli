package cmd

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/util"
	platformgo "github.com/axilioai/platform-go"
	"github.com/spf13/cobra"
)

// maxWorkflowsListLimit mirrors the backend's workflows-list page bound.
const maxWorkflowsListLimit int64 = 500

// maxWorkflowRevisionsLimit mirrors the backend's revisions-list page bound.
const maxWorkflowRevisionsLimit int64 = 200

// maxWorkflowSourceBytes mirrors the backend's cap on a code revision's
// source. Preflighting it keeps an oversized push a clear usage error instead
// of a 422 after the bytes have already moved.
const maxWorkflowSourceBytes = 256 * 1024

// workflowNameRe mirrors the backend's workflow-name rule (^[A-Za-z0-9_-]+$,
// unique within the org).
var workflowNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func workflowsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflows",
		Short: "Create, inspect, edit, and version workflows.",
		Long: "Manage workflows in the active organization. `workflows list` discovers " +
			"workflow IDs; create, get, and delete manage the records; pull, push, " +
			"revisions, and restore round-trip the Python source: pull the current " +
			"code to a file, edit locally, push it back as a new revision, and " +
			"restore any earlier revision. A workflow ID can be passed to `runs " +
			"start` or `sessions start --workflow`.\n\n" +
			"Running `axilio workflows` without a subcommand is equivalent to " +
			"`axilio workflows --help`: it only displays this help and does not list " +
			"workflows. Global flags shown here therefore have no effect. Pass flags " +
			"to a workflows subcommand instead.",
	}
	cmd.AddCommand(
		workflowsListCmd(),
		workflowsCreateCmd(),
		workflowsGetCmd(),
		workflowsDeleteCmd(),
		workflowsPullCmd(),
		workflowsPushCmd(),
		workflowsRevisionsCmd(),
		workflowsRestoreCmd(),
	)
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

func workflowsCreateCmd() *cobra.Command {
	var (
		platform  string
		codePath  string
		recording bool
		telemetry bool
		capture   bool
	)
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a workflow, optionally seeding its first code revision.",
		Long: "Create a workflow in the active organization. The name must contain only " +
			"letters, digits, hyphens, and underscores, and be unique within the org. " +
			"Pass --code with a Python file to save the workflow's first code revision " +
			"atomically with it; omit it to create an empty workflow and `workflows " +
			"push` code later. Recording, telemetry, and capture default to on " +
			"server-side; each toggle flag is sent only when set explicitly, so " +
			"omitted toggles keep the server defaults.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !workflowNameRe.MatchString(name) {
				return exit.Usagef("workflow name must contain only letters, digits, hyphens, and underscores (got %q)", name)
			}
			req := &platformgo.WorkflowCreateRequest{Name: name}
			if platform != "" {
				// The generated enum validates for us: a typo becomes a clear
				// client-side error instead of a 422 from the API.
				pf, err := platformgo.NewWorkflowCreateRequestPlatformFromString(strings.ToLower(strings.TrimSpace(platform)))
				if err != nil {
					return exit.Usagef("unsupported --platform value; use ios, android, or both")
				}
				req.Platform = &pf
			}
			// Send a toggle only when the user set it, so the backend keeps
			// owning the defaults (all three are true server-side).
			if cmd.Flags().Changed("recording") {
				req.Recording = &recording
			}
			if cmd.Flags().Changed("telemetry") {
				req.Telemetry = &telemetry
			}
			if cmd.Flags().Changed("capture") {
				req.Capture = &capture
			}
			if codePath != "" {
				code, err := readWorkflowSource(codePath, "--code")
				if err != nil {
					return err
				}
				req.Code = &code
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Workflows.Create(cmd.Context(), req)
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() {
				pairs := [][2]string{{"Workflow", resp.WorkflowID}}
				if id := resp.GetRevisionID(); id != nil {
					pairs = append(pairs, [2]string{"Revision", revisionLabel(resp.GetRevision())})
					pairs = append(pairs, [2]string{"Revision ID", *id})
				}
				p.KV(pairs)
			})
		},
	}
	cmd.Flags().StringVar(&platform, "platform", "", "Target platform: ios, android, or both; omitted uses the server default")
	cmd.Flags().StringVar(&codePath, "code", "", "Python file saved as the workflow's first code revision")
	cmd.Flags().BoolVar(&recording, "recording", true, "Record this workflow's runs; --recording=false suppresses video and thumbnails")
	cmd.Flags().BoolVar(&telemetry, "telemetry", true, "Persist telemetry spans for runs; --telemetry=false skips the durable trace store")
	cmd.Flags().BoolVar(&capture, "capture", true, "Capture media produced on the phone into the org library; --capture=false disables it")
	return cmd
}

func workflowsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <workflow-id>",
		Short: "Show one workflow's details and run statistics.",
		Long: "Show a single workflow: name, platform, status, OCR engine, the " +
			"recording/telemetry/capture toggles, timestamps, and run statistics. " +
			"Use `workflows list` to discover the workflow ID, `workflows pull` for " +
			"its code, and `workflows revisions` for its code history.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Workflows.Get(cmd.Context(), &platformgo.WorkflowsGetRequest{WorkflowID: args[0]})
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() {
				w := resp.GetWorkflow()
				if w == nil {
					p.Result("No workflow record in response.")
					return
				}
				pairs := [][2]string{
					{"Workflow", w.ID},
					{"Name", w.Name},
					{"Platform", string(w.Platform)},
					{"Status", string(w.Status)},
					{"OCR engine", string(w.OcrEngine)},
					{"Recording", fmt.Sprintf("%t", w.Recording)},
					{"Telemetry", fmt.Sprintf("%t", w.Telemetry)},
					{"Capture", fmt.Sprintf("%t", w.Capture)},
					{"Created", ts(w.CreatedAt)},
					{"Updated", ts(w.UpdatedAt)},
					{"Last run", util.OrDash(tsp(w.LastRunAt))},
				}
				if s := resp.GetStats(); s != nil {
					pairs = append(pairs, [2]string{"Total runs", fmt.Sprintf("%d", s.TotalRuns)})
					rate := "-"
					if s.TotalRuns > 0 {
						// success_rate is a 0.0-1.0 fraction in the API.
						rate = fmt.Sprintf("%.0f%%", s.SuccessRate*100)
					}
					pairs = append(pairs, [2]string{"Success rate", rate})
				}
				p.KV(pairs)
			})
		},
	}
	return cmd
}

func workflowsDeleteCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "delete <workflow-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a workflow.",
		Long: "Delete a workflow from the active organization, including its code " +
			"revisions. Runs already recorded against it are not removed. Without " +
			"--yes, table mode prompts only when stdin is a terminal. Redirected, " +
			"JSON, and quiet execution do not prompt and require --yes. The alias " +
			"`workflows rm` performs the same operation.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			id := args[0]
			p := printer()
			prompt := fmt.Sprintf("Delete workflow %s and its code revisions?", id)
			if !yes && !p.Confirm(prompt) {
				if err := p.Err(); err != nil {
					return err
				}
				return exit.Usagef("aborted (pass --yes to delete non-interactively)")
			}
			if _, err := cl.Workflows.Delete(cmd.Context(), &platformgo.WorkflowsDeleteRequest{WorkflowID: id}); err != nil {
				return err
			}
			// Emit rather than Note so `--output json` produces a result a
			// script can read, matching the other delete verbs.
			return p.Emit(deletedWorkflow{ID: id, Deleted: true}, func() {
				p.Ack("Deleted %s", id)
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Delete without prompting; required in JSON, quiet, or redirected execution")
	return cmd
}

func workflowsPullCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "pull <workflow-id>",
		Short: "Fetch the workflow's current code revision.",
		Long: "Fetch the Python source of the workflow's current code revision. By " +
			"default the source prints to stdout so it can be piped; --out writes it " +
			"to a file instead, overwriting existing contents without confirmation. " +
			"A workflow with no revisions yet yields empty source at revision 0. " +
			"Edit the pulled file locally and `workflows push` it back as a new " +
			"revision.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Workflows.GetCode(cmd.Context(), &platformgo.WorkflowsGetCodeRequest{WorkflowID: args[0]})
			if err != nil {
				return err
			}
			p := printer()
			if out == "" {
				return p.Emit(resp, func() {
					// Result appends one newline; trim the source's own so the
					// bytes round-trip through pull | push unchanged.
					p.Result("%s", strings.TrimSuffix(resp.Source, "\n"))
				})
			}
			if err := os.WriteFile(out, []byte(resp.Source), 0o644); err != nil {
				return err
			}
			return p.Emit(pulledWorkflowCode{
				Path:       out,
				Bytes:      len(resp.Source),
				Revision:   resp.Revision,
				RevisionID: resp.RevisionID,
			}, func() {
				p.Ack("Wrote %s (revision %d, %d bytes)", out, resp.Revision, len(resp.Source))
			})
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "File to write the source to; omitted prints it to stdout")
	return cmd
}

func workflowsPushCmd() *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "push <workflow-id> <file>",
		Short: "Save a local file as the workflow's new code revision.",
		Long: "Save a local Python file as a new code revision of the workflow, with " +
			"an optional commit-style --message. The server deduplicates against the " +
			"current revision by content hash: pushing unchanged source is a no-op " +
			"that creates no revision. Source is capped at 256 KiB. Use `workflows " +
			"revisions` to inspect history and `workflows restore` to return to an " +
			"earlier revision.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			source, err := readWorkflowSource(args[1], "the code")
			if err != nil {
				return err
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			req := &platformgo.WorkflowSaveCodeRequest{WorkflowID: args[0], Source: source}
			if message != "" {
				req.Message = &message
			}
			resp, err := cl.Workflows.SaveCode(cmd.Context(), req)
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() {
				if resp.NoOp {
					p.Ack("No change: source matches current revision %d", resp.Revision)
					return
				}
				p.Ack("Saved revision %d (%s)", resp.Revision, resp.RevisionID)
			})
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "Commit-style note stored with the revision")
	return cmd
}

func workflowsRevisionsCmd() *cobra.Command {
	var (
		limit  int64
		before int64
	)
	cmd := &cobra.Command{
		Use:   "revisions <workflow-id>",
		Short: "List a workflow's code revisions, newest first.",
		Long: "List the workflow's code revision history in reverse-chronological " +
			"order: revision number, revision ID, author, size, save time, and the " +
			"optional message. Page older history with --before, which returns " +
			"revisions numbered strictly below the given revision number. A listed " +
			"revision ID can be passed to `workflows restore`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit < 1 || limit > maxWorkflowRevisionsLimit {
				return exit.Usagef("--limit must be between 1 and %d (got %d)", maxWorkflowRevisionsLimit, limit)
			}
			if before < 0 {
				return exit.Usagef("--before must be zero or positive (got %d)", before)
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			req := &platformgo.WorkflowsListRevisionsRequest{WorkflowID: args[0], Limit: &limit}
			if before > 0 {
				req.Before = &before
			}
			resp, err := cl.Workflows.ListRevisions(cmd.Context(), req)
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() {
				if len(resp.Revisions) == 0 {
					p.Result("No revisions found.")
					return
				}
				rows := [][]string{{"REV", "REVISION ID", "AUTHOR", "SIZE", "CREATED", "MESSAGE"}}
				var oldest int64
				for _, rev := range resp.Revisions {
					rows = append(rows, []string{
						fmt.Sprintf("%d", rev.Revision), rev.ID, rev.AuthorUserID,
						humanBytes(rev.Bytes), ts(rev.CreatedAt), util.OrDash(strv(rev.Message)),
					})
					oldest = rev.Revision
				}
				p.Table(rows)
				if int64(len(resp.Revisions)) == limit && oldest > 1 {
					p.Note("showing %d; use --before %d to page older revisions", limit, oldest)
				}
			})
		},
	}
	cmd.Flags().Int64Var(&limit, "limit", 50, "Maximum revisions to return (1-200)")
	cmd.Flags().Int64Var(&before, "before", 0, "Return revisions numbered below this; 0 starts from the newest")
	return cmd
}

func workflowsRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <workflow-id> <revision-id>",
		Short: "Restore an earlier code revision as a new revision.",
		Long: "Create a new code revision containing the source of the named earlier " +
			"revision. Nothing is deleted and the action is not deduplicated, so the " +
			"restore itself is visible in `workflows revisions` history. Use " +
			"`workflows revisions` to discover revision IDs.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Workflows.RestoreRevision(cmd.Context(), &platformgo.WorkflowRestoreRevisionRequest{
				WorkflowID: args[0],
				RevisionID: args[1],
			})
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() {
				p.Ack("Restored %s as new revision %d (%s)", args[1], resp.Revision, resp.RevisionID)
			})
		},
	}
	return cmd
}

// readWorkflowSource reads a local Python file destined for a code revision
// and preflights the backend's source cap, so an unreadable or oversized file
// is a usage error before credentials or any HTTP request.
func readWorkflowSource(path, label string) (string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return "", exit.Usagef("cannot read %s file: %v", label, err)
	}
	if len(src) > maxWorkflowSourceBytes {
		return "", exit.Usagef("%s file exceeds the %d KiB source cap (got %d bytes)",
			label, maxWorkflowSourceBytes/1024, len(src))
	}
	return string(src), nil
}

// revisionLabel renders an optional revision number for detail views.
func revisionLabel(n *int64) string {
	if n == nil {
		return "-"
	}
	return fmt.Sprintf("%d", n)
}

// deletedWorkflow is the JSON shape of a successful delete, mirroring the
// other delete verbs so scripts read one contract.
type deletedWorkflow struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

// pulledWorkflowCode is the JSON shape of `workflows pull --out`: the source
// went to the file, so the document carries provenance rather than the bytes.
type pulledWorkflowCode struct {
	Path       string  `json:"path"`
	Bytes      int     `json:"bytes"`
	Revision   int64   `json:"revision"`
	RevisionID *string `json:"revision_id"`
}
