package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/output"
	platformgo "github.com/axilioai/platform-go"
	files "github.com/axilioai/platform-go/drivers/files"
	"github.com/spf13/cobra"
)

// uploadsCmd is `axilio uploads` (AXI-1447): the org file library, which until
// now the CLI could only fill. `axilio phone send` uploads and pushes in one
// shot, so a CLI user could consume the org's storage quota and had no
// supported way to see what was in it or free it again — the API and the
// dashboard had list and delete from the start.
//
// The library is a flat, org-scoped namespace on purpose (there are no
// folders, by decision), so `list` leans on search and sort rather than
// navigation, and prints standing quota usage as a footer because a capped
// resource should show its cap wherever you look at it.
func uploadsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uploads",
		Short: "Add, list, push, and delete files in your organization's library.",
		Long: "Manage the org file library. Files live here until deleted and can be " +
			"pushed to any phone the org holds, so one upload serves many phones. " +
			"`add` stores a local file, `list` discovers uploads and quota, `push` " +
			"delivers a stored upload, and `delete` frees library quota. `phone send` " +
			"combines add and push for the selected session's phone.\n\n" +
			"Running `axilio uploads` without a subcommand is equivalent to " +
			"`axilio uploads --help`: it only displays this help and does not add, " +
			"list, push, or delete uploads. Global flags shown here therefore have no " +
			"effect. Pass flags to an uploads subcommand instead.",
	}
	cmd.AddCommand(uploadsAddCmd(), uploadsListCmd(), uploadsPushCmd(), uploadsDeleteCmd())
	return cmd
}

func uploadsAddCmd() *cobra.Command {
	var filename, mimeType string
	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Upload a local file into the library without pushing it anywhere.",
		Long: "Register a local file, upload its bytes, and verify them, leaving a ready " +
			"file in the library. Use `axilio uploads push` to send it to a phone, or " +
			"`axilio phone send` to do both in one step. The stored filename defaults " +
			"to the local basename and MIME type is inferred from the extension; " +
			"--filename and --mime-type override those values. The library stores " +
			"files up to 1 GiB, including files too large to deliver to a phone " +
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
					{"Status", string(f.Status)},
				})
			})
		},
	}
	cmd.Flags().StringVar(&filename, "filename", "", "Stored filename; omitted uses the local file's basename")
	cmd.Flags().StringVar(&mimeType, "mime-type", "", "Stored MIME type; omitted infers it from the file extension")
	return cmd
}

func uploadsListCmd() *cobra.Command {
	var (
		limit         int64
		offset        int64
		search        string
		sortBy, order string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the files in your organization's library.",
		Long: "List uploads and standing storage quota for the active organization. " +
			"Results include upload ID, filename, size, MIME type, status, and creation " +
			"time. Page with --limit and --offset, search filenames by " +
			"case-insensitive substring, and sort by created_at, filename, or " +
			"size_bytes in asc or desc order. Omitting sort/order uses server defaults.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			req := &platformgo.UploadsListRequest{Limit: &limit, Offset: &offset}
			if search != "" {
				req.Q = &search
			}
			// The generated enums validate for us, which turns a typo into a
			// clear client-side error instead of a 422 from the API.
			if sortBy != "" {
				s, err := platformgo.NewUploadsListRequestSortFromString(sortBy)
				if err != nil {
					return exit.Usagef("unsupported --sort value; use created_at, filename, or size_bytes")
				}
				req.Sort = &s
			}
			if order != "" {
				o, err := platformgo.NewUploadsListRequestOrderFromString(order)
				if err != nil {
					return exit.Usagef("unsupported --order value; use asc or desc")
				}
				req.Order = &o
			}

			resp, err := files.List(cmd.Context(), cl, req)
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() {
				if len(resp.Files) == 0 {
					p.Result("No files in the library.")
				} else {
					rows := [][]string{{"ID", "FILENAME", "SIZE", "TYPE", "STATUS", "CREATED"}}
					for _, f := range resp.Files {
						rows = append(rows, []string{
							f.ID, f.Filename, humanBytes(f.SizeBytes), f.MimeType, string(f.Status), ts(f.CreatedAt),
						})
					}
					p.Table(rows)
				}
				// Usage is on the listing so a caller can show "X of Y"
				// without a second call; print it whether or not this page
				// had rows, since an empty page still has a real quota.
				if u := resp.Usage; u != nil {
					p.Note("\n%d of %d files, %s of %s used",
						u.FileCount, u.FileLimit, humanBytes(u.TotalBytes), humanBytes(u.ByteLimit))
				}
				if resp.Total > int64(len(resp.Files)) {
					p.Note("showing %d of %d; use --limit / --offset to page", len(resp.Files), resp.Total)
				}
			})
		},
	}
	cmd.Flags().Int64Var(&limit, "limit", 50, "Maximum files in this page")
	cmd.Flags().Int64Var(&offset, "offset", 0, "Number of matching files to skip before this page")
	cmd.Flags().StringVar(&search, "search", "", "Filter filenames by case-insensitive substring")
	cmd.Flags().StringVar(&sortBy, "sort", "", "Sort key: created_at, filename, or size_bytes; omitted uses server default")
	cmd.Flags().StringVar(&order, "order", "", "Sort direction: asc or desc; omitted uses server default")
	return cmd
}

