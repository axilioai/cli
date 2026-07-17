package cmd

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/session"
	"github.com/axilioai/platform-go/drivers/mobile"
	"github.com/spf13/cobra"
)

// flagPhoneSession is the --session override for the phone verbs.
var flagPhoneSession string

func phoneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "phone",
		Short: "Drive the current phone session: observe, find, tap, type, ...",
		Long: "Drive a phone leased with `axilio sessions start`.\n\n" +
			"The loop:\n\n" +
			"  axilio phone observe -o json               # what's on screen, with coordinates\n" +
			"  axilio phone tap --query \"the search box\"   # act on it by describing it\n" +
			"  axilio phone type \"androiddev\"\n\n" +
			"Target things by describing them, not by coordinate. --query routes through the\n" +
			"grounding model and re-locates the target on the live screen every run, so it keeps\n" +
			"working across screen sizes, layouts, scroll positions and app versions. A\n" +
			"coordinate is true only for the exact screen you read it from — and it fails\n" +
			"silently, by tapping the wrong thing.\n\n" +
			"Screenshots are welcome: look all you like. Just act with --query rather than\n" +
			"reading coordinates off the image. Coordinates need an explicit --raw.\n\n" +
			"The verbs are thin projections of the SDK's MobileDriver, so a session you explore\n" +
			"here maps onto SDK code. Use -o json to parse observe/find results.",
	}
	cmd.PersistentFlags().StringVar(&flagPhoneSession, "session", "", "Target session id (defaults to the current session)")
	cmd.AddCommand(
		phoneObserveCmd(), phoneFindCmd(), phoneFindTextCmd(),
		phoneTapCmd(), phoneLongPressCmd(), phoneSwipeCmd(),
		phoneTypeCmd(), phoneKeyCmd(), phoneScreenshotCmd(), phoneWaitForCmd(),
	)
	return cmd
}

// currentDriver resolves which lease to drive (precedence: --session flag >
// AXILIO_SESSION env > sole active lease > current pointer) and opens a
// MobileDriver on its control URL. The control URL is captured at
// `sessions start` (it is minted only then).
func currentDriver() (*mobile.MobileDriver, error) {
	s, err := session.Resolve(flagPhoneSession)
	if err != nil {
		return nil, err
	}
	if s.ControlURL == "" {
		return nil, fmt.Errorf("session %s has no control URL; re-run `axilio sessions start`", s.SessionID)
	}
	return mobile.ConnectRemote(s.ControlURL), nil
}

func visionOpts(engine, model string) []mobile.CallOption {
	var opts []mobile.CallOption
	if engine != "" {
		opts = append(opts, mobile.WithOCREngine(engine))
	}
	if model != "" {
		opts = append(opts, mobile.WithModel(model))
	}
	return opts
}

// Raw coordinates are a real capability with a narrow legitimate use, and the
// default loop of any agent handed a screenshot — a vision model reasons in
// pixels. Left alone, that loop hardcodes a coordinate that is only true for one
// screen size, layout, scroll position and app version, and it bypasses the
// grounding model that makes `--query` work at all.
//
// So coordinates are opt-in rather than removed: `--raw` makes the choice
// deliberate, greppable in review, and impossible to reach by accident. The
// usage error below is the teaching moment; rawCoordsWarning is the reminder for
// anyone who opted in.
//
// The messages are flowing prose on purpose: the help renderer reflows them, so
// indentation or line structure here collapses into a paragraph. They also carry
// no trailing period — the renderer appends one.

// rawCoordsUsage is for the verbs where a semantic target is almost always
// available, so --raw is the exception that needs justifying.
func rawCoordsUsage(verb, semantic, raw string) error {
	return exit.Usagef(
		"%s needs --raw to accept coordinates. Prefer `%s`, which describes the target and "+
			"re-locates it on the live screen every run — so it keeps working across screen "+
			"sizes, layouts, scroll positions and app versions, where a coordinate silently "+
			"taps the wrong thing. If the target genuinely has no semantic handle (a point on "+
			"a map, a freehand gesture), say so explicitly with `%s`",
		verb, semantic, raw,
	)
}

// rawSwipeUsage is separate because swipe is genuinely different: a scroll has no
// element to aim at, so coordinates are frequently the right answer rather than a
// concession. --raw is still required, so a reader can tell a drag from a scroll
// without reconstructing the geometry in their head.
func rawSwipeUsage() error {
	return exit.Usagef(
		"swipe needs either two semantic targets or an explicit --raw. To drag one thing onto " +
			"another, describe both ends: `axilio phone swipe --from-query \"the photo\" " +
			"--to-query \"the trash icon\"`. For a scroll or a freehand gesture there is no " +
			"element to aim at, so coordinates are the right answer — just say so: " +
			"`axilio phone swipe --raw 540 1500 540 500`",
	)
}

