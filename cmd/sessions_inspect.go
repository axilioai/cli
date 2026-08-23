package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/util"
	platformgo "github.com/axilioai/platform-go"
	"github.com/axilioai/platform-go/client"
	"github.com/axilioai/platform-go/drivers/telemetry"
	"github.com/spf13/cobra"
)

// framesPageLimit is the backend's maximum frames page size; trace paginates
// with the largest page to keep round-trips minimal.
const framesPageLimit int64 = 1000

// inferenceIDAttr is the axilio.* span attribute carrying the inference id
// that joins an inference span to the response-level inference_costs map
// (mirrors the dashboard's frames vocabulary).
const inferenceIDAttr = "axilio.inference.id"

func sessionsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <session-id>",
		Short: "Show one session in detail.",
		Long: "Fetch one session by ID, active or historical. The result includes " +
			"status, source, phone, allocation and release times, duration, tags, " +
			"workflow, capture and telemetry settings, and the recording status and " +
			"playback URL when ready. Active sessions also include a short-lived " +
			"screen thumbnail URL. Discover IDs with `sessions list --remote` or " +
			"`sessions list --history`.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			d, err := cl.Phones.GetSession(context.Background(), &platformgo.PhonesGetSessionRequest{SessionID: args[0]})
			if err != nil {
				return err
			}
			// The JSON payload is the canonical detail response plus the
			// thumbnail fields, which live behind their own endpoint. The
			// thumbnail only exists while a session is active, and its absence
			// must not fail the command — it is enrichment, not the record.
			payload, err := toMap(d)
			if err != nil {
				return err
			}
			var thumbURL string
			if d.Status == platformgo.PhoneSessionDetailResponseStatusActive {
				th, thErr := cl.Phones.SessionThumbnail(context.Background(), &platformgo.PhonesSessionThumbnailRequest{SessionID: args[0]})
				if thErr == nil {
					thumbURL = strv(th.URL)
					payload["thumbnail_status"] = string(th.Status)
					if th.URL != nil {
						payload["thumbnail_url"] = *th.URL
					}
				}
			}
			p := printer()
			return p.Emit(payload, func() {
				p.KV([][2]string{
					{"Session", d.SessionID},
					{"Name", util.OrDash(strv(d.Name))},
					{"Status", string(d.Status)},
					{"Source", string(d.Source)},
					{"Allocated by", util.OrDash(enumv(d.AllocatedBy))},
					{"Phone", d.PhoneID},
					{"Phone name", util.OrDash(strv(d.PhoneName))},
					{"Nickname", util.OrDash(strv(d.Nickname))},
					{"Model", util.OrDash(strv(d.ModelName))},
					{"Type", util.OrDash(enumv(d.PhoneType))},
					{"Location", util.OrDash(strv(d.Location))},
					{"Dedicated", fmt.Sprintf("%t", d.IsDedicatedPhone)},
					{"Workflow", util.OrDash(strv(d.WorkflowID))},
					{"Workflow name", util.OrDash(strv(d.WorkflowName))},
					{"Allocated", ts(d.AllocatedAt)},
					{"Released", util.OrDash(tsp(d.DeallocatedAt))},
					{"Duration", sessionDuration(d.AllocatedAt, d.DeallocatedAt)},
					{"Tags", util.OrDash(tagList(d.Tags))},
					{"Capture", fmt.Sprintf("%t", d.CaptureEnabled)},
					{"Telemetry", fmt.Sprintf("%t", !d.TelemetryDisabled)},
					{"Recording", string(d.RecordingStatus)},
					{"Recording URL", util.OrDash(strv(d.RecordingURL))},
					{"Thumbnail URL", util.OrDash(thumbURL)},
				})
			})
		},
	}
}

// toMap round-trips a response through JSON so extra CLI-composed fields can
// be added without losing the canonical wire field names.
func toMap(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// sessionDuration renders the session length, running to now while active.
func sessionDuration(allocated time.Time, deallocated *time.Time) string {
	if allocated.IsZero() {
		return ""
	}
	end := time.Now()
	suffix := " (ongoing)"
	if deallocated != nil {
		end = *deallocated
		suffix = ""
	}
	return humanDuration(end.Sub(allocated)) + suffix
}

func tagList(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(tags))
	for k, v := range tags {
		pairs = append(pairs, k+"="+v)
	}
	// Deterministic order for stable output.
	for i := range pairs {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j] < pairs[i] {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}
	return strings.Join(pairs, ", ")
}

