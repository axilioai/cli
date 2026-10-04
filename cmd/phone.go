package cmd

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/session"
	"github.com/axilioai/platform-go/drivers/mobile"
	"github.com/spf13/cobra"
)

// _phoneWaitDefault is the documented wait for `phone find` and `phone
// wait-for` when --timeout is omitted or non-positive.
const _phoneWaitDefault = 10 * time.Second

// flagPhoneSession is the --session override for the phone verbs.
var flagPhoneSession string

func phoneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "phone",
		Short: "Observe and control the selected session's phone.",
		Long: "Drive a phone session started with `axilio sessions start`.\n\n" +
			"A reliable loop is " +
			"observe the screen, find or semantically target an element, act, then " +
			"observe again to verify.\n\n" +
			"Available verbs are observe, find, find-text, find-all-text, " +
			"tap, long-press, swipe, type, key, screenshot, wait-for, tree, " +
			"accessibility, and send. " +
			"Every successful verb emits a structured result with -o json.\n\n" +
			"Session selection precedence is --session, AXILIO_SESSION, the only locally " +
			"saved session, the most recently started session, then an ambiguity error. " +
			"Phone command names match the Axilio SDKs, so an interaction explored in " +
			"the CLI can be transferred directly into Python or Go code.\n\n" +
			"Running `axilio phone` without a subcommand is equivalent to " +
			"`axilio phone --help`: it only displays this help and does not observe " +
			"or control a phone. Phone and global flags shown here therefore have no " +
			"effect. Pass flags to a phone subcommand instead.",
	}
	cmd.PersistentFlags().StringVar(&flagPhoneSession, "session", "", "Session ID; overrides AXILIO_SESSION and automatic session selection")
	cmd.AddCommand(
		phoneObserveCmd(), phoneFindCmd(), phoneFindTextCmd(), phoneFindAllTextCmd(),
		phoneTapCmd(), phoneLongPressCmd(), phoneSwipeCmd(),
		phoneTypeCmd(), phoneKeyCmd(), phoneScreenshotCmd(), phoneWaitForCmd(),
		phoneTreeCmd(), phoneAccessibilityCmd(), phoneSendCmd(),
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

// observeOpts is the OCR engine for a raw observe: a per-call option.
func observeOpts(engine string) []mobile.CallOption {
	if engine == "" {
		return nil
	}
	return []mobile.CallOption{mobile.WithOCREngine(engine)}
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

// phoneWait is the on-phone wait budget for a --timeout flag value: the
// flag itself, or _phoneWaitDefault when it is zero or negative (the SDK's
// own locator default is shorter, and must not apply here).
func phoneWait(flag time.Duration) time.Duration {
	if flag <= 0 {
		return _phoneWaitDefault
	}
	return flag
}

// locatorKV renders a locator result: how the target was resolved and where
// it was when the phone acted.
func locatorKV(r mobile.LocatorResult) [][2]string {
	b := r.Bounds
	kv := [][2]string{
		{"Resolved by", r.ResolvedBy},
		{"Center", fmt.Sprintf("%d,%d", b.X+b.Width/2, b.Y+b.Height/2)},
		{"BBox", fmt.Sprintf("%d,%d %dx%d", b.X, b.Y, b.Width, b.Height)},
		{"Took", fmt.Sprintf("%dms", r.TookMs)},
	}
	if r.ModelName != "" {
		kv = append(kv, [2]string{"Model", r.ModelName})
	}
	return kv
}

func phoneObserveCmd() *cobra.Command {
	var engine string
	cmd := &cobra.Command{
		Use:   "observe",
		Short: "Capture the screen: text + icon elements with coordinates.",
		Long: "Capture and analyze the selected phone's current screen. OCR uses the " +
			"free engine when --ocr-engine is omitted. Table output lists recognized " +
			"text with center coordinates and confidence, then summarizes icons and " +
			"screen dimensions. JSON returns the complete screen object, including " +
			"texts, icons, dimensions, screen hash, and capture time.",
		RunE: func(_ *cobra.Command, _ []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			screen, err := d.Observe(observeOpts(engine)...)
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(screen, func() {
				rows := [][]string{{"TEXT", "X", "Y", "CONF"}}
				for _, t := range screen.Texts {
					rows = append(rows, []string{t.Text, strconv.Itoa(t.Center.X), strconv.Itoa(t.Center.Y), fmt.Sprintf("%.2f", t.Confidence)})
				}
				p.Table(rows)
				p.Note("%d texts, %d icons  %dx%d", len(screen.Texts), len(screen.Icons), screen.Width, screen.Height)
			})
		},
	}
	cmd.Flags().StringVar(&engine, "ocr-engine", "", "OCR engine: free or premium; omitted uses free")
	return cmd
}

func phoneFindCmd() *cobra.Command {
	var lf locatorFlags
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "find [query]",
		Short: "Locate an element by natural-language query, or by role, name, or id.",
		Long: "Locate one element from a natural-language description, or from " +
			"accessibility selectors (--role, --name, --id) on a session with " +
			"accessibility mode on. The phone waits for it to appear, up to " +
			"--timeout (10 seconds when omitted), and returns how it was resolved " +
			"(a11y, ocr, or vlm), its center, bounding box, and the model that found " +
			"it. Every given selector must match. The OCR engine defaults to free " +
			"and the vision model is selected by the server. A target that never " +
			"appears exits with the timeout code; use `find-text` for a successful " +
			"empty result.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 1 {
				lf.query = args[0]
			}
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			loc, err := lf.build(d)
			if err != nil {
				return err
			}
			res, err := loc.BoundingBox(mobile.WithTimeout(phoneWait(timeout)))
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(res, func() { p.KV(locatorKV(res)) })
		},
	}
	lf.registerTree(cmd.Flags())
	cmd.Flags().BoolVar(&lf.exact, "exact", false, "Require a case-sensitive exact match on --name")
	cmd.Flags().StringVar(&lf.engine, "ocr-engine", "", "OCR engine: free or premium; omitted uses free")
	cmd.Flags().StringVar(&lf.model, "model", "", "Vision model override; omitted lets the server select the model")
	documentedDurationVar(cmd.Flags(), &timeout, "timeout", 10*time.Second, visionTimeoutHelp)
	return cmd
}

