package cmd

import (
	"path/filepath"
	"time"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/session"
	platformgo "github.com/axilioai/platform-go"
	files "github.com/axilioai/platform-go/drivers/files"
	"github.com/spf13/cobra"
)

// phoneSendCmd is `axilio phone send <path>`: upload a local file into the org
// library and push it to the current session's phone in one step. Unlike the
// other phone verbs it drives the REST API (files.Send over the SDK), not the
// DCP MobileDriver — the phone pulls the bytes over its own network — so it
// resolves the session only for its phone_id, not a control URL.
//
// Named `send` because it does two things the rest of the surface names
// separately: `axilio uploads add` puts a file in the library, `axilio uploads
// push` sends a stored file to a phone, and `send` is both. It was briefly
// called `upload`, which collided with the library-only meaning of that word
// one layer down; no released binary ever carried that name.
func phoneSendCmd() *cobra.Command {
	var (
		wait       bool
		timeout    time.Duration
		collection string
	)
	cmd := &cobra.Command{
		Use:   "send <path>",
		Short: "Upload a local image/video and push it to the phone's gallery.",
		Long: "Upload a local file into the org library and push it into the phone's media " +
			"library so it appears in the gallery. Targets the current session's phone " +
			"(override with --session). Supported images are jpg, jpeg, png, webp, gif, " +
			"and heic; supported videos are mp4, webm, mov, 3gp, and mkv. The target " +
			"collection is inferred by media type unless DCIM, Pictures, or Movies is " +
			"selected. Phones accept deliveries up to 100 MiB. By default the command " +
			"returns after dispatch; --wait blocks for delivered or failed, and " +
			"--timeout applies only with --wait. This combines `uploads add` and " +
			"`uploads push` and therefore retains the upload in the org library.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			p.Step("Sending %s to phone %s", filepath.Base(args[0]), s.PhoneID)
			if err := p.Err(); err != nil {
				return err
			}
			d, err := files.Send(cmd.Context(), cl, s.PhoneID, args[0], opts...)
			if err != nil {
				return err
			}
			return p.Emit(d, func() { printDelivery(p, d, wait) })
		},
	}
	cmd.Flags().BoolVar(&wait, "wait", false, "Block until the phone reports delivered or failed instead of returning at dispatch")
	documentedDurationVar(cmd.Flags(), &timeout, "timeout", time.Minute, deliveryTimeoutHelp)
	cmd.Flags().StringVar(&collection, "collection", "", "Target DCIM, Pictures, or Movies; omitted infers from media type")
	return cmd
}