// humanDuration renders a duration at the precision a trace reader needs:
// sub-second in milliseconds, sub-minute in seconds, then minutes+seconds.
func humanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Millisecond:
		return "<1ms"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// costMicro renders billed microdollars the way the invoice reads them.
func costMicro(micro int64) string {
	return fmt.Sprintf("$%.4f", float64(micro)/1e6)
}

func sessionsTraceCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "trace <session-id>",
		Short: "Show a session's telemetry trace: spans, logs, and billed costs.",
		Long: "Fetch the durable telemetry frames for a session and render them as an " +
			"ordered listing of spans and logs with durations and billed costs. Costs " +
			"join at read time: sdk_call spans price by span ID and inference spans by " +
			"inference ID, in microdollars as billed. All pages are fetched, so the " +
			"output is the complete archived trace; frames with an unrecognized kind " +
			"are listed generically rather than dropped. JSON output is the canonical " +
			"frames response (frames, sdk_call_costs, inference_costs, totals) merged " +
			"across pages.\n\n" +
			"--follow keeps the trace open on an active session: after the archived " +
			"listing, new frames stream live (attaching to the telemetry WebSocket, " +
			"with the archive as fallback) until the session-end frame. Live rows " +
			"show COST as \"-\"; billing joins at read time, so live totals settle " +
			"in the closing summary. With JSON output the follow contract becomes " +
			"newline-delimited JSON: one object per frame (archived first, then " +
			"live), then a final `{\"trace_end\": true, ...}` object carrying the " +
			"merged cost maps. If the session is no longer active, the archive " +
			"prints and the command exits cleanly.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			if follow {
				return traceFollow(cl, printer(), args[0])
			}
			merged, err := fetchAllFrames(context.Background(), cl, args[0])
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(merged, func() {
				if merged.RetentionExpired {
					p.Result("Trace is past the org's retention window; frames are no longer available.")
					return
				}
				if len(merged.Frames) == 0 {
					p.Result("No telemetry frames for this session.")
					return
				}
				rows := [][]string{{"TIME", "KIND", "NAME", "DURATION", "STATUS", "COST"}}
				var billed int64
				for _, f := range merged.Frames {
					rows = append(rows, traceRow(f, merged.SdkCallCosts, merged.InferenceCosts))
					if f.Span != nil {
						billed += merged.SdkCallCosts[f.Span.SpanID]
					}
				}
				p.Table(rows)
				p.Note("%d frames; billed sdk_call cost %s", len(merged.Frames), costMicro(billed))
			})
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "After the archived trace, stream new frames until the session ends")
	return cmd
}

