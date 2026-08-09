package cmd

import (
	"os"
	"path/filepath"
	"time"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/session"
	platformgo "github.com/axilioai/platform-go"
	files "github.com/axilioai/platform-go/drivers/files"
	"github.com/spf13/cobra"
)

// maxDeliveryBytes mirrors the server's phone-delivery ceiling, pinned there
// by a backend regression test (AXI-1581) and exported by the SDK as
// files.MaxDeliveryBytes from platform-go v0.6.1 (adopt on the next dep
// bump). Mirrored so an oversize one-shot send fails HERE — before the upload
// registers and the file is retained in the library, consuming quota for a
// command that failed. The server stays authoritative either way. The
// library's own ceiling is 1 GiB and `uploads add` deliberately keeps it.
const maxDeliveryBytes int64 = 100 << 20 // 100 MiB

// oversizeForDelivery preflights the phone-delivery ceiling for a local file
// (AXI-1581). Single-purpose on purpose: an unreadable path or a directory
// returns nil so those keep their existing error paths — this check exists
// only to stop a file that could never be delivered from being uploaded
// first. Usage-coded (exit 2), the same class as a server-side 400.
func oversizeForDelivery(path string) error {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil
	}
	if info.Size() > maxDeliveryBytes {
		return exit.Usagef(
			"%s is %d bytes; phone delivery is limited to 100 MiB per file, so nothing was uploaded. "+
				"The org library itself stores files up to 1 GiB — use `axilio uploads add` if you only need it stored",
			filepath.Base(path), info.Size())
	}
	return nil
}

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
			"selected. Phone delivery is limited to 100 MiB per file, and larger files " +
			"are rejected before anything is uploaded; the org library itself stores " +
			"files up to 1 GiB (`axilio uploads add`), including files too large to " +
			"deliver to a phone. By default the command " +
			"returns after dispatch; --wait blocks for delivered or failed, and " +
			"--timeout applies only with --wait. This combines `uploads add` and " +
			"`uploads push` and therefore retains the upload in the org library.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Preflight before resolving the session or dialing anywhere:
			// the cheapest check runs first, and a refusal must precede any
			// side effect (AXI-1581).
			if err := oversizeForDelivery(args[0]); err != nil {
				return err
			}
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
