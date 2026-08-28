package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/output"
	platformgo "github.com/axilioai/platform-go"
	"github.com/axilioai/platform-go/client"
	files "github.com/axilioai/platform-go/drivers/files"
	"github.com/spf13/cobra"
)

// maxFilesListLimit mirrors the backend's files-list page bound.
const maxFilesListLimit int64 = 100

// filesCmd is `axilio files` (AXI-1905): the org file library, unified.
//
// Files enter the library two ways — a customer/agent uploads one directly, or
// a session captures one off a phone (AXI-1449) — and both live in one library
// with a shared id space. That provenance is an attribute on each file
// (`source` = upload|capture, plus `surface` for a capture), not a separate
// collection: the CLI used to split it across `uploads` and `downloads`, which
// this command replaces. `uploads`/`downloads` remain as hidden aliases so
// existing scripts keep working.
//
// The library is a flat, org-scoped namespace (no folders, by decision), so
// `list` leans on search, filters, and sort rather than navigation, and prints
// standing quota usage as a footer because a capped resource should show its
// cap wherever you look at it.
func filesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "files",
		Aliases: []string{"uploads", "downloads"},
		Short:   "Upload, list, download, push, and delete files in your organization's library.",
		Long: "Manage the org file library. Files live here until deleted and can be " +
			"pushed to any phone the org holds, so one file serves many phones. Every " +
			"file has a source: `upload` (you put it in) or `capture` (a session pulled " +
			"it off a phone). `upload` stores a local file, `list` discovers files and " +
			"quota (filter by --source/--surface/--session), `download` saves a file's " +
			"bytes locally, `push` delivers a stored file to a phone, and `delete` frees " +
			"quota and recalls delivered copies. `phone send` combines upload and push " +
			"for the selected session's phone.\n\n" +
			"Running `axilio files` without a subcommand is equivalent to " +
			"`axilio files --help`: it only displays this help. Global flags shown here " +
			"therefore have no effect. Pass flags to a files subcommand instead.",
	}
	cmd.AddCommand(filesUploadCmd(), filesListCmd(), filesDownloadCmd(), filesPushCmd(), filesDeleteCmd())
	return cmd
}

func filesUploadCmd() *cobra.Command {
	var filename, mimeType string
	cmd := &cobra.Command{
		Use:     "upload <path>",
		Aliases: []string{"add"},
		Short:   "Upload a local file into the library without pushing it anywhere.",
		Long: "Register a local file, upload its bytes, and verify them, leaving a ready " +
			"file in the library (source=upload). Use `axilio files push` to send it to " +
			"a phone, or `axilio phone send` to do both in one step. The stored filename " +
			"defaults to the local basename and MIME type is inferred from the " +
			"extension; --filename and --mime-type override those values. The library " +
			"stores files up to 1 GiB, including files too large to deliver to a phone " +
			"(phone delivery is limited to 100 MiB per file).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			opts := []files.Option{}
			if filename != "" {
				opts = append(opts, files.WithFilename(filename))
			}
			if mimeType != "" {
				opts = append(opts, files.WithMimeType(mimeType))
			}
			p := printer()
			p.Step("Uploading %s", filepath.Base(args[0]))
			if err := p.Err(); err != nil {
				return err
			}
			f, err := files.Upload(cmd.Context(), cl, args[0], opts...)
			if err != nil {
				return err
			}
			return p.Emit(f, func() {
				p.KV([][2]string{
					{"ID", f.ID},
					{"Filename", f.Filename},
					{"Size", humanBytes(f.SizeBytes)},
					{"Type", f.MimeType},
					{"Source", string(f.Source)},
					{"Status", string(f.Status)},
				})
			})
		},
	}
	cmd.Flags().StringVar(&filename, "filename", "", "Stored filename; omitted uses the local file's basename")
	cmd.Flags().StringVar(&mimeType, "mime-type", "", "Stored MIME type; omitted infers it from the file extension")
	return cmd
}

// filesListFlags is the filter set shared by `files list` and `sessions files`;
// both back onto the same query parameters.
type filesListFlags struct {
	limit         int64
	offset        int64
	search        string
	mimeType      string
	minSize       int64
	maxSize       int64
	createdAfter  string
	createdBefore string
}

func (f *filesListFlags) register(cmd *cobra.Command) {
	cmd.Flags().Int64Var(&f.limit, "limit", 50, "Maximum files in this page (1-100)")
	cmd.Flags().Int64Var(&f.offset, "offset", 0, "Number of matching files to skip before this page")
	cmd.Flags().StringVar(&f.search, "search", "", "Filter filenames by case-insensitive substring")
	cmd.Flags().StringVar(&f.mimeType, "mime", "", "Only files of exactly this media type, e.g. image/png")
	cmd.Flags().Int64Var(&f.minSize, "min-size", 0, "Only files at least this many bytes; 0 means no bound")
	cmd.Flags().Int64Var(&f.maxSize, "max-size", 0, "Only files at most this many bytes; 0 means no bound")
	cmd.Flags().StringVar(&f.createdAfter, "created-after", "", "Only files registered at or after this RFC 3339 time")
	cmd.Flags().StringVar(&f.createdBefore, "created-before", "", "Only files registered at or before this RFC 3339 time")
}