// traceFollow renders the archived trace, then follows the session live until
// its end frame: the live telemetry leg when the session accepts an attach,
// the archive otherwise. The deduper guarantees each frame appears once
// across the seam; a mint refusal is the "nothing left to follow" signal (an
// ended session's telemetry is served entirely by the archive).
func traceFollow(cl *client.Client, p *output.Printer, sessionID string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	merged, err := fetchAllFrames(ctx, cl, sessionID)
	if err != nil {
		return err
	}
	sdkCosts := merged.SdkCallCosts
	infCosts := merged.InferenceCosts

	dedupe := newFrameDeduper(func(f *platformgo.RunSessionFramesResponseFramesItem) error {
		if p.JSON {
			return p.JSONLine(f)
		}
		p.Result("%s", joinTraceRow(traceRow(f, sdkCosts, infCosts)))
		return p.Err()
	})

	// The archived prefix: JSON mode streams it through the deduper (NDJSON
	// contract), table mode renders today's table and seeds the deduper so
	// live delivery skips what the table already showed.
	if p.JSON {
		for _, f := range merged.Frames {
			if err := dedupe.deliver(f); err != nil {
				return err
			}
		}
	} else {
		switch {
		case merged.RetentionExpired:
			p.Result("Trace is past the org's retention window; frames are no longer available.")
		case len(merged.Frames) == 0:
			p.Result("No telemetry frames for this session yet.")
		default:
			rows := [][]string{{"TIME", "KIND", "NAME", "DURATION", "STATUS", "COST"}}
			for _, f := range merged.Frames {
				rows = append(rows, traceRow(f, sdkCosts, infCosts))
			}
			p.Table(rows)
		}
		for _, f := range merged.Frames {
			dedupe.seed(f)
		}
		if err := p.Err(); err != nil {
			return err
		}
	}
	fetched := int64(len(merged.Frames))

	for !dedupe.endSeen {
		tok, mintErr := cl.Phones.SessionTelemetryToken(ctx, &platformgo.PhonesSessionTelemetryTokenRequest{SessionID: sessionID})
		if ctx.Err() != nil {
			return traceFollowInterrupted(p, sessionID)
		}
		if mintErr == nil {
			if stream, dialErr := telemetry.Tail(ctx, tok.TelemetryURL); dialErr == nil {
				p.Step("attached to live telemetry — Ctrl-C to stop")
				if _, err := streamLive(ctx, stream, dedupe); err != nil {
					if ctx.Err() != nil {
						return traceFollowInterrupted(p, sessionID)
					}
					return err
				}
			}
			if ctx.Err() != nil {
				return traceFollowInterrupted(p, sessionID)
			}
		}
		// Backfill what the live window missed — and, when live is
		// unavailable, the polling leg itself.
		n, err := traceDrainFrames(ctx, cl, p, dedupe, sessionID, fetched, sdkCosts, infCosts)
		if err != nil {
			if ctx.Err() != nil {
				return traceFollowInterrupted(p, sessionID)
			}
			return err
		}
		fetched += n
		if dedupe.endSeen {
			break
		}
		if mintErr != nil {
			p.Note("session is not active — the archived trace above is complete")
			break
		}
		select {
		case <-ctx.Done():
			return traceFollowInterrupted(p, sessionID)
		case <-time.After(watchPollInterval):
		}
	}

	if p.JSON {
		return p.JSONLine(map[string]any{
			"trace_end":       true,
			"session_id":      sessionID,
			"frames":          len(dedupe.seen),
			"session_ended":   dedupe.endSeen,
			"sdk_call_costs":  sdkCosts,
			"inference_costs": infCosts,
		})
	}
	var billed int64
	for _, micro := range sdkCosts {
		billed += micro
	}
	p.Note("%d frames; billed sdk_call cost %s", len(dedupe.seen), costMicro(billed))
	return p.Err()
}

// traceDrainFrames delivers archived frames from offset onward through the
// deduper, folding each page's read-time cost maps into the accumulators so
// late-billed spans price correctly in the closing summary.
func traceDrainFrames(ctx context.Context, cl *client.Client, p *output.Printer, d *frameDeduper, sessionID string, offset int64, sdkCosts, infCosts map[string]int64) (int64, error) {
	var fetched int64
	limit := framesPageLimit
	for {
		off := offset + fetched
		resp, err := cl.Runs.SessionsListFrames(ctx, &platformgo.SessionsListFramesRequest{
			SessionID: sessionID,
			Limit:     &limit,
			Offset:    &off,
		})
		if err != nil {
			return fetched, err
		}
		if resp.RetentionExpired {
			p.Warn("trace is past the org's retention window; frames withheld")
			return fetched, nil
		}
		for k, v := range resp.SdkCallCosts {
			sdkCosts[k] = v
		}
		for k, v := range resp.InferenceCosts {
			infCosts[k] = v
		}
		for _, f := range resp.Frames {
			if err := d.deliver(f); err != nil {
				return fetched, err
			}
			fetched++
		}
		if len(resp.Frames) == 0 || offset+fetched >= resp.Total {
			return fetched, nil
		}
	}
}

// traceFollowInterrupted maps Ctrl-C during --follow onto the canceled exit
// code, with the resume hint.
func traceFollowInterrupted(p *output.Printer, sessionID string) error {
	p.Note("follow interrupted — `axilio sessions trace %s --follow` replays and resumes", sessionID)
	return exit.With(exit.Canceled, fmt.Errorf("follow interrupted"))
}

