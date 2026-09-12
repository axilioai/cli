package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/util"
	platformgo "github.com/axilioai/platform-go"
	"github.com/axilioai/platform-go/client"
	"github.com/spf13/cobra"
)

// Recording lookup and download bounds. The poll interval is a variable so the
// wait loop can be exercised in tests without sleeping for real.
const (
	// defaultRecordingWaitTimeout bounds --wait: recordings finalize within
	// minutes of a session ending, so a stuck one should stop polling well
	// before an agent's own patience runs out.
	defaultRecordingWaitTimeout = 10 * time.Minute
	// maxRecordingBytes caps a download so a misbehaving storage response can
	// never fill the disk; a one-hour session at the recorder's bitrate is
	// well under this.
	maxRecordingBytes int64 = 8 << 30
	// maxRecordingRedirects bounds how many hops a presigned URL may redirect
	// through before the download gives up.
	maxRecordingRedirects = 10
)

var recordingPollInterval = 5 * time.Second

// recordingResult is the JSON shape of `sessions recording`. URL is present
// only for a lookup of a ready recording; Path and SizeBytes only after --out
// saved the file. The presigned URL is bearer-like, so a download reports the
// path it wrote instead of echoing the URL.
type recordingResult struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	URL       string `json:"url,omitempty"`
	Path      string `json:"path,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

func sessionsRecordingCmd() *cobra.Command {
	var (
		outPath string
		wait    bool
		timeout time.Duration
		force   bool
	)
	cmd := &cobra.Command{
		Use:   "recording <session-id>",
		Short: "Look up a session's recording, or download the MP4.",
		Long: "Report a session recording's status and, when ready, a fresh short-lived " +
			"playback URL. Every session is recorded by default; the recording is " +
			"`pending` while it is processed after the session ends, `ready` once an " +
			"MP4 exists, and `expired` once it is past the plan's retention window. " +
			"Without --out the command prints the status and URL and exits 0 for any " +
			"of the three states, so a caller can branch on the status. --wait polls " +
			"a pending recording until it is ready, expired, or --timeout elapses " +
			"(timeout exits with status 5).\n\n" +
			"With --out the MP4 is downloaded from the URL and written atomically: the " +
			"bytes go to a temporary file beside the destination and are renamed into " +
			"place only after the whole body arrived, so an interrupted download never " +
			"leaves a valid-looking partial MP4. An existing destination is refused " +
			"unless --force is passed. A pending recording is refused without --wait, " +
			"and an expired one cannot be downloaded (not-found status 4). Returned " +
			"URLs are short-lived; re-run to mint a fresh one. Discover session IDs " +
			"with `sessions list --history`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("timeout") && !wait {
				return exit.Usagef("--timeout only applies with --wait")
			}
			if timeout <= 0 {
				return exit.Usagef("--timeout must be positive (got %s)", timeout)
			}
			if force && outPath == "" {
				return exit.Usagef("--force only applies with --out")
			}
			if outPath != "" && !force {
				if _, err := os.Lstat(outPath); err == nil {
					return exit.Usagef("%s already exists; pass --force to overwrite", outPath)
				}
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			p := printer()
			rec, err := lookupRecording(cmd.Context(), cl, args[0], wait, timeout, func() {
				p.Step("Recording for %s is still being processed; waiting", args[0])
			})
			if err != nil {
				return err
			}
			result := recordingResult{SessionID: args[0], Status: string(rec.Status)}
			if outPath == "" {
				result.URL = strv(rec.URL)
				return p.Emit(result, func() {
					p.KV([][2]string{
						{"Session", args[0]},
						{"Recording", result.Status},
						{"URL", util.OrDash(result.URL)},
					})
					switch rec.Status {
					case platformgo.PhoneSessionRecordingResponseStatusPending:
						p.Note("The recording is still being processed; re-run with --wait to poll until it is ready.")
					case platformgo.PhoneSessionRecordingResponseStatusExpired:
						p.Note("The recording is past the plan's retention window and can no longer be downloaded.")
					case platformgo.PhoneSessionRecordingResponseStatusReady:
						p.Note("The URL is short-lived; download with --out <path> or re-run for a fresh one.")
					}
				})
			}
			switch rec.Status {
			case platformgo.PhoneSessionRecordingResponseStatusPending:
				return fmt.Errorf("recording for %s is still being processed; pass --wait to poll until it is ready", args[0])
			case platformgo.PhoneSessionRecordingResponseStatusExpired:
				return exit.With(exit.NotFound, fmt.Errorf("recording for %s has expired and can no longer be downloaded", args[0]))
			}
			if rec.URL == nil || *rec.URL == "" {
				return fmt.Errorf("recording for %s is %s but the API returned no URL", args[0], rec.Status)
			}
			p.Step("Saving recording for %s to %s", args[0], outPath)
			if err := p.Err(); err != nil {
				return err
			}
			written, err := saveRecording(cmd.Context(), *rec.URL, outPath, force)
			if err != nil {
				return err
			}
			result.Path = outPath
			result.SizeBytes = written
			return p.Emit(result, func() {
				p.Ack("Saved %s (%s)", outPath, humanBytes(written))
			})
		},
	}
	// --out has no -o shorthand: the root command owns -o for --output.
	cmd.Flags().StringVar(&outPath, "out", "", "Download the MP4 to this path instead of printing the URL")
	cmd.Flags().BoolVar(&wait, "wait", false, "Poll a pending recording until it is ready, expired, or --timeout elapses")
	cmd.Flags().DurationVar(&timeout, "timeout", defaultRecordingWaitTimeout, "Longest time --wait polls before giving up with timeout status 5")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite the --out destination if it already exists")
	return cmd
}

// lookupRecording fetches the recording state once, or, with wait, keeps
// polling while it is pending. onPending runs once before the first sleep so
// a human sees why the command is quiet. The timeout is an exit 5, not a
// generic error, so scripts can tell "not ready yet" from "broken".
func lookupRecording(ctx context.Context, cl *client.Client, sessionID string, wait bool, timeout time.Duration, onPending func()) (*platformgo.PhoneSessionRecordingResponse, error) {
	req := &platformgo.PhonesSessionRecordingRequest{SessionID: sessionID}
	rec, err := cl.Phones.SessionRecording(ctx, req)
	if err != nil {
		return nil, err
	}
	if !wait || rec.Status != platformgo.PhoneSessionRecordingResponseStatusPending {
		return rec, nil
	}
	onPending()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, exit.With(exit.Timeout, fmt.Errorf("recording for %s was still pending after %s", sessionID, timeout))
		case <-time.After(recordingPollInterval):
		}
		rec, err = cl.Phones.SessionRecording(ctx, req)
		if err != nil {
			return nil, err
		}
		if rec.Status != platformgo.PhoneSessionRecordingResponseStatusPending {
			return rec, nil
		}
	}
}

// saveRecording streams the presigned URL's body to dest atomically: the bytes
// land in a temporary file in dest's directory and are moved into place only
// after the whole body was read, so a failure or interruption at any point
// leaves either the previous file or nothing, never a truncated MP4.
func saveRecording(ctx context.Context, url, dest string, force bool) (int64, error) {
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(dest)+".*.part")
	if err != nil {
		return 0, fmt.Errorf("creating temporary file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	// Any early return removes the temp. A signal cancels ctx (fang wires
	// SIGINT/SIGTERM into the command context), which aborts the request below
	// so io.Copy returns and this still runs. saved flips once dest owns the
	// bytes, so the finished file is never removed.
	saved := false
	defer func() {
		if !saved {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := recordingHTTPClient().Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetching recording: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fetching recording: %s (presigned URLs expire; re-run to mint a fresh one)", resp.Status)
	}
	written, err := io.Copy(tmp, io.LimitReader(resp.Body, maxRecordingBytes+1))
	if err != nil {
		return 0, fmt.Errorf("downloading recording: %w", err)
	}
	if written > maxRecordingBytes {
		return 0, fmt.Errorf("recording exceeds the %s download limit", humanBytes(maxRecordingBytes))
	}
	if resp.ContentLength >= 0 && written != resp.ContentLength {
		return 0, fmt.Errorf("downloading recording: got %d of %d bytes", written, resp.ContentLength)
	}
	if err := tmp.Sync(); err != nil {
		return 0, fmt.Errorf("writing recording: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, fmt.Errorf("writing recording: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return 0, err
	}
	// Move the finished bytes into place. --force overwrites; without it the
	// link fails if dest exists, so a file that appeared during the download is
	// never silently replaced. Rename/link and the existence test are one
	// atomic step, which the pre-flight Lstat could not be.
	if force {
		if err := os.Rename(tmpPath, dest); err != nil {
			return 0, fmt.Errorf("saving recording to %s: %w", dest, err)
		}
	} else {
		if err := os.Link(tmpPath, dest); err != nil {
			if errors.Is(err, os.ErrExist) {
				return 0, exit.Usagef("%s already exists; pass --force to overwrite", dest)
			}
			return 0, fmt.Errorf("saving recording to %s: %w", dest, err)
		}
		_ = os.Remove(tmpPath)
	}
	saved = true
	return written, nil
}

// recordingHTTPClient fetches a presigned recording URL. The URL comes from the
// authenticated API, but a redirect must not carry the request off that origin
// (onto an internal host, say) or downgrade the transport, so redirects are
// bounded and pinned to the original URL's host and scheme.
func recordingHTTPClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRecordingRedirects {
				return fmt.Errorf("recording URL redirected too many times (>%d)", maxRecordingRedirects)
			}
			origin := via[0].URL
			if req.URL.Host != origin.Host {
				return fmt.Errorf("recording URL redirected off its origin (%s -> %s)", origin.Host, req.URL.Host)
			}
			if origin.Scheme == "https" && req.URL.Scheme != "https" {
				return fmt.Errorf("recording URL redirected to an insecure %s URL", req.URL.Scheme)
			}
			return nil
		},
	}
}