func (f *filesListFlags) validate() error {
	if f.limit < 1 || f.limit > maxFilesListLimit {
		return exit.Usagef("--limit must be between 1 and %d (got %d)", maxFilesListLimit, f.limit)
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

// apply folds the shared filters onto a FilesListRequest, so the global and
// session-scoped listings can never drift on what is filterable.
func (f *filesListFlags) apply(req *platformgo.FilesListRequest) error {
	if f.search != "" {
		req.Q = &f.search
	}
	if f.mimeType != "" {
		req.MimeType = &f.mimeType
	}
	if f.minSize > 0 {
		req.MinSizeBytes = &f.minSize
	}
	if f.maxSize > 0 {
		req.MaxSizeBytes = &f.maxSize
	}
	if f.createdAfter != "" {
		t, err := parseFlagTime("created-after", f.createdAfter)
		if err != nil {
			return err
		}
		req.CreatedAfter = t
	}
	if f.createdBefore != "" {
		t, err := parseFlagTime("created-before", f.createdBefore)
		if err != nil {
			return err
		}
		req.CreatedBefore = t
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

func filesListCmd() *cobra.Command {
	var (
		flags         filesListFlags
		source        string
		surface       string
		sessionID     string
		sortBy, order string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the files in your organization's library, newest first.",
		Long: "List files and standing storage quota for the active organization. Each " +
			"row shows its source (upload or capture) and, for a capture, its state — a " +
			"skipped or failed capture is a visible entry with a reason, not an absence. " +
			"Filter by --source (upload/capture), --surface (phone), --session, --mime, " +
			"size and time bounds; search filenames by case-insensitive substring; page " +
			"with --limit and --offset; sort by created_at, filename, size_bytes, or " +
			"source in asc or desc order. Omitting sort/order uses server defaults " +
			"(newest first).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := flags.validate(); err != nil {
				return err
			}
			req := &platformgo.FilesListRequest{Limit: &flags.limit, Offset: &flags.offset}
			if err := flags.apply(req); err != nil {
				return err
			}
			if sessionID != "" {
				req.SessionID = &sessionID
			}
			// The generated enums validate for us, which turns a typo into a
			// clear client-side error instead of a 422 from the API.
			if source != "" {
				s, err := platformgo.NewFilesListRequestSourceFromString(source)
				if err != nil {
					return exit.Usagef("unsupported --source value; use upload or capture")
				}
				req.Source = &s
			}
			if surface != "" {
				s, err := platformgo.NewFilesListRequestSurfaceFromString(surface)
				if err != nil {
					return exit.Usagef("unsupported --surface value; use phone")
				}
				req.Surface = &s
			}
			if sortBy != "" {
				s, err := platformgo.NewFilesListRequestSortFromString(sortBy)
				if err != nil {
					return exit.Usagef("unsupported --sort value; use created_at, filename, size_bytes, or source")
				}
				req.Sort = &s
			}
			if order != "" {
				o, err := platformgo.NewFilesListRequestOrderFromString(order)
				if err != nil {
					return exit.Usagef("unsupported --order value; use asc or desc")
				}
				req.Order = &o
			}

			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := files.List(cmd.Context(), cl, req)
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() { printFileList(p, resp) })
		},
	}
	flags.register(cmd)
	cmd.Flags().StringVar(&source, "source", "", "Only files of this source: upload or capture")
	cmd.Flags().StringVar(&surface, "surface", "", "Only captures off this surface: phone")
	cmd.Flags().StringVar(&sessionID, "session", "", "Only files captured by this session ID")
	cmd.Flags().StringVar(&sortBy, "sort", "", "Sort key: created_at, filename, size_bytes, or source; omitted uses server default")
	cmd.Flags().StringVar(&order, "order", "", "Sort direction: asc or desc; omitted uses server default")
	return cmd
}

// fileState renders the lifecycle a scanner cares about: a capture's explicit
// capture_state (with any failure reason) when present, else the storage status.
func fileState(f *platformgo.FileSummary) string {
	if f.CaptureState != nil {
		state := string(*f.CaptureState)
		if f.CaptureError != nil && *f.CaptureError != "" {
			state += ": " + *f.CaptureError
		}
		return state
	}
	return string(f.Status)
}

// printFileList renders one page of the library the same way for `files list`
// and `sessions files`, so the two entry points to the same data agree.
func printFileList(p *output.Printer, resp *platformgo.FileListResponse) {
	if len(resp.Files) == 0 {
		p.Result("No files in the library.")
	} else {
		rows := [][]string{{"ID", "FILENAME", "SIZE", "TYPE", "SOURCE", "STATE", "SESSION", "CREATED"}}
		for _, f := range resp.Files {
			rows = append(rows, []string{
				f.ID, f.Filename, humanBytes(f.SizeBytes), f.MimeType,
				string(f.Source), fileState(f), strv(f.SessionID), ts(f.CreatedAt),
			})
		}
		p.Table(rows)
	}
	// Usage rides every listing so a caller can show "X of Y" without a second
	// call; print it whether or not this page had rows, since an empty page
	// still has a real quota.
	if u := resp.Usage; u != nil {
		p.Note("\n%d of %d files, %s of %s used",
			u.FileCount, u.FileLimit, humanBytes(u.TotalBytes), humanBytes(u.ByteLimit))
	}
	if resp.Total > int64(len(resp.Files)) {
		p.Note("showing %d of %d; use --limit / --offset to page", len(resp.Files), resp.Total)
	}
}

// savedFile is the JSON shape of a successful `files download`.
type savedFile struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
}

