package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/output"
	platformgo "github.com/axilioai/platform-go"
	"github.com/axilioai/platform-go/client"
	"github.com/spf13/cobra"
)

// maxDownloadsListLimit mirrors the backend's downloads-list page bound.
const maxDownloadsListLimit int64 = 100

// downloadsCmd is `axilio downloads` (AXI-1863): the capture half of the file
// library. Sessions capture files coming off phones (AXI-1449) into the same
// org library that `uploads` fills from the other direction, but until now the
// CLI only handled uploads — a session could capture a file and its owner had
// no way to retrieve it without the dashboard. `list` discovers captures,
// `get` saves one locally via its signed URL, and `delete` recalls it, the
// same recall an upload's delete runs.
func downloadsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "downloads",
		Short: "List, save, and delete files captured off phones during sessions.",
		Long: "Manage captured downloads: files that came off phones during sessions. " +
			"Captures land in the org library next to uploads and share its storage " +
			"quota; a ready capture can be delivered to any phone by its id with " +
			"`uploads push`. `list` discovers captures across sessions, `get` saves " +
			"a capture's bytes to a local file, and `delete` removes it everywhere. " +
			"For one session's captures, `sessions downloads <session-id>` answers " +
			"directly.\n\n" +
			"Running `axilio downloads` without a subcommand is equivalent to " +
			"`axilio downloads --help`: it only displays this help. Global flags " +
			"shown here therefore have no effect. Pass flags to a downloads " +
			"subcommand instead.",
	}
	cmd.AddCommand(downloadsListCmd(), downloadsGetCmd(), downloadsDeleteCmd())
	return cmd
}

// downloadListFlags is the filter set shared by `downloads list` and
// `sessions downloads`; both back onto the same query parameters.
type downloadListFlags struct {
	limit         int64
	offset        int64
	search        string
	mimeType      string
	minSize       int64
	maxSize       int64
	createdAfter  string
	createdBefore string
}

func (f *downloadListFlags) register(cmd *cobra.Command) {
	cmd.Flags().Int64Var(&f.limit, "limit", 50, "Maximum captures in this page (1-100)")
	cmd.Flags().Int64Var(&f.offset, "offset", 0, "Number of matching captures to skip before this page")
	cmd.Flags().StringVar(&f.search, "search", "", "Filter filenames by case-insensitive substring")
	cmd.Flags().StringVar(&f.mimeType, "mime", "", "Only captures of exactly this media type, e.g. image/png")
	cmd.Flags().Int64Var(&f.minSize, "min-size", 0, "Only captures at least this many bytes; 0 means no bound")
	cmd.Flags().Int64Var(&f.maxSize, "max-size", 0, "Only captures at most this many bytes; 0 means no bound")
	cmd.Flags().StringVar(&f.createdAfter, "created-after", "", "Only captures registered at or after this RFC 3339 time")
	cmd.Flags().StringVar(&f.createdBefore, "created-before", "", "Only captures registered at or before this RFC 3339 time")
}

// validate applies the backend's documented bounds before credentials or any
// HTTP request, mirroring the uploads/runs preflight pattern.
func (f *downloadListFlags) validate() error {
	if f.limit < 1 || f.limit > maxDownloadsListLimit {
		return exit.Usagef("--limit must be between 1 and %d (got %d)", maxDownloadsListLimit, f.limit)
	}
	if f.offset < 0 {
		return exit.Usagef("--offset must be zero or positive (got %d)", f.offset)
	}
	if f.minSize < 0 {
		return exit.Usagef("--min-size must be zero or positive (got %d)", f.minSize)
	}
	if f.maxSize < 0 {
		return exit.Usagef("--max-size must be zero or positive (got %d)", f.maxSize)
	}
	return nil
}

// parseFlagTime turns an RFC 3339 flag value into a time, naming the flag in
// the usage error so a bad timestamp reads as the user's mistake, not the API's.
func parseFlagTime(flag, value string) (*time.Time, error) {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, exit.Usagef("--%s must be RFC 3339, e.g. 2026-08-22T15:04:05Z (got %q)", flag, value)
	}
	return &t, nil
}

