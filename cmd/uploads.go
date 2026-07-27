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
			"pushed to any phone the org holds, so one upload serves many phones.",
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
			"`axilio phone send` to do both in one step.",
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
			f, err := files.Upload(cmd.Context(), cl, args[0], opts...)
			if err != nil {
				return err
			}
			p.Emit(f, func() {
				output.KV([][2]string{
					{"ID", f.ID},
					{"Filename", f.Filename},
					{"Size", humanBytes(f.SizeBytes)},
					{"Type", f.MimeType},
					{"Status", string(f.Status)},
				})
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&filename, "filename", "", "Name to store the file under (default: the file's basename)")
	cmd.Flags().StringVar(&mimeType, "mime-type", "", "Content type (default: guessed from the extension)")
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
			p.Emit(resp, func() {
				if len(resp.Files) == 0 {
					fmt.Println("No files in the library.")
				} else {
					rows := [][]string{{"ID", "FILENAME", "SIZE", "TYPE", "STATUS", "CREATED"}}
					for _, f := range resp.Files {
						rows = append(rows, []string{
							f.ID, f.Filename, humanBytes(f.SizeBytes), f.MimeType, string(f.Status), ts(f.CreatedAt),
						})
					}
					output.Table(rows)
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
			return nil
		},
	}
	cmd.Flags().Int64Var(&limit, "limit", 50, "Max files to return")
	cmd.Flags().Int64Var(&offset, "offset", 0, "Pagination offset")
	cmd.Flags().StringVar(&search, "search", "", "Filter by filename (case-insensitive substring)")
	cmd.Flags().StringVar(&sortBy, "sort", "", "Sort by: created_at, filename, or size_bytes")
	cmd.Flags().StringVar(&order, "order", "", "Sort direction: asc or desc")
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
			"pushed to many phones without re-uploading it or consuming more quota.",
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
			d, err := files.Push(cmd.Context(), cl, phoneID, args[0], opts...)
			if err != nil {
				return err
			}
			p.Emit(d, func() { printDelivery(p, d, wait) })
			return nil
		},
	}
	cmd.Flags().StringVar(&phoneID, "phone-id", "", "Target phone id (required)")
	cmd.Flags().StringVar(&collection, "collection", "", "Media collection: DCIM, Pictures, or Movies (default: by media type)")
	cmd.Flags().BoolVar(&wait, "wait", false, "Block until the phone reports the file delivered or failed")
	cmd.Flags().DurationVar(&timeout, "timeout", 60*time.Second, "Max time to wait with --wait")
	_ = cmd.MarkFlagRequired("phone-id")
	return cmd
}

func uploadsDeleteCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "delete <upload-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a file from the library and free its quota.",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			id := args[0]
			p := printer()
			// Deliberately precise about scope: copies already delivered to
			// phones are not recalled today. When delete recall ships this
			// prompt is the copy that has to change with it.
			prompt := fmt.Sprintf(
				"Delete upload %s? Copies already delivered to phones are not removed.", id)
			if !yes && !p.Confirm(prompt) {
				return exit.Usagef("aborted (pass --yes to delete non-interactively)")
			}
			if err := files.Delete(cmd.Context(), cl, id); err != nil {
				return err
			}
			// Emit rather than Note, so `--output json` produces a result a
			// script can read. Note writes nothing in JSON mode, which made
			// deletion the one verb with no machine-readable outcome.
			p.Emit(deletedUpload{ID: id, Deleted: true}, func() {
				p.Note("Deleted %s", id)
			})
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt")
	return cmd
}

// deletedUpload is the JSON shape of a successful delete. Small on purpose:
// the API returns only a confirmation message, so inventing richer output
// would be inventing facts.
type deletedUpload struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

// printDelivery renders a delivery the same way for `uploads push` and
// `phone send`, so the two verbs that create one agree on how it reads.
func printDelivery(p *output.Printer, d *platformgo.FileDeliverySummary, waited bool) {
	output.KV([][2]string{
		{"Delivery", d.ID},
		{"File", d.Filename},
		{"Status", string(d.Status)},
	})
	if d.Error != nil && *d.Error != "" {
		p.Note("phone reported: %s", *d.Error)
	} else if !waited {
		p.Note("pushed; re-run with --wait to confirm delivery")
	}
}