func uploadsPushCmd() *cobra.Command {
	var (
		phoneID    string
		collection string
		wait       bool
		timeout    time.Duration
	)
	cmd := &cobra.Command{
		Use:   "push <upload-id>",
		Short: "Push a file already in the library to a phone.",
		Long: "Send a stored library file to a phone's media library. Unlike " +
			"`axilio phone send` this uploads nothing, so the same file can be " +
			"pushed to many phones without re-uploading it or consuming more quota. " +
			"--phone-id is required and can be discovered with `phones mine` or " +
			"`sessions list --remote`. The collection is inferred unless DCIM, " +
			"Pictures, or Movies is selected. Phone delivery is limited to 100 MiB " +
			"per file; the server rejects a stored file above that even though the " +
			"library holds it. By default the command returns after " +
			"dispatch; --wait blocks for delivered or failed, and --timeout applies " +
			"only with --wait.",
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

func uploadsDeleteCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "delete <upload-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a file from the library and free its quota.",
		Long: "Delete an upload from the active organization's library and free its " +
			"storage quota. Use `uploads list` to discover the upload ID. Deletion also " +
			"schedules removal from every phone holding or receiving a copy. Without --yes, " +
			"table mode prompts only when stdin is a terminal. Redirected, JSON, and " +
			"quiet execution do not prompt and require --yes. The alias `uploads rm` " +
			"performs the same operation.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			id := args[0]
			p := printer()
			// The API tombstones the library object immediately and schedules
			// removal from every phone holding or receiving a copy.
			prompt := fmt.Sprintf(
				"Delete upload %s? Also recall it from phones holding or receiving a copy?", id)
			if !yes && !p.Confirm(prompt) {
				if err := p.Err(); err != nil {
					return err
				}
				return exit.Usagef("aborted (pass --yes to delete non-interactively)")
			}
			if err := files.Delete(cmd.Context(), cl, id); err != nil {
				return err
			}
			// Emit rather than Note, so `--output json` produces a result a
			// script can read. Note writes nothing in JSON mode, which made
			// deletion the one verb with no machine-readable outcome.
			return p.Emit(deletedUpload{ID: id, Deleted: true}, func() {
				p.Ack("Deleted %s", id)
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Delete without prompting; required in JSON, quiet, or redirected execution")
	return cmd
}

// deletedUpload is the JSON shape of a successful delete. The pinned
// files.Delete helper returns only an error and discards the API response, so
// the CLI cannot expose its phones_pending_removal count here.
type deletedUpload struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

// printDelivery renders a delivery the same way for `uploads push` and
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