// joinTraceRow lays a trace row out as one streamed line, approximating the
// archived table's columns.
func joinTraceRow(row []string) string {
	return strings.TrimRight(fmt.Sprintf("%-13s %-10s %-34s %-9s %-8s %s",
		row[0], row[1], row[2], row[3], row[4], util.OrDash(row[5])), " ")
}

// fetchAllFrames pages through the frames archive until every frame is
// collected, merging the response-level cost maps along the way. The maps are
// merged rather than taken from one page so the result never depends on which
// page the server computed them for.
func fetchAllFrames(ctx context.Context, cl *client.Client, sessionID string) (*platformgo.RunSessionFramesResponse, error) {
	merged := &platformgo.RunSessionFramesResponse{
		Frames:         []*platformgo.RunSessionFramesResponseFramesItem{},
		SdkCallCosts:   map[string]int64{},
		InferenceCosts: map[string]int64{},
		Limit:          framesPageLimit,
	}
	limit := framesPageLimit
	for offset := int64(0); ; {
		resp, err := cl.Runs.SessionsListFrames(ctx, &platformgo.SessionsListFramesRequest{
			SessionID: sessionID,
			Limit:     &limit,
			Offset:    &offset,
		})
		if err != nil {
			return nil, err
		}
		merged.Frames = append(merged.Frames, resp.Frames...)
		for k, v := range resp.SdkCallCosts {
			merged.SdkCallCosts[k] = v
		}
		for k, v := range resp.InferenceCosts {
			merged.InferenceCosts[k] = v
		}
		merged.Total = resp.Total
		merged.RetentionExpired = resp.RetentionExpired
		offset += int64(len(resp.Frames))
		if len(resp.Frames) == 0 || offset >= resp.Total {
			return merged, nil
		}
	}
}

// traceRow renders one frame. Unknown kinds are the tolerant-reader case:
// list them with their kind and move on, never error.
func traceRow(f *platformgo.RunSessionFramesResponseFramesItem, sdkCosts, inferenceCosts map[string]int64) []string {
	switch {
	case f.Span != nil:
		s := f.Span
		start := time.Unix(0, s.StartTimeUnixNano)
		// End time is optional on the wire (in-flight spans on the live
		// stream omit it); the archive this command reads always sets it.
		var dur time.Duration
		if s.EndTimeUnixNano != nil {
			dur = time.Duration(*s.EndTimeUnixNano - s.StartTimeUnixNano)
		}
		status := ""
		if s.Status != nil {
			status = s.Status.Code
			if s.Status.Message != "" {
				status += ": " + s.Status.Message
			}
		}
		return []string{
			start.Local().Format("15:04:05.000"),
			s.SpanType,
			s.Name,
			humanDuration(dur),
			util.OrDash(status),
			util.OrDash(spanCost(s, sdkCosts, inferenceCosts)),
		}
	case f.Log != nil:
		l := f.Log
		body := l.Body
		if len(body) > 120 {
			body = body[:117] + "..."
		}
		return []string{
			time.Unix(0, l.TimeUnixNano).Local().Format("15:04:05.000"),
			"log:" + l.LogType,
			body,
			"",
			l.Severity,
			"",
		}
	default:
		return []string{"", util.OrDash(f.Kind), "(unknown frame kind)", "", "", ""}
	}
}

// spanCost joins a span to its billed cost: sdk_call spans by span id,
// inference spans by their inference-id attribute (the per-model detail
// behind the parent call's billed number). Other span types carry no cost.
func spanCost(s *platformgo.RunSpanFrame, sdkCosts, inferenceCosts map[string]int64) string {
	switch s.SpanType {
	case "sdk_call":
		if micro, ok := sdkCosts[s.SpanID]; ok {
			return costMicro(micro)
		}
	case "inference":
		id, _ := s.Attributes[inferenceIDAttr].(string)
		if micro, ok := inferenceCosts[id]; ok && id != "" {
			return costMicro(micro)
		}
	}
	return ""
}