func downloadsListCmd() *cobra.Command {
	var (
		flags         downloadListFlags
		sessionID     string
		sortBy, order string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the files captured off phones, newest first.",
		Long: "List captured downloads for the active organization. Each row carries " +
			"its capture state: a skipped or failed capture is a visible entry with a " +
			"reason, not an absence. Results include capture ID, filename, size, MIME " +
			"type, state, source session, and creation time. Page with --limit and " +
			"--offset, narrow by --session, --mime, size and time bounds, and sort by " +
			"created_at, filename, or size_bytes in asc or desc order. Omitting " +
			"sort/order uses server defaults (newest first).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := flags.validate(); err != nil {
				return err
			}
			req := &platformgo.DownloadsListRequest{Limit: &flags.limit, Offset: &flags.offset}
			if flags.search != "" {
				req.Q = &flags.search
			}
			if sessionID != "" {
				req.SessionID = &sessionID
			}
			if flags.mimeType != "" {
				req.MimeType = &flags.mimeType
			}
			if flags.minSize > 0 {
				req.MinSizeBytes = &flags.minSize
			}
			if flags.maxSize > 0 {
				req.MaxSizeBytes = &flags.maxSize
			}
			if flags.createdAfter != "" {
				t, err := parseFlagTime("created-after", flags.createdAfter)
				if err != nil {
					return err
				}
				req.CreatedAfter = t
			}
			if flags.createdBefore != "" {
				t, err := parseFlagTime("created-before", flags.createdBefore)
				if err != nil {
					return err
				}
				req.CreatedBefore = t
			}
			// The generated enums validate for us, which turns a typo into a
			// clear client-side error instead of a 422 from the API.
			if sortBy != "" {
				s, err := platformgo.NewDownloadsListRequestSortFromString(sortBy)
				if err != nil {
					return exit.Usagef("unsupported --sort value; use created_at, filename, or size_bytes")
				}
				req.Sort = &s
			}
			if order != "" {
				o, err := platformgo.NewDownloadsListRequestOrderFromString(order)
				if err != nil {
					return exit.Usagef("unsupported --order value; use asc or desc")
				}
				req.Order = &o
			}

			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Downloads.List(cmd.Context(), req)
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() { printDownloadList(p, resp) })
		},
	}
	flags.register(cmd)
	cmd.Flags().StringVar(&sessionID, "session", "", "Only captures produced by this session ID")
	cmd.Flags().StringVar(&sortBy, "sort", "", "Sort key: created_at, filename, or size_bytes; omitted uses server default")
	cmd.Flags().StringVar(&order, "order", "", "Sort direction: asc or desc; omitted uses server default")
	return cmd
}

// printDownloadList renders one page of captures the same way for
// `downloads list` and `sessions downloads`, so the two entry points to the
// same data agree on how it reads.
func printDownloadList(p *output.Printer, resp *platformgo.FileDownloadListResponse) {
	if len(resp.Downloads) == 0 {
		p.Result("No captured downloads.")
		return
	}
	rows := [][]string{{"ID", "FILENAME", "SIZE", "TYPE", "STATE", "SESSION", "CREATED"}}
	for _, d := range resp.Downloads {
		state := string(d.CaptureState)
		if d.CaptureError != nil && *d.CaptureError != "" {
			state += ": " + *d.CaptureError
		}
		rows = append(rows, []string{
			d.ID, d.Filename, humanBytes(d.SizeBytes), d.MimeType, state, strv(d.SessionID), ts(d.CreatedAt),
		})
	}
	p.Table(rows)
	if resp.Total > int64(len(resp.Downloads)) {
		p.Note("showing %d of %d; use --limit / --offset to page", len(resp.Downloads), resp.Total)
	}
}

// savedDownload is the JSON shape of a successful `downloads get`.
type savedDownload struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
}

func downloadsGetCmd() *cobra.Command {
	var (
		outPath string
		force   bool
	)
	cmd := &cobra.Command{
		Use:   "get <download-id>",
		Short: "Save a captured file's bytes to a local file.",
		Long: "Fetch a ready capture through its short-lived signed URL and write the " +
			"bytes to a local file. The destination defaults to the capture's own " +
			"filename in the current directory; -o overrides it. An existing " +
			"destination is refused unless --force is passed. Captures still in " +
			"flight or terminally skipped have no bytes to fetch and are refused " +
			"with their capture state. Use `downloads list` to discover capture IDs " +
			"and states.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			d, err := findDownload(cmd, cl, args[0])
			if err != nil {
				return err
			}
			if d.DownloadURL == nil || *d.DownloadURL == "" {
				state := string(d.CaptureState)
				if d.CaptureError != nil && *d.CaptureError != "" {
					state += ": " + *d.CaptureError
				}
				return fmt.Errorf("download %s has no bytes to fetch (capture state %s)", d.ID, state)
			}
			dest := outPath
			if dest == "" {
				dest = d.Filename
			}
			p := printer()
			p.Step("Saving %s to %s", d.Filename, dest)
			if err := p.Err(); err != nil {
				return err
			}
			written, err := saveURL(cmd, *d.DownloadURL, dest, force)
			if err != nil {
				return err
			}
			saved := savedDownload{ID: d.ID, Filename: d.Filename, Path: dest, SizeBytes: written}
			return p.Emit(saved, func() {
				p.Ack("Saved %s (%s)", dest, humanBytes(written))
			})
		},
	}
	// --out has no -o shorthand: the root command owns -o for --output.
	cmd.Flags().StringVar(&outPath, "out", "", "Destination path; omitted uses the capture's filename in the current directory")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite the destination if it already exists")
	return cmd
}

