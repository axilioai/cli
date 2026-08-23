package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/output"
	platformgo "github.com/axilioai/platform-go"
	"github.com/axilioai/platform-go/client"
	"github.com/spf13/cobra"
)

const (
	// watchPageLimit is the frames page size: the op's maximum, so a catch-up
	// costs the fewest round-trips.
	watchPageLimit int64 = 1000

	// watchEndGraceRounds bounds how many extra polls watch spends waiting
	// for the session's terminal frame after the run itself reports a
	// terminal status. The archive flushes shortly after the run ends; if the
	// end frame still has not appeared, the run status is authoritative.
	watchEndGraceRounds = 3

	// Session-root span vocabulary: the terminal session frame is the
	// platform's only session-end signal. Traces recorded before the
	// 2026-08-21 vocabulary cutover carry the retired phone_session value.
	spanTypeSession       = "session"
	spanTypeSessionLegacy = "phone_session"
	spanPhaseEnd          = "end"
)

// watchPollInterval is the delay between polls of the run and its frame
// archive. A variable so tests can collapse the wait.
var watchPollInterval = 2 * time.Second

func runsWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch <run-id>",
		Short: "Stream a run's telemetry until it finishes.",
		Long: "Follow a run live: wait for it to start, then stream its session's " +
			"telemetry (output logs and completed spans) until the session ends, " +
			"and exit with the run's outcome. Watching an already-finished run " +
			"replays its recorded telemetry and exits the same way.\n\n" +
			"The exit code reflects the outcome: 0 when the run completed, 1 when " +
			"it failed (the error message is printed), 7 when it was cancelled or " +
			"the watch was interrupted. Interrupting the watch does not affect the " +
			"run; re-run `runs watch` to replay and resume following.\n\n" +
			"JSON mode streams newline-delimited JSON rather than a single " +
			"document: one object per telemetry frame in archive order, then a " +
			"final `{\"watch_end\": true, ...}` object carrying the run's status " +
			"and error message.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			return watchRun(cl, printer(), args[0])
		},
	}
}

// watchRun follows runID until it reaches a terminal status and its session's
// terminal frame has been rendered (or the bounded grace expires). It renders
// every frame exactly once, in archive order, resuming by offset.
func watchRun(cl *client.Client, p *output.Printer, runID string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var (
		sessionID string
		rendered  int64 // frames already rendered = next archive offset
		endSeen   bool  // session-root end frame rendered
		waitNoted bool
		graceUsed int
		run       *platformgo.RunResponse
	)
	for {
		r, err := cl.Runs.Get(ctx, &platformgo.RunsGetRequest{RunID: runID})
		if err != nil {
			return watchInterrupted(ctx, p, runID, err)
		}
		run = r

		if sessionID == "" && r.SessionID != nil && *r.SessionID != "" {
			sessionID = *r.SessionID
			p.Step("Run %s %s — streaming session %s", runID, r.Status, sessionID)
		}
		if sessionID == "" && !waitNoted {
			waitNoted = true
			p.Step("Run %s is %s — waiting for a phone", runID, r.Status)
		}

		if sessionID != "" && !endSeen {
			n, sawEnd, err := watchDrainFrames(ctx, cl, p, sessionID, rendered)
			if err != nil {
				return watchInterrupted(ctx, p, runID, err)
			}
			rendered += n
			endSeen = sawEnd
		}

		terminal := r.Status == platformgo.RunResponseStatusCompleted ||
			r.Status == platformgo.RunResponseStatusFailed ||
			r.Status == platformgo.RunResponseStatusCancelled
		if terminal {
			// The archive can flush a beat behind the run status; give the
			// terminal frame a bounded grace, then trust the run status.
			if endSeen || sessionID == "" || graceUsed >= watchEndGraceRounds {
				return watchOutcome(p, run)
			}
			graceUsed++
		}

		select {
		case <-ctx.Done():
			return watchInterrupted(ctx, p, runID, ctx.Err())
		case <-time.After(watchPollInterval):
		}
	}
}

// watchDrainFrames renders every archived frame from offset onward and
// reports how many it rendered and whether the session-root end frame was
// among them.
func watchDrainFrames(ctx context.Context, cl *client.Client, p *output.Printer, sessionID string, offset int64) (int64, bool, error) {
	var rendered int64
	sawEnd := false
	for {
		limit := watchPageLimit
		off := offset + rendered
		resp, err := cl.Runs.SessionsListFrames(ctx, &platformgo.SessionsListFramesRequest{
			SessionID: sessionID,
			Limit:     &limit,
			Offset:    &off,
		})
		if err != nil {
			return rendered, sawEnd, err
		}
		if resp.RetentionExpired {
			p.Warn("trace is past the org's retention window; frames withheld")
			return rendered, sawEnd, nil
		}
		for _, f := range resp.Frames {
			if err := renderWatchFrame(p, f); err != nil {
				return rendered, sawEnd, err
			}
			rendered++
			if isSessionEndFrame(f) {
				sawEnd = true
			}
		}
		if len(resp.Frames) == 0 || offset+rendered >= resp.Total {
			return rendered, sawEnd, nil
		}
	}
}