func filesDownloadCmd() *cobra.Command {
	var (
		outPath string
		force   bool
	)
	cmd := &cobra.Command{
		Use:     "download <file-id>",
		Aliases: []string{"get"},
		Short:   "Save a file's bytes to a local file.",
		Long: "Fetch a ready file through its short-lived signed URL and write the bytes " +
			"to a local file. Works for any source — an upload or a capture. The " +
			"destination defaults to the file's own filename in the current directory; " +
			"--out overrides it. An existing destination is refused unless --force is " +
			"passed. Files still uploading or terminally skipped have no bytes to fetch " +
			"and are refused with their state. Use `files list` to discover file IDs.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			f, err := findFile(cmd, cl, args[0])
			if err != nil {
				return err
			}
			if f.DownloadURL == nil || *f.DownloadURL == "" {
				return fmt.Errorf("file %s has no bytes to fetch (state %s)", f.ID, fileState(f))
			}
			dest := outPath
			if dest == "" {
				dest = f.Filename
			}
			p := printer()
			p.Step("Saving %s to %s", f.Filename, dest)
			if err := p.Err(); err != nil {
				return err
			}
			written, err := saveURL(cmd, *f.DownloadURL, dest, force)
			if err != nil {
				return err
			}
			saved := savedFile{ID: f.ID, Filename: f.Filename, Path: dest, SizeBytes: written}
			return p.Emit(saved, func() {
				p.Ack("Saved %s (%s)", dest, humanBytes(written))
			})
		},
	}
	// --out has no -o shorthand: the root command owns -o for --output.
	cmd.Flags().StringVar(&outPath, "out", "", "Destination path; omitted uses the file's filename in the current directory")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite the destination if it already exists")
	return cmd
}