// findDownload resolves a capture by id by walking the list pages: the API has
// no get-by-id operation (the dashboard only ever renders the list), so the
// CLI resolves ids client-side rather than inventing a route.
func findDownload(cmd *cobra.Command, cl *client.Client, id string) (*platformgo.FileDownloadSummary, error) {
	limit := maxDownloadsListLimit
	for offset := int64(0); ; {
		resp, err := cl.Downloads.List(cmd.Context(), &platformgo.DownloadsListRequest{Limit: &limit, Offset: &offset})
		if err != nil {
			return nil, err
		}
		for _, d := range resp.Downloads {
			if d.ID == id {
				return d, nil
			}
		}
		offset += int64(len(resp.Downloads))
		if len(resp.Downloads) == 0 || offset >= resp.Total {
			return nil, fmt.Errorf("no download with id %s; use `downloads list` to discover ids", id)
		}
	}
}

// saveURL streams a signed URL's body to dest. Creation is exclusive unless
// force, so a stray id can't silently clobber a local file.
func saveURL(cmd *cobra.Command, url, dest string, force bool) (int64, error) {
	openFlags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		openFlags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(dest, openFlags, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return 0, exit.Usagef("%s already exists; pass --force to overwrite", dest)
		}
		return 0, err
	}
	defer f.Close()

	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	// The signed URL is self-authorizing; no API key header is sent to it.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fetching download bytes: %s (signed URLs expire; re-run to mint a fresh one)", resp.Status)
	}
	return io.Copy(f, resp.Body)
}

// deletedDownload is the JSON shape of a successful `downloads delete`.
type deletedDownload struct {
	ID                   string `json:"id"`
	Deleted              bool   `json:"deleted"`
	PhonesPendingRemoval int64  `json:"phones_pending_removal"`
}

func downloadsDeleteCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "delete <download-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a captured file and free its quota.",
		Long: "Delete a capture from the org library and free its storage quota. " +
			"Deletion also recalls every copy delivered to phones; the source phone's " +
			"own copy from the capture session is outside the recall. Use " +
			"`downloads list` to discover the capture ID. Without --yes, table mode " +
			"prompts only when stdin is a terminal. Redirected, JSON, and quiet " +
			"execution do not prompt and require --yes. The alias `downloads rm` " +
			"performs the same operation.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			id := args[0]
			p := printer()
			prompt := fmt.Sprintf(
				"Delete download %s? Also recall it from phones holding or receiving a copy?", id)
			if !yes && !p.Confirm(prompt) {
				if err := p.Err(); err != nil {
					return err
				}
				return exit.Usagef("aborted (pass --yes to delete non-interactively)")
			}
			req := &platformgo.DownloadsDeleteRequest{DownloadID: id}
			resp, err := cl.Downloads.Delete(cmd.Context(), req)
			if err != nil {
				return err
			}
			out := deletedDownload{ID: id, Deleted: true}
			if resp != nil {
				out.PhonesPendingRemoval = resp.PhonesPendingRemoval
			}
			return p.Emit(out, func() {
				p.Ack("Deleted %s", id)
				if out.PhonesPendingRemoval > 0 {
					p.Note("recall scheduled on %d phone(s)", out.PhonesPendingRemoval)
				}
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Delete without prompting; required in JSON, quiet, or redirected execution")
	return cmd
}

// sessionsDownloadsCmd is `axilio sessions downloads`: the per-session view of
// the same captures, backed by the session-scoped endpoint so it answers
// "what did this session download" without client-side filtering.
func sessionsDownloadsCmd() *cobra.Command {
	var flags downloadListFlags
	cmd := &cobra.Command{
		Use:   "downloads <session-id>",
		Short: "List the files a session captured off its phone.",
		Long: "List one session's captured downloads, newest first — the direct answer " +
			"to \"what did this session download\". Rows appear at detection, before " +
			"the bytes finish moving, so a caller waiting on a file watches its " +
			"capture state progress rather than an empty list. Save a ready capture " +
			"with `downloads get`. Filters and paging match `downloads list`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := flags.validate(); err != nil {
				return err
			}
			req := &platformgo.PhonesSessionDownloadsRequest{
				SessionID: args[0],
				Limit:     &flags.limit,
				Offset:    &flags.offset,
			}
			if flags.search != "" {
				req.Q = &flags.search
			}
			if flags.mimeType != "" {
				req.MimeType = &flags.mimeType
			}
			if flags.minSize > 0 {
				req.MinSizeBytes = &flags.minSize
			}
			if flags.maxSize > 0 {
				req.MaxSizeBytes = &flags.maxSize
			}
			if flags.createdAfter != "" {
				t, err := parseFlagTime("created-after", flags.createdAfter)
				if err != nil {
					return err
				}
				req.CreatedAfter = t
			}
			if flags.createdBefore != "" {
				t, err := parseFlagTime("created-before", flags.createdBefore)
				if err != nil {
					return err
				}
				req.CreatedBefore = t
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Downloads.PhonesSessionDownloads(cmd.Context(), req)
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() { printDownloadList(p, resp) })
		},
	}
	flags.register(cmd)
	return cmd
}