// renderWatchFrame prints one frame: a compact single line per frame in human
// mode, the frame's own JSON in JSON mode. Unknown kinds render generically —
// never dropped, never an error.
func renderWatchFrame(p *output.Printer, f *platformgo.RunSessionFramesResponseFramesItem) error {
	if p.JSON {
		return p.JSONLine(f)
	}
	switch {
	case f.Span != nil:
		s := f.Span
		line := fmt.Sprintf("%s  span  %-11s %s  %s", frameClock(s.StartTimeUnixNano), s.SpanType, s.Name, spanDuration(s))
		if s.Status != nil && s.Status.Code == "error" {
			line += "  error"
			if s.Status.Message != "" {
				line += ": " + s.Status.Message
			}
		}
		p.Result("%s", line)
	case f.Log != nil:
		l := f.Log
		p.Result("%s  %-5s %-11s %s", frameClock(l.TimeUnixNano), logSeverity(l.Severity), l.LogType, l.Body)
	default:
		p.Result("%s  %s frame (unrecognized kind)", frameClock(0), f.Kind)
	}
	return p.Err()
}

// watchOutcome emits the final summary and maps the run's terminal status
// onto the published exit-code contract.
func watchOutcome(p *output.Printer, run *platformgo.RunResponse) error {
	if p.JSON {
		if err := p.JSONLine(map[string]any{
			"watch_end":     true,
			"run_id":        run.ID,
			"status":        run.Status,
			"error_message": run.ErrorMessage,
		}); err != nil {
			return err
		}
	}
	switch run.Status {
	case platformgo.RunResponseStatusCompleted:
		p.Success("Run %s completed", run.ID)
		return p.Err()
	case platformgo.RunResponseStatusCancelled:
		return exit.With(exit.Canceled, fmt.Errorf("run %s was cancelled", run.ID))
	default: // failed
		msg := strv(run.ErrorMessage)
		if msg == "" {
			msg = "no error message recorded"
		}
		return exit.With(exit.Err, fmt.Errorf("run %s failed: %s", run.ID, msg))
	}
}

// watchInterrupted turns a mid-watch error into the right exit: a local
// interrupt maps to the canceled code with a resume hint, anything else
// propagates for the standard classifier.
func watchInterrupted(ctx context.Context, p *output.Printer, runID string, err error) error {
	if ctx.Err() == nil {
		return err
	}
	p.Note("watch interrupted — the run continues; `axilio runs watch %s` replays and resumes", runID)
	return exit.With(exit.Canceled, fmt.Errorf("watch interrupted"))
}

// isSessionEndFrame reports whether f is the session-root end frame — the
// platform's only session-end signal (peer_disconnected is never emitted).
func isSessionEndFrame(f *platformgo.RunSessionFramesResponseFramesItem) bool {
	if f.Span == nil {
		return false
	}
	if f.Span.SpanType != spanTypeSession && f.Span.SpanType != spanTypeSessionLegacy {
		return false
	}
	return f.Span.Phase == spanPhaseEnd
}

// frameClock renders a frame timestamp as a UTC wall-clock instant. Frames
// carry absolute nanoseconds; the date is visible in JSON mode and `sessions
// get`, so the stream keeps each line short.
func frameClock(unixNano int64) string {
	if unixNano == 0 {
		return "--:--:--.---"
	}
	return time.Unix(0, unixNano).UTC().Format("15:04:05.000")
}

// spanDuration prefers the producer's monotonic axilio.duration_ns attribute
// and falls back to wall-clock end−start, mirroring the dashboard.
func spanDuration(s *platformgo.RunSpanFrame) time.Duration {
	if v, ok := s.Attributes["axilio.duration_ns"]; ok {
		if f, ok := v.(float64); ok && f >= 0 {
			return time.Duration(int64(f))
		}
	}
	d := s.EndTimeUnixNano - s.StartTimeUnixNano
	if d < 0 {
		return 0
	}
	return time.Duration(d)
}

// logSeverity compacts a severity for the stream column.
func logSeverity(sev string) string {
	if sev == "" {
		return "-"
	}
	return sev
}