// findFile resolves a file by id by walking the list pages: the API has no
// get-by-id operation (the dashboard only ever renders the list), so the CLI
// resolves ids client-side rather than inventing a route.
func findFile(cmd *cobra.Command, cl *client.Client, id string) (*platformgo.FileSummary, error) {
	limit := maxFilesListLimit
	for offset := int64(0); ; {
		resp, err := files.List(cmd.Context(), cl, &platformgo.FilesListRequest{Limit: &limit, Offset: &offset})
		if err != nil {
			return nil, err
		}
		for i := range resp.Files {
			if resp.Files[i].ID == id {
				return resp.Files[i], nil
			}
		}
		offset += int64(len(resp.Files))
		if len(resp.Files) == 0 || offset >= resp.Total {
			return nil, fmt.Errorf("no file with id %s; use `files list` to discover ids", id)
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
		return 0, fmt.Errorf("fetching file bytes: %s (signed URLs expire; re-run to mint a fresh one)", resp.Status)
	}
	return io.Copy(f, resp.Body)
}

func filesPushCmd() *cobra.Command {
	var (
		phoneID    string
		collection string
		wait       bool
		timeout    time.Duration
	)
	cmd := &cobra.Command{
		Use:   "push <file-id>",
		Short: "Push a file already in the library to a phone.",
		Long: "Send a stored library file to a phone's media library. Unlike " +
			"`axilio phone send` this uploads nothing, so the same file can be pushed to " +
			"many phones without re-uploading it or consuming more quota; a captured file " +
			"is deliverable to a phone by its id just like an upload. --phone-id is " +
			"required and can be discovered with `phones mine` or `sessions list " +
			"--remote`. The collection is inferred unless DCIM, Pictures, or Movies is " +
			"selected. Phone delivery is limited to 100 MiB per file; the server rejects " +
			"a stored file above that even though the library holds it. By default the " +
			"command returns after dispatch; --wait blocks for delivered or failed, and " +
			"--timeout applies only with --wait.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			opts := []files.Option{}
			if collection != "" {
				// Validate through the generated enum rather than casting an
				// arbitrary string: a typo becomes a clear message here instead
				// of a 422 from the API after the bytes have already moved.
				if _, err := platformgo.NewFileDeliveryCreateRequestCollectionFromString(collection); err != nil {
					return exit.Usagef("unsupported --collection value; use DCIM, Pictures, or Movies")
				}
				opts = append(opts, files.WithCollection(collection))
			}
			if wait {
				opts = append(opts, files.WithWait(timeout))
			}
			p := printer()
			p.Step("Pushing %s to phone %s", args[0], phoneID)
			if err := p.Err(); err != nil {
				return err
			}
			d, err := files.Push(cmd.Context(), cl, phoneID, args[0], opts...)
			if err != nil {
				return err
			}
			return p.Emit(d, func() { printDelivery(p, d, wait) })
		},
	}
	cmd.Flags().StringVar(&phoneID, "phone-id", "", "Target phone ID from `phones mine` or remote sessions (required)")
	cmd.Flags().StringVar(&collection, "collection", "", "Target DCIM, Pictures, or Movies; omitted infers from media type")
	cmd.Flags().BoolVar(&wait, "wait", false, "Block until the phone reports delivered or failed instead of returning at dispatch")
	documentedDurationVar(cmd.Flags(), &timeout, "timeout", time.Minute, deliveryTimeoutHelp)
	_ = cmd.MarkFlagRequired("phone-id")
	return cmd
}

// deletedFile is the JSON shape of a successful `files delete`.
type deletedFile struct {
	ID                   string `json:"id"`
	Deleted              bool   `json:"deleted"`
	PhonesPendingRemoval int64  `json:"phones_pending_removal"`
}

func filesDeleteCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "delete <file-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a file from the library and free its quota.",
		Long: "Delete a file from the active organization's library and free its storage " +
			"quota. Works for any source. Deletion also recalls every copy delivered to " +
			"phones; for a capture, the source phone's own copy from the capture session " +
			"is outside the recall. Use `files list` to discover the file ID. Without " +
			"--yes, table mode prompts only when stdin is a terminal. Redirected, JSON, " +
			"and quiet execution do not prompt and require --yes. The alias `files rm` " +
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
				"Delete file %s? Also recall it from phones holding or receiving a copy?", id)
			if !yes && !p.Confirm(prompt) {
				if err := p.Err(); err != nil {
					return err
				}
				return exit.Usagef("aborted (pass --yes to delete non-interactively)")
			}
			resp, err := cl.Files.Delete(cmd.Context(), &platformgo.FilesDeleteRequest{FileID: id})
			if err != nil {
				return err
			}
			out := deletedFile{ID: id, Deleted: true}
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

// printDelivery renders a delivery the same way for `files push` and
// `phone send`, so the two verbs that create one agree on how it reads.
func printDelivery(p *output.Printer, d *platformgo.FileDeliverySummary, waited bool) {
	p.KV([][2]string{
		{"Delivery", d.ID},
		{"File", d.Filename},
		{"Status", string(d.Status)},
	})
	if d.Error != nil && *d.Error != "" {
		p.Warn("phone reported: %s", *d.Error)
	} else if !waited {
		p.Note("pushed without requesting delivery receipt. In the future, add --wait if you want the cli to wait for delivery confirmation and report result.")
	}
}

// sessionsFilesCmd is `axilio sessions files`: the per-session view of the
// captures a session produced, backed by the session-scoped endpoint so it
// answers "what did this session capture" without client-side filtering.
func sessionsFilesCmd() *cobra.Command {
	var flags filesListFlags
	cmd := &cobra.Command{
		Use:     "files <session-id>",
		Aliases: []string{"downloads"},
		Short:   "List the files a session captured off its phone.",
		Long: "List one session's captured files, newest first — the direct answer to " +
			"\"what did this session capture\". Rows appear at detection, before the " +
			"bytes finish moving, so a caller waiting on a file watches its capture " +
			"state progress rather than an empty list. Save a ready file with `files " +
			"download`. Filters and paging match `files list`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := flags.validate(); err != nil {
				return err
			}
			req := &platformgo.PhonesSessionFilesRequest{
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
			resp, err := cl.Files.PhonesSessionFiles(cmd.Context(), req)
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() { printFileList(p, resp) })
		},
	}
	flags.register(cmd)
	return cmd
}
