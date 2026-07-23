package cmd

import (
	"context"
	"path/filepath"
	"time"

	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/session"
	files "github.com/axilioai/platform-go/drivers/files"
	"github.com/spf13/cobra"
)

// phoneUploadCmd is `axilio phone upload <path>` (AXI-1412): register a local
// file, upload it, and push it into the current session's phone media library
// so it shows up in the gallery. Unlike the other phone verbs it drives the
// REST API (files.Send over the SDK), not the DCP MobileDriver — the phone
// pulls the bytes over its own network — so it resolves the session only for
// its phone_id, not a control URL.
func phoneUploadCmd() *cobra.Command {
	var (
		wait       bool
		timeout    time.Duration
		collection string
	)
	cmd := &cobra.Command{
		Use:   "upload <path>",
		Short: "Upload a local image/video and push it to the phone's gallery.",
		Long: "Register a local file, upload it, and push it into the phone's media " +
			"library so it appears in the gallery. Targets the current session's phone " +
			"(override with --session). Images and short videos up to 5 MiB (the phone " +
			"downloads over its own cellular link). With --wait, blocks until the phone " +
			"reports the file delivered (or failed) instead of returning at dispatch.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			s, err := session.Resolve(flagPhoneSession)
			if err != nil {
				return err
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			opts := []files.Option{}
			if collection != "" {
				opts = append(opts, files.WithCollection(collection))
			}
			if wait {
				opts = append(opts, files.WithWait(timeout))
			}
			p := printer()
			p.Step("Uploading %s to phone %s", filepath.Base(args[0]), s.PhoneID)
			d, err := files.Send(context.Background(), cl, s.PhoneID, args[0], opts...)
			if err != nil {
				return err
			}
			p.Emit(d, func() {
				output.KV([][2]string{
					{"Delivery", d.ID},
					{"File", d.Filename},
					{"Status", string(d.Status)},
				})
				if d.Error != nil && *d.Error != "" {
					p.Note("phone reported: %s", *d.Error)
				} else if !wait {
					p.Note("pushed; re-run with --wait to confirm delivery")
				}
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&wait, "wait", false, "Block until the phone reports the file delivered or failed")
	cmd.Flags().DurationVar(&timeout, "timeout", 60*time.Second, "Max time to wait with --wait")
	cmd.Flags().StringVar(&collection, "collection", "", "Media collection: DCIM, Pictures, or Movies (default: by media type)")
	return cmd
}