func phoneFindTextCmd() *cobra.Command {
	var exact bool
	cmd := &cobra.Command{
		Use:   "find-text <text>",
		Short: "Return the first OCR text match, or an empty successful result.",
		Long: "Search OCR text without semantic vision. By default, match a " +
			"case-insensitive substring and return the first element. --exact uses " +
			"a case-sensitive exact match. No match is successful: table output " +
			"prints `No match.` and JSON output is null rather than a not-found error.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			screen, err := d.Observe()
			if err != nil {
				return err
			}
			el := screen.FindText(args[0], exact)
			p := printer()
			return p.Emit(el, func() {
				if el == nil {
					p.Result("No match.")
					return
				}
				p.KV(elementKV(*el))
			})
		},
	}
	cmd.Flags().BoolVar(&exact, "exact", false, "Require a case-sensitive exact match instead of case-insensitive substring")
	return cmd
}

func phoneFindAllTextCmd() *cobra.Command {
	var pattern, engine string
	cmd := &cobra.Command{
		Use:   "find-all-text [contains]",
		Short: "Return every OCR text match, or all texts with no criteria.",
		Long: "Return every OCR element matching the criteria. The positional " +
			"argument is a case-insensitive substring; --pattern is a Go regular " +
			"expression instead. They are mutually exclusive. With neither, every " +
			"text element on the screen is returned. No match is successful: table " +
			"output prints `No matches.` and JSON output is an empty array.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			contains := ""
			if len(args) == 1 {
				contains = args[0]
			}
			if contains != "" && pattern != "" {
				return exit.Usagef("pass at most one of <contains> / --pattern")
			}
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			screen, err := d.Observe(observeOpts(engine)...)
			if err != nil {
				return err
			}
			els, err := screen.FindAllText(contains, pattern)
			if err != nil {
				return err
			}
			// Non-nil so an empty result serializes as [] in JSON output.
			if els == nil {
				els = []mobile.Element{}
			}
			p := printer()
			return p.Emit(els, func() {
				if len(els) == 0 {
					p.Result("No matches.")
					return
				}
				rows := [][]string{{"TEXT", "X", "Y", "CONF"}}
				for _, el := range els {
					rows = append(rows, []string{el.Text, strconv.Itoa(el.Center.X), strconv.Itoa(el.Center.Y), fmt.Sprintf("%.2f", el.Confidence)})
				}
				p.Table(rows)
			})
		},
	}
	cmd.Flags().StringVar(&pattern, "pattern", "", "Go regular expression to match instead of a substring")
	cmd.Flags().StringVar(&engine, "ocr-engine", "", "OCR engine: free or premium; omitted uses free")
	return cmd
}

