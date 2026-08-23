package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/util"
	platformgo "github.com/axilioai/platform-go"
	"github.com/spf13/cobra"
)

// maxPhoneNicknameLen mirrors the backend's nickname bound (1-100, AXI-1680)
// so an oversized rename is refused before any request is made.
const maxPhoneNicknameLen = 100

func phonesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "phones",
		Short: "Discover and manage your dedicated phones.",
		Long: "Discover and manage phones. `phones list` shows the " +
			"dedicated phones that are free to start a session on right now. " +
			"`phones mine` shows the organization's complete dedicated inventory, " +
			"including busy and offline phones, and is the place to find a phone ID " +
			"for `sessions start --phone-id`. The shared pool is not listed; it is " +
			"drawn from automatically when you start a session without a phone ID.\n\n" +
			"Management verbs act on dedicated phones by ID: `phones rename` sets " +
			"the nickname, `phones wipe` requests an on-demand factory reset, and " +
			"`phones preview` returns a short-lived URL for the current screen " +
			"preview.\n\n" +
			"Running `axilio phones` without a subcommand is equivalent to " +
			"`axilio phones --help`: it only displays this help and does not list or " +
			"change phones. Global flags shown here therefore have no effect. Pass " +
			"flags to a phones subcommand instead.",
	}
	cmd.AddCommand(phonesListCmd())
	cmd.AddCommand(phonesMineCmd())
	cmd.AddCommand(phonesRenameCmd())
	cmd.AddCommand(phonesWipeCmd())
	cmd.AddCommand(phonesPreviewCmd())
	return cmd
}

func phonesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your dedicated phones that are free to start a session on right now.",
		Long: "List your org's dedicated phones that are ready to start a session on immediately: active " +
			"and not currently held by a session. Busy, offline, or maintenance phones do not appear here; " +
			"use `axilio phones mine` to see the full dedicated inventory. The shared pool is not listed - it " +
			"is drawn from automatically when you start a session without a phone ID. Results include phone " +
			"ID, nickname, type, and model.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Phones.List(context.Background(), &platformgo.PhonesListRequest{
				Ownership: platformgo.PhonesListRequestOwnershipDedicated,
			})
			if err != nil {
				return err
			}
			// Startable now = an active dedicated phone not currently held by a
			// session. (The shared pool is not enumerable; it auto-allocates.)
			// Non-nil so an empty result still serializes as [] in JSON output.
			free := []*platformgo.PhoneSummary{}
			for _, ph := range resp.Phones {
				if ph.Status == "active" && ph.CurrentSessionID == nil {
					free = append(free, ph)
				}
			}
			resp.SetPhones(free)
			p := printer()
			return p.Emit(resp, func() {
				if len(resp.Phones) == 0 {
					p.Result("No free dedicated phones. Start a session without --phone-id to draw from the shared pool.")
					return
				}
				rows := [][]string{{"PHONE ID", "NICKNAME", "TYPE", "MODEL"}}
				for _, ph := range resp.Phones {
					rows = append(rows, []string{
						ph.PhoneID, util.OrDash(strv(ph.Nickname)), util.OrDash(enumv(ph.PhoneType)), util.OrDash(strv(ph.ModelName)),
					})
				}
				p.Table(rows)
			})
		},
	}
}

func phonesMineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mine",
		Short: "List your org's dedicated phones, including ones currently in use.",
		Long: "List your organization's full dedicated (private/rented) phone inventory in every state - free, " +
			"busy in an active session, or offline. This is how you discover a phone_id to pin with " +
			"`axilio sessions start --phone-id`. Results include phone ID, nickname, type, model, " +
			"status, and the session ID holding a busy phone.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			resp, err := cl.Phones.List(context.Background(), &platformgo.PhonesListRequest{
				Ownership: platformgo.PhonesListRequestOwnershipDedicated,
			})
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(resp, func() {
				if len(resp.Phones) == 0 {
					p.Result("No dedicated phones.")
					return
				}
				rows := [][]string{{"PHONE ID", "NICKNAME", "TYPE", "MODEL", "STATUS", "SESSION"}}
				for _, ph := range resp.Phones {
					rows = append(rows, []string{
						ph.PhoneID,
						util.OrDash(strv(ph.Nickname)),
						util.OrDash(enumv(ph.PhoneType)),
						util.OrDash(strv(ph.ModelName)),
						string(ph.Status),
						util.OrDash(strv(ph.CurrentSessionID)),
					})
				}
				p.Table(rows)
			})
		},
	}
}

func phonesRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <phone-id> <nickname>",
		Short: "Set a dedicated phone's nickname.",
		Long: "Set the nickname shown for a dedicated phone in listings and the " +
			"dashboard. The nickname must be 1-100 characters. Use `phones mine` to " +
			"discover the phone ID.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, nickname := args[0], args[1]
			if nickname == "" || len(nickname) > maxPhoneNicknameLen {
				return exit.Usagef("nickname must be 1-%d characters (got %d)", maxPhoneNicknameLen, len(nickname))
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			req := &platformgo.PhoneUpdateNicknameRequest{}
			req.SetPhoneID(id)
			req.SetNickname(nickname)
			ph, err := cl.Phones.Nickname(cmd.Context(), req)
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(ph, func() {
				p.Ack("Renamed %s to %q", ph.PhoneID, util.OrDash(strv(ph.Nickname)))
			})
		},
	}
}

func phonesWipeCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "wipe <phone-id>",
		Short: "Factory-reset a dedicated phone you own.",
		Long: "Request an on-demand factory reset of a private phone the " +
			"organization owns. The phone must be active and not currently held by a " +
			"session; it is set to maintenance while the wipe is carried out. Without " +
			"--yes, table mode prompts only when stdin is a terminal. Redirected, " +
			"JSON, and quiet execution do not prompt and require --yes.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			id := args[0]
			p := printer()
			prompt := fmt.Sprintf(
				"Wipe phone %s? This factory-resets the device and erases its data.", id)
			if !yes && !p.Confirm(prompt) {
				if err := p.Err(); err != nil {
					return err
				}
				return exit.Usagef("aborted (pass --yes to wipe non-interactively)")
			}
			req := &platformgo.PhonesWipeRequest{}
			req.SetPhoneID(id)
			resp, err := cl.Phones.Wipe(cmd.Context(), req)
			if err != nil {
				return err
			}
			return p.Emit(resp, func() {
				p.Ack("%s", resp.Message)
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Wipe without prompting; required in JSON, quiet, or redirected execution")
	return cmd
}

func phonesPreviewCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "preview <phone-id>",
		Short: "Get a short-lived URL for the phone's current screen preview.",
		Long: "Return a short-lived URL for the phone's current screen preview - a " +
			"rolling JPEG refreshed every few seconds while the phone is paired, " +
			"available with or without an active session. Every call mints a fresh " +
			"URL; poll and swap the image. Status is pending when no preview exists " +
			"yet. With --out, the JPEG is downloaded and written to the given path " +
			"instead, overwriting existing contents without confirmation.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			req := &platformgo.PhonesPreviewRequest{}
			req.SetPhoneID(args[0])
			resp, err := cl.Phones.Preview(cmd.Context(), req)
			if err != nil {
				return err
			}
			p := printer()
			if out == "" {
				return p.Emit(resp, func() {
					if resp.Status == platformgo.PhonePreviewResponseStatusPending {
						p.Result("Preview pending; no frame captured yet. Retry shortly.")
						return
					}
					p.KV([][2]string{
						{"Status", string(resp.Status)},
						{"URL", util.OrDash(strv(resp.URL))},
					})
				})
			}
			if resp.Status != platformgo.PhonePreviewResponseStatusReady || resp.URL == nil {
				return fmt.Errorf("preview is not ready yet (status %s); retry shortly", resp.Status)
			}
			n, err := downloadToFile(cmd.Context(), *resp.URL, out)
			if err != nil {
				return err
			}
			return p.Emit(map[string]any{"action": "preview", "path": out, "bytes": n}, func() {
				p.Ack("Wrote %s (%d bytes)", out, n)
			})
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "JPEG path to create; overwrite existing contents without confirmation")
	return cmd
}

// downloadToFile fetches a presigned URL (no API credentials attached) and
// writes the body to path, returning the byte count.
func downloadToFile(ctx context.Context, url, path string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("preview fetch failed: %s", resp.Status)
	}
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, resp.Body)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return n, err
}
