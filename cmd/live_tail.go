package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/axilioai/cli/internal/oauth"
	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/util"
	platformgo "github.com/axilioai/platform-go"
	"github.com/axilioai/platform-go/drivers/telemetry"
)

// frameRenderer renders one frame; watch and trace --follow plug their own
// line formats into the shared tail/dedupe machinery through it.
type frameRenderer func(f *platformgo.RunSessionFramesResponseFramesItem) error

// frameDeduper delivers each frame to its renderer exactly once across the
// archive/live seam. The archive and the live leg overlap by design (a first
// live attach replays from the window head, and the post-live backfill
// re-reads the archive), so correctness lives here, not in either source.
//
// Identity mirrors the contract's upsert keys, the same rules the dashboard
// and the SDK transports dedupe by: a span reconciles on
// (trace_id, span_id, phase), a log has no id and keys on its content, an
// unknown frame keys on its raw JSON.
type frameDeduper struct {
	render  frameRenderer
	seen    map[string]struct{}
	endSeen bool
}

func newFrameDeduper(render frameRenderer) *frameDeduper {
	return &frameDeduper{render: render, seen: map[string]struct{}{}}
}

// deliver renders f unless it was already rendered or is an in-flight span.
// Start-phase span frames exist only on the live leg; both watch and trace
// render span completions, so in-flight frames are skipped, which also keeps
// the archive's completed copy from reading as a duplicate.
func (d *frameDeduper) deliver(f *platformgo.RunSessionFramesResponseFramesItem) error {
	if s := f.Span; s != nil && s.Phase != spanPhaseEnd && spanEndNano(s) == 0 {
		return nil
	}
	key := frameKey(f)
	if _, ok := d.seen[key]; ok {
		return nil
	}
	d.seen[key] = struct{}{}
	if isSessionEndFrame(f) {
		d.endSeen = true
	}
	return d.render(f)
}

// seed marks f as already rendered without rendering it, for callers that
// presented the archived prefix through a different renderer (the trace
// table) before switching to the streaming one.
func (d *frameDeduper) seed(f *platformgo.RunSessionFramesResponseFramesItem) {
	d.seen[frameKey(f)] = struct{}{}
	if isSessionEndFrame(f) {
		d.endSeen = true
	}
}

// frameKey is the contract's dedupe identity for one frame.
func frameKey(f *platformgo.RunSessionFramesResponseFramesItem) string {
	switch {
	case f.Span != nil:
		return "span:" + f.Span.TraceID + ":" + f.Span.SpanID + ":" + f.Span.Phase
	case f.Log != nil:
		l := f.Log
		span := ""
		if l.SpanID != nil {
			span = *l.SpanID
		}
		nano, _ := json.Marshal(l.TimeUnixNano)
		return "log:" + l.TraceID + ":" + span + ":" + string(nano) + ":" + l.Body
	default:
		raw, err := json.Marshal(f)
		if err != nil {
			// An unknown frame that cannot re-marshal has no stable
			// identity; deliver it rather than risk a silent drop.
			return "unknown:unmarshalable"
		}
		return "unknown:" + string(raw)
	}
}

// spanEndNano reads a span's optional end time; 0 means in flight.
func spanEndNano(s *platformgo.RunSpanFrame) int64 {
	if s.EndTimeUnixNano == nil {
		return 0
	}
	return *s.EndTimeUnixNano
}

// openLiveStream opens a session's telemetry event stream on the API host
// with the CLI's own credentials: the API key header, or the OAuth bearer.
// Same precedence as newClient.
func openLiveStream(ctx context.Context, sessionID string) (*telemetry.Stream, error) {
	key, host := resolvedCreds()
	header := cliHeader("")
	if key != "" {
		header.Set("X-Axilio-Api-Key", key)
	} else {
		tok, err := oauth.ValidAccessToken(ctx, util.FirstNonEmpty(host, defaultAPIHost))
		if err != nil {
			return nil, err
		}
		header = cliHeader(tok)
	}
	return telemetry.Tail(ctx, telemetry.StreamURL(sdkBaseURL(host), sessionID), telemetry.WithHeader(header))
}

// tryLiveAttach opens the live stream. Live attach is an upgrade over archive
// polling, never a new failure mode: any refusal (telemetry off, cross-org
// read as not found, network trouble) reports nil and the caller stays on the
// archive.
func tryLiveAttach(ctx context.Context, p *output.Printer, sessionID string) *telemetry.Stream {
	stream, err := openLiveStream(ctx, sessionID)
	if err != nil {
		p.Step("live telemetry unavailable — following the archive")
		return nil
	}
	p.Step("attached to live telemetry")
	return stream
}

// streamLive delivers live frames through the deduper until the stream ends.
// It reports (true, nil) on a clean end (the session is over: its end frame
// arrived, or the server had nothing left to send), (false, nil) when the
// live leg failed and the caller should fall back to the archive, and a
// non-nil error only for context cancellation or a renderer failure. The
// stream replays the archive before going live, so the deduper drops what the
// caller already printed.
func streamLive(ctx context.Context, stream *telemetry.Stream, d *frameDeduper) (bool, error) {
	defer func() { _ = stream.Close() }()
	for {
		f, err := stream.Next(ctx)
		if err == nil {
			if renderErr := d.deliver(f); renderErr != nil {
				return false, renderErr
			}
			continue
		}
		switch {
		case errors.Is(err, io.EOF):
			return true, nil
		case ctx.Err() != nil:
			return false, ctx.Err()
		default:
			// Token rejected or reconnect budget exhausted: degrade to the
			// archive rather than failing a watch that polling can finish.
			return false, nil
		}
	}
}