// rawCoordsWarning fires only for verbs that have a semantic alternative. Swipe
// deliberately stays quiet: a scroll gesture has no target element, so
// coordinates are frequently the *correct* answer there, and a warning on every
// swipe would train the reader to tune warnings out — costing us the one on tap,
// where it matters.
func rawCoordsWarning(semantic string) {
	printer().Warn("Used raw coordinates — brittle. They are only valid for this screen "+
		"size, layout and scroll position, and they skip the grounding model. Prefer: %s", semantic)
}

func elementKV(el mobile.Element) [][2]string {
	return [][2]string{
		{"Text", el.Text},
		{"Center", fmt.Sprintf("%d,%d", el.Center.X, el.Center.Y)},
		{"BBox", fmt.Sprintf("%d,%d %dx%d", el.BBox.X, el.BBox.Y, el.BBox.Width, el.BBox.Height)},
		{"Confidence", fmt.Sprintf("%.2f", el.Confidence)},
		{"Source", string(el.Source)},
	}
}

func phoneObserveCmd() *cobra.Command {
	var engine string
	cmd := &cobra.Command{
		Use:   "observe",
		Short: "Capture the screen: text + icon elements with coordinates.",
		RunE: func(_ *cobra.Command, _ []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			screen, err := d.Observe(visionOpts(engine, "")...)
			if err != nil {
				return err
			}
			p := printer()
			p.Emit(screen, func() {
				rows := [][]string{{"TEXT", "X", "Y", "CONF"}}
				for _, t := range screen.Texts {
					rows = append(rows, []string{t.Text, strconv.Itoa(t.Center.X), strconv.Itoa(t.Center.Y), fmt.Sprintf("%.2f", t.Confidence)})
				}
				output.Table(rows)
				p.Note("%d texts, %d icons  %dx%d", len(screen.Texts), len(screen.Icons), screen.Width, screen.Height)
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&engine, "ocr-engine", "", "OCR engine (free|premium)")
	return cmd
}

func phoneFindCmd() *cobra.Command {
	var engine, model string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "find <query>",
		Short: "Locate an element by natural-language query (vision).",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			opts := visionOpts(engine, model)
			if timeout > 0 {
				opts = append(opts, mobile.WithTimeout(timeout))
			}
			el, err := d.Find(args[0], opts...)
			if err != nil {
				return err
			}
			printer().Emit(el, func() { output.KV(elementKV(*el)) })
			return nil
		},
	}
	cmd.Flags().StringVar(&engine, "ocr-engine", "", "OCR engine (free|premium)")
	cmd.Flags().StringVar(&model, "model", "", "Vision model override")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "Deadline, e.g. 15s")
	return cmd
}