func phoneTapCmd() *cobra.Command {
	var lf locatorFlags
	cmd := &cobra.Command{
		Use:   "tap [x y]",
		Short: "Tap at coordinates, or at a target found by --query, --role, --name, or --id.",
		Long: "Perform a tap action on the selected phone.\n\n" +
			"The coordinate form takes x and y as frame-space pixels, with (0,0) " +
			"at the screen's top-left.\n\n" +
			"Use --query to find an element by natural-language description and tap " +
			"its center in one step: the phone waits for the target to appear (up to " +
			"5 seconds) before tapping. On a session with accessibility mode on, " +
			"--role, --name, and --id select the element from the accessibility tree " +
			"instead; every given selector must match. If coordinates are also " +
			"provided, --query takes precedence, as do --role, --name, and --id.\n\n" +
			"Session selection precedence is --session, AXILIO_SESSION, the only " +
			"locally saved session, the most recently started session, then an " +
			"ambiguity error.",
		Args: cobra.MaximumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			p := printer()
			if lf.hasSelector() {
				loc, err := lf.build(d)
				if err != nil {
					return err
				}
				res, err := loc.Tap()
				if err != nil {
					return err
				}
				x, y := res.Bounds.X+res.Bounds.Width/2, res.Bounds.Y+res.Bounds.Height/2
				out := lf.fields(map[string]any{"action": "tap", "x": x, "y": y, "resolved_by": res.ResolvedBy})
				return p.Emit(out, func() {
					p.Ack("Tapped %s at %d,%d", lf.describe(), x, y)
				})
			}
			c, err := coordsArg(args)
			if err != nil {
				return err
			}
			if err := d.Tap(c); err != nil {
				return err
			}
			return p.Emit(map[string]any{"action": "tap", "x": c.X, "y": c.Y}, func() {
				p.Ack("Tapped %d,%d", c.X, c.Y)
			})
		},
	}
	cmd.Flags().StringVar(&lf.query, "query", "", "Recommended natural-language target; vision finds it and taps its center")
	lf.registerTree(cmd.Flags())
	cmd.Flags().BoolVar(&lf.exact, "exact", false, "Require a case-sensitive exact match on --name")
	cmd.Flags().StringVar(&lf.engine, "ocr-engine", "", "OCR engine for --query only: free or premium; omitted uses free")
	cmd.Flags().StringVar(&lf.model, "model", "", "Vision model for --query only; omitted lets the server select")
	return cmd
}

func phoneLongPressCmd() *cobra.Command {
	var durationMs int
	cmd := &cobra.Command{
		Use:   "long-press <x> <y>",
		Short: "Press and hold at coordinates.",
		Long: "Press and hold at frame-space pixel coordinates, with (0,0) at the " +
			"screen's top-left. This command is coordinate-only; use observe or find " +
			"to inspect the current frame before choosing a point. The default hold " +
			"duration is 800 milliseconds.",
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			c, err := coordsArg(args)
			if err != nil {
				return err
			}
			if err := d.LongPress(c, durationMs); err != nil {
				return err
			}
			p := printer()
			return p.Emit(map[string]any{"action": "long_press", "x": c.X, "y": c.Y, "duration_ms": durationMs}, func() {
				p.Ack("Long-pressed %d,%d for %dms", c.X, c.Y, durationMs)
			})
		},
	}
	cmd.Flags().IntVar(&durationMs, "duration-ms", 800, "How long to hold the coordinate, in milliseconds")
	return cmd
}

func phoneSwipeCmd() *cobra.Command {
	var durationMs int
	cmd := &cobra.Command{
		Use:   "swipe <x1> <y1> <x2> <y2>",
		Short: "Swipe from one point to another.",
		Long: "Swipe between two frame-space pixel coordinates, with (0,0) at the " +
			"screen's top-left. This command is coordinate-only; use observe to " +
			"inspect the current frame. The default gesture duration is 300 milliseconds.",
		Args: cobra.ExactArgs(4),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			nums, err := intArgs(args)
			if err != nil {
				return err
			}
			start := mobile.Coords{X: nums[0], Y: nums[1]}
			end := mobile.Coords{X: nums[2], Y: nums[3]}
			if err := d.Swipe(start, end, durationMs); err != nil {
				return err
			}
			p := printer()
			return p.Emit(
				map[string]any{"action": "swipe", "x1": start.X, "y1": start.Y, "x2": end.X, "y2": end.Y, "duration_ms": durationMs},
				func() { p.Ack("Swiped %d,%d -> %d,%d", start.X, start.Y, end.X, end.Y) },
			)
		},
	}
	cmd.Flags().IntVar(&durationMs, "duration-ms", 300, "How long the swipe gesture takes, in milliseconds")
	return cmd
}

func phoneTypeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "type <text>",
		Short: "Type a string of text.",
		Long: "Type text into the focused field on the selected phone.\n\n" +
			"Enclose text in quotes when it contains spaces or shell-special characters. " +
			"Text is entered through a US-layout keyboard. Printable ASCII characters " +
			"are supported; emoji and other non-ASCII characters are silently skipped.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			if err := d.TypeText(args[0]); err != nil {
				return err
			}
			p := printer()
			return p.Emit(map[string]any{"action": "type", "text": args[0]}, func() {
				p.Ack("Typed %q", args[0])
			})
		},
	}
}

func phoneKeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "key <name>",
		Short: "Press a named key. Currently supported: enter.",
		Long: "Press a named key on the selected phone. The name is passed through " +
			"to the phone, which accepts the supported named-key set and rejects " +
			"anything else. The set is currently just `enter` (submits forms / fires " +
			"the on-screen keyboard's Go or Search action) and grows in lockstep " +
			"with the device side, so new names work here without a CLI update.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			if err := d.KeyPress(args[0]); err != nil {
				return err
			}
			p := printer()
			return p.Emit(map[string]any{"action": "key", "key": args[0]}, func() {
				p.Ack("Pressed %s", args[0])
			})
		},
	}
}

func phoneScreenshotCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "screenshot",
		Short: "Capture the screen as a PNG file.",
		Long: "Capture the selected phone's screen as PNG bytes and write them to " +
			"--out. The default destination is screenshot.png in the current " +
			"directory. If the destination already exists, its contents are overwritten " +
			"without confirmation; the CLI does not create a backup. On " +
			"success the human result reports the path and byte count.",
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
			p := printer()
			return p.Emit(map[string]any{"action": "screenshot", "path": out, "bytes": len(png)}, func() {
				p.Ack("Wrote %s (%d bytes)", out, len(png))
			})
		},
	}
	cmd.Flags().StringVar(&out, "out", "screenshot.png", "PNG path to create; overwrite existing contents without confirmation")
	return cmd
}

func phoneWaitForCmd() *cobra.Command {
	var (
		lf      locatorFlags
		timeout time.Duration
		gone    bool
	)
	cmd := &cobra.Command{
		Use:   "wait-for [text]",
		Short: "Wait until text (or a --role, --name, or --id target) appears, or disappears with --gone.",
		Long: "Wait until text appears on the phone, or until it disappears with " +
			"--gone. The phone does the waiting, re-reading the screen only when it " +
			"changes, so this is one call rather than a polling loop. The default " +
			"match is a case-insensitive substring; --exact requires a case-sensitive " +
			"exact match. On a session with accessibility mode on, --role, --name, " +
			"and --id wait for an element from the accessibility tree instead of (or " +
			"as well as) text; every given selector must match. The default timeout " +
			"is 10 seconds (at most 60). A timeout returns the CLI timeout exit code " +
			"(5). Waiting for presence returns how the target was resolved and where " +
			"it is; waiting for absence is action-only.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 1 {
				lf.text = args[0]
			}
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			loc, err := lf.build(d)
			if err != nil {
				return err
			}
			if gone {
				if _, err := loc.WaitFor(mobile.StateHidden, mobile.WithTimeout(phoneWait(timeout))); err != nil {
					return err
				}
				p := printer()
				return p.Emit(lf.fields(map[string]any{"action": "wait_for", "gone": true}), func() {
					p.Ack("%s gone", lf.describe())
				})
			}
			res, err := loc.WaitFor(mobile.StateVisible, mobile.WithTimeout(phoneWait(timeout)))
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(res, func() { p.KV(locatorKV(*res)) })
		},
	}
	documentedDurationVar(cmd.Flags(), &timeout, "timeout", 10*time.Second, ocrTimeoutHelp)
	cmd.Flags().BoolVar(&lf.exact, "exact", false, "Require a case-sensitive exact match instead of substring")
	cmd.Flags().BoolVar(&gone, "gone", false, "Wait for the target to disappear instead of appear")
	lf.registerTree(cmd.Flags())
	return cmd
}

// coordsArg parses exactly two positional ints into Coords.
func coordsArg(args []string) (mobile.Coords, error) {
	if len(args) != 2 {
		return mobile.Coords{}, fmt.Errorf("need x and y (or use --query, --role, --name, or --id)")
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