func phoneFindTextCmd() *cobra.Command {
	var exact bool
	cmd := &cobra.Command{
		Use:   "find-text <text>",
		Short: "First OCR element matching text (nil if none).",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			el, err := d.FindText(args[0], exact)
			if err != nil {
				return err
			}
			printer().Emit(el, func() {
				if el == nil {
					fmt.Println("No match.")
					return
				}
				output.KV(elementKV(*el))
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&exact, "exact", false, "Exact match instead of case-insensitive substring")
	return cmd
}

func phoneTapCmd() *cobra.Command {
	var query, engine, model string
	var raw bool
	cmd := &cobra.Command{
		Use:   "tap --query <target> | --raw <x> <y>",
		Short: "Tap a natural-language target with --query (preferred), or raw coordinates with --raw.",
		Long: "Tap the phone.\n\n" +
			"  axilio phone tap --query \"the search box\"   # preferred: located on the live screen\n" +
			"  axilio phone tap --raw 540 1200             # only when there is no semantic handle\n\n" +
			"--query routes through the grounding model and re-locates the target every run, so it " +
			"keeps working across screen sizes, layouts, scroll positions and app versions. A " +
			"coordinate read off a screenshot is true only for the screen you read it from.",
		Args: cobra.MaximumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			if query != "" && (raw || len(args) > 0) {
				return exit.Usagef("pass either --query or --raw <x> <y>, not both")
			}
			if query == "" && !raw {
				return rawCoordsUsage("tap",
					`axilio phone tap --query "the search box"`,
					`axilio phone tap --raw 540 1200`)
			}
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			if query != "" {
				el, err := d.Find(query, visionOpts(engine, model)...)
				if err != nil {
					return err
				}
				if err := el.Tap(); err != nil {
					return err
				}
				printer().Note("Tapped %q at %d,%d", query, el.Center.X, el.Center.Y)
				return nil
			}
			c, err := coordsArg(args)
			if err != nil {
				return err
			}
			if err := d.Tap(c); err != nil {
				return err
			}
			rawCoordsWarning(`axilio phone tap --query "..."`)
			printer().Note("Tapped %d,%d", c.X, c.Y)
			return nil
		},
	}
	cmd.Flags().StringVar(&query, "query", "", "Natural-language target (routed through the grounding model) — preferred")
	cmd.Flags().BoolVar(&raw, "raw", false, "Accept raw x y coordinates (brittle; prefer --query)")
	cmd.Flags().StringVar(&engine, "ocr-engine", "", "OCR engine for --query")
	cmd.Flags().StringVar(&model, "model", "", "Vision model for --query")
	return cmd
}

func phoneLongPressCmd() *cobra.Command {
	var durationMs int
	var query, engine, model string
	var raw bool
	cmd := &cobra.Command{
		Use:   "long-press --query <target> | --raw <x> <y>",
		Short: "Press and hold a natural-language target with --query (preferred), or raw coordinates with --raw.",
		Long: "Press and hold on the phone.\n\n" +
			"  axilio phone long-press --query \"the first message\"   # preferred\n" +
			"  axilio phone long-press --raw 540 1200                # only with no semantic handle\n\n" +
			"--query routes through the grounding model and re-locates the target every run.",
		Args: cobra.MaximumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			if query != "" && (raw || len(args) > 0) {
				return exit.Usagef("pass either --query or --raw <x> <y>, not both")
			}
			if query == "" && !raw {
				return rawCoordsUsage("long-press",
					`axilio phone long-press --query "the first message"`,
					`axilio phone long-press --raw 540 1200`)
			}
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			if query != "" {
				el, err := d.Find(query, visionOpts(engine, model)...)
				if err != nil {
					return err
				}
				if err := el.LongPress(durationMs); err != nil {
					return err
				}
				printer().Note("Long-pressed %q at %d,%d for %dms", query, el.Center.X, el.Center.Y, durationMs)
				return nil
			}
			c, err := coordsArg(args)
			if err != nil {
				return err
			}
			if err := d.LongPress(c, durationMs); err != nil {
				return err
			}
			rawCoordsWarning(`axilio phone long-press --query "..."`)
			printer().Note("Long-pressed %d,%d for %dms", c.X, c.Y, durationMs)
			return nil
		},
	}
	cmd.Flags().StringVar(&query, "query", "", "Natural-language target (routed through the grounding model) — preferred")
	cmd.Flags().BoolVar(&raw, "raw", false, "Accept raw x y coordinates (brittle; prefer --query)")
	cmd.Flags().IntVar(&durationMs, "duration-ms", 800, "Hold duration in milliseconds")
	cmd.Flags().StringVar(&engine, "ocr-engine", "", "OCR engine for --query")
	cmd.Flags().StringVar(&model, "model", "", "Vision model for --query")
	return cmd
}

func phoneSwipeCmd() *cobra.Command {
	var durationMs int
	var fromQuery, toQuery, engine, model string
	var raw bool
	cmd := &cobra.Command{
		Use:   "swipe --from-query <a> --to-query <b> | --raw <x1> <y1> <x2> <y2>",
		Short: "Swipe between two natural-language targets, or between raw coordinates with --raw.",
		Long: "Swipe on the phone.\n\n" +
			"  axilio phone swipe --from-query \"the photo\" --to-query \"the trash icon\"\n" +
			"  axilio phone swipe --raw 540 1500 540 500     # a scroll gesture\n\n" +
			"Dragging one thing onto another is a --from-query/--to-query swipe: both ends are\n" +
			"re-located on the live screen, so it survives a layout change.\n\n" +
			"Unlike tap and long-press, --raw here is a first-class answer, not a fallback: a\n" +
			"scroll or a freehand gesture has no target element, so coordinates are simply what\n" +
			"it is. --raw is still required, so a reader can tell the two apart at a glance.",
		Args: cobra.MaximumNArgs(4),
		RunE: func(_ *cobra.Command, args []string) error {
			semantic := fromQuery != "" || toQuery != ""
			if semantic && (raw || len(args) > 0) {
				return exit.Usagef("pass either --from-query/--to-query or --raw <x1> <y1> <x2> <y2>, not both")
			}
			if semantic && (fromQuery == "" || toQuery == "") {
				return exit.Usagef("a semantic swipe needs both --from-query and --to-query; " +
					"for a scroll or freehand gesture use --raw <x1> <y1> <x2> <y2>")
			}
			if !semantic && !raw {
				return rawSwipeUsage()
			}
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			if semantic {
				opts := visionOpts(engine, model)
				from, err := d.Find(fromQuery, opts...)
				if err != nil {
					return err
				}
				to, err := d.Find(toQuery, opts...)
				if err != nil {
					return err
				}
				if err := from.SwipeTo(*to, durationMs); err != nil {
					return err
				}
				printer().Note("Swiped %q -> %q", fromQuery, toQuery)
				return nil
			}
			if len(args) != 4 {
				return exit.Usagef("--raw needs exactly four coordinates: <x1> <y1> <x2> <y2>")
			}
			nums, err := intArgs(args)
			if err != nil {
				return err
			}
			start := mobile.Coords{X: nums[0], Y: nums[1]}
			end := mobile.Coords{X: nums[2], Y: nums[3]}
			if err := d.Swipe(start, end, durationMs); err != nil {
				return err
			}
			// No warning here by design: see rawCoordsWarning.
			printer().Note("Swiped %d,%d -> %d,%d", start.X, start.Y, end.X, end.Y)
			return nil
		},
	}
	cmd.Flags().StringVar(&fromQuery, "from-query", "", "Natural-language target to swipe from")
	cmd.Flags().StringVar(&toQuery, "to-query", "", "Natural-language target to swipe to")
	cmd.Flags().BoolVar(&raw, "raw", false, "Accept raw x1 y1 x2 y2 coordinates (correct for scroll gestures)")
	cmd.Flags().IntVar(&durationMs, "duration-ms", 300, "Swipe duration in milliseconds")
	cmd.Flags().StringVar(&engine, "ocr-engine", "", "OCR engine for the queries")
	cmd.Flags().StringVar(&model, "model", "", "Vision model for the queries")
	return cmd
}

func phoneTypeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "type <text>",
		Short: "Type a string of text.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			if err := d.TypeText(args[0]); err != nil {
				return err
			}
			printer().Note("Typed %q", args[0])
			return nil
		},
	}
}

func phoneKeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "key <name>",
		Short: "Press a named key, e.g. `enter`.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			if err := d.KeyPress(args[0]); err != nil {
				return err
			}
			printer().Note("Pressed %s", args[0])
			return nil
		},
	}
}

func phoneScreenshotCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "screenshot",
		Short: "Capture the screen as a PNG file.",
		Long: "Capture the screen as a PNG.\n\n" +
			"Looking at the screen is a good idea — take as many as you need. Just act on what\n" +
			"you see with `tap --query \"...\"` rather than reading coordinates off the image: a\n" +
			"coordinate measured from this PNG is true only for this screen, and it will tap the\n" +
			"wrong thing on the next one without telling you.\n\n" +
			"For a structured view of the same screen — text and icons with their coordinates,\n" +
			"as JSON — use `axilio phone observe -o json`.",
		RunE: func(_ *cobra.Command, _ []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			png, err := d.Screenshot()
			if err != nil {
				return err
			}
			if err := os.WriteFile(out, png, 0o644); err != nil {
				return err
			}
			printer().Note("Wrote %s (%d bytes)", out, len(png))
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "screenshot.png", "Output PNG path")
	return cmd
}

func phoneWaitForCmd() *cobra.Command {
	var (
		timeout time.Duration
		exact   bool
		gone    bool
	)
	cmd := &cobra.Command{
		Use:   "wait-for <text>",
		Short: "Poll until text appears (or disappears with --gone).",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			if gone {
				if err := d.WaitUntilGone(args[0], timeout, exact); err != nil {
					return err
				}
				printer().Note("%q gone", args[0])
				return nil
			}
			el, err := d.WaitForText(args[0], timeout, exact)
			if err != nil {
				return err
			}
			printer().Emit(el, func() { output.KV(elementKV(*el)) })
			return nil
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "How long to poll")
	cmd.Flags().BoolVar(&exact, "exact", false, "Exact match instead of substring")
	cmd.Flags().BoolVar(&gone, "gone", false, "Wait until the text disappears instead")
	return cmd
}

// coordsArg parses exactly two positional ints into Coords.
func coordsArg(args []string) (mobile.Coords, error) {
	if len(args) != 2 {
		return mobile.Coords{}, fmt.Errorf("need x and y (or use --query)")
	}
	nums, err := intArgs(args)
	if err != nil {
		return mobile.Coords{}, err
	}
	return mobile.Coords{X: nums[0], Y: nums[1]}, nil
}

func intArgs(args []string) ([]int, error) {
	out := make([]int, len(args))
	for i, a := range args {
		n, err := strconv.Atoi(a)
		if err != nil {
			return nil, fmt.Errorf("%q is not an integer", a)
		}
		out[i] = n
	}
	return out, nil
}
